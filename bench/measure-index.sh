#!/usr/bin/env bash
# measure-index.sh — measure laptop resource cost of indexing a repo
#
# Usage: bench/measure-index.sh <repo-path> [project-name]
#
# Output: stdout summary + bench/results/index-<timestamp>/{daemon.log,samples.csv,summary.md}
#
# Requirements:
#   - codesearch binary in PATH (or set $CODESEARCH)
#   - Qdrant up on localhost:6334 (gRPC)
#   - Ollama up on localhost:11434
#   - Target repo has .codesearch.yaml (auto-created if missing, with prompt)
#
# How it works:
#   1. Verify prereqs.
#   2. Ensure .codesearch.yaml exists in target repo.
#   3. Start daemon as subprocess; capture pid.
#   4. Sample daemon pid (and ollama pid) every 2s: %CPU, RSS, VSZ. Log to samples.csv.
#   5. Poll Qdrant collection point count every 5s. Index considered "done"
#      when point count has been stable for 30s.
#   6. Compute totals: wall time, peak RSS for daemon + ollama, final point count.
#   7. Stop daemon. Write summary.md.

set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <repo-path> [project-name]" >&2
  exit 2
fi

REPO=$(cd "$1" && pwd)
PROJECT="${2:-$(basename "$REPO")}"
CODESEARCH="${CODESEARCH:-codesearch}"

if ! command -v "$CODESEARCH" >/dev/null 2>&1; then
  echo "codesearch binary not found in PATH; set \$CODESEARCH" >&2
  exit 1
fi

# Verify Qdrant + Ollama
if ! curl -sf -o /dev/null http://localhost:6333/; then
  echo "Qdrant not reachable on http://localhost:6333" >&2
  exit 1
fi
if ! curl -sf -o /dev/null http://localhost:11434/; then
  echo "Ollama not reachable on http://localhost:11434" >&2
  exit 1
fi

# Ensure .codesearch.yaml exists in target
if [[ ! -f "$REPO/.codesearch.yaml" ]]; then
  echo "No .codesearch.yaml in $REPO — creating default" >&2
  cat > "$REPO/.codesearch.yaml" <<EOF
project: $PROJECT
qdrant_url: localhost:6334
ollama_url: http://localhost:11434
ollama_model: nomic-embed-text
EOF
fi

# Output dir
TS=$(date -u +%Y%m%dT%H%M%SZ)
OUT="bench/results/index-$TS"
mkdir -p "$OUT"
LOG="$OUT/daemon.log"
CSV="$OUT/samples.csv"
SUMMARY="$OUT/summary.md"

echo "repo=$REPO project=$PROJECT out=$OUT"

# Pre-existing point count (in case the index already has data)
qdrant_points() {
  curl -sf "http://localhost:6333/collections/$PROJECT" 2>/dev/null \
    | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d.get("result",{}).get("points_count",0))' 2>/dev/null \
    || echo 0
}

INITIAL_POINTS=$(qdrant_points)
echo "initial qdrant points for project $PROJECT: $INITIAL_POINTS"

# Start daemon
echo "starting daemon..."
"$CODESEARCH" daemon "$REPO" > "$LOG" 2>&1 &
DPID=$!
trap 'kill "$DPID" 2>/dev/null || true' EXIT

# Wait for daemon to actually start
sleep 2
if ! kill -0 "$DPID" 2>/dev/null; then
  echo "daemon died early; see $LOG" >&2
  cat "$LOG" >&2
  exit 1
fi

# Find ollama pid (best-effort; might be inside Docker on some setups)
OPID=$(pgrep -x ollama | head -1 || echo "")

# Sample loop
echo "timestamp_unix,elapsed_s,daemon_cpu,daemon_rss_kb,daemon_vsz_kb,ollama_cpu,ollama_rss_kb,qdrant_points" > "$CSV"
START=$(date +%s)
LAST_POINTS=-1
STABLE_SINCE=0
PEAK_DAEMON_RSS=0
PEAK_OLLAMA_RSS=0
SAMPLES=0

echo "sampling... (Ctrl-C to stop manually)"

