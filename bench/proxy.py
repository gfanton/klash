#!/usr/bin/env python3
"""Metering proxy for the gnosh harness bench (bench/README.md).

One proxy per run, between a harness and the model server (llama-server on
llama-server or vLLM, say). It:

- pins the model: "model" in every POST body becomes --model;
- answers GET /models and /v1/models itself, with the pinned model only and a
  llama.cpp-style meta block (n_ctx = --window), so every harness that reads
  it is told the same context window;
- forwards only chat completions and responses (with or without /v1);
  anything else gets a 404, so nothing can load, unload or reconfigure
  models on the server;
- asks for usage on streams (stream_options.include_usage);
- writes one JSON line per request: what was sent and what it cost.

Usage:
  proxy.py serve --listen 127.0.0.1:0 --port-file run/port \
      --upstream http://127.0.0.1:8080 --model qwen3.8-27b --window 65536 \
      --log run/proxy.jsonl [--max-tokens-cap N]
  proxy.py summarize run/proxy.jsonl [--window 65536]   -> one JSON object

Timings: llama-server reports prompt_ms and predicted_ms (its "timings"
block), which is GPU time per request. vLLM reports none, so for vLLM the
proxy's own clock stands in: prompt_ms = time to the first streamed token
(queueing plus prefill), predicted_ms = the rest (decode). Each log line
says which ("timing": "server" or "proxy").

Python 3.8+, standard library only.
"""

import argparse
import hashlib
import http.client
import json
import os
import re
import socket
import sys
import threading
import time
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

FORWARD = {
    "/v1/chat/completions": "/v1/chat/completions",
    "/chat/completions": "/v1/chat/completions",
    "/v1/responses": "/v1/responses",
    "/responses": "/v1/responses",
}
MODELS_PATHS = {"/models", "/v1/models"}
# The summary (compaction) prompts, as each harness sends them as the newest
# user message: crush "Provide a detailed summary of our conversation
# above.", gnosh summary.md "... create a detailed summary of the
# conversation so far", Codex "... CONTEXT CHECKPOINT COMPACTION ... handoff
# summary for another LLM". Matching "summar" alone is wrong: gnosh's
# "Continue the conversation from where it left off" turn and its close
# checks mention summaries too.
SUMMARY_RE = re.compile(r"detailed summary of (our|the) conversation|CONTEXT CHECKPOINT COMPACTION|handoff summary for another LLM", re.I)
TITLE_RE = re.compile(r"\btitle\b", re.I)


def kind_of(system, last_user):
    if SUMMARY_RE.search(last_user[:600]):
        return "summary"
    if TITLE_RE.search(system[:300]):
        return "title"
    return ""
UPSTREAM_TIMEOUT = 3600  # seconds a single read from the server may take


def text_of(content):
    """The plain text of a message's content (a string or a list of parts)."""
    if content is None:
        return ""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        out = []
        for part in content:
            if isinstance(part, str):
                out.append(part)
            elif isinstance(part, dict):
                for key in ("text", "input_text", "output_text", "content"):
                    v = part.get(key)
                    if isinstance(v, str):
                        out.append(v)
                        break
        return "\n".join(out)
    if isinstance(content, dict):
        return text_of(content.get("text") or content.get("content"))
    return str(content)


