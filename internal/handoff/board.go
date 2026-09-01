package handoff

import (
	"html/template"
	"io"
	"strings"
	"time"
)

const (
	previewCount = 3
	previewChars = 240
)

type boardMessage struct {
	From  string
	Body  string
	At    string
	Asked bool
}

type boardThread struct {
	ID        string
	From      string
	To        string
	Subject   string
	Status    string
	WaitingOn string
	Updated   string
	Count     int
	Earlier   int
	Messages  []boardMessage
}

type boardView struct {
	Threads []boardThread
	Now     string
}

type threadView struct {
	Thread boardThread
	Now    string
}

func RenderBoard(w io.Writer, threads []Thread) error {
	view := boardView{Now: time.Now().Format("Mon 2 Jan · 15:04")}
	for i := len(threads) - 1; i >= 0; i-- {
		view.Threads = append(view.Threads, previewOf(threads[i]))
	}
	return boardTemplates.ExecuteTemplate(w, "board", view)
}

func RenderThread(w io.Writer, thread Thread) error {
	view := threadView{Thread: fullOf(thread), Now: time.Now().Format("Mon 2 Jan · 15:04")}
	return boardTemplates.ExecuteTemplate(w, "thread", view)
}

func previewOf(t Thread) boardThread {
	board := headerOf(t)
	from := 0
	if len(t.Messages) > previewCount {
		from = len(t.Messages) - previewCount
	}
	board.Earlier = from
	for _, message := range t.Messages[from:] {
		board.Messages = append(board.Messages, messageOf(message, t.From, true))
	}
	return board
}

func fullOf(t Thread) boardThread {
	board := headerOf(t)
	for _, message := range t.Messages {
		board.Messages = append(board.Messages, messageOf(message, t.From, false))
	}
	return board
}

func headerOf(t Thread) boardThread {
	return boardThread{
		ID:        t.ID,
		From:      t.From,
		To:        t.To,
		Subject:   t.Subject,
		Status:    t.Status,
		WaitingOn: t.WaitingOn,
		Updated:   clock(t.UpdatedAt, "2 Jan · 15:04"),
		Count:     len(t.Messages),
	}
}

func messageOf(m Message, asker string, preview bool) boardMessage {
	body := m.Body
	if preview {
		body = clip(body, previewChars)
	}
	return boardMessage{
		From:  m.From,
		Body:  body,
		At:    stamp(m.At),
		Asked: m.From == asker,
	}
}

func clock(at, layout string) string {
	parsed, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return at
	}
	return parsed.Local().Format(layout)
}

func stamp(at string) string {
	parsed, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return at
	}
	local := parsed.Local()
	now := time.Now()
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return local.Format("15:04:05")
	}
	return local.Format("2 Jan · 15:04")
}

func clip(body string, limit int) string {
	runes := []rune(body)
	if len(runes) <= limit {
		return body
	}
	return strings.TrimRight(string(runes[:limit]), " \t\r\n") + "…"
}

var boardTemplates = func() *template.Template {
	t := template.Must(template.New("style").Parse(boardStyle))
	template.Must(t.New("board").Parse(boardHTML))
	template.Must(t.New("thread").Parse(threadHTML))
	return t
}()

