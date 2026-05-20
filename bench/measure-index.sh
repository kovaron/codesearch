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

CLEAN=0
ARGS=()
for arg in "$@"; do
  case "$arg" in
    --clean) CLEAN=1 ;;
    -h|--help)
      cat <<USAGE
Usage: $0 [--clean] <repo-path> [project-name]

  --clean   Delete the Qdrant collection for <project-name> before indexing,
            so the measurement starts from zero. Stops any running daemon first.

  CODESEARCH=<path>   Override the codesearch binary (default: 'codesearch' from PATH).
USAGE
      exit 0 ;;
    *) ARGS+=("$arg") ;;
  esac
done

if [[ ${#ARGS[@]} -lt 1 ]]; then
  echo "Usage: $0 [--clean] <repo-path> [project-name]" >&2
  exit 2
fi

REPO=$(cd "${ARGS[0]}" && pwd)
PROJECT="${ARGS[1]:-$(basename "$REPO")}"
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

# Optional clean: stop any running daemon and drop the existing collection
# so the measurement starts from a true zero.
if (( CLEAN == 1 )); then
  echo "--clean: stopping any running codesearch daemon..."
  pkill -f 'codesearch daemon' 2>/dev/null || true
  sleep 1
  echo "--clean: deleting Qdrant collection '$PROJECT'..."
  HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "http://localhost:6333/collections/$PROJECT")
  case "$HTTP_CODE" in
    200|404)
      echo "--clean: collection dropped (http $HTTP_CODE)"
      ;;
    *)
      echo "--clean: unexpected http $HTTP_CODE while deleting collection — continuing anyway" >&2
      ;;
  esac
  sleep 1
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

# Qdrant on-disk size for the collection (bytes; returns 0 when unavailable)
qdrant_disk_bytes() {
  curl -sf "http://localhost:6333/collections/$PROJECT" 2>/dev/null \
    | python3 -c '
import sys, json
try:
    d = json.load(sys.stdin)
    r = d.get("result", {})
    # Qdrant >=1.10 returns disk_data_size; older versions omit it.
    print(int(r.get("disk_data_size", 0)))
except Exception:
    print(0)
' 2>/dev/null || echo 0
}

# Count indexable source files in the target repo (best-effort heuristic).
count_source_files() {
  local repo="$1"
  find "$repo" \
    -path "$repo/.git" -prune -o \
    -path "$repo/node_modules" -prune -o \
    -path "$repo/vendor" -prune -o \
    -path "$repo/.codesearch" -prune -o \
    -type f \( -name '*.go' -o -name '*.ts' -o -name '*.tsx' -o -name '*.java' -o -name '*.py' -o -name '*.js' -o -name '*.rs' \) \
    -print 2>/dev/null | wc -l | tr -d ' '
}

# Total ncpu (for normalising CPU% — `ps pcpu` is per-core × 100, so a fully
# pegged 8-core box reports up to 800%)
NCPU=$(sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo 1)
SRC_FILES=$(count_source_files "$REPO")
echo "source files in repo: $SRC_FILES across ${NCPU} cores"

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

# Ollama isn't one process — `ollama serve` spawns runner subprocesses that
# actually hold the model weights. Sum RSS across every process whose
# command line contains "ollama" so we capture the runners too.
ollama_total_rss_kb() {
  pgrep -f ollama 2>/dev/null \
    | xargs -I{} ps -o rss= -p {} 2>/dev/null \
    | awk '{sum+=$1} END {print sum+0}'
}
ollama_total_cpu() {
  pgrep -f ollama 2>/dev/null \
    | xargs -I{} ps -o pcpu= -p {} 2>/dev/null \
    | awk '{sum+=$1} END {printf "%.1f", sum+0}'
}

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
  ORSS=$(ollama_total_rss_kb)
  OCPU=$(ollama_total_cpu)

  POINTS=$(qdrant_points)

  echo "$NOW,$ELAPSED,$DCPU,$DRSS,$DVSZ,$OCPU,$ORSS,$POINTS" >> "$CSV"

  # Track peaks
  if (( DRSS > PEAK_DAEMON_RSS )); then PEAK_DAEMON_RSS=$DRSS; fi
  if (( ORSS > PEAK_OLLAMA_RSS )); then PEAK_OLLAMA_RSS=$ORSS; fi
  SAMPLES=$((SAMPLES + 1))

  # Done detection — two signals:
  #   1. Daemon logs "daemon: watching" once initial walk finishes (authoritative).
  #   2. Qdrant point count stable for 30s with at least one new point ingested
  #      (fallback if log marker doesn't appear).
  if grep -q "daemon: watching" "$LOG" 2>/dev/null; then
    echo "daemon emitted 'watching' marker at elapsed=${ELAPSED}s — initial walk complete"
    break
  fi
  if [[ "$POINTS" == "$LAST_POINTS" ]]; then
    if (( STABLE_SINCE == 0 )); then
      STABLE_SINCE=$NOW
    elif (( NOW - STABLE_SINCE >= 30 && ELAPSED >= 30 && POINTS > INITIAL_POINTS )); then
      echo "index stable at $POINTS points for ${STABLE_SINCE}s — assuming first pass done (fallback)"
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

