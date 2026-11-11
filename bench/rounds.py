#!/usr/bin/env python3
"""Wall time per round and the median, by task, lane, harness and swarm size, from rescored rows.

Usage: rounds.py rows.jsonl
"""
import collections, json, statistics, sys
groups = collections.defaultdict(list)
for line in open(sys.argv[1]):
    r = json.loads(line)
    key = (r["task"], r["lane"], r["harness"], r.get("swarm") or 1)
    groups[key].append((r.get("rep"), r["wall_s"], r["passed"], r["requests"], r.get("planned_s"), r.get("read_tokens"), r.get("generated")))
for key in sorted(groups):
    rows = groups[key]
    walls = [w for _, w, _, _, _, _, _ in rows]
    print("%-6s %-22s %-12s x%d  rounds %s  median %.0f  passed %d/%d  requests %s  plan %s" % (
        key[0], key[1], key[2], key[3],
        " ".join("r%s=%.0f" % (rep, w) for rep, w, _, _, _, _, _ in rows),
        statistics.median(walls), sum(1 for x in rows if x[2]), len(rows),
        "/".join(str(x[3]) for x in rows),
        "/".join("%s" % (x[4] if x[4] is not None else "-") for x in rows)))
