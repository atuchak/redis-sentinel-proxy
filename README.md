redis-sentinel-proxy
====================

This fork is based on:
* https://github.com/patrickdk77/redis-sentinel-proxy
* https://hub.docker.com/r/flant/redis-sentinel-proxy

The key features:
* Drop connections on master change
* Docker image https://hub.docker.com/r/atuchak/redis-sentinel-proxy
* Configurable logging level

Small command utility that:

* Given a redis sentinel server listening on `SENTINEL_PORT`, keeps asking it for the address of a master named `NAME`

* Proxies all tcp requests that it receives on `PORT` to that master


Usage:

`./redis-sentinel-proxy -listen IP:PORT -sentinel :SENTINEL_PORT -master NAME -log_level info`
