#!/bin/bash
# run.sh: one harness x one task x one repetition -> one JSON row.
# See bench/README.md.
#
# Usage: run.sh <gnosh|gnosh-swarm|crush|codex> <task> <rep>
#
# Environment:
#   LANE       label for the row (qwen-64k, h100-gpt-oss-120b, ...)
#   UPSTREAM   model server base URL, e.g. http://127.0.0.1:8080
#   MODEL      the model id the server knows; the proxy pins every request to it
#   WINDOW     context window told to every harness (default 65536)
#   GATE       optional command, polled every 30 s until it exits 0, before the run
#   GNOSH_BIN, CRUSH_BIN, CODEX_BIN   harness binaries
#   GO         go binary; its directory goes first on PATH
#   TASKS      tasks directory (default: tasks/ next to this script)
#   RUNS       where run directories go (default: ~/runs)
#   RESULTS    JSONL file the row is appended to (default: ~/results.jsonl)
#   TIMEOUT    seconds one harness session may take (default 3600); a session
#              that runs out is killed and the row says timed_out
#   MAX_TOKENS_CAP  optional cap the proxy puts on max_tokens (default 0 = off)
#   REASONING  optional reasoning effort passed to every harness (low|medium|high);
#              gnosh and crush refuse it for a model their discovery does not mark
#              as reasoning, and send the model's default ("medium") themselves
#   CODEX_REASONING  reasoning effort for Codex alone (its model_reasoning_effort)
#   SERVER_REASONING  the proxy puts this reasoning effort on every request of every
#              harness, as a local server's default would be (the fair way to set it)
#   GNOSH_APPEND_FILE, PROMPT_SUFFIX_FILE, VARIANT   gnosh prompt experiments: text
#              added to the end of the system prompt, text added to the end of
#              each user prompt, and the experiment's name for the row
#   DUMP       if set, the proxy saves the first 40 request bodies in requests/
#   CONTINUE_MODE  "new" gives prompt2.txt to a brand-new session instead of the
#              harness's continue flag (what carries over is only what the
#              harness keeps outside the conversation: gnosh's mem, Codex's memories)
#   GNOSH_TEMPLATES  optional dir of prompt templates (coder.md.tpl, ...) copied to
#              gnosh's user template dir, for prompt experiments
#   SWARM      gnosh-swarm only: how many gnosh sessions work the task at once
#              (default 3). The first gets the task and a line saying the others
#              will help; once its plan is in mem (waitplan.py), the others start
#              with SWARM_HELPER_PROMPT. All share the run's HOME, so one mem; each
#              has its own -D (data-1, data-2, ...). Wall time runs from the first
#              session's start until the last one ends.
#
# A run directory holds: home/ (the harness's HOME, config and sessions),
# work/ (the task's repository), data/ (gnosh's and crush's -D), proxy.jsonl
# (one line per model request), session-N.out/.err, check.txt and row.json.
set -u
harness=${1:?harness}; task=${2:?task}; rep=${3:?rep}
BENCH=$(cd "$(dirname "$0")" && pwd)
TASKS=${TASKS:-$BENCH/tasks}
RUNS=${RUNS:-$HOME/runs}
RESULTS=${RESULTS:-$HOME/results.jsonl}
WINDOW=${WINDOW:-65536}
TIMEOUT=${TIMEOUT:-3600}
LANE=${LANE:-unknown}
: "${UPSTREAM:?UPSTREAM}" "${MODEL:?MODEL}"
[ -n "${GO:-}" ] && export PATH="$(dirname "$GO"):$PATH"
tdir=$TASKS/$task
[ -x "$tdir/make.sh" ] && [ -x "$tdir/check.sh" ] && [ -f "$tdir/prompt.txt" ] || { echo "run.sh: no task $task in $TASKS" >&2; exit 2; }

if [ -n "${GATE:-}" ]; then
  until bash -c "$GATE" >/dev/null 2>&1; do sleep 30; done
fi

id=$(date -u +%Y%m%dT%H%M%S)-$LANE-$harness-$task-r$rep-$$
dir=$RUNS/$id
mkdir -p "$dir/home" "$dir/data"
work=$dir/work
"$tdir/make.sh" "$work" > "$dir/make.log" 2>&1 || { echo "run.sh: make.sh failed, see $dir/make.log" >&2; exit 2; }

# The proxy: one per run, its own port, its own log.
python3 "$BENCH/proxy.py" serve --listen 127.0.0.1:0 --port-file "$dir/port" \
  --upstream "$UPSTREAM" --model "$MODEL" --window "$WINDOW" \
  --max-tokens-cap "${MAX_TOKENS_CAP:-0}" ${DUMP:+--dump "$dir/requests"} \
  ${SERVER_REASONING:+--reasoning-effort "$SERVER_REASONING"} \
  --log "$dir/proxy.jsonl" > "$dir/proxy.err" 2>&1 &
proxy=$!
trap 'kill $proxy 2>/dev/null' EXIT
for _ in $(seq 100); do [ -s "$dir/port" ] && break; sleep 0.1; done
port=$(cat "$dir/port")

