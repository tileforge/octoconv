package api

import (
	"net/http"
	"time"
)

const (
	apiReadHeaderTimeout = 10 * time.Second
	apiMaxHeaderBytes    = 64 << 10
	apiIdleTimeout       = 120 * time.Second
)

func NewHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: apiReadHeaderTimeout,
	}
}
