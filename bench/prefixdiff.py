#!/usr/bin/env python3
"""For consecutive dumped requests of one run: where the message list stops
being a prefix of the previous one (the first changed message), and what
changed there. A request whose messages are the previous ones plus new ones
at the end keeps the server's prefix cache; any earlier change breaks it."""
import glob, json, sys
d = sys.argv[1]
files = sorted(glob.glob(d + "/requests/req-*.json"))
prev = None
for f in files:
    b = json.load(open(f))
    msgs = b.get("messages") or []
    sysm = msgs[0]["content"] if msgs and msgs[0].get("role") == "system" else None
    if prev is not None and len(prev) > 3 and sysm == prev_sys:
        n = min(len(prev), len(msgs))
        first = next((i for i in range(n) if json.dumps(prev[i], sort_keys=True) != json.dumps(msgs[i], sort_keys=True)), None)
        if first is not None:
            a, c = prev[first], msgs[first]
            def short(m):
                keys = {k: (v if not isinstance(v, str) else v[:120]) for k, v in m.items() if k not in ("tool_calls",)}
                return json.dumps(keys)[:300]
            print("%s: msgs %d -> %d, first change at message %d (%s)" % (f[-12:], len(prev), len(msgs), first, a.get("role")))
            print("    before:", short(a))
            print("    after: ", short(c))
            diffkeys = sorted(k for k in set(a) | set(c) if json.dumps(a.get(k), sort_keys=True) != json.dumps(c.get(k), sort_keys=True))
            print("    keys that differ:", diffkeys)
    prev, prev_sys = msgs, sysm
