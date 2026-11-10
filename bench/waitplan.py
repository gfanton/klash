#!/usr/bin/env python3
"""Waits until a gnosh session has made a plan in mem: a gnovm_run call on
mem whose code plans todos (mem.Todo) and whose result is not an error.

Usage: waitplan.py <data dir> [timeout seconds] [pid]
  exit 0 = planned; 1 = timed out, or process pid (the planning session) ended first

Reads the session store (<data dir>/gnosh.db) read-only while gnosh writes
it, every second.
"""
import json
import os
import sqlite3
import sys
import time


def planned(db):
    try:
        c = sqlite3.connect("file:%s?mode=ro" % db, uri=True, timeout=1)
        rows = c.execute("select parts from messages order by created_at").fetchall()
        c.close()
    except sqlite3.Error:
        return False
    calls = {}
    for (parts,) in rows:
        try:
            parts = json.loads(parts or "[]")
        except ValueError:
            continue
        for p in parts:
            d = p.get("data") or {}
            if p.get("type") == "tool_call" and d.get("name") == "gnovm_run":
                try:
                    inp = json.loads(d.get("input") or "{}")
                except ValueError:
                    inp = {}
                if inp.get("name") == "mem" and "mem.Todo(" in (inp.get("code") or ""):
                    calls[d.get("id")] = True
            elif p.get("type") == "tool_result" and calls.get(d.get("tool_call_id")):
                if not (d.get("content") or "").startswith("ERROR"):
                    return True
    return False


def alive(pid):
    try:
        os.kill(pid, 0)
        return True
    except ProcessLookupError:
        return False
    except PermissionError:
        return True


def main():
    db = os.path.join(sys.argv[1], "gnosh.db")
    deadline = time.time() + (float(sys.argv[2]) if len(sys.argv) > 2 else 900)
    pid = int(sys.argv[3]) if len(sys.argv) > 3 else 0
    while time.time() < deadline:
        if os.path.exists(db) and planned(db):
            return 0
        if pid and not alive(pid):
            return 1
        time.sleep(1)
    return 1


if __name__ == "__main__":
    sys.exit(main())
