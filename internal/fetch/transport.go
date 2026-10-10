package fetch

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	maxIdleConns        = 256
	maxIdleConnsPerHost = 64
	idleConnTimeout     = 90 * time.Second
	dialTimeout         = 30 * time.Second
	keepAlive           = 30 * time.Second
	tlsHandshakeTimeout = 10 * time.Second
	responseHdrTimeout  = 30 * time.Second
)

var sharedTransport = sync.OnceValue(newTransport)

func newTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: keepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHdrTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func sharedHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: sharedTransport(),
		Timeout:   timeout,
	}
}

func SharedPeerHTTPClient(timeout time.Duration) *http.Client {
	return sharedHTTPClient(timeout)
}
