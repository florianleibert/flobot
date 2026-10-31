#!/usr/bin/env bash
# demo-kit.sh proves the whole integration chain on this Mac: install the ten
# ready-made automations, fire each one with its own example event, and print
# what the local model answered.
#
# It is the scripted version of what a human does in the UI:
#
#   Integrations -> Use cases -> pick one -> Send a test event -> read the run
#
# Nothing here needs a provider account. The script talks to flobot's own API,
# so it works against a local server, a tunnel, or a colleague's box.
#
# Usage:
#   scripts/demo-kit.sh [--base URL] [--only id,id] [--webhook-demo]
#                       [--fresh] [--timeout SECONDS] [--out DIR]
#
#   --base URL        flobot's base URL (default http://127.0.0.1:4120)
#   --only a,b        only these use case ids (default: all ten)
#   --webhook-demo    also fire the real /fire receiver with a signed request,
#                     proving the credential path, not just the test path
#   --fresh           remove the current kit first (threads, integrations,
#                     routines, bots, skills) and reinstall it from scratch
#   --timeout SECONDS how long one run may take (default 600)
#   --out DIR         where the report is written (default /tmp/flobot-demo)
#
# Exit code is 0 only when every selected use case produced an answer.
set -u

BASE=${FLOBOT_BASE:-http://127.0.0.1:4120}
ONLY=""
WEBHOOK_DEMO=0
FRESH=0
TIMEOUT=600
OUT=${FLOBOT_DEMO_OUT:-/tmp/flobot-demo}
STORE=${FLOBOT_STORE:-$HOME/.flobot/integrations.json}

while [ $# -gt 0 ]; do
  case "$1" in
    --base) BASE=$2; shift 2;;
    --only) ONLY=$2; shift 2;;
    --webhook-demo) WEBHOOK_DEMO=1; shift;;
    --fresh) FRESH=1; shift;;
    --timeout) TIMEOUT=$2; shift 2;;
    --out) OUT=$2; shift 2;;
    -h|--help) sed -n '2,30p' "$0"; exit 0;;
    *) echo "unknown flag: $1" >&2; exit 2;;
  esac
done

mkdir -p "$OUT"
FAILURES=0

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
info() { printf '  %s\n' "$*"; }
bad() { printf '  \033[31m✗\033[0m %s\n' "$*"; FAILURES=$((FAILURES+1)); }
ok() { printf '  \033[32m✓\033[0m %s\n' "$*"; }

# api GET path -> body on stdout
api_get() { curl -sS -m 30 "$BASE$1"; }
# api POST path body -> body on stdout
api_post() {
  curl -sS -m 60 -X POST "$BASE$1" -H 'Content-Type: application/json' -d "$2"
}

# ---------------------------------------------------------------------------
# 0. Preflight: is flobot up, and is the local model it depends on up?
# ---------------------------------------------------------------------------
say "Preflight"
if ! api_get /api/v1/roster >/dev/null 2>&1; then
  bad "flobot is not answering at $BASE — start it with: flobot serve --addr 127.0.0.1:4120"
  exit 1
fi
ok "flobot is answering at $BASE"

PIN=$(api_get /api/v1/local | python3 -c "import sys,json;print(json.load(sys.stdin).get('pin',''))" 2>/dev/null)
if [ -n "$PIN" ]; then
  ok "local route pinned to $PIN"
else
  info "no local pin reported — the kit still activates, but a run needs a model"
fi

# ---------------------------------------------------------------------------
# 1. Optional: wipe the kit so the demo starts from nothing.
# ---------------------------------------------------------------------------
if [ "$FRESH" = "1" ]; then
  say "Removing the current kit"
  api_get /api/v1/threads | python3 -c "import sys,json;[print(t['id']) for t in json.load(sys.stdin).get('threads',[])]" 2>/dev/null |
    while read -r id; do [ -n "$id" ] && curl -sS -m 20 -X DELETE "$BASE/api/v1/threads/$id" -o /dev/null; done
  api_get /api/v1/integrations | python3 -c "import sys,json;[print(i['id']) for i in json.load(sys.stdin).get('integrations',[])]" 2>/dev/null |
    while read -r id; do [ -n "$id" ] && curl -sS -m 20 -X DELETE "$BASE/api/v1/integrations/$id" -o /dev/null; done
  api_get /api/v1/roster | python3 -c "