const boardStyle = `<style>
:root{
  --bg:#0b0d11; --panel:#14171d; --line:#242a33;
  --ink:#e8eaed; --muted:#9ca3af; --faint:#6b7280;
  --open:#fbbf24; --answered:#a8db8f; --closed:#6b7280;
  --accent:#e8734a; --label:#c084fc;
  --sans:ui-sans-serif,-apple-system,"Segoe UI",Roboto,sans-serif;
  --mono:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
}
*{box-sizing:border-box}
html{color-scheme:dark}
body{margin:0;padding:0 0 64px;background:var(--bg);color:var(--ink);font-family:var(--sans);line-height:1.6}
.page{max-width:820px;margin:0 auto;padding:36px 22px 0}
.top{display:flex;align-items:baseline;gap:12px;padding-bottom:16px;border-bottom:1px solid var(--line);margin-bottom:24px}
.brand{font-family:var(--mono);font-size:15px;font-weight:600;letter-spacing:.14em;text-transform:uppercase;color:var(--accent)}
.top .when{margin-left:auto;font-family:var(--mono);font-size:12px;color:var(--faint)}
.count{font-family:var(--mono);font-size:12px;color:var(--muted)}
.thread{background:var(--panel);border:1px solid var(--line);border-left:3px solid var(--closed);border-radius:10px;margin-bottom:14px;overflow:hidden}
.thread.open{border-left-color:var(--open)}
.thread.answered{border-left-color:var(--answered)}
summary{display:block;padding:14px 18px 8px;cursor:pointer;list-style:none}
summary::-webkit-details-marker{display:none}
.row{display:flex;align-items:flex-start;gap:10px}
.subject{flex:1;min-width:0;margin:0;font-size:16px;font-weight:600}
.status{flex:0 0 auto;font-family:var(--mono);font-size:11px;letter-spacing:.12em;text-transform:uppercase;padding-top:4px}
.thread.open .status{color:var(--open)}
.thread.answered .status{color:var(--answered)}
.thread.closed .status{color:var(--closed)}
.close{flex:0 0 auto;margin-top:1px;padding:2px 7px;border:0;border-radius:6px;background:none;color:var(--faint);font-family:var(--mono);font-size:13px;cursor:pointer}
.close:hover{color:#ff6b6b;background:rgba(255,107,107,.12)}
.close:disabled{opacity:.4;cursor:default}
.meta{display:flex;flex-wrap:wrap;align-items:center;gap:8px;padding:0 18px;font-family:var(--mono);font-size:11.5px;color:var(--muted)}
.meta .route{color:var(--label)}
.meta .waiting{color:var(--open)}
.meta .id{color:var(--faint)}
.meta .when{margin-left:auto;color:var(--faint)}
.sep{color:var(--line)}
.messages{list-style:none;margin:12px 18px 0;padding:12px 0 16px;border-top:1px dashed var(--line)}
.earlier{font-family:var(--mono);font-size:11.5px;color:var(--faint);padding-bottom:8px}
.message{padding:7px 0}
.message .who{font-family:var(--mono);font-size:12px;font-weight:600;color:var(--answered)}
.message.asked .who{color:var(--label)}
.message .at{font-family:var(--mono);font-size:10.5px;color:var(--faint);margin-left:8px}
.message .body{margin:4px 0 0;white-space:pre-wrap;overflow-wrap:anywhere;font-size:13.5px}
.open-thread{display:inline-block;margin:0 18px 16px;padding:6px 12px;border:1px solid var(--line);border-radius:8px;font-family:var(--mono);font-size:11.5px;color:var(--accent);text-decoration:none}
.open-thread:hover{border-color:var(--accent)}
.empty{padding:80px 0;text-align:center;font-family:var(--mono);color:var(--faint)}
.back{display:inline-block;margin-bottom:20px;font-family:var(--mono);font-size:12px;color:var(--muted);text-decoration:none}
.back:hover{color:var(--accent)}
.detail h1{margin:0 0 10px;font-size:24px;line-height:1.25}
.detail .head{padding-bottom:18px;border-bottom:1px solid var(--line);margin-bottom:22px}
.detail .meta{padding:0}
.detail .messages{margin:0;padding:0;border:0}
.detail .message{border:1px solid var(--line);border-left:3px solid var(--answered);border-radius:9px;padding:13px 16px;margin-bottom:12px;background:var(--panel)}
.detail .message.asked{border-left-color:var(--label)}
.detail .message .body{font-size:14px;line-height:1.7}
</style>`

const boardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="5">
<title>lid · handoff</title>
{{template "style" .}}
</head>
<body>
<div class="page">
  <header class="top">
    <span class="brand">lid handoff</span>
    <span class="count">{{len .Threads}} active</span>
    <span class="when">{{.Now}}</span>
  </header>
  {{range .Threads}}
  <details class="thread {{.Status}}" data-thread="{{.ID}}"{{if eq .Status "open"}} open{{end}}>
    <summary>
      <div class="row">
        <h2 class="subject">{{.Subject}}</h2>
        <span class="status">{{.Status}}</span>
        <button type="button" class="close" title="close this thread" aria-label="close this thread">✕</button>
      </div>
    </summary>
    <div class="meta">
      <span class="route">{{.From}} → {{.To}}</span><span class="sep">·</span>
      {{if .WaitingOn}}<span class="waiting">waiting on {{.WaitingOn}}</span><span class="sep">·</span>{{end}}
      <span>{{.Count}} message{{if ne .Count 1}}s{{end}}</span><span class="sep">·</span>
      <span class="id">{{.ID}}</span>
      <span class="when">{{.Updated}}</span>
    </div>
    <ul class="messages">
      {{if .Earlier}}<li class="earlier">+{{.Earlier}} earlier message{{if ne .Earlier 1}}s{{end}}</li>{{end}}
      {{range .Messages}}
      <li class="message{{if .Asked}} asked{{end}}">
        <span class="who">{{.From}}</span><span class="at">{{.At}}</span>
        <p class="body">{{.Body}}</p>
      </li>
      {{end}}
    </ul>
    <a class="open-thread" href="/handoff/thread/{{.ID}}">read the whole thread →</a>
  </details>
  {{else}}
  <p class="empty">No threads yet.</p>
  {{end}}
</div>
<script>
(function () {
  var key = 'lid:handoff:open:';
  var store = null;
  try { store = window.localStorage; } catch (e) { store = null; }

  document.querySelectorAll('details.thread').forEach(function (thread) {
    var id = thread.getAttribute('data-thread');
    if (store) {
      try {
        var saved = store.getItem(key + id);
        if (saved === '1') thread.open = true;
        else if (saved === '0') thread.open = false;
      } catch (e) {}
    }
    thread.addEventListener('toggle', function () {
      if (!store) return;
      try { store.setItem(key + id, thread.open ? '1' : '0'); } catch (e) {}
    });
  });

  document.querySelectorAll('button.close').forEach(function (button) {
    button.addEventListener('click', function (event) {
      event.preventDefault();
      event.stopPropagation();
      var thread = button.closest('details.thread');
      if (!thread) return;
      var id = thread.getAttribute('data-thread');
      button.disabled = true;
      fetch('/handoff/threads/' + encodeURIComponent(id) + '/close', { method: 'POST' })
        .then(function (response) {
          if (!response.ok) { button.disabled = false; return; }
          if (store) { try { store.removeItem(key + id); } catch (e) {} }
          thread.remove();
        })
        .catch(function () { button.disabled = false; });
    });
  });
})();
</script>
</body>
</html>`

const threadHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>lid · {{.Thread.Subject}}</title>
{{template "style" .}}
</head>
<body>
<div class="page detail">
  <a class="back" href="/handoff">← every thread</a>
  <header class="head">
    <h1>{{.Thread.Subject}}</h1>
    <div class="meta">
      <span class="route">{{.Thread.From}} → {{.Thread.To}}</span><span class="sep">·</span>
      {{if .Thread.WaitingOn}}<span class="waiting">waiting on {{.Thread.WaitingOn}}</span><span class="sep">·</span>{{end}}
      <span>{{.Thread.Status}}</span><span class="sep">·</span>
      <span>{{.Thread.Count}} message{{if ne .Thread.Count 1}}s{{end}}</span><span class="sep">·</span>
      <span class="id">{{.Thread.ID}}</span>
      <span class="when">{{.Thread.Updated}}</span>
    </div>
  </header>
  <ol class="messages">
    {{range .Thread.Messages}}
    <li class="message{{if .Asked}} asked{{end}}">
      <span class="who">{{.From}}</span><span class="at">{{.At}}</span>
      <p class="body">{{.Body}}</p>
    </li>
    {{end}}
  </ol>
</div>
</body>
</html>`
