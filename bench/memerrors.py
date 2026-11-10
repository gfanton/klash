#!/usr/bin/env python3
"""How gnosh's mem calls went: every gnovm_run call in the given runs' gnosh
session stores, and the errors they returned, by kind.

Usage: memerrors.py <run dir>...
"""
import collections
import glob
import json
import os
import sqlite3
import sys

RULES = [
    ("nothing came between your entering", "pop with no work since entering the todo"),
    ("you are in `", "pop or close aimed at another todo than the one it stands in"),
    ("not popped: the check of", "close refused by the close review"),
    ("not closed: the check of", "close refused by the close review"),
    ("is a name, not a task", "a todo name given where a task text is wanted"),
    ("has open todos", "pop of a todo whose own todos are still open"),
    ("name mem not declared", "mem code run without name \"mem\""),
    ("not declared", "undeclared name in the Gno code"),
    ("expected", "Gno syntax error"),
    ("cannot use", "Gno type error"),
    ("no such todo", "unknown todo name"),
    ("not found", "unknown todo name"),
]


def classify(text):
    for key, label in RULES:
        if key in text:
            return label
    return "other: " + text[30:120].replace("\n", " ")


def main():
    calls = 0
    kinds = collections.Counter()
    example = {}
    for d in sys.argv[1:]:
        for db in sorted(glob.glob(os.path.join(d, "data*", "gnosh.db"))):
            c = sqlite3.connect("file:%s?mode=ro" % db, uri=True)
            ids = set()
            try:
                rows = list(c.execute("select parts from messages order by created_at"))
            except sqlite3.Error:
                continue
            for (parts,) in rows:
                for p in json.loads(parts or "[]"):
                    data = p.get("data") or {}
                    if p.get("type") == "tool_call" and data.get("name") == "gnovm_run":
                        ids.add(data.get("id"))
                        calls += 1
                    elif p.get("type") == "tool_result" and data.get("tool_call_id") in ids:
                        text = data.get("content") or ""
                        if text.startswith("ERROR"):
                            k = classify(text)
                            kinds[k] += 1
                            example.setdefault(k, text[:240].replace("\n", " | "))
    errors = sum(kinds.values())
    print("gnovm_run calls: %d, errors: %d (%.0f%%)" % (calls, errors, 100.0 * errors / max(calls, 1)))
    for k, n in kinds.most_common():
        print("%4d  %s" % (n, k))
    others = [v for k, v in example.items() if k.startswith("other")]
    for v in others[:5]:
        print("  e.g. " + v)


if __name__ == "__main__":
    main()