def describe_request(body):
    """Fields that classify a request: messages, system and last user text."""
    if "messages" in body:  # chat completions
        msgs = body.get("messages") or []
        system = "\n".join(text_of(m.get("content")) for m in msgs
                           if isinstance(m, dict) and m.get("role") in ("system", "developer"))
        users = [m for m in msgs if isinstance(m, dict) and m.get("role") == "user"]
        last_user = text_of(users[-1].get("content")) if users else ""
        n = len(msgs)
        chars = len(json.dumps(msgs, ensure_ascii=False))
    else:  # responses
        items = body.get("input")
        if isinstance(items, str):
            items = [{"role": "user", "content": items}]
        items = items or []
        system = text_of(body.get("instructions")) + "\n".join(
            text_of(i.get("content")) for i in items
            if isinstance(i, dict) and i.get("role") in ("system", "developer"))
        users = [i for i in items if isinstance(i, dict) and i.get("role") == "user"]
        last_user = text_of(users[-1].get("content")) if users else ""
        n = len(items)
        chars = len(json.dumps(items, ensure_ascii=False)) + len(text_of(body.get("instructions")))
    tools = body.get("tools") or []
    # A summary request is one whose newest user message asks for a summary
    # or a compaction (gnosh and crush send summary.md as the user prompt
    # under the agent's own system prompt; Codex sends its compaction
    # prompt). The system prompts of all three mention summaries, so the
    # system message alone says nothing.
    kind = kind_of(system, last_user)
    return {
        "n_messages": n,
        "chars": chars,
        "tools": len(tools),
        "tools_chars": len(json.dumps(tools, ensure_ascii=False)) if tools else 0,
        "sys_sha": hashlib.sha1(system.encode("utf-8", "replace")).hexdigest()[:8],
        "sys_head": system[:100],
        "user_head": last_user[:100],
        "summary": kind == "summary",
        "kind": kind,
        "stream": bool(body.get("stream")),
        "max_tokens": body.get("max_tokens") or body.get("max_completion_tokens") or body.get("max_output_tokens"),
    }


