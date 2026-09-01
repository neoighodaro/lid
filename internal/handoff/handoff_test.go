package handoff

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "threads.json"))
}

func do(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var decoded map[string]any
	if bytes.HasPrefix(bytes.TrimSpace(rec.Body.Bytes()), []byte("{")) {
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode %s %s: %v (%s)", method, path, err, rec.Body.String())
		}
	}
	return rec, decoded
}

func openThread(t *testing.T, store *Store) *Thread {
	t.Helper()
	thread, err := store.Open("web", "api", "auth", "which header?")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return thread
}

func TestAThreadStartsWaitingOnWhoWasAsked(t *testing.T) {
	thread := openThread(t, testStore(t))

	if thread.WaitingOn != "api" {
		t.Errorf("WaitingOn = %q, want api: the question is theirs to answer", thread.WaitingOn)
	}
	if thread.Status != StatusOpen {
		t.Errorf("Status = %q, want %q", thread.Status, StatusOpen)
	}
}

func TestAReplyHandsTheTurnBack(t *testing.T) {
	store := testStore(t)
	thread := openThread(t, store)

	updated, err := store.Reply(thread.ID, "api", "the bearer one", "web", false)
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if updated.WaitingOn != "web" {
		t.Errorf("WaitingOn = %q, want web", updated.WaitingOn)
	}
	if updated.Status != StatusOpen {
		t.Errorf("Status = %q, want the thread still open", updated.Status)
	}
	if len(updated.Messages) != 2 {
		t.Errorf("Messages = %d, want 2", len(updated.Messages))
	}
}

func TestAnAnsweredThreadIsNobodysTurn(t *testing.T) {
	store := testStore(t)
	thread := openThread(t, store)

	updated, err := store.Reply(thread.ID, "api", "the bearer one", "", true)
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if updated.Status != StatusAnswered || updated.WaitingOn != "" {
		t.Errorf("status/waiting_on = %q/%q, want answered and nobody's turn", updated.Status, updated.WaitingOn)
	}
}