import sys,json
d=json.load(sys.stdin)
for r in d.get('routines',[]): print('rtn',r['id'])
for b in d.get('bots',[]): print('bot',b['id'])
for s in d.get('skills',[]): print('skill',s['id'])
" 2>/dev/null | while read -r kind id; do
    [ -z "$id" ] && continue
    case "$kind" in
      rtn) curl -sS -m 20 -X DELETE "$BASE/api/v1/routines/$id" -o /dev/null;;
      bot) curl -sS -m 20 -X DELETE "$BASE/api/v1/bots/$id" -o /dev/null;;
      skill) curl -sS -m 20 -X DELETE "$BASE/api/v1/skills/$id" -o /dev/null;;
    esac
  done
  ok "old kit removed"
fi

# ---------------------------------------------------------------------------
# 2. Install: every use case needs an integration with an enabled routine.
# ---------------------------------------------------------------------------
say "Installing the kit"
api_get /api/v1/usecases > "$OUT/usecases.json"
[ -s "$OUT/usecases.json" ] || { bad "could not read the kit from $BASE/api/v1/usecases"; exit 1; }
api_get /api/v1/integrations > "$OUT/integrations-before.json"

# bash 3.2 (the macOS default) has no mapfile, so read into an array by hand.
UCS=()
while IFS= read -r line; do
  [ -n "$line" ] && UCS+=("$line")
