package panes

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu   sync.Mutex
	cmds []string
	err  error
}

func (r *recorder) open(c string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cmds = append(r.cmds, c)
	return r.err
}

func (r *recorder) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.cmds...)
}

func start(t *testing.T, r *recorder) *Listener {
	t.Helper()
	l, err := Listen(r.open)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func do(t *testing.T, l *Listener, method, path, auth, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", l.Port(), path), strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestPanesOpensAPane(t *testing.T) {
	r := &recorder{}
	l := start(t, r)
	if len(l.Token) != 64 {
		t.Fatalf("token length %d", len(l.Token))
	}
	code, body := do(t, l, "POST", "/panes", "Bearer "+l.Token, `{"command":"npm run dev"}`)
	if code != 201 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("got %d %s", code, body)
	}
	if c := r.calls(); len(c) != 1 || c[0] != "npm run dev" {
		t.Fatalf("calls %v", c)
	}
}

func TestPanesRejectsABadToken(t *testing.T) {
	r := &recorder{}
	l := start(t, r)
	for _, auth := range []string{"", "Bearer wrong", "Basic " + l.Token, l.Token} {
		if code, _ := do(t, l, "POST", "/panes", auth, `{"command":"x"}`); code != 401 {
			t.Fatalf("auth %q: %d", auth, code)
		}
	}
	if len(r.calls()) != 0 {
		t.Fatal("opener called")
	}
}

func TestPanesRejectsBadBodies(t *testing.T) {
	r := &recorder{}
	l := start(t, r)
	bodies := []string{
		"",
		`{"command":""}`,
		`{"command":"   \n"}`,
		`{"command":"` + strings.Repeat("a", 4097) + `"}`,
		`{bad`,
		`{}`,
		`{"command":"ls\u0000-la"}`,
	}
	for _, b := range bodies {
		if code, _ := do(t, l, "POST", "/panes", "Bearer "+l.Token, b); code != 400 {
			t.Fatalf("body %.30q: %d", b, code)
		}
	}
	if len(r.calls()) != 0 {
		t.Fatal("opener called")
	}
}

func TestPanesReportsOpenerFailure(t *testing.T) {
	r := &recorder{err: errors.New("herdr exploded")}
	l := start(t, r)
	code, body := do(t, l, "POST", "/panes", "Bearer "+l.Token, `{"command":"x"}`)
	if code != 502 || !strings.Contains(body, `"error"`) || !strings.Contains(body, "herdr exploded") {
		t.Fatalf("got %d %s", code, body)
	}
}

func TestPanesOtherRoutes(t *testing.T) {
	r := &recorder{}
	l := start(t, r)
	if code, _ := do(t, l, "GET", "/panes", "Bearer "+l.Token, ""); code != 405 {
		t.Fatalf("GET: %d", code)
	}
	if code, _ := do(t, l, "POST", "/other", "Bearer "+l.Token, `{"command":"x"}`); code != 404 {
		t.Fatalf("path: %d", code)
	}
	if len(r.calls()) != 0 {
		t.Fatal("opener called")
	}
}

func TestCloseStopsServing(t *testing.T) {
	r := &recorder{}
	l, err := Listen(r.open)
	if err != nil {
		t.Fatal(err)
	}
	port := l.Port()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/panes", port)); err == nil {
		t.Fatal("still serving")
	}
}

func TestCloseWaitsForOpenerAndBlocksQueuedRequests(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var calls []string
	l, err := Listen(func(c string) error {
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		if c == "first" {
			close(started)
			<-release
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	post := func(cmd string) {
		defer wg.Done()
		req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/panes", l.Port()), strings.NewReader(`{"command":"`+cmd+`"}`))
		req.Header.Set("Authorization", "Bearer "+l.Token)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}
	wg.Add(1)
	go post("first")
	<-started
	wg.Add(1)
	go post("second")
	time.Sleep(100 * time.Millisecond)

	closed := make(chan struct{})
	go func() { l.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close returned while the opener was in progress")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-closed
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || calls[0] != "first" {
		t.Fatalf("calls %v", calls)
	}
}
