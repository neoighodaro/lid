package handoff

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const MaxWait = 5 * time.Minute

func NewServer(store *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /handoff/threads", handleList(store))
	mux.HandleFunc("POST /handoff/threads", handleOpen(store))
	mux.HandleFunc("GET /handoff/threads/{id}", handleGet(store))
	mux.HandleFunc("POST /handoff/threads/{id}/messages", handleReply(store))
	mux.HandleFunc("POST /handoff/threads/{id}/close", handleClose(store))
	mux.HandleFunc("DELETE /handoff/threads/{id}", handleDelete(store))
	mux.HandleFunc("GET /handoff/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	return mux
}

type openReq struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type replyReq struct {
	From      string `json:"from"`
	Body      string `json:"body"`
	WaitingOn string `json:"waiting_on"`
	Status    string `json:"status"`
}

func handleList(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		since, err := parseSince(q.Get("since"))
		if err != nil {
			writeValidation(w, map[string]string{"since": "The since field must be an RFC 3339 timestamp."})
			return
		}
		filter := Filter{
			To:        q.Get("to"),
			Status:    q.Get("status"),
			WaitingOn: q.Get("waiting_on"),
			Since:     since,
		}
		wait, err := waitFor(q.Get("wait"))
		if err != nil {
			writeValidation(w, map[string]string{"wait": "The wait field is a number of seconds."})
			return
		}
		threads, err := store.WaitFor(r.Context(), filter, wait)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Cannot read the threads.")
			return
		}
		writeJSON(w, http.StatusOK, threads)
	}
}

func waitFor(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || seconds < 0 {
		return 0, errBadRequest
	}
	d := time.Duration(seconds * float64(time.Second))
	if d > MaxWait {
		return MaxWait, nil
	}
	return d, nil
}

func parseSince(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, errBadRequest
	}
	return at, nil
}

var errBadRequest = errors.New("bad request")

func handleOpen(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req openReq
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, badBody(err))
			return
		}
		errs := map[string]string{}
		validateName(errs, "from", req.From)
		validateName(errs, "to", req.To)
		if strings.TrimSpace(req.Subject) == "" {
			errs["subject"] = "The subject field is required."
		}
		if strings.TrimSpace(req.Body) == "" {
			errs["body"] = "The body field is required."
		}
		if len(errs) > 0 {
			writeValidation(w, errs)
			return
		}
		thread, err := store.Open(req.From, req.To, req.Subject, req.Body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Cannot open the thread.")
			return
		}
		writeJSON(w, http.StatusCreated, thread)
	}
}

func handleGet(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		thread, err := store.Find(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Cannot read the thread.")
			return
		}
		if thread == nil {
			writeError(w, http.StatusNotFound, "Thread not found.")
			return
		}
		writeJSON(w, http.StatusOK, thread)
	}
}

func handleReply(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req replyReq
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, badBody(err))
			return
		}
		errs := map[string]string{}
		validateName(errs, "from", req.From)
		if strings.TrimSpace(req.Body) == "" {
			errs["body"] = "The body field is required."
		}
		switch {
		case req.Status != "" && req.Status != StatusAnswered:
			errs["status"] = `The status may only be "answered".`
		case req.Status != "" && req.WaitingOn != "":
			errs["waiting_on"] = "Specify either waiting_on or status, not both."
		case req.Status == "" && req.WaitingOn == "":
			errs["waiting_on"] = `Specify waiting_on, or status "answered".`
		case req.WaitingOn != "":
			validateName(errs, "waiting_on", req.WaitingOn)
		}
		if len(errs) > 0 {
			writeValidation(w, errs)
			return
		}
		thread, err := store.Reply(r.PathValue("id"), req.From, req.Body, req.WaitingOn, req.Status == StatusAnswered)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Cannot add the message.")
			return
		}
		if thread == nil {
			writeError(w, http.StatusNotFound, "Thread not found.")
			return
		}
		writeJSON(w, http.StatusOK, thread)
	}
}

func handleClose(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		thread, err := store.Close(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Cannot close the thread.")
			return
		}
		if thread == nil {
			writeError(w, http.StatusNotFound, "Thread not found.")
			return
		}
		writeJSON(w, http.StatusOK, thread)
	}
}

func handleDelete(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deleted, err := store.Delete(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Cannot delete the thread.")
			return
		}
		if !deleted {
			writeError(w, http.StatusNotFound, "Thread not found.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	}
}

func validateName(errs map[string]string, field, value string) {
	if strings.TrimSpace(value) == "" {
		errs[field] = "The " + field + " field is required."
	}
}

func decode(r *http.Request, into any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(into)
}

func badBody(err error) string {
	if field, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		return "Unknown field " + field + " in the body."
	}
	return "Invalid JSON body."
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"message": message})
}

func writeValidation(w http.ResponseWriter, errs map[string]string) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"message": "The given data was invalid.",
		"errors":  errs,
	})
}