done < <(python3 -c "
import json
d=json.load(open('$OUT/usecases.json'))
want=set(filter(None,'$ONLY'.split(',')))
for u in d['usecases']:
    if not want or u['id'] in want:
        print(u['id'])
")

declare -a UC_NAME UC_ID UC_INT
for UC in "${UCS[@]}"; do
  NAME=$(python3 -c "
import json
d=json.load(open('$OUT/usecases.json'))
print(next(u['name'] for u in d['usecases'] if u['id']=='$UC'))")
  # An integration with this name already exists?
  EXIST=$(python3 -c "
import json
d=json.load(open('$OUT/integrations-before.json'))
for i in d.get('integrations',[]):
    if i.get('name')=='''$NAME''':
        live=[r for r in (i.get('routines') or []) if not r.get('paused')]
        print(i['id'], 'live' if live else 'unbound')
        break
" 2>/dev/null)
  IID=$(printf '%s' "$EXIST" | awk '{print $1}')
  STATE=$(printf '%s' "$EXIST" | awk '{print $2}')

  if [ -n "$IID" ] && [ "$STATE" = "live" ]; then
    info "$UC: reusing $IID"
    # Keep the tool policy in step with the kit: an installation from an
    # older flobot still carries the old policy, and the run would answer
    # with a tool call instead of the answer.
    TOOLS=$(python3 -c "
import json
d=json.load(open('$OUT/usecases.json'))
print(next(u['tools'] for u in d['usecases'] if u['id']=='$UC'))" 2>/dev/null)
    RTID=$(python3 -c "
import json
d=json.load(open('$OUT/integrations-before.json'))
for i in d.get('integrations',[]):
    if i.get('id')=='''$IID''':
        rs=[r for r in (i.get('routines') or []) if not r.get('paused')]
        print(rs[0]['id'] if rs else '')
        break
" 2>/dev/null)
    if [ -n "$RTID" ] && [ -n "$TOOLS" ]; then
      curl -sS -m 20 -X PATCH "$BASE/api/v1/routines/$RTID" \
        -H 'Content-Type: application/json' -d "{\"tools\":\"$TOOLS\"}" -o /dev/null
    fi
  else
    if [ -n "$IID" ] && [ "$STATE" = "unbound" ]; then
      BODY="{\"integration_id\":\"$IID\"}"
    else
      BODY='{"create_integration":true}'
    fi
    RES=$(api_post "/api/v1/usecases/$UC/activate" "$BODY")
    IID=$(printf '%s' "$RES" | python3 -c "import sys,json;d=json.load(sys.stdin);print((d.get('integration') or {}).get('id',''))" 2>/dev/null)
    if [ -z "$IID" ]; then
      bad "$UC: activation failed"
      continue
    fi
    info "$UC: activated, integration $IID"
  fi
  UC_NAME+=("$NAME"); UC_ID+=("$UC"); UC_INT+=("$IID")
done

# ---------------------------------------------------------------------------
# 3. Fire: each use case's own example event, one at a time.
# ---------------------------------------------------------------------------
say "Firing the example events"
: > "$OUT/report.jsonl"

for i in "${!UC_ID[@]}"; do
  UC=${UC_ID[$i]}
  NAME=${UC_NAME[$i]}
  IID=${UC_INT[$i]}
  # The example payload can contain quotes and escapes, so python writes the
  # request body and curl sends the file verbatim.
  BODYFILE="$OUT/body-$UC.json"
  python3 - "$OUT/usecases.json" "$UC" "$BODYFILE" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
ev = next(u["event"] for u in d["usecases"] if u["id"] == sys.argv[2])
json.dump({"event": ev}, open(sys.argv[3], "w"))
PY
  FIRE=$(curl -sS -m 60 -X POST "$BASE/api/v1/integrations/$IID/test" \
    -H 'Content-Type: application/json' --data-binary "@$BODYFILE")
  RUN=$(printf '%s' "$FIRE" | python3 -c "import sys,json;d=json.load(sys.stdin);r=d.get('runs') or [''];print(r[0])" 2>/dev/null)
  FIRED=$(printf '%s' "$FIRE" | python3 -c "import sys,json;print(json.load(sys.stdin).get('fired',0))" 2>/dev/null)
  if [ -z "$RUN" ]; then
    REASON=$(printf '%s' "$FIRE" | python3 -c "import sys,json;print(json.load(sys.stdin).get('reason','no run'))" 2>/dev/null)
    bad "$UC: nothing fired ($REASON)"
    printf '{"use_case":"%s","status":"no-run","reason":"%s"}\n' "$UC" "$REASON" >> "$OUT/report.jsonl"
    continue
  fi

  # Wait for the run to finish. A cold local model can take minutes.
  DEADLINE=$(( $(date +%s) + TIMEOUT ))
  ANSWER=""
  STATUS="timeout"
  while [ "$(date +%s)" -lt "$DEADLINE" ]; do
    sleep 5
    EV=$(curl -sS -m 20 "$BASE/api/v1/runs/$RUN/events" 2>/dev/null)
    printf '%s' "$EV" > "$OUT/events-$UC.json"
    RES=$(printf '%s' "$EV" | python3 -c "
import sys,json
try:
    d=json.load(sys.stdin)
except Exception:
    raise SystemExit(0)
rows=d if isinstance(d,list) else d.get('events',[]) or []
kinds=[r.get('kind') for r in rows]
if 'err' in kinds:
    txt=''.join(str(r.get('text','')) for r in rows if r.get('kind')=='err')
    print('error\t'+' '.join(txt.split())[:300]); raise SystemExit(0)
if 'done' in kinds:
    # The content events are token fragments: join them without a separator
    # or every token gets a space in the middle of it.
    txt=''.join(str(r.get('text','')) for r in rows if r.get('kind') in ('content','done'))
    print('done\t'+' '.join(txt.split())); raise SystemExit(0)
" 2>/dev/null)
    case "$RES" in
      done*) STATUS="done"; ANSWER=${RES#done$'\t'}; break;;
      error*) STATUS="error"; ANSWER=${RES#error$'\t'}; break;;
    esac

    # A run whose child died before it emitted anything (the model server is
    # down, the run hit its own deadline) has no events at all, so the stream
    # above never says done or err. Without this the loop spins the full
    # --timeout per use case and then reports "timeout" for a failure that
    # already happened. The run's own record is the authoritative end signal.
    RST=$(curl -sS -m 20 "$BASE/api/v1/runs/$RUN" 2>/dev/null | python3 -c "
import sys,json
try:
    d=json.load(sys.stdin)
except Exception:
    raise SystemExit(0)
if not d.get('ended_at'):
    raise SystemExit(0)          # still running: keep polling
st=(d.get('status') or 'done').strip() or 'done'
err=' '.join(str(d.get('error') or '').split())[:300]
print(('done' if st == 'done' else 'error')+'\t'+('' if st == 'done' else (err or 'finished with status '+st)))
" 2>/dev/null)
    case "$RST" in
      done*) STATUS="done"; ANSWER="(the run finished without emitting any events)"; break;;
      error*) STATUS="error"; ANSWER=${RST#error$'\t'}; break;;
    esac
  done

  printf '%s\n' "$ANSWER" > "$OUT/$UC.txt"
  python3 - "$UC" "$NAME" "$RUN" "$STATUS" "$ANSWER" >> "$OUT/report.jsonl" <<'PY'
import json,sys
uc,name,run,status,answer=sys.argv[1:6]
print(json.dumps({"use_case":uc,"name":name,"run":run,"status":status,"answer":answer}))
PY
  if [ "$STATUS" = "done" ] && [ -n "$ANSWER" ]; then
    ok "$UC  $NAME"
    printf '     %s\n' "$(printf '%s' "$ANSWER" | head -c 240)"
  else
    bad "$UC  $NAME — $STATUS"
    [ -n "$ANSWER" ] && printf '     %s\n' "$(printf '%s' "$ANSWER" | head -c 240)"
  fi
done

# ---------------------------------------------------------------------------
# 4. Optional: the real receiver, with a correctly signed request.
# ---------------------------------------------------------------------------
if [ "$WEBHOOK_DEMO" = "1" ]; then
  say "Firing the real webhook receiver"
  if [ ! -f "$STORE" ]; then
    bad "no secret store at $STORE — cannot sign a real request"
  else
    python3 - "$BASE" "$STORE" "$OUT" <<'PY'
import hashlib, hmac, json, os, sys, time, urllib.request, urllib.error

base, store_path, out = sys.argv[1], sys.argv[2], sys.argv[3]
store = json.load(open(store_path))
items = store.get("integrations") or store.get("items") or []
# Prefer a plain webhook integration: its scheme is the one any curl can do.
target = next((i for i in items if i.get("kind") == "webhook"), None) or (items[0] if items else None)
if target is None:
    print("  \033[31m✗\033[0m no integration to fire"); raise SystemExit(1)

secret = target.get("secret", "")
iid = target.get("id")
mode = target.get("auth_mode") or "bearer"
body = json.dumps({"text": "say hello from the demo script", "source": "demo-kit"}).encode()

url = "%s/api/v1/integrations/%s/fire" % (base, iid)
headers = {"Content-Type": "application/json"}
if mode == "query":
    url += "?key=" + secret
elif mode == "hmac":
    mac = hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
    headers["X-Hub-Signature-256"] = "sha256=" + mac
elif mode == "stripe":
    ts = str(int(time.time()))
    mac = hmac.new(secret.encode(), (ts + ".").encode() + body, hashlib.sha256).hexdigest()
    headers["Stripe-Signature"] = "t=%s,v1=%s" % (ts, mac)
elif mode == "slack":
    ts = str(int(time.time()))
    payload = ("v0:%s:" % ts).encode() + body
    mac = hmac.new(secret.encode(), payload, hashlib.sha256).hexdigest()
    headers["X-Slack-Request-Timestamp"] = ts
    headers["X-Slack-Signature"] = "v0=" + mac
else:
    headers["Authorization"] = "Bearer " + secret

# A wrong credential must be refused — that is half the proof.
bad_req = urllib.request.Request(url, data=body, headers=dict(headers), method="POST")
if "Authorization" in headers:
    bad_headers = dict(headers); bad_headers["Authorization"] = "Bearer wrong-secret"
    bad_req = urllib.request.Request(url, data=body, headers=bad_headers, method="POST")
try:
    urllib.request.urlopen(bad_req, timeout=30)
    print("  \033[31m✗\033[0m a wrong credential was accepted — that is a bug")
except urllib.error.HTTPError as e:
    if e.code == 401:
        print("  \033[32m✓\033[0m wrong credential refused with 401")
    else:
        print("  \033[31m✗\033[0m wrong credential got %d, expected 401" % e.code)

req = urllib.request.Request(url, data=body, headers=headers, method="POST")
try:
    with urllib.request.urlopen(req, timeout=60) as r:
        payload = json.loads(r.read().decode())
    print("  \033[32m✓\033[0m signed request accepted: fired=%s runs=%s" %
          (payload.get("fired"), payload.get("runs")))
    json.dump(payload, open(os.path.join(out, "webhook-fire.json"), "w"), indent=1)
except urllib.error.HTTPError as e:
    print("  \033[31m✗\033[0m signed request refused: %s %s" % (e.code, e.read().decode()[:200]))
PY
  fi
fi

# ---------------------------------------------------------------------------
# 5. Summary.
# ---------------------------------------------------------------------------
say "Summary"
python3 - "$OUT/report.jsonl" <<'PY'
import json, sys
rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
for r in rows:
    mark = "OK " if r["status"] == "done" else "FAIL"
    ans = " ".join(str(r.get("answer", "")).split())[:110]
    print("  %-4s %-12s %-34s %s" % (mark, r["use_case"], r["name"][:34], ans))
print("\n  %d of %d use cases answered" % (sum(1 for r in rows if r["status"] == "done"), len(rows)))
PY
info "report: $OUT/report.jsonl"
[ "$FAILURES" -gt 0 ] && exit 1
exit 0
