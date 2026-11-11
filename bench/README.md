# The gnosh bench

How gnosh compares with other coding harnesses when all of them use the
same model, on the same server, on the same tasks. The results are in
[RESULTS.md](RESULTS.md).

## What it measures

Per run: whether the task's own check passed (and its partial score); wall
time; model requests; prompt tokens sent; tokens the server actually had to
read (prompt minus what its prefix cache served); tokens generated; the
peak context; compactions (summaries); and, for gnosh, slides and calls to
mem.

## The pieces

- `proxy.py` sits between a harness and the model server, one per run. It
  pins every request to the server's model, answers `/models` itself so
  every harness is told the same context window, forwards only chat
  completions and responses, and logs one line per request: what was sent,
  what was cached, what was read and generated, and whether it was a
  summary. With `--reasoning-effort` it puts the same effort on every
  request of every harness, as a local server's default would.
- `run.sh` runs one harness on one task once and writes one JSON row.
  Harnesses: `gnosh`, `gnosh-swarm` (several gnosh sessions on one plan,
  below), `crush` (Charm's) and `codex` (OpenAI's Codex CLI). Each run has
  its own home folder, telemetry and update checks off, and no keys.
- `matrix.sh` runs several harnesses side by side, task by task, so any
  load on the server falls on all of them alike.
- `tasks/` holds the tasks, each a repository built by a script, a prompt
  and a check (see [tasks/README.md](tasks/README.md); `tasks/selftest.sh`
  checks that each check fails on the task and passes on a solved copy).
- `rescore.py`, `table.py`, `compare.py` recount rows from each run's own
  logs and make the tables; `analyze.py` counts the tools each model
  called, from each harness's own session store; `memerrors.py` sorts
  gnosh's mem calls that failed.
- `cachetest.py` asks whether a user message in the middle of a turn makes
  the server re-read the turn, for a given model's chat template;
  `reads.py` and `prefixdiff.py` show where a run's uncached reads come from.
- `streams.py` measures the server's total speed with 1 to 8 requests at
  once, which is what a swarm runs on.
- `rounds.py` lists each run's time by round and the median, and
  `reviewab.py` replays a session's close reviews with two versions of
  the review's prompt.

## Running it

Start an OpenAI-compatible server (llama-server, vLLM, …). Put the run's
settings in a file:

```bash
export LANE=qwen-64k UPSTREAM=http://127.0.0.1:8000 MODEL=qwen3.8-27b WINDOW=65536
export GNOSH_BIN=$HOME/bin/gnosh CRUSH_BIN=$HOME/bin/crush CODEX_BIN=$HOME/bin/codex
export GO=/usr/local/go/bin/go TIMEOUT=2400 SERVER_REASONING=medium
```

Then, with it sourced:

```bash
bench/run.sh gnosh algos 1                    # one harness, one task, once
SWARM=3 bench/run.sh gnosh-swarm algos 1      # three gnosh sessions, one plan
bench/matrix.sh settings.env 2 rename algos bugs continue
bench/rescore.py ~/results.jsonl > rows.jsonl && bench/table.py rows.jsonl
```

## The swarm

`gnosh-swarm` starts one gnosh session with the task and a line saying
that N−1 more sessions will help, and asking it to plan the work in mem
with the todos that don't depend on each other under `.Parallel()`. Once
its plan is in mem (`waitplan.py` watches its session store), the other
sessions start, each told to take free todos with `mem.Next()` until none
are left. All of them share one mem, so each todo is taken once, by one
session, under mem's lock, and a session with nothing free waits on mem
instead of polling. Wall time runs from the first session's start until
the last one ends.

The same works by hand: open several terminals in a project, give the task
to gnosh in one of them, and once it has planned, tell the others to help
with the plan in mem.

## Notes on the other harnesses

- Codex 0.167 speaks only the Responses API. It sends its sub-agent tools
  as a `"namespace"` tool that vLLM and llama.cpp reject, so `run.sh` turns
  its sub-agents off; and for a model outside its own catalog it offers no
  patch tool, so on a local server it edits through its shell.
- gnosh and crush discover the model's window from the proxy's `/models`
  answer (`meta.n_ctx`), as they would from llama-server.
