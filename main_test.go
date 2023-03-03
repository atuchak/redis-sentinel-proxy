package main

import (
	"fmt"
	"net"
	"testing"
)

func TestAdd(t *testing.T) {

	//if true {
	//	t.Errorf("got %q, wanted %q", 1, 2)
	//}

	addr, err := net.ResolveTCPAddr("tcp", "ya.ru:80")
	fmt.Printf("%s %s", addr, err)
}