class Meter:
    """Accumulates what one response reports: usage, timings, tool calls."""

    def __init__(self):
        self.usage = None
        self.timings = None
        self.finish = None
        self.tool_ids = set()
        self.first_token = None
        self.error = None

    def saw_token(self):
        if self.first_token is None:
            self.first_token = time.time()

    def chat_obj(self, obj):
        if not isinstance(obj, dict):
            return
        if obj.get("usage"):
            self.usage = obj["usage"]
        if obj.get("timings"):
            self.timings = obj["timings"]
        if obj.get("error"):
            self.error = str(obj["error"])[:300]
        for ch in obj.get("choices") or []:
            if ch.get("finish_reason"):
                self.finish = ch["finish_reason"]
            d = ch.get("delta") or ch.get("message") or {}
            if d.get("content") or d.get("reasoning_content") or d.get("reasoning") or d.get("tool_calls"):
                self.saw_token()
            for i, tc in enumerate(d.get("tool_calls") or []):
                key = tc.get("index", i) if "delta" in ch else i
                self.tool_ids.add(key)

    def responses_obj(self, obj):
        if not isinstance(obj, dict):
            return
        typ = obj.get("type") or ""
        if typ.endswith(".delta"):
            self.saw_token()
        if typ == "error" or obj.get("error"):
            self.error = str(obj.get("error") or obj.get("message"))[:300]
        resp = obj.get("response") if typ.startswith("response.") else obj
        if isinstance(resp, dict):
            if resp.get("usage"):
                self.usage = resp["usage"]
            if resp.get("status") and typ in ("response.completed", "response.incomplete", "response.failed", ""):
                self.finish = resp.get("status")
                if (resp.get("incomplete_details") or {}).get("reason"):
                    self.finish = resp["incomplete_details"]["reason"]
            for item in resp.get("output") or []:
                if isinstance(item, dict) and item.get("type") == "function_call":
                    self.tool_ids.add(item.get("call_id") or item.get("id") or len(self.tool_ids))
        item = obj.get("item")
        if typ == "response.output_item.done" and isinstance(item, dict) and item.get("type") == "function_call":
            self.tool_ids.add(item.get("call_id") or item.get("id") or len(self.tool_ids))

    def numbers(self):
        u = self.usage or {}
        prompt = u.get("prompt_tokens", u.get("input_tokens"))
        gen = u.get("completion_tokens", u.get("output_tokens"))
        details = u.get("prompt_tokens_details") or u.get("input_tokens_details") or {}
        cached = details.get("cached_tokens")
        t = self.timings or {}
        if cached is None and t.get("cache_n") is not None:
            cached = t.get("cache_n")
        if prompt is None and t.get("prompt_n") is not None:
            prompt = (t.get("prompt_n") or 0) + (t.get("cache_n") or 0)
        if gen is None and t.get("predicted_n") is not None:
            gen = t.get("predicted_n")
        out = {"prompt_tokens": prompt, "cached": cached, "generated": gen}
        if prompt is not None:
            out["read"] = prompt - (cached or 0)
        if t.get("prompt_n") is not None:
            out["read"] = t.get("prompt_n")
        return out


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    server_version = "bench-proxy"

    def log_message(self, fmt, *args):  # quiet; the JSONL is the log
        pass

    # --- helpers -----------------------------------------------------------
    def _send_json(self, code, obj):
        data = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def _path(self):
        return urllib.parse.urlsplit(self.path).path.rstrip("/") or "/"

    # --- GET -----------------------------------------------------------------
    def do_GET(self):
        cfg = self.server.cfg
        cfg.write({"t0": time.time(), "method": "GET", "path": self._path(),
                   "status": 200 if self._path() in MODELS_PATHS else 404})
        if self._path() in MODELS_PATHS:
            w = cfg.window
            self._send_json(200, {"object": "list", "data": [{
                "id": cfg.model, "object": "model", "owned_by": "bench", "created": 0,
                "meta": {"n_ctx": w, "n_ctx_train": w},
                "context_length": w, "max_model_len": w,
            }]})
            return
        self._send_json(404, {"error": {"message": "bench proxy: not forwarded", "type": "not_found"}})

    def do_DELETE(self):
        self._send_json(404, {"error": {"message": "bench proxy: not forwarded", "type": "not_found"}})

    do_PUT = do_PATCH = do_DELETE

    # --- POST ----------------------------------------------------------------
    def do_POST(self):
        cfg = self.server.cfg
        path = self._path()
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b""
        if path not in FORWARD:
            self._send_json(404, {"error": {"message": "bench proxy: not forwarded", "type": "not_found"}})
            return
        try:
            body = json.loads(raw or b"{}")
        except ValueError:
            self._send_json(400, {"error": {"message": "bench proxy: body is not JSON"}})
            return
        upath = FORWARD[path]
        rec = {"t0": time.time(), "path": path, "asked_model": body.get("model")}
        body["model"] = cfg.model
        if body.get("stream") and upath.endswith("/chat/completions"):
            so = body.get("stream_options") or {}
            so["include_usage"] = True
            body["stream_options"] = so
        if cfg.reasoning_effort:
            # The same reasoning effort for every harness, as a local server's
            # default would be: on chat completions as reasoning_effort (and in
            # the chat template's kwargs, which Qwen's template reads), on
            # responses as reasoning.effort.
            e = cfg.reasoning_effort
            if upath.endswith("/chat/completions"):
                body["reasoning_effort"] = e
                kw = body.get("chat_template_kwargs") or {}
                kw["reasoning_effort"] = e
                body["chat_template_kwargs"] = kw
            else:
                r = body.get("reasoning") or {}
                r["effort"] = e
                body["reasoning"] = r
            rec["reasoning_effort"] = e
        if cfg.max_tokens_cap:
            for k in ("max_tokens", "max_completion_tokens", "max_output_tokens"):
                if isinstance(body.get(k), int) and body[k] > cfg.max_tokens_cap:
                    rec["clamped_" + k] = body[k]
                    body[k] = cfg.max_tokens_cap
        try:
            rec.update(describe_request(body))
        except Exception as e:  # never let classification break a request
            rec["describe_error"] = repr(e)[:200]
        cfg.dump_body(body)
        meter = Meter()
        is_chat = upath.endswith("/chat/completions")
        feed = meter.chat_obj if is_chat else meter.responses_obj
        out = json.dumps(body).encode()
        conn = None
        status = None
        try:
            conn = cfg.connect()
            headers = {"Content-Type": "application/json", "Content-Length": str(len(out))}
            if cfg.api_key:
                headers["Authorization"] = "Bearer " + cfg.api_key
            conn.request("POST", cfg.base_path + upath, body=out, headers=headers)
            resp = conn.getresponse()
            status = resp.status
            ctype = resp.getheader("Content-Type") or "application/json"
            if "text/event-stream" in ctype:
                self.send_response(status)
                self.send_header("Content-Type", ctype)
                self.send_header("Cache-Control", "no-cache")
                self.send_header("Transfer-Encoding", "chunked")
                self.end_headers()
                buf = b""
                while True:
                    line = resp.readline()
                    if not line:
                        break
                    self.wfile.write(b"%x\r\n%s\r\n" % (len(line), line))
                    if line.strip() == b"":
                        self.wfile.flush()
                    s = line.strip()
                    if s.startswith(b"data:"):
                        payload = s[5:].strip()
                        if payload and payload != b"[DONE]":
                            try:
                                feed(json.loads(payload))
                            except ValueError:
                                pass
                self.wfile.write(b"0\r\n\r\n")
                self.wfile.flush()
            else:
                data = resp.read()
                self.send_response(status)
                self.send_header("Content-Type", ctype)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
                try:
                    obj = json.loads(data)
                    feed(obj)
                    if meter.usage or meter.timings:
                        meter.saw_token()
                    if status >= 400 and not meter.error:
                        meter.error = data[:300].decode("utf-8", "replace")
                except ValueError:
                    if status >= 400:
                        meter.error = data[:300].decode("utf-8", "replace")
        except (BrokenPipeError, ConnectionResetError) as e:
            rec["client_gone"] = repr(e)[:100]
        except (OSError, http.client.HTTPException) as e:
            meter.error = "upstream: " + repr(e)[:300]
            if status is None:
                try:
                    self._send_json(502, {"error": {"message": "bench proxy: upstream failed: " + repr(e)[:200]}})
                except OSError:
                    pass
        finally:
            if conn is not None:
                conn.close()
        rec["t1"] = time.time()
        rec["status"] = status
        rec.update(meter.numbers())
        rec["finish_reason"] = meter.finish
        rec["n_tool_calls"] = len(meter.tool_ids)
        if meter.error:
            rec["error"] = meter.error
        t = meter.timings or {}
        if t.get("prompt_ms") is not None or t.get("predicted_ms") is not None:
            rec["prompt_ms"] = t.get("prompt_ms")
            rec["predicted_ms"] = t.get("predicted_ms")
            rec["timing"] = "server"
        else:
            first = meter.first_token or rec["t1"]
            rec["prompt_ms"] = round((first - rec["t0"]) * 1000, 1)
            rec["predicted_ms"] = round((rec["t1"] - first) * 1000, 1)
            rec["timing"] = "proxy"
        ctx = (rec.get("prompt_tokens") or 0) + (rec.get("generated") or 0)
        rec["context"] = ctx
        if ctx > cfg.window:
            rec["over_window"] = True
        cfg.write(rec)


