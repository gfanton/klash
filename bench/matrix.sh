#!/bin/bash
# matrix.sh: run every harness on every task, side by side, rep after rep.
# Usage: matrix.sh <envfile> <reps> <task>...
# The env file sets what run.sh needs (LANE, UPSTREAM, MODEL, the binaries, ...).
# HARNESSES (default "gnosh crush codex") run at once on the same task and rep,
# so any load on the server falls on all of them alike. Rows go to $RESULTS.
set -u
source "$1"; reps=$2; shift 2
BENCH=$(cd "$(dirname "$0")" && pwd)
log=${MATRIX_LOG:-$HOME/matrix-$LANE.log}
for rep in $(seq "${REP0:-1}" "$reps"); do
  for task in "$@"; do
    # MATRIX_GATE, when set, is polled once per pair, so the harnesses start
    # together (a per-run GATE can let one start and then hold the other).
    if [ -n "${MATRIX_GATE:-}" ]; then
      until bash -c "$MATRIX_GATE" >/dev/null 2>&1; do sleep 30; done
    fi
    echo "$(date -u +%FT%TZ) start $task r$rep" >> "$log"
    for h in ${HARNESSES:-gnosh crush codex}; do
      "$BENCH/run.sh" "$h" "$task" "$rep" >> "$log" 2>&1 &
    done
    wait
    echo "$(date -u +%FT%TZ) done $task r$rep" >> "$log"
  done
done
echo "$(date -u +%FT%TZ) MATRIX DONE $LANE" >> "$log"
