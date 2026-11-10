#!/usr/bin/env python3
"""Does appending a user message mid-turn break the server's prefix cache?
Builds one turn (user task, then assistant steps with reasoning and tool
calls, each followed by a tool result), sends it, then sends it again with
(a) one more step appended, (b) a user-role reminder appended, (c) the same
reminder appended to the last tool result instead. Prints cached tokens."""
import json, sys, urllib.request
import os
URL = os.environ.get("URL", "http://127.0.0.1:8000/v1/chat/completions")
MODEL = sys.argv[1]
def post(msgs):
    body = {"model": MODEL, "messages": msgs, "max_tokens": 1, "temperature": 0,
            "tools": [{"type": "function", "function": {"name": "view", "description": "view a file",
                       "parameters": {"type": "object", "properties": {"path": {"type": "string"}}}}}]}
    req = urllib.request.Request(URL, json.dumps(body).encode(), {"Content-Type": "application/json"})
    r = json.load(urllib.request.urlopen(req, timeout=900))
    u = r["usage"]
    cached = (u.get("prompt_tokens_details") or {}).get("cached_tokens")
    if cached is None:  # llama-server reports its prefix cache in timings
        cached = (r.get("timings") or {}).get("cache_n") or 0
    return u["prompt_tokens"], cached
filler = " ".join("line %d of the file says nothing much" % i for i in range(300))
msgs = [{"role": "system", "content": "You are a coding agent. " * 50},
        {"role": "user", "content": "Fix the bug in main.go. Unique run marker: %s" % sys.argv[2]}]
for i in range(6):
    msgs.append({"role": "assistant", "content": "", "reasoning_content": "I should look at file %d next because %s" % (i, "the reason is long " * 40),
                 "tool_calls": [{"id": "call_%d" % i, "type": "function", "function": {"name": "view", "arguments": json.dumps({"path": "f%d.go" % i})}}]})
    msgs.append({"role": "tool", "tool_call_id": "call_%d" % i, "content": filler})
p0 = post(msgs)
step = [{"role": "assistant", "content": "", "reasoning_content": "Next file. " * 30,
         "tool_calls": [{"id": "call_x", "type": "function", "function": {"name": "view", "arguments": "{\"path\": \"x.go\"}"}}]},
        {"role": "tool", "tool_call_id": "call_x", "content": "package x"}]
rem = "<system_reminder>mem hasn't been used recently. This is just a gentle reminder.</system_reminder>"
a = post(msgs + step)
b = post(msgs + step + [{"role": "user", "content": rem}])
c_msgs = msgs + [step[0], dict(step[1], content=step[1]["content"] + "\n\n" + rem)]
post(msgs + step)  # re-warm
c = post(c_msgs)
print("model %s: base prompt %s (cached %s)" % (MODEL, p0[0], p0[1]))
print("  (a) one more step appended:          prompt %6d cached %6d -> read %6d" % (a[0], a[1], a[0] - a[1]))
print("  (b) + reminder as a new user message: prompt %6d cached %6d -> read %6d" % (b[0], b[1], b[0] - b[1]))
print("  (c) + reminder inside the tool result: prompt %6d cached %6d -> read %6d" % (c[0], c[1], c[0] - c[1]))