class Config:
    def __init__(self, a):
        u = urllib.parse.urlsplit(a.upstream)
        self.scheme = u.scheme or "http"
        self.host = u.hostname or "127.0.0.1"
        self.port = u.port or (443 if self.scheme == "https" else 80)
        self.base_path = u.path.rstrip("/")
        if self.base_path.endswith("/v1"):
            self.base_path = self.base_path[:-3]
        self.model = a.model
        self.window = a.window
        self.max_tokens_cap = a.max_tokens_cap
        self.reasoning_effort = a.reasoning_effort
        self.api_key = os.environ.get("BENCH_UPSTREAM_KEY", "")
        self.log = open(a.log, "a", buffering=1)
        self.lock = threading.Lock()
        self.dump = a.dump
        self.dumped = 0

    def dump_body(self, body):
        """Saves the first 40 request bodies as sent upstream (--dump DIR)."""
        if not self.dump:
            return
        with self.lock:
            if self.dumped >= 40:
                return
            self.dumped += 1
            n = self.dumped
        os.makedirs(self.dump, exist_ok=True)
        with open(os.path.join(self.dump, "req-%03d.json" % n), "w") as f:
            json.dump(body, f, ensure_ascii=False, indent=1)

    def connect(self):
        if self.scheme == "https":
            return http.client.HTTPSConnection(self.host, self.port, timeout=UPSTREAM_TIMEOUT)
        return http.client.HTTPConnection(self.host, self.port, timeout=UPSTREAM_TIMEOUT)

    def write(self, rec):
        line = json.dumps(rec, ensure_ascii=False)
        with self.lock:
            self.log.write(line + "\n")
            self.log.flush()


