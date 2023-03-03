package main

import (
	"flag"
	"fmt"
	"github.com/getsentry/sentry-go"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

const ProxyDialTimeout = 50 * time.Millisecond
const DialTimeout = 100 * time.Millisecond
const SentryFlushTimeout = 5 * time.Second

var (
	log        = logrus.New()
	masterAddr *net.TCPAddr

	localAddrAsStr    = flag.String("listen", ":9999", "local address")
	sentinelAddrAsStr = flag.String("sentinel", ":26379", "remote address")
	masterNameAsStr   = flag.String("master", "", "name of the master redis node")
	logLevelAsStr     = flag.String("log_level", "", "log level. Valid options are .")
)

func parseLogLevel(levelAsStr string) logrus.Level {
	for _, levelOption := range logrus.AllLevels {
		if levelOption.String() == levelAsStr {
			return levelOption
		}
	}
	return logrus.ErrorLevel
}

func setupLoggers() {
	log.Out = os.Stdout
	logLevel := parseLogLevel(*logLevelAsStr)
	log.SetLevel(logLevel)
	logrus.Infof("Current log level is '%s'", logLevel)
}

func checkArgs() {
	if *masterNameAsStr == "" {
		log.Fatal("Name of the master redis node is required argument.")
	}

	_, err := net.ResolveTCPAddr("tcp", *localAddrAsStr)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to resolve local address '%s': %s", *localAddrAsStr, err))
	}
}

func getLocalListener() *net.TCPListener {
	laddr, _ := net.ResolveTCPAddr("tcp", *localAddrAsStr)
	listener, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		log.Fatal(err)
		return nil
	}
	return listener
}

func flushSentry() {
	// Flush buffered events before the program terminates.
	// Set the timeout to the maximum duration the program can afford to wait.
	defer sentry.Flush(SentryFlushTimeout)
}

func noop() {

}

func initSentry() func() {
	sentryDSN, isPresent := os.LookupEnv("SENTRY_DSN")
	if isPresent == false {
		sentryDSN = ""
	}

	if sentryDSN == "" {
		return noop
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:   sentryDSN,
		Debug: true,
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}

	return flushSentry
}

func main() {
	sentryFinalizer := initSentry()
	defer sentryFinalizer()

	flag.Parse()
	setupLoggers()
	checkArgs()

	masterStopChan := make(chan string)
	go update_master(&masterStopChan)

	listener := getLocalListener()
	defer listener.Close()

	for {
		local, err := listener.AcceptTCP()
		if err != nil {
			log.Error(err)
			continue
		}

		go proxy(local, masterAddr, masterStopChan)
	}
}

func update_master(masterStopChan *chan string) {
	for {
		possibleMaster, err := getMasterAddr(*sentinelAddrAsStr, *masterNameAsStr)
		if err != nil {
			log.Errorf("[MASTER] Error polling for new master: %s.", err)
			time.Sleep(1 * time.Second)
			continue
		}

		if possibleMaster.String() != masterAddr.String() {
			log.Errorf("[MASTER] Master Address changed from %s to %s.", masterAddr.String(), possibleMaster.String())
			masterAddr = possibleMaster
			close(*masterStopChan)
			*masterStopChan = make(chan string)
		}

		if masterAddr == nil {
			// if we haven't discovered master at all, then slow our roll as the cluster is
			// probably still coming up
			time.Sleep(1 * time.Second)
			continue
		}

		time.Sleep(250 * time.Millisecond)
	}
}

func pipe(
	r net.Conn,
	w net.Conn,
	proxyChan chan<- string,
) {
	bytes, err := io.Copy(w, r)
	if err != nil {
		log.Errorf("[PROXY %s => %s] Shutting down stream; transferred %v bytes: %v\n", w.RemoteAddr().String(), r.RemoteAddr().String(), bytes, err)
	} else {
		log.Infof("[PROXY %s => %s] Shutting down stream; transferred %v bytes: %v\n", w.RemoteAddr().String(), r.RemoteAddr().String(), bytes, err)
	}
	close(proxyChan)
}

