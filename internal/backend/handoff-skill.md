---
name: handoff
description: Use when you need to ask an agent in another sandbox a question, when you are asked to watch for incoming handoff questions, or whenever someone says "handoff", "ask the other sandbox", or "start monitoring handoff"
---

# Handoff

Agents in different sandboxes ask each other threaded questions through a broker
running on the host. You are `$LID_SANDBOX`. The broker is `$LID_HANDOFF_URL`.

If either variable is empty, handoff is not enabled here — say so instead of
guessing an address.

## The one rule: every thread is waiting on somebody

`waiting_on` names whose turn it is. That single field is what you watch, and it
covers **both** directions:

- someone asked **you** a question → the thread is waiting on you
- you asked someone and **they replied** → the thread is waiting on you again

So a watch on `waiting_on=$LID_SANDBOX` catches everything that needs you. Do not
poll for your own threads separately — you would miss incoming questions, which
are the more urgent half.

A thread stops waiting on anyone when it is answered or closed.

## Start monitoring

When you are asked to start handoff monitoring, or you have asked a question and
expect an answer, arm a **persistent** monitor. Do not sit in a foreground loop
waiting — you would block, and you would miss questions while you worked.

Use the Monitor tool with `persistent: true` and a `description` naming this
sandbox, like `handoff for $LID_SANDBOX` — the description appears in every
notification, so a bare "watching" tells the user nothing. When you are asked to
stop watching, stop it with TaskStop; a persistent monitor otherwise runs until
the session ends.

```bash
since=""
down=""
while true; do
  poll=300
  [ -n "$down" ] && poll=0
  page=$(curl -s --max-time 310 \
    "$LID_HANDOFF_URL/handoff/threads?waiting_on=$LID_SANDBOX&wait=$poll${since:+&since=$since}" 2>/dev/null)
  case "$page" in
    "["*)
      if [ -n "$down" ]; then echo "handoff: broker reachable again"; down=""; fi
      echo "$page" | jq -r '.[] | "handoff: \(.from) — \(.subject) [\(.id)]"' 2>/dev/null
      next=$(echo "$page" | jq -r '[.[].updated_at] | max // empty' 2>/dev/null)
      [ -n "$next" ] && since="$next"
      ;;
    *)
      if [ -z "$down" ]; then echo "handoff: broker unreachable at $LID_HANDOFF_URL"; down=1; fi
      sleep 5
      ;;
  esac
done
```

What each part is doing, so you can adapt it rather than cargo-cult it:

- `wait=300` is a **long poll**: the broker holds the request open until
  something needs you, so you hear about a question within a second of it being
  asked instead of on your next poll. `--max-time 310` has to stay above it, or
  curl cuts the connection first. The broker caps any wait at 5 minutes.
- `since` is the **cursor**, and it is what makes the long poll actually wait.
  Without it the broker answers instantly for as long as any thread is waiting on
  you — which is the normal state — and the loop hammers it until the monitor is
  rate-limited and stopped. Carrying the newest `updated_at` you have seen means
  the next poll only returns what happened after that, so it parks properly until
  something really changes. Leave it empty on the first pass to pick up anything
  already waiting for you.
- The `down` flag is why the broker being dead is not silent. A monitor that only
  reports questions looks exactly the same whether nobody is asking or the broker
  died an hour ago, and you would keep reporting that you are watching. It emits
  once when the broker goes away and once when it comes back — once, not every
  loop, or the notifications become the problem. While it is down the poll drops
  to `wait=0`, so recovery is noticed on the next try instead of up to five
  minutes later, parked on a long poll nobody is answering.

When you see `broker unreachable`, say so and tell the user to run
`lid handoff -D` on their host. The loop keeps trying, so once they do, the next
event you emit is `broker reachable again` and nothing needs restarting.

Report each event to the user when it arrives, then act on it.

## Ask a question

```bash
curl -s -X POST "$LID_HANDOFF_URL/handoff/threads" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg from "$LID_SANDBOX" --arg to "other-sandbox" \
        --arg subject "Auth header format" \
        --arg body "Which header does the API expect, Authorization or X-Token?" \
        '{from:$from,to:$to,subject:$subject,body:$body}')"
```

The reply comes back on the watch above. Arm the monitor **before** you ask, or
you may be idle when the answer lands.

## Reply to a thread

Every reply must say what happens next. Exactly one of:

```bash
# Hand the turn back to whoever needs to act
curl -s -X POST "$LID_HANDOFF_URL/handoff/threads/$ID/messages" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg from "$LID_SANDBOX" --arg body "Which endpoint do you mean?" \
        --arg waiting_on "web" '{from:$from,body:$body,waiting_on:$waiting_on}')"

# Or resolve it — the question is fully answered
curl -s -X POST "$LID_HANDOFF_URL/handoff/threads/$ID/messages" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg from "$LID_SANDBOX" --arg body "Authorization: Bearer <token>." \
        '{from:$from,body:$body,status:"answered"}')"
```

Sending neither is rejected with a 422, on purpose: a reply that names nobody
leaves the asker waiting forever with no way to know.

## The rest of the API

```bash
GET    $LID_HANDOFF_URL/handoff/threads?to=&status=&waiting_on=&since=&wait=
GET    $LID_HANDOFF_URL/handoff/threads/$ID
POST   $LID_HANDOFF_URL/handoff/threads/$ID/close     # drop it, unanswered
DELETE $LID_HANDOFF_URL/handoff/threads/$ID
GET    $LID_HANDOFF_URL/handoff/health
```

## When the broker is not running

Every call fails with a connection error. The broker runs on the **host**, not in
here, and nothing you can do from inside the sandbox will start it. Tell the user
to run `lid handoff -D` on their machine.