QDRANT_DISK=$(qdrant_disk_bytes)

# Files actually indexed (per daemon log)
INDEXED_FILES=$(grep -c "^[0-9/]* [0-9:]* index " "$LOG" 2>/dev/null || echo 0)
INDEX_ERRORS=$(grep -c "index error:" "$LOG" 2>/dev/null || echo 0)

# Throughput (use bc-free awk arithmetic)
POINTS_PER_SEC=$(awk -v p=$NEW_POINTS -v w=$WALL 'BEGIN{if(w>0)printf "%.1f", p/w; else print "0"}')
FILES_PER_SEC=$(awk -v f=$SRC_FILES -v w=$WALL 'BEGIN{if(w>0)printf "%.2f", f/w; else print "0"}')

# Normalise CPU: ps reports per-core%, max = ncpu × 100. Show both raw and normalised.
MEAN_DAEMON_CPU_NORM=$(awk -v c=$MEAN_DAEMON_CPU -v n=$NCPU 'BEGIN{printf "%.1f", c/n}')

# Summary
cat > "$SUMMARY" <<EOF
# codesearch index measurement — $TS

## Repo

- path: \`$REPO\`
- project: \`$PROJECT\`
- source files (heuristic): **$SRC_FILES**

## Headline

| Metric | Value |
|---|---|
| Wall time | **${WALL}s** |
| Files indexed (daemon log) | **$INDEXED_FILES** of $SRC_FILES discoverable |
| Index errors | $INDEX_ERRORS |
| Indexed chunks (Qdrant points) | **$NEW_POINTS** |
| Avg chunks per file | $(awk -v p=$NEW_POINTS -v f=$INDEXED_FILES 'BEGIN{if(f>0)printf "%.1f", p/f; else print "n/a"}') |
| Throughput | **${POINTS_PER_SEC} chunks/s**, **${FILES_PER_SEC} files/s** |
| Daemon peak RSS | **$(awk -v k=$PEAK_DAEMON_RSS 'BEGIN{printf "%.1f MB", k/1024}')** |
| Daemon mean CPU | **${MEAN_DAEMON_CPU}%** raw (${MEAN_DAEMON_CPU_NORM}% of ${NCPU}-core box) |
| Ollama peak RSS (sum) | $(awk -v k=$PEAK_OLLAMA_RSS 'BEGIN{printf "%.1f MB", k/1024}') ¹ |
| Qdrant on-disk (this project) | $(awk -v b=$QDRANT_DISK 'BEGIN{if(b>0)printf "%.1f MB", b/1048576; else print "n/a (qdrant version too old)"}') |

¹ On macOS, model weights are memory-mapped and only count toward RSS for
pages actually faulted in. Treat Ollama RSS as a **lower bound** — the
on-disk model size is the better upper bound (\`du -sh ~/.ollama/models\`).

## Run config

- model: \`nomic-embed-text\` (Ollama, 768-dim)
- machine: $(uname -mns) — ${NCPU} cores
- qdrant: $(curl -sf http://localhost:6333/ 2>/dev/null | python3 -c 'import sys,json; print(json.load(sys.stdin).get("version","?"))' 2>/dev/null || echo "?")
- ollama: $(curl -sf http://localhost:11434/ 2>/dev/null | head -1 || echo "?")

## Raw

- final Qdrant points: $FINAL_POINTS (was $INITIAL_POINTS at start)
- samples taken: $SAMPLES (every 2s)
- mean daemon RSS: $(awk -v k=$MEAN_DAEMON_RSS 'BEGIN{printf "%.1f MB", k/1024}')

## Files

- \`daemon.log\` — daemon stderr/stdout
- \`samples.csv\` — per-sample raw ps + qdrant point count

## Share this

\`\`\`
Indexed $SRC_FILES files into $NEW_POINTS chunks in ${WALL}s
on ${NCPU} cores. Daemon peak ${PEAK_DAEMON_RSS}KB RSS,
mean ${MEAN_DAEMON_CPU_NORM}% of cpu. Throughput: ${POINTS_PER_SEC} chunks/s.
Ollama (nomic-embed-text) peak ${PEAK_OLLAMA_RSS}KB RSS.
\`\`\`
EOF

echo
echo "=== summary ==="
cat "$SUMMARY"
echo
echo "raw samples: $CSV"
echo "daemon log:  $LOG"
