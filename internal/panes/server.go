package panes

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxBody = 4096

type Opener func(command string) error

type Listener struct {
	Token string

	ln     net.Listener
	srv    *http.Server
	open   Opener
	mu     sync.Mutex
	closed atomic.Bool
	done   chan struct{}
}

func Listen(open Opener) (*Listener, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	l := &Listener{Token: hex.EncodeToString(raw), ln: ln, open: open, done: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /panes", l.handle)
	l.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		l.srv.Serve(ln)
		close(l.done)
	}()
	return l, nil
}

func (l *Listener) Port() int {
	return l.ln.Addr().(*net.TCPAddr).Port
}

func (l *Listener) Close() error {
	l.closed.Store(true)
	l.mu.Lock()
	l.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := l.srv.Shutdown(ctx)
	if err != nil {
		err = l.srv.Close()
	}
	<-l.done
	return err
}

func (l *Listener) handle(w http.ResponseWriter, r *http.Request) {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") ||
		subtle.ConstantTimeCompare([]byte(got), []byte(l.Token)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var body struct {
		Command string `json:"command"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody+512)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(body.Command) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command is required"})
		return
	}
	if strings.ContainsRune(body.Command, 0) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command contains a NUL byte"})
		return
	}
	if len(body.Command) > maxBody {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command too long"})
		return
	}
	l.mu.Lock()
	if l.closed.Load() {
		l.mu.Unlock()
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "closed"})
		return
	}
	err := l.open(body.Command)
	l.mu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
