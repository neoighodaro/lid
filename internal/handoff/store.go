package handoff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	StatusOpen     = "open"
	StatusAnswered = "answered"
	StatusClosed   = "closed"
)

type Message struct {
	From string `json:"from"`
	Body string `json:"body"`
	At   string `json:"at"`
}

type Thread struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Status    string    `json:"status"`
	WaitingOn string    `json:"waiting_on"`
	CreatedAt string    `json:"created_at"`
	UpdatedAt string    `json:"updated_at"`
	Messages  []Message `json:"messages"`
}

type file struct {
	Threads []Thread `json:"threads"`
}

type Store struct {
	path string
	mu   sync.Mutex

	notifyMu sync.Mutex
	notifyCh chan struct{}
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func Dir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "lid", "handoff")
}

func DefaultStorePath() string { return filepath.Join(Dir(), "threads.json") }
func DefaultPidPath() string   { return filepath.Join(Dir(), "handoff.pid") }
func DefaultLogPath() string   { return filepath.Join(Dir(), "handoff.log") }

func (s *Store) subscribe() <-chan struct{} {
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	if s.notifyCh == nil {
		s.notifyCh = make(chan struct{})
	}
	return s.notifyCh
}

func (s *Store) broadcast() {
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	if s.notifyCh != nil {
		close(s.notifyCh)
		s.notifyCh = nil
	}
}

func (s *Store) load() ([]Thread, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Thread{}, nil
		}
		return nil, err
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		_ = os.Rename(s.path, fmt.Sprintf("%s.corrupt.%d", s.path, time.Now().UnixNano()))
		return []Thread{}, nil
	}
	if f.Threads == nil {
		return []Thread{}, nil
	}
	return f.Threads, nil
}

func (s *Store) save(threads []Thread) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file{Threads: threads}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

type Filter struct {
	To        string
	Status    string
	WaitingOn string
	Since     time.Time
}

func (f Filter) matches(t Thread) bool {
	if f.To != "" && t.To != f.To {
		return false
	}
	if f.Status != "" && t.Status != f.Status {
		return false
	}
	if f.WaitingOn != "" && t.WaitingOn != f.WaitingOn {
		return false
	}
	if !f.Since.IsZero() {
		at, err := time.Parse(time.RFC3339Nano, t.UpdatedAt)
		if err == nil && !at.After(f.Since) {
			return false
		}
	}
	return true
}

func (s *Store) All(f Filter) ([]Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	threads, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Thread, 0, len(threads))
	for _, t := range threads {
		if f.matches(t) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *Store) WaitFor(ctx context.Context, f Filter, wait time.Duration) ([]Thread, error) {
	if wait <= 0 {
		return s.All(f)
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		ch := s.subscribe()
		threads, err := s.All(f)
		if err != nil || len(threads) > 0 {
			return threads, err
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return threads, nil
		}
	}
}

func (s *Store) Find(id string) (*Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	threads, err := s.load()
	if err != nil {
		return nil, err
	}
	for i := range threads {
		if threads[i].ID == id {
			t := threads[i]
			return &t, nil
		}
	}
	return nil, nil
}

func (s *Store) Open(from, to, subject, body string) (*Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	threads, err := s.load()
	if err != nil {
		return nil, err
	}
	now := timestamp()
	t := Thread{
		ID:        NewID(),
		From:      from,
		To:        to,
		Subject:   subject,
		Status:    StatusOpen,
		WaitingOn: to,
		CreatedAt: now,
		UpdatedAt: now,
		Messages:  []Message{{From: from, Body: body, At: now}},
	}
	if err := s.save(append(threads, t)); err != nil {
		return nil, err
	}
	s.broadcast()
	return &t, nil
}

func (s *Store) Reply(id, from, body, waitingOn string, answered bool) (*Thread, error) {
	return s.update(id, func(t *Thread) {
		now := timestamp()
		t.Messages = append(t.Messages, Message{From: from, Body: body, At: now})
		if answered {
			t.Status = StatusAnswered
			t.WaitingOn = ""
		} else {
			t.Status = StatusOpen
			t.WaitingOn = waitingOn
		}
		t.UpdatedAt = now
	})
}

func (s *Store) Close(id string) (*Thread, error) {
	return s.update(id, func(t *Thread) {
		t.Status = StatusClosed
		t.WaitingOn = ""
		t.UpdatedAt = timestamp()
	})
}

func (s *Store) update(id string, apply func(*Thread)) (*Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	threads, err := s.load()
	if err != nil {
		return nil, err
	}
	for i := range threads {
		if threads[i].ID != id {
			continue
		}
		apply(&threads[i])
		if err := s.save(threads); err != nil {
			return nil, err
		}
		s.broadcast()
		t := threads[i]
		return &t, nil
	}
	return nil, nil
}

func (s *Store) Delete(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	threads, err := s.load()
	if err != nil {
		return false, err
	}
	for i := range threads {
		if threads[i].ID == id {
			if err := s.save(append(threads[:i], threads[i+1:]...)); err != nil {
				return false, err
			}
			s.broadcast()
			return true, nil
		}
	}
	return false, nil
}

const timestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

func timestamp() string { return time.Now().UTC().Format(timestampLayout) }
