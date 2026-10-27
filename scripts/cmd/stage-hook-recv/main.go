package main

import (
	"crypto/hmac"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/tileforge/octoconv/internal/webhook"
)

const maxBody = 1 << 20

type record struct {
	ReceivedNS int64 `json:"received_ns"`
	SigOK      bool  `json:"sig_ok"`
	Body       any   `json:"body"`
}

func newHandler(secret []byte, w io.Writer) http.Handler {
	var mu sync.Mutex
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			rw.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		sigOK := false
		if ts, err := strconv.ParseInt(r.Header.Get("X-OctoConv-Timestamp"), 10, 64); err == nil {
			sigOK = hmac.Equal([]byte(webhook.SignPayload(secret, ts, body)), []byte(r.Header.Get("X-OctoConv-Signature")))
		}
		rec := record{ReceivedNS: time.Now().UnixNano(), SigOK: sigOK}
		if json.Valid(body) {
			rec.Body = json.RawMessage(body)
		} else {
			rec.Body = string(body)
		}
		line, err := json.Marshal(rec)
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		mu.Lock()
		_, err = w.Write(append(line, '\n'))
		mu.Unlock()
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		rw.WriteHeader(http.StatusOK)
	})
}

func main() {
	addr := flag.String("addr", "0.0.0.0:18777", "listen address")
	out := flag.String("out", "", "JSONL file, one line per delivery")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "usage: stage-hook-recv -out FILE [-addr HOST:PORT]")
		os.Exit(64)
	}
	secret := os.Getenv("STAGE_HOOK_SECRET")
	if secret == "" {
		fmt.Fprintln(os.Stderr, "stage-hook-recv: STAGE_HOOK_SECRET is empty")
		os.Exit(64)
	}
	f, err := os.OpenFile(*out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatalf("stage-hook-recv: %v", err)
	}
	defer f.Close()
	srv := &http.Server{Addr: *addr, Handler: newHandler([]byte(secret), f), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
