package main

import (
	"io"
	"net/http"
)

func newHandler(secret []byte, w io.Writer) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})
}

func main() {}