func proxy(
	local *net.TCPConn,
	remoteAddr *net.TCPAddr,
	masterStopChan <-chan string,
) {
	remote, err := net.DialTimeout("tcp4", remoteAddr.String(), ProxyDialTimeout)
	if err != nil {
		log.Infof("[PROXY %s => %s] Can't establish connection: %s\n", local.RemoteAddr().String(), remoteAddr.String(), err)
		local.Close()
		return
	}
	defer local.Close()
	defer remote.Close()

	localChan := make(chan string)
	remoteChan := make(chan string)

	go pipe(local, remote, remoteChan)
	go pipe(remote, local, localChan)

	select {
	case <-masterStopChan:
	case <-localChan:
	case <-remoteChan:
	}

	log.Infof("[PROXY %s => %s] Closing connection\n", local.RemoteAddr().String(), remoteAddr.String())
}

func getSentinels(sentinelAddress string) (sentinelsWithPort []*net.TCPAddr, err error) {
	sentinelHost, sentinelPort, err := net.SplitHostPort(sentinelAddress)
	if err != nil {
		return nil, fmt.Errorf("Can't find Sentinel: %s", err)
	}

	sentinels, err := net.LookupIP(sentinelHost)
	if err != nil {
		return nil, fmt.Errorf("Can't lookup Sentinel: %s", err)
	}

	for _, sentinelIP := range sentinels {
		addr := net.JoinHostPort(sentinelIP.String(), sentinelPort)
		log.Tracef("Sentinel address: %s", addr)
		netAddr, err := net.ResolveTCPAddr("tcp", addr)
		if err != nil {
			log.Errorf("Can not resolve sentinel address: %s", addr)
		}
		sentinelsWithPort = append(sentinelsWithPort, netAddr)
	}

	return sentinelsWithPort, err
}

func getMasterAddrFromSentinelResponse(response []byte) (*net.TCPAddr, error) {
	responseParts := strings.Split(string(response), "\r\n")
	if len(responseParts) < 5 {
		log.Errorf("Wrong sentinel response: '%s'", response)
		return nil, fmt.Errorf("Couldn't get update_master address from sentinel.")
	}

	stringAddr := fmt.Sprintf("%s:%s", responseParts[2], responseParts[4])

	return net.ResolveTCPAddr("tcp", stringAddr)
}

func getMasterAddrFromSentinel(sentinelAddress *net.TCPAddr) (*net.TCPAddr, error) {
	conn, err := net.DialTimeout("tcp", sentinelAddress.String(), DialTimeout)
	if err != nil {
		return nil, fmt.Errorf("Sentinel no not respond: %s", *sentinelAddrAsStr)
	}
	defer conn.Close()

	conn.Write([]byte(fmt.Sprintf("sentinel get-master-addr-by-name %s\n", *masterNameAsStr)))

	b := make([]byte, 256)
	_, err = conn.Read(b)
	if err != nil {
		log.Errorf("Cannot read from sentinel: %s", *sentinelAddrAsStr)
		return nil, fmt.Errorf("Cannot read from sentinel: %s", *sentinelAddrAsStr)
	}
	return getMasterAddrFromSentinelResponse(b)
}

func getMasterAddr(sentinelAddress string, masterName string) (*net.TCPAddr, error) {
	sentinels, err := getSentinels(sentinelAddress)
	if err != nil {
		return nil, fmt.Errorf("Can't lookup Sentinel: %s", err)
	}

	for _, sentinelAddress := range sentinels {

		netMasterAddr, err := getMasterAddrFromSentinel(sentinelAddress)
		if err != nil {
			log.Errorf("Can not get master address from sentinel. %s", err)
		}

		//check that there's actually someone listening on that address
		conn2, err := net.DialTimeout("tcp", netMasterAddr.String(), DialTimeout)
		if err != nil {
			log.Errorf("Can not dial master: %s", netMasterAddr.String())
			continue
		}
		defer conn2.Close()
		return netMasterAddr, err
	}

	// No available masters.
	return nil, nil
}