// A closed thread that still named a party would keep waking that party's
// long-poll with a thread they can do nothing about.
func TestClosingAThreadTakesItOffTheWatch(t *testing.T) {
	store := testStore(t)
	thread := openThread(t, store)

	if _, err := store.Close(thread.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	waiting, err := store.All(Filter{WaitingOn: "api"})
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(waiting) != 0 {
		t.Errorf("threads waiting on api = %d, want none once closed", len(waiting))
	}
}

func TestTheWatchSeesBothIncomingQuestionsAndRepliesOwed(t *testing.T) {
	store := testStore(t)
	asked := openThread(t, store)
	mine, err := store.Open("api", "web", "schema", "which column?")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Reply(mine.ID, "web", "the created_at one", "api", false); err != nil {
		t.Fatalf("Reply: %v", err)
	}

	waiting, err := store.All(Filter{WaitingOn: "api"})
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(waiting) != 2 {
		t.Fatalf("threads waiting on api = %d, want the question asked of it and the reply it owes", len(waiting))
	}
	ids := map[string]bool{waiting[0].ID: true, waiting[1].ID: true}
	if !ids[asked.ID] || !ids[mine.ID] {
		t.Errorf("waiting = %v, want both %s and %s", ids, asked.ID, mine.ID)
	}
}

func TestALongPollReturnsAsSoonAsAThreadArrives(t *testing.T) {
	store := testStore(t)

	go func() {
		time.Sleep(20 * time.Millisecond)
		if _, err := store.Open("web", "api", "auth", "which header?"); err != nil {
			panic(err)
		}
	}()

	start := time.Now()
	threads, err := store.WaitFor(context.Background(), Filter{WaitingOn: "api"}, 5*time.Second)
	if err != nil {
		t.Fatalf("WaitFor: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads = %d, want the one that arrived mid-wait", len(threads))
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("waited %s, want a wake on the change rather than the timeout", elapsed)
	}
}

func TestALongPollGivesUpAtTheDeadline(t *testing.T) {
	store := testStore(t)

	start := time.Now()
	threads, err := store.WaitFor(context.Background(), Filter{WaitingOn: "api"}, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitFor: %v", err)
	}
	if len(threads) != 0 {
		t.Errorf("threads = %d, want none", len(threads))
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("waited %s, want the deadline to end it", elapsed)
	}
}

func TestWaitIsCappedSoAConnectionCannotBeParkedForever(t *testing.T) {
	got, err := waitFor("100000")
	if err != nil || got != MaxWait {
		t.Errorf("waitFor(100000) = %s, %v, want it capped at %s", got, err, MaxWait)
	}
	if got, err := waitFor("30"); err != nil || got != 30*time.Second {
		t.Errorf("waitFor(30) = %s, %v, want 30s", got, err)
	}
	if _, err := waitFor(""); err != nil {
		t.Errorf("waitFor(empty) = %v, want no wait and no error", err)
	}
}

// The seconds are bare on purpose. Appending "s" to whatever arrived would read
// a Go-looking duration as the wrong unit and silently cancel the long poll.
func TestAWaitThatLooksLikeADurationIsRefusedRatherThanMisread(t *testing.T) {
	for _, raw := range []string{"5m", "nonsense", "-1", "1h"} {
		if _, err := waitFor(raw); err == nil {
			t.Errorf("waitFor(%q) was accepted, want it refused", raw)
		}
	}
}

// The store survives a machine that dies mid-write, so a half-written file must
// never become the live store.
func TestAnUnreadableStoreIsKeptRatherThanOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "threads.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)

	if _, err := store.Open("web", "api", "auth", "which header?"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	kept, _ := filepath.Glob(path + ".corrupt.*")
	if len(kept) != 1 {
		t.Errorf("the unreadable file was not kept aside: %v", kept)
	}
}

// A second corruption must not erase the evidence of the first.
func TestEachUnreadableStoreIsKeptSeparately(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "threads.json")
	store := NewStore(path)

	for i := 0; i < 2; i++ {
		if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Open("web", "api", "auth", "which header?"); err != nil {
			t.Fatalf("Open: %v", err)
		}
	}

	kept, _ := filepath.Glob(path + ".corrupt.*")
	if len(kept) != 2 {
		t.Errorf("kept %d corrupt files, want both", len(kept))
	}
}