# A clean environment: the run's own HOME and XDG dirs, telemetry and update
# checks off, no keys of anyone's.
H=$dir/home
runenv=(env -i PATH="$PATH" HOME="$H" USER="$(id -un)" LANG=C.UTF-8 TERM=dumb
  XDG_CONFIG_HOME="$H/.config" XDG_DATA_HOME="$H/.local/share" XDG_CACHE_HOME="$H/.cache"
  XDG_STATE_HOME="$H/.local/state" GOPATH="$H/go" GOCACHE="$H/.cache/go-build" GOFLAGS=-mod=mod
  GOTOOLCHAIN=local DO_NOT_TRACK=1
  CRUSH_DISABLE_METRICS=1 CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1
  GNOSH_DISABLE_METRICS=1 GNOSH_DISABLE_PROVIDER_AUTO_UPDATE=1
  GNOSH_DISABLE_DEFAULT_PROVIDERS=1 GNOSH_DISABLE_UPDATE_CHECK=1)
reason=()
case $harness in
  gnosh|gnosh-swarm)
    mkdir -p "$H/.config/gnosh"
    printf 'provider add box --type llamacpp --base-url "http://127.0.0.1:%s"\n' "$port" > "$H/.config/gnosh/gnoshrc"
    if [ -n "${GNOSH_TEMPLATES:-}" ]; then  # experiments: the user's own prompt templates
      mkdir -p "$H/.config/gnosh/templates" && cp "$GNOSH_TEMPLATES"/* "$H/.config/gnosh/templates/"
    fi
    [ -n "${REASONING:-}" ] && reason=(--reasoning-effort "$REASONING")
    # Prompt experiments (gnosh only): GNOSH_APPEND_FILE is added to the end of
    # the system prompt, PROMPT_SUFFIX_FILE to the end of each user prompt, and
    # VARIANT names the experiment in the row.
    append=(); [ -n "${GNOSH_APPEND_FILE:-}" ] && append=(--append-system-prompt-file "$GNOSH_APPEND_FILE")
    suffix=""; [ -n "${PROMPT_SUFFIX_FILE:-}" ] && suffix=$(cat "$PROMPT_SUFFIX_FILE")
    version=$("$GNOSH_BIN" --version 2>&1 | head -1)${GNOSH_TEMPLATES:+ +templates $(basename "$GNOSH_TEMPLATES")}${VARIANT:+ +variant $VARIANT}
    session() { local cont=(); [ "$1" -gt 1 ] && [ "${CONTINUE_MODE:-}" != new ] && cont=(--continue)
      "${runenv[@]}" timeout -k 30 "$TIMEOUT" "$GNOSH_BIN" run -q -y -c "$work" -D "$dir/data" \
      -m "box/$MODEL" --small-model "box/$MODEL" "${reason[@]}" "${append[@]}" "${cont[@]}" "$2$suffix" < /dev/null; }
    # swarm_session <data dir> <prompt>: one session of a swarm, in its own -D.
    swarm_session() { "${runenv[@]}" timeout -k 30 "$TIMEOUT" "$GNOSH_BIN" run -q -y -c "$work" -D "$1" \
      -m "box/$MODEL" --small-model "box/$MODEL" "${reason[@]}" "${append[@]}" "$2" < /dev/null; }
    [ "$harness" = gnosh-swarm ] && version="$version +swarm ${SWARM:-3}" ;;
  crush)
    mkdir -p "$H/.config/crush"
    printf 'provider add box --type llamacpp --base-url "http://127.0.0.1:%s"\n' "$port" > "$H/.config/crush/crushrc"
    [ -n "${REASONING:-}" ] && reason=(--reasoning-effort "$REASONING")
    version=$("$CRUSH_BIN" --version 2>&1 | head -1)
    session() { local cont=(); [ "$1" -gt 1 ] && [ "${CONTINUE_MODE:-}" != new ] && cont=(--continue)
      "${runenv[@]}" timeout -k 30 "$TIMEOUT" "$CRUSH_BIN" run -q -y -c "$work" -D "$dir/data" \
      -m "box/$MODEL" --small-model "box/$MODEL" "${reason[@]}" "${cont[@]}" "$2" < /dev/null; } ;;
  codex)
    mkdir -p "$H/.codex"
    reason_line=""
    r=${CODEX_REASONING:-${REASONING:-}}
    [ -n "$r" ] && reason_line="model_reasoning_effort = \"$r\""
    cat > "$H/.codex/config.toml" <<EOF
model = "$MODEL"
model_provider = "bench"
model_context_window = $WINDOW
approval_policy = "never"
sandbox_mode = "danger-full-access"
check_for_update_on_startup = false
web_search = "disabled"
$reason_line

[analytics]
enabled = false

# Codex groups its sub-agent tools in a "namespace" tool, which vLLM's and
# llama.cpp's Responses endpoints reject ("tool type 'namespace' is not
# supported"), so sub-agents are off for Codex.
[features]
multi_agent = false

[model_providers.bench]
name = "bench"
base_url = "http://127.0.0.1:$port/v1"
wire_api = "responses"
request_max_retries = 2
stream_max_retries = 2
stream_idle_timeout_ms = 1800000
EOF
    version=$("$CODEX_BIN" --version 2>&1 | head -1)
    session() { local cont=(); [ "$1" -gt 1 ] && [ "${CONTINUE_MODE:-}" != new ] && cont=(resume --last)
      "${runenv[@]}" CODEX_HOME="$H/.codex" timeout -k 30 "$TIMEOUT" "$CODEX_BIN" exec \
      --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox -C "$work" "${cont[@]}" "$2" < /dev/null; } ;;
  *) echo "run.sh: unknown harness $harness" >&2; exit 2 ;;
esac

# Sessions: prompt.txt, then prompt2.txt if the task has one, given with the
# harness's continue flag in the same work dir and HOME. Between the two, the
# check runs once and its line is kept as check1.
runenv+=(GO="${GO:-go}")
t0=$(date +%s.%N); exits=(); timed_out=false; n=0; check1=""; planned_s=""
if [ "$harness" = gnosh-swarm ]; then
  N=${SWARM:-3}
  [ -f "$tdir/prompt2.txt" ] && { echo "run.sh: gnosh-swarm runs one-prompt tasks only" >&2; exit 2; }
  note=${SWARM_PLANNER_NOTE:-"$((N-1)) more gnosh sessions in this project share your mem and will work on this with you. First plan it in mem, with the todos that do not depend on each other under .Parallel(); then take todos with mem.Next(), as they will."}
  helper=${SWARM_HELPER_PROMPT:-"Another gnosh session in this project has planned a task in mem, and you are here to help with it. Take the next free todo with mem.Next(), do its work, pop it with what came of it, and keep taking todos until mem.Next() has nothing left for you. Do not plan the task again."}
  pids=()
  mkdir -p "$dir/data-1"
  { swarm_session "$dir/data-1" "$(cat "$tdir/prompt.txt")

$note" > "$dir/session-1.out" 2> "$dir/session-1.err"; echo $? > "$dir/exit-1"; } &
  pids+=($!)
  python3 "$BENCH/waitplan.py" "$dir/data-1" "$TIMEOUT" "${pids[0]}" && planned_s=$(python3 -c "import time; print(round(time.time()-$t0,1))")
  for i in $(seq 2 "$N"); do
    mkdir -p "$dir/data-$i"
    { swarm_session "$dir/data-$i" "$helper" > "$dir/session-$i.out" 2> "$dir/session-$i.err"; echo $? > "$dir/exit-$i"; } &
    pids+=($!)
  done
  wait "${pids[@]}"
  for i in $(seq 1 "$N"); do
    rc=$(cat "$dir/exit-$i" 2>/dev/null || echo 255); exits+=("$rc")
    { [ "$rc" = 124 ] || [ "$rc" = 137 ]; } && timed_out=true
  done
fi
for p in "$tdir/prompt.txt" "$tdir/prompt2.txt"; do
  [ "$harness" = gnosh-swarm ] && break
  [ -f "$p" ] || continue
  n=$((n+1))
  if [ "$n" -gt 1 ]; then
    check1=$("${runenv[@]}" "$tdir/check.sh" "$work" 2>&1 | tail -1)
  fi
  session "$n" "$(cat "$p")" > "$dir/session-$n.out" 2> "$dir/session-$n.err"
  rc=$?
  exits+=("$rc")
  { [ "$rc" = 124 ] || [ "$rc" = 137 ]; } && timed_out=true
done
t1=$(date +%s.%N)

"${runenv[@]}" "$tdir/check.sh" "$work" > "$dir/check.txt" 2>&1; passed_rc=$?
check_msg=$(tail -1 "$dir/check.txt")
kill $proxy 2>/dev/null; wait $proxy 2>/dev/null
summary=$(python3 "$BENCH/proxy.py" summarize "$dir/proxy.jsonl")

python3 - "$dir" "$LANE" "$MODEL" "$WINDOW" "$harness" "$version" "$task" "$rep" "$passed_rc" "$check_msg" \
  "$t0" "$t1" "$timed_out" "$summary" "${exits[*]}" "$check1" "${VARIANT:-}" "${N:-}" "$planned_s" <<'PY' | tee "$dir/row.json" >> "$RESULTS"
import json, sys
(d, lane, model, window, harness, version, task, rep, rc, msg, t0, t1, to, summ, exits, check1, variant, swarm, planned) = sys.argv[1:20]
row = {"lane": lane, "model": model, "window": int(window), "harness": harness, "version": version,
       "task": task, "rep": int(rep), "passed": rc == "0", "check_msg": msg,
       "wall_s": round(float(t1) - float(t0), 1), "timed_out": to == "true",
       "exit_codes": [int(x) for x in exits.split()], "dir": d}
if check1:
    row["check1"] = check1
if variant:
    row["variant"] = variant
if harness == "gnosh-swarm":
    row["swarm"] = int(swarm)
    row["planned_s"] = float(planned) if planned else None
row.update(json.loads(summ))
print(json.dumps(row))
PY
