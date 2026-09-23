package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type contextKey string

const externalHostKey contextKey = "external-host"

func rewriteCDPResponse(resp *http.Response) error {
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "application/json") {
		return nil
	}

	externalHost, _ := resp.Request.Context().Value(externalHostKey).(string)
	if strings.TrimSpace(externalHost) == "" {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read CDP discovery response: %w", err)
	}
	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("close CDP discovery response: %w", err)
	}

	rewritten := bytes.ReplaceAll(body, []byte("ws://127.0.0.1:9223"), []byte("ws://"+externalHost))
	rewritten = bytes.ReplaceAll(rewritten, []byte("ws://localhost:9223"), []byte("ws://"+externalHost))
	resp.Body = io.NopCloser(bytes.NewReader(rewritten))
	resp.ContentLength = int64(len(rewritten))
	resp.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	return nil
}

func main() {
	upstreamRaw := strings.TrimSpace(os.Getenv("CDP_UPSTREAM"))
	if upstreamRaw == "" {
		upstreamRaw = "http://127.0.0.1:9223"
	}
	upstream, err := url.Parse(upstreamRaw)
	if err != nil {
		log.Fatalf("invalid CDP_UPSTREAM: %v", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(upstream)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		externalHost := req.Host
		*req = *req.WithContext(context.WithValue(req.Context(), externalHostKey, externalHost))
		director(req)
		req.Host = upstream.Host
	}
	proxy.ModifyResponse = rewriteCDPResponse
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, proxyErr error) {
		log.Printf("CDP upstream error: %v", proxyErr)
		http.Error(w, "CDP upstream unavailable", http.StatusBadGateway)
	}
	proxy.FlushInterval = -1

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/", proxy)

	server := &http.Server{
		Addr:              ":9222",
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	log.Printf("CDP proxy listening on %s and forwarding to %s", server.Addr, upstream)
	log.Fatal(server.ListenAndServe())
}