func TestTheServerOpensAndListsAThread(t *testing.T) {
	store := testStore(t)
	h := NewServer(store)

	rec, body := do(t, h, "POST", "/handoff/threads", `{"from":"web","to":"api","subject":"auth","body":"which header?"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /handoff/threads = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	if body["waiting_on"] != "api" {
		t.Errorf("waiting_on = %v, want api", body["waiting_on"])
	}

	rec, _ = do(t, h, "GET", "/handoff/threads?waiting_on=api", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /handoff/threads = %d, want 200", rec.Code)
	}
	var threads []Thread
	if err := json.Unmarshal(rec.Body.Bytes(), &threads); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(threads) != 1 {
		t.Errorf("threads = %d, want 1", len(threads))
	}
}

func TestTheServerRefusesAReplyThatSaysNothingAboutWhoseTurnItIs(t *testing.T) {
	store := testStore(t)
	thread := openThread(t, store)
	h := NewServer(store)

	rec, body := do(t, h, "POST", "/handoff/threads/"+thread.ID+"/messages", `{"from":"api","body":"maybe"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST reply = %d, want 422 (%s)", rec.Code, rec.Body.String())
	}
	errs, _ := body["errors"].(map[string]any)
	if _, named := errs["waiting_on"]; !named {
		t.Errorf("errors = %v, want it to name waiting_on", body["errors"])
	}
}

func TestTheServerRefusesAReplyThatClaimsBothOutcomes(t *testing.T) {
	store := testStore(t)
	thread := openThread(t, store)
	h := NewServer(store)

	rec, _ := do(t, h, "POST", "/handoff/threads/"+thread.ID+"/messages",
		`{"from":"api","body":"done","status":"answered","waiting_on":"web"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("POST reply = %d, want 422: answered and waiting_on contradict", rec.Code)
	}
}

func TestTheServerIsHonestAboutAThreadThatIsNotThere(t *testing.T) {
	h := NewServer(testStore(t))

	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/handoff/threads/nope", ""},
		{"POST", "/handoff/threads/nope/messages", `{"from":"api","body":"hi","waiting_on":"web"}`},
		{"DELETE", "/handoff/threads/nope", ""},
	} {
		rec, _ := do(t, h, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
}

func TestIDsDoNotCollide(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := NewID()
		if seen[id] {
			t.Fatalf("duplicate id %s after %d", id, i)
		}
		seen[id] = true
	}
}

// Without a cursor a long poll returns instantly for as long as anything is
// waiting on the watcher, which is the normal state — so the wait never waits.
func TestACursorMakesTheLongPollActuallyWait(t *testing.T) {
	store := testStore(t)
	thread, err := store.Open("web", "api", "auth", "which header?")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	seen, err := time.Parse(timestampLayout, thread.UpdatedAt)
	if err != nil {
		t.Fatalf("the stored timestamp is not a cursor: %v", err)
	}

	start := time.Now()
	threads, err := store.WaitFor(context.Background(), Filter{WaitingOn: "api", Since: seen}, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitFor: %v", err)
	}
	if len(threads) != 0 {
		t.Errorf("threads = %d, want the already-seen thread held back", len(threads))
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Errorf("returned after %s, want it to park until the deadline", elapsed)
	}
}

func TestACursorStillDeliversTheNextTurn(t *testing.T) {
	store := testStore(t)
	thread, err := store.Open("web", "api", "auth", "which header?")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seen, _ := time.Parse(timestampLayout, thread.UpdatedAt)

	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = store.Reply(thread.ID, "api", "which endpoint?", "web", false)
	}()

	threads, err := store.WaitFor(context.Background(), Filter{WaitingOn: "web", Since: seen}, 2*time.Second)
	if err != nil {
		t.Fatalf("WaitFor: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads = %d, want the reply to wake the watch", len(threads))
	}
}

// Clients pick the newest timestamp in a page by string comparison, so the text
// order and the time order have to agree. RFC3339Nano trims trailing zeros and
// they do not.
func TestTimestampsSortTheSameWayAsStringsAndAsTimes(t *testing.T) {
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	for _, pair := range [][2]time.Duration{
		{500 * time.Millisecond, 500*time.Millisecond + time.Microsecond},
		{0, time.Nanosecond},
		{999 * time.Millisecond, time.Second},
	} {
		earlier := base.Add(pair[0]).Format(timestampLayout)
		later := base.Add(pair[1]).Format(timestampLayout)
		if !(earlier < later) {
			t.Errorf("%s sorts above %s as a string", earlier, later)
		}
		if _, err := time.Parse(time.RFC3339Nano, earlier); err != nil {
			t.Errorf("%s does not parse as RFC 3339: %v", earlier, err)
		}
	}
}

func TestTheServerRefusesACursorItCannotRead(t *testing.T) {
	server := NewServer(testStore(t))
	req := httptest.NewRequest(http.MethodGet, "/handoff/threads?since=yesterday", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

// A misspelled field would otherwise be dropped and the reply rejected for
// saying nothing about whose turn it is, sending the caller after a syntax error
// that is not there.
func TestAMisspelledFieldSaysWhichFieldItWas(t *testing.T) {
	store := testStore(t)
	thread, _ := store.Open("web", "api", "auth", "which header?")
	server := NewServer(store)

	body := `{"from":"api","body":"the Authorization one","waitingon":"web"}`
	req := httptest.NewRequest(http.MethodPost, "/handoff/threads/"+thread.ID+"/messages", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "waitingon") {
		t.Errorf("body = %s, want it to name the field", rec.Body.String())
	}
}

func getHTML(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec, rec.Body.String()
}

func TestTheBoardShowsAThreadAndItsMessages(t *testing.T) {
	store := testStore(t)
	thread := openThread(t, store)
	if _, err := store.Reply(thread.ID, "api", "the Authorization one", "", true); err != nil {
		t.Fatal(err)
	}

	rec, body := getHTML(t, NewServer(store), "/handoff")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want html so a browser renders the board", ct)
	}
	for _, want := range []string{"auth", "which header?", "the Authorization one", "web → api", thread.ID} {
		if !strings.Contains(body, want) {
			t.Errorf("the board is missing %q", want)
		}
	}
}

func TestTheBoardLeavesOutClosedThreadsButKeepsThemReachable(t *testing.T) {
	store := testStore(t)
	thread := openThread(t, store)
	if _, err := store.Close(thread.ID); err != nil {
		t.Fatal(err)
	}
	server := NewServer(store)

	_, board := getHTML(t, server, "/handoff")
	if strings.Contains(board, thread.ID) {
		t.Error("a closed thread is still on the board, so finished work never clears")
	}
	if !strings.Contains(board, "No threads yet") {
		t.Errorf("a board with nothing live on it says nothing:\n%s", board)
	}

	rec, page := getHTML(t, server, "/handoff/thread/"+thread.ID)
	if rec.Code != http.StatusOK || !strings.Contains(page, "which header?") {
		t.Errorf("a closed thread is unreachable by its own URL: status %d", rec.Code)
	}
}

func TestTheBoardShowsTheNewestThreadFirst(t *testing.T) {
	store := testStore(t)
	older := openThread(t, store)
	newer, err := store.Open("api", "web", "rate limits", "how many?")
	if err != nil {
		t.Fatal(err)
	}

	_, body := getHTML(t, NewServer(store), "/handoff")
	if strings.Index(body, newer.ID) > strings.Index(body, older.ID) {
		t.Error("the oldest thread is at the top, so the thread that just moved is the hardest to find")
	}
}

func TestTheBoardClipsALongMessageAndLinksToTheWholeThread(t *testing.T) {
	store := testStore(t)
	long := strings.Repeat("x", previewChars+50)
	thread, err := store.Open("web", "api", "logs", long)
	if err != nil {
		t.Fatal(err)
	}

	_, board := getHTML(t, NewServer(store), "/handoff")
	if strings.Contains(board, long) {
		t.Error("the board printed a whole long message, which buries every other thread")
	}
	if !strings.Contains(board, "/handoff/thread/"+thread.ID) {
		t.Error("nothing links to the full thread the board clipped")
	}

	_, page := getHTML(t, NewServer(store), "/handoff/thread/"+thread.ID)
	if !strings.Contains(page, long) {
		t.Error("the thread page clipped the message too, so the full text is nowhere")
	}
}

func TestTheBoardCountsTheMessagesItDidNotShow(t *testing.T) {
	store := testStore(t)
	thread := openThread(t, store)
	for i := 0; i < previewCount+2; i++ {
		if _, err := store.Reply(thread.ID, "api", "another", "web", false); err != nil {
			t.Fatal(err)
		}
	}

	_, board := getHTML(t, NewServer(store), "/handoff")
	if !strings.Contains(board, "earlier message") {
		t.Errorf("the board hid messages without saying so:\n%s", board)
	}
}

func TestAThreadPageThatIsNotThereIsNotFound(t *testing.T) {
	rec, _ := getHTML(t, NewServer(testStore(t)), "/handoff/thread/nope")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestTheJSONAPIStaysJSONAlongsideTheBoard(t *testing.T) {
	store := testStore(t)
	openThread(t, store)

	req := httptest.NewRequest(http.MethodGet, "/handoff/threads", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	NewServer(store).ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want json; a browser asking nicely must not change what agents get", ct)
	}
}

func TestTheBoardEscapesWhatAnAgentWrote(t *testing.T) {
	store := testStore(t)
	if _, err := store.Open("web", "api", "<script>alert(1)</script>", "body"); err != nil {
		t.Fatal(err)
	}

	_, board := getHTML(t, NewServer(store), "/handoff")
	if strings.Contains(board, "<script>alert(1)</script>") {
		t.Error("a subject an agent chose was rendered as markup, so any agent can script the board")
	}
}
