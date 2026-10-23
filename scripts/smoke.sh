#!/bin/sh
# End-to-end smoke test: index one generated clip, serve it, fetch a Range.
set -eu
BIN=${1:-./nuitelle}
TMP=$(mktemp -d)
trap 'kill "${PID:-}" 2>/dev/null || true; rm -rf "$TMP"' EXIT
mkdir -p "$TMP/media"
ffmpeg -v error -f lavfi -i testsrc2=size=320x240:rate=24 -t 2 -c:v libx264 -pix_fmt yuv420p "$TMP/media/a.mp4"
export NUITTELLE_ROOTS="$TMP/media" NUITTELLE_DB="$TMP/nuitelle.db" NUITTELLE_COVERS="$TMP/covers" NUITTELLE_ADDR=127.0.0.1:8770
"$BIN" scan
"$BIN" serve >"$TMP/serve.log" 2>&1 &
PID=$!
for i in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:8770/api/health >/dev/null 2>&1; then break; fi
  sleep 0.5
done
echo "health: $(curl -fsS http://127.0.0.1:8770/api/health)"
ID=$(curl -fsS http://127.0.0.1:8770/api/media | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
test -n "$ID" || { echo "no media indexed"; exit 1; }
CODE=$(curl -s -o /dev/null -w '%{http_code}' -r 0-100 "http://127.0.0.1:8770/api/media/$ID/stream")
echo "range: $CODE"
test "$CODE" = "206"
curl -fsS -X POST -H 'Content-Type: application/json' -d '{"add":["smoke"]}' "http://127.0.0.1:8770/api/media/$ID/tags" >/dev/null
curl -fsS "http://127.0.0.1:8770/api/tags" | grep -q smoke
echo "smoke: ok"
