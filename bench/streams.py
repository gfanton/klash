#!/usr/bin/env python3
"""Aggregate decode speed of the model server with N streams at once: each
stream asks for 256 tokens (thinking off, a fixed prompt), all start together.

Usage: URL=http://127.0.0.1:8000/v1/chat/completions MODEL=qwen3.8-27b streams.py
"""
import json, os, sys, threading, time, urllib.request
URL = os.environ.get("URL", "http://127.0.0.1:8000/v1/chat/completions")
MODEL = os.environ.get("MODEL", "qwen3.8-27b")
def one(i, out):
    body = {"model": MODEL, "max_tokens": 256, "min_tokens": 256, "temperature": 0.7,
            "messages": [{"role": "user", "content": "Write a long story about a lighthouse keeper, number %d." % i}],
            "chat_template_kwargs": {"enable_thinking": False}}
    t = time.time()
    r = json.load(urllib.request.urlopen(urllib.request.Request(URL, json.dumps(body).encode(), {"Content-Type": "application/json"}), timeout=600))
    out[i] = (r["usage"]["completion_tokens"], time.time() - t)
for n in [1, 2, 3, 4, 6, 8]:
    out = {}
    ts = [threading.Thread(target=one, args=(i, out)) for i in range(n)]
    t0 = time.time()
    [t.start() for t in ts]; [t.join() for t in ts]
    wall = time.time() - t0
    toks = sum(v[0] for v in out.values())
    print("%d stream(s): %4d tokens in %5.1f s -> %5.1f tok/s in all, %4.1f per stream" % (n, toks, wall, toks / wall, toks / wall / n), flush=True)