def serve(a):
    cfg = Config(a)
    host, _, port = a.listen.rpartition(":")
    srv = ThreadingHTTPServer((host or "127.0.0.1", int(port)), Handler)
    srv.daemon_threads = True
    srv.cfg = cfg
    if a.port_file:
        tmp = a.port_file + ".tmp"
        with open(tmp, "w") as f:
            f.write(str(srv.server_address[1]))
        os.replace(tmp, a.port_file)
    srv.serve_forever()


def summarize(a):
    rows = []
    with open(a.log) as f:
        for line in f:
            line = line.strip()
            if line:
                try:
                    rows.append(json.loads(line))
                except ValueError:
                    pass

    def total(key):
        return sum((r.get(key) or 0) for r in rows)

    gets = [r for r in rows if r.get("method") == "GET"]
    rows = [r for r in rows if r.get("method") != "GET"]
    for r in rows:
        r["kind"] = kind_of(r.get("sys_head") or "", r.get("user_head") or "")
        r["summary"] = r["kind"] == "summary"
    errors = [r.get("error") for r in rows if r.get("error")]
    out = {
        "requests": len(rows),
        "prompt_tokens": total("prompt_tokens"),
        "cached_tokens": total("cached"),
        "read_tokens": total("read"),
        "generated": total("generated"),
        "gpu_ms": round(total("prompt_ms") + total("predicted_ms")),
        "prompt_ms": round(total("prompt_ms")),
        "predicted_ms": round(total("predicted_ms")),
        "timing": sorted({r.get("timing") for r in rows if r.get("timing")}),
        "peak_context": max([r.get("context") or 0 for r in rows] or [0]),
        "summaries": sum(1 for r in rows if r.get("summary")),
        "titles": sum(1 for r in rows if r.get("kind") == "title"),
        "summary_tokens": sum((r.get("generated") or 0) for r in rows if r.get("summary")),
        "over_window": sum(1 for r in rows if r.get("over_window")),
        "tool_calls": total("n_tool_calls"),
        "systems": sorted({r.get("sys_sha") for r in rows if r.get("sys_sha")}),
        "errors": len(errors),
        "first_errors": errors[:3],
        "gets": sorted({"%s %s" % (g.get("path"), g.get("status")) for g in gets}),
    }
    print(json.dumps(out))


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("serve")
    s.add_argument("--listen", default="127.0.0.1:0")
    s.add_argument("--port-file", default="")
    s.add_argument("--upstream", required=True)
    s.add_argument("--model", required=True)
    s.add_argument("--window", type=int, required=True)
    s.add_argument("--log", required=True)
    s.add_argument("--max-tokens-cap", type=int, default=0)
    s.add_argument("--reasoning-effort", default="", help="put this reasoning effort on every request")
    s.add_argument("--dump", default="", help="save the first 40 request bodies here")
    m = sub.add_parser("summarize")
    m.add_argument("log")
    a = p.parse_args()
    if a.cmd == "serve":
        serve(a)
    else:
        summarize(a)


if __name__ == "__main__":
    socket.setdefaulttimeout(None)
    main()
