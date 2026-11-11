#!/usr/bin/env python3
"""Replays a gnosh session's close reviews with two versions of the review's
prompt, to compare them on real closes.

For each todo the session closed with mem.Pop, it rebuilds what gnosh's
closePrompt asks the review (internal/agent/close_check.go): the todo, the
user's prompt, the files changed and commands run, the result, and what
happened from the call that entered the todo to the call that closed it.
It asks the model with each system prompt and its question: "old" ends with
"Does what happened in the todo show the result?", "new" names the todo and
asks first whether the result is its own. Each request is sent as gnosh
sends the review: " /no_think" after the system prompt, thinking off in
chat_template_kwargs, max_tokens 400.

Usage: reviewab.py <gnosh.db> <old close_check.md> <new close_check.md> <url> <model> [samples]
Environment: TEMP (default 0), ONLY (a todo title to keep), MAXTOK (400),
DEBUG (show finish reason and reasoning for every answer).
"""
import json, re, sqlite3, sys, urllib.request

db, oldfile, newfile, url, model = sys.argv[1:6]
samples = int(sys.argv[6]) if len(sys.argv) > 6 else 2
SYSTEMS = {"old": open(oldfile).read() + "\n /no_think", "new": open(newfile).read() + "\n /no_think"}
OLD = "Does what happened in the todo show the result?"
NEW = "Is the result what came of this todo, %s, and not of other work; and does what happened in the todo show it?"

c = sqlite3.connect("file:" + db + "?mode=ro", uri=True)
rows = []  # (kind, payload)
USER = ""
for (role, parts) in c.execute("select role, parts from messages order by created_at"):
    for p in json.loads(parts):
        d = p.get("data") or {}
        t = p.get("type")
        if role == "user" and t == "text" and not USER:
            USER = d.get("text", "").strip()
        if role == "assistant" and t == "text" and d.get("text", "").strip():
            rows.append(("say", d["text"].strip()))
        elif t == "tool_call":
            rows.append(("call", d))
        elif t == "tool_result":
            rows.append(("result", d))

def clip(s, n):
    return s if len(s) <= n else s[:n] + "…"

def transcript(window):
    out = []
    for kind, x in window:
        if kind == "say":
            out.append("Agent: " + clip(x, 4000))
        elif kind == "call":
            out.append("Agent called %s: %s" % (x.get("name"), clip(x.get("input") or "", 2000)))
        else:
            verb = " failed: " if x.get("is_error") else " returned: "
            out.append((x.get("name") or "tool") + verb + clip(x.get("content") or "", 2000))
    return "\n\n".join(out)[-24000:]

calls = {}
for i, (kind, x) in enumerate(rows):
    if kind == "call":
        calls[x.get("id")] = (i, x)
start = next(i for i, (k, x) in enumerate(rows) if k == "call" and x.get("name") == "gnovm_run" and "mem.Next()" in (x.get("input") or ""))
entry_call = {}
closes = []
for i, (kind, x) in enumerate(rows):
    if kind != "result":
        continue
    content = x.get("content") or ""
    mm = re.match(r"in `([^`]+)`", content)
    if mm and x.get("tool_call_id") in calls:
        entry_call[mm.group(1)] = calls[x["tool_call_id"]][0]
    md = re.match(r'done `([^`]+)` "([^"]+)" → ', content)
    if md and md.group(1) in entry_call and x.get("tool_call_id") in calls:
        ci, call = calls[x["tool_call_id"]]
        code = json.loads(call.get("input") or "{}").get("code", "")
        r = re.search(r'mem\.Pop\("((?:[^"\\]|\\.)*)"\)', code)
        if r:
            # the window: from the call that entered the todo to the call that closes it
            closes.append((md.group(2), json.loads('"' + r.group(1) + '"'), rows[entry_call[md.group(1)]:ci]))

def files_and_commands(window):
    edited, ran = [], []
    res = {x.get("tool_call_id"): x for k, x in window if k == "result"}
    for k, x in window:
        if k != "call":
            continue
        inp = json.loads(x.get("input") or "{}")
        r = res.get(x.get("id"), {})
        if x.get("name") in ("edit", "write", "multiedit") and not r.get("is_error"):
            f = re.sub(r".*/work/", "", inp.get("file_path", ""))
            if f and f not in edited:
                edited.append(f)
        if x.get("name") == "bash":
            code = 0 if not r.get("is_error") else 1
            ran.append("`%s` → exit %d" % (inp.get("command", ""), code))
    return edited, ran

def ask(system, user):
    body = {"model": model, "temperature": float(__import__("os").environ.get("TEMP", "0")), "max_tokens": int(__import__("os").environ.get("MAXTOK", "400")), "reasoning_effort": "medium",
            "chat_template_kwargs": {"reasoning_effort": "medium", "enable_thinking": False},
            "messages": [{"role": "system", "content": system}, {"role": "user", "content": user}]}
    req = urllib.request.Request(url, json.dumps(body).encode(), {"Content-Type": "application/json"})
    r = json.load(urllib.request.urlopen(req, timeout=300))
    ch = r["choices"][0]
    content = (ch["message"].get("content") or "").strip()
    if not content or __import__("os").environ.get("DEBUG"):
        rc = ch["message"].get("reasoning_content") or ch["message"].get("reasoning") or ""
        content += "  [finish=%s, completion_tokens=%s, reasoning %d chars: %r]" % (ch.get("finish_reason"), r["usage"].get("completion_tokens"), len(rc), rc[-160:])
    return content, r["usage"]["prompt_tokens"]

ONLY = __import__("os").environ.get("ONLY", "")
for title, result, window in closes:
    if ONLY and ONLY not in title:
        continue
    edited, ran = files_and_commands(window)
    base = ("The todo: %s\n\nThoughts kept on it: (none)\n\n" % title +
            ("What the user asked in this turn: %s\n\n" % clip(USER, 2000) if USER else "") +
            "Files the agent's edits and writes changed in the todo: %s\n" % (", ".join(edited) or "none") +
            "Commands it ran in the todo: %s\n" % ("; ".join(ran) or "none") +
            "\nThe result it is being closed with (the agent's claim): %s\n\n" % result +
            "What happened in the todo, from when its plan was made (its work may have been done before the agent entered it, which is fine):\n%s\n\n" % transcript(window))
    print("==== %s  ← %s" % (title, clip(result, 90)))
    for name, q in (("old", OLD), ("new", NEW % json.dumps(title))):
        for s in range(samples):
            out, pt = ask(SYSTEMS[name], base + q)
            print("  %s #%d (%d tokens in): %s" % (name, s + 1, pt, clip(out.replace("\n", " "), 260)))