while true; do
  NOW=$(date +%s)
  ELAPSED=$((NOW - START))

  if ! kill -0 "$DPID" 2>/dev/null; then
    echo "daemon exited unexpectedly at elapsed=${ELAPSED}s" >&2
    break
  fi

  # macOS ps: %cpu rss vsz
  read DCPU DRSS DVSZ < <(ps -o pcpu=,rss=,vsz= -p "$DPID" 2>/dev/null || echo "0 0 0")
  if [[ -n "$OPID" ]] && kill -0 "$OPID" 2>/dev/null; then
    read OCPU ORSS _ < <(ps -o pcpu=,rss=,vsz= -p "$OPID" 2>/dev/null || echo "0 0 0")
  else
    OCPU=0
    ORSS=0
  fi

  POINTS=$(qdrant_points)

  echo "$NOW,$ELAPSED,$DCPU,$DRSS,$DVSZ,$OCPU,$ORSS,$POINTS" >> "$CSV"

  # Track peaks
  if (( DRSS > PEAK_DAEMON_RSS )); then PEAK_DAEMON_RSS=$DRSS; fi
  if (( ORSS > PEAK_OLLAMA_RSS )); then PEAK_OLLAMA_RSS=$ORSS; fi
  SAMPLES=$((SAMPLES + 1))

  # Stability check: same points for 30s = done
  if [[ "$POINTS" == "$LAST_POINTS" ]]; then
    if (( STABLE_SINCE == 0 )); then
      STABLE_SINCE=$NOW
    elif (( NOW - STABLE_SINCE >= 30 && ELAPSED >= 30 && POINTS > INITIAL_POINTS )); then
      echo "index stable at $POINTS points for ${STABLE_SINCE}s — assuming first pass done"
      break
    fi
  else
    LAST_POINTS=$POINTS
    STABLE_SINCE=0
  fi

  # Print progress every 10s
  if (( ELAPSED % 10 == 0 )); then
    printf "  elapsed=%ds points=%s daemon_rss=%sKB cpu=%s%%\n" "$ELAPSED" "$POINTS" "$DRSS" "$DCPU"
  fi

  sleep 2
done

FINAL_POINTS=$(qdrant_points)
WALL=$((NOW - START))
NEW_POINTS=$((FINAL_POINTS - INITIAL_POINTS))

# Stop daemon cleanly
kill "$DPID" 2>/dev/null || true
wait "$DPID" 2>/dev/null || true
trap - EXIT

# Compute means from CSV
MEAN_DAEMON_CPU=$(awk -F, 'NR>1 {sum+=$3; n++} END {if (n>0) printf "%.1f", sum/n; else print "0"}' "$CSV")
MEAN_DAEMON_RSS=$(awk -F, 'NR>1 {sum+=$4; n++} END {if (n>0) printf "%d", sum/n; else print "0"}' "$CSV")

# Summary
cat > "$SUMMARY" <<EOF
# codesearch index measurement — $TS

## Repo

- path: \`$REPO\`
- project: \`$PROJECT\`

## Result

- wall time: **${WALL}s**
- points indexed: **$NEW_POINTS** (final $FINAL_POINTS, initial $INITIAL_POINTS)
- samples taken: $SAMPLES (every 2s)

## codesearch daemon resource use

- peak RSS: **$(awk -v k=$PEAK_DAEMON_RSS 'BEGIN{printf "%.1f MB", k/1024}')**
- mean CPU: **${MEAN_DAEMON_CPU}%**
- mean RSS: $(awk -v k=$MEAN_DAEMON_RSS 'BEGIN{printf "%.1f MB", k/1024}')

## Ollama resource use

- peak RSS: $(awk -v k=$PEAK_OLLAMA_RSS 'BEGIN{printf "%.1f MB", k/1024}')

## Files

- \`daemon.log\` — daemon stderr/stdout
- \`samples.csv\` — per-sample raw ps + qdrant point count
EOF

echo
echo "=== summary ==="
cat "$SUMMARY"
echo
echo "raw samples: $CSV"
echo "daemon log:  $LOG"
