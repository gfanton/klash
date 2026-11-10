#!/usr/bin/env python3
"""compare.py RESULTS.jsonl... : one Markdown table per lane, a row per task
and harness. Each row shows medians over the runs and how many runs passed.
Under each task there is a line comparing gnosh with every other harness: the
ratio of gnosh's median to theirs (below 1 means gnosh used less).

Standard library only. Rows come from bench/run.sh.
"""
import json
import statistics
import sys
from collections import defaultdict

METRICS = [  # (row key, column title, scale, format)
    ("wall_s", "wall", 1, "time"),
    ("requests", "requests", 1, "int"),
    ("prompt_tokens", "prompt K", 1e-3, "f1"),
    ("read_tokens", "read K", 1e-3, "f1"),
    ("generated", "gen K", 1e-3, "f1"),
    ("gpu_ms", "GPU s", 1e-3, "int"),
    ("peak_context", "peak K", 1e-3, "f1"),
    ("summaries", "summaries", 1, "f1"),
]


def fmt(v, kind):
    if v is None:
        return "-"
    if kind == "time":
        v = int(round(v))
        return "%d:%02d" % (v // 60, v % 60)
    if kind == "int":
        return str(int(round(v)))
    return "%.1f" % v


def med(rows, key):
    vals = [r.get(key) for r in rows if isinstance(r.get(key), (int, float))]
    return statistics.median(vals) if vals else None


def main(paths):
    rows = []
    for p in paths:
        with open(p) as f:
            for line in f:
                line = line.strip()
                if line:
                    try:
                        rows.append(json.loads(line))
                    except ValueError:
                        pass
    by = defaultdict(list)
    for r in rows:
        by[(r.get("lane"), r.get("task"), r.get("harness"))].append(r)
    lanes = sorted({k[0] for k in by})
    for lane in lanes:
        print("### %s\n" % lane)
        head = ["task", "harness", "passed"] + [m[1] for m in METRICS]
        print("| " + " | ".join(head) + " |")
        print("|" + "---|" * len(head))
        tasks = sorted({k[1] for k in by if k[0] == lane})
        notes = []
        for task in tasks:
            harnesses = sorted({k[2] for k in by if k[0] == lane and k[1] == task},
                               key=lambda h: (h != "gnosh", h))
            meds = {}
            for h in harnesses:
                rs = by[(lane, task, h)]
                passed = sum(1 for r in rs if r.get("passed"))
                meds[h] = {m[0]: med(rs, m[0]) for m in METRICS}
                cells = [task, h, "%d/%d" % (passed, len(rs))]
                cells += [fmt(None if meds[h][k] is None else meds[h][k] * s, kind) for k, _, s, kind in METRICS]
                print("| " + " | ".join(cells) + " |")
            if "gnosh" in meds:
                for h in harnesses:
                    if h == "gnosh":
                        continue
                    parts = []
                    for k, title, _, _ in METRICS:
                        a, b = meds["gnosh"][k], meds[h][k]
                        if a is not None and b:
                            parts.append("%s %.2f" % (title, a / b))
                    notes.append("%s, gnosh/%s: %s" % (task, h, ", ".join(parts)))
        print()
        for n in notes:
            print("- " + n)
        print()


if __name__ == "__main__":
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    main(sys.argv[1:])
