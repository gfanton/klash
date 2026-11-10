#!/usr/bin/env python3
"""Where a run's uncached reads come from: main-agent vs side requests, and
the main agent's big re-reads (cache misses after a slide, pop or compaction)."""
import collections, json, sys
for d in sys.argv[1:]:
    rows = [json.loads(l) for l in open(d + "/proxy.jsonl") if l.strip()]
    rows = [r for r in rows if r.get("method") != "GET"]
    if not rows:
        continue
    main = collections.Counter(r.get("sys_sha") for r in rows).most_common(1)[0][0]
    tot = collections.Counter()
    big = []
    prev_ctx = 0
    for r in rows:
        rd = r.get("read") or 0
        if r.get("sys_sha") != main:
            tot["side requests (titles, naming, close checks, goal checks)"] += rd
        elif r.get("kind") == "summary" or "detailed summary" in (r.get("user_head") or ""):
            tot["summary requests"] += rd
        elif rd > 3000 and r is not rows[0]:
            tot["main: big re-reads (>3K, cache lost)"] += rd
            big.append((rd, r.get("prompt_tokens"), (r.get("user_head") or "")[:50].replace("\n", " ")))
        elif r is rows[0]:
            tot["main: first request"] += rd
        else:
            tot["main: ordinary new tokens"] += rd
    print("==", d.split("/")[-1][-46:], "total read", sum(tot.values()))
    for k, v in tot.most_common():
        print("   %7d  %s" % (v, k))
    for b in sorted(big, reverse=True)[:4]:
        print("      big re-read %6d of %6d  user: %s" % b)
