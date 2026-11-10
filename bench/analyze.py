#!/usr/bin/env python3
"""What the model did in a bench run: tool calls by name, and for gnosh the
mem statements it ran (pushes, pops, todos, facts) and the messages it popped.

Usage: analyze.py <run dir>...   -> one JSON line per run

Reads the harness's own session store in the run directory:
  gnosh  data/gnosh.db    (messages.parts JSON; messages.popped)
  crush  data/crush.db
  codex  home/.codex/sessions/**/rollout-*.jsonl
"""
import collections
import glob
import json
import os
import re
import sqlite3
import sys

MEM_RE = re.compile(r"\bmem\.([A-Z][A-Za-z]*)\s*\(")


def from_db(path):
    tools = collections.Counter()
    mem = collections.Counter()
    out = {"popped": 0, "summary_messages": 0, "sessions": 0}
    # mode=ro reads a live database's WAL too; immutable=1 is the fallback
    # for a copy without its -shm file.
    c = sqlite3.connect("file:%s?mode=ro" % path, uri=True)
    try:
        cols = [r[1] for r in c.execute("pragma table_info(messages)")]
    except sqlite3.OperationalError:
        c = sqlite3.connect("file:%s?mode=ro&immutable=1" % path, uri=True)
        cols = [r[1] for r in c.execute("pragma table_info(messages)")]
    sel = "parts, is_summary_message" + (", popped" if "popped" in cols else ", 0")
    for parts, is_summary, popped in c.execute("select %s from messages" % sel):
        out["summary_messages"] += 1 if is_summary else 0
        out["popped"] += 1 if popped else 0
        try:
            parts = json.loads(parts)
        except (TypeError, ValueError):
            continue
        for p in parts or []:
            if p.get("type") != "tool_call":
                continue
            d = p.get("data") or {}
            name = d.get("name") or "?"
            tools[name] += 1
            if name == "gnovm_run":
                for verb in MEM_RE.findall(d.get("input") or ""):
                    mem[verb] += 1
    out["sessions"] = c.execute("select count(*) from sessions").fetchone()[0]
    out["tools"] = dict(tools.most_common())
    if mem:
        out["mem"] = dict(mem.most_common())
    return out


def from_codex(home):
    tools = collections.Counter()
    out = {"compactions": 0, "sessions": 0}
    for f in glob.glob(os.path.join(home, ".codex", "sessions", "**", "*.jsonl"), recursive=True):
        out["sessions"] += 1
        for line in open(f, errors="replace"):
            try:
                ev = json.loads(line)
            except ValueError:
                continue
            p = ev.get("payload") or {}
            t = ev.get("type")
            if t == "response_item" and p.get("type") in ("function_call", "custom_tool_call", "local_shell_call"):
                tools[p.get("name") or p.get("type")] += 1
            if t == "compacted" or p.get("type") in ("compacted", "context_compacted"):
                out["compactions"] += 1
    out["tools"] = dict(tools.most_common())
    return out


def gnosh_log(d):
    """Slides gnosh logged, over every data dir of run d."""
    out = {}
    for data in sorted(glob.glob(os.path.join(d, "data*"))):
        merge(out, gnosh_log_dir(data))
    return out


def gnosh_log_dir(data):
    """Slides and compactions gnosh logged in one data dir (logs/gnosh.log)."""
    out = {}
    path = os.path.join(data, "logs", "gnosh.log")
    if not os.path.exists(path):
        return out
    slides = slid_tokens = 0
    for line in open(path, errors="replace"):
        if "Slid the start of a frame" in line:
            slides += 1
            try:
                slid_tokens += json.loads(line).get("tokens") or 0
            except ValueError:
                pass
    out["slides"] = slides
    out["slid_tokens"] = slid_tokens
    return out


def merge(total, part):
    """Adds part's counts into total (a swarm has one store per session)."""
    for k, v in part.items():
        if isinstance(v, dict):
            t = total.setdefault(k, {})
            for kk, vv in v.items():
                t[kk] = t.get(kk, 0) + vv
        elif isinstance(v, (int, float)):
            total[k] = total.get(k, 0) + v
    return total


def analyze(d):
    """What the model did in run d; a swarm's stores (data-1, data-2, ...) add up."""
    res = {"dir": d}
    for name in ("gnosh.db", "crush.db"):
        dbs = sorted(glob.glob(os.path.join(d, "data*", name)))
        if dbs:
            total = {}
            for db in dbs:
                merge(total, from_db(db))
            merge(total, gnosh_log(d))
            res.update(total)
            return res
    if os.path.isdir(os.path.join(d, "home", ".codex")):
        res.update(from_codex(os.path.join(d, "home")))
    return res


if __name__ == "__main__":
    for d in sys.argv[1:]:
        try:
            print(json.dumps(analyze(d.rstrip("/"))))
        except Exception as e:  # one bad run must not stop the rest
            print(json.dumps({"dir": d, "error": repr(e)[:200]}))
