#!/usr/bin/env python3
"""Long-running reliability soak for superbot.

Each iteration checks the web origin, an authenticated API call, a memory
create/delete round trip, and a short conversation SSE read. Results are
appended as JSON lines so a 72h run can be audited afterwards.

Usage (on the server):
    SUPERBOT_SOAK_HOURS=72 nohup python3 deploy/soak.py >/dev/null 2>&1 &
    tail -f /srv/superbot/soak.jsonl

Reads the admin credentials from /srv/superbot/.env; never prints them.
"""

import http.cookiejar
import json
import os
import time
import urllib.request
from pathlib import Path

BASE = os.environ.get("SUPERBOT_SOAK_BASE", "http://127.0.0.1:32608")
ENV_FILE = Path(os.environ.get("SUPERBOT_SOAK_ENV", "/srv/superbot/.env"))
LOG_FILE = Path(os.environ.get("SUPERBOT_SOAK_LOG", "/srv/superbot/soak.jsonl"))
HOURS = float(os.environ.get("SUPERBOT_SOAK_HOURS", "72"))
INTERVAL = float(os.environ.get("SUPERBOT_SOAK_INTERVAL", "60"))
SSE_SECONDS = float(os.environ.get("SUPERBOT_SOAK_SSE_SECONDS", "8"))


def read_env():
    values = {}
    for line in ENV_FILE.read_text(encoding="utf-8", errors="replace").splitlines():
        if "=" not in line or line.lstrip().startswith("#"):
            continue
        key, value = line.split("=", 1)
        values[key.strip()] = value.strip().strip('"')
    return values


env = read_env()
jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))


def call(method, path, payload=None, timeout=20):
    data = json.dumps(payload).encode() if payload is not None else None
    request = urllib.request.Request(
        BASE + path,
        data=data,
        method=method,
        headers={"Content-Type": "application/json"},
    )
    started = time.time()
    with opener.open(request, timeout=timeout) as response:
        body = response.read()
        return response.status, body, (time.time() - started) * 1000


def sse_read(path, seconds):
    request = urllib.request.Request(BASE + path, headers={"Accept": "text/event-stream"})
    deadline = time.time() + seconds
    chunks = 0
    started = time.time()
    with opener.open(request, timeout=seconds + 5) as response:
        try:
            while time.time() < deadline:
                chunk = response.read1(1024)
                if not chunk:
                    break
                chunks += len(chunk)
        except TimeoutError:
            # A quiet stream is the normal case; the initial "retry:" frame
            # should already have been counted.
            pass
    return chunks, (time.time() - started) * 1000


def login():
    status, _, _ = call(
        "POST",
        "/api/auth/login",
        {"email": env.get("SUPERBOT_ADMIN_EMAIL", ""), "password": env.get("SUPERBOT_ADMIN_PASSWORD", "")},
    )
    if status != 200:
        raise RuntimeError(f"login failed: {status}")


def conversation_id():
    status, body, _ = call("GET", "/api/conversations")
    if status != 200:
        return None
    data = json.loads(body or b"{}")
    items = data.get("conversations") or data.get("items") or []
    return items[0]["id"] if items else None


def iteration(index, conversation):
    record = {"ts": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "iteration": index}
    try:
        status, _, ms = call("GET", "/", timeout=10)
        record["web"] = {"status": status, "ms": round(ms, 1)}
    except Exception as exc:  # noqa: BLE001 - record and continue
        record["web"] = {"error": str(exc)}
    try:
        status, _, ms = call("GET", "/api/notifications")
        record["api"] = {"status": status, "ms": round(ms, 1)}
    except Exception as exc:  # noqa: BLE001
        record["api"] = {"error": str(exc)}
    memory_id = None
    try:
        status, body, ms = call("POST", "/api/memories", {"content": f"soak-{index}-{time.time()}"})
        record["memory_create"] = {"status": status, "ms": round(ms, 1)}
        if status in (200, 201):
            memory_id = json.loads(body)["memory"]["id"]
    except Exception as exc:  # noqa: BLE001
        record["memory_create"] = {"error": str(exc)}
    finally:
        if memory_id:
            try:
                status, _, ms = call("DELETE", f"/api/memories/{memory_id}", {})
                record["memory_delete"] = {"status": status, "ms": round(ms, 1)}
            except Exception as exc:  # noqa: BLE001
                record["memory_delete"] = {"error": str(exc)}
    if conversation:
        try:
            chunks, ms = sse_read(f"/api/conversations/{conversation}/stream", SSE_SECONDS)
            record["sse"] = {"chunks": chunks, "ms": round(ms, 1)}
        except Exception as exc:  # noqa: BLE001
            record["sse"] = {"error": str(exc)}
    return record


def main():
    login()
    conversation = conversation_id()
    started = time.time()
    deadline = started + HOURS * 3600
    index = 0
    with LOG_FILE.open("a", encoding="utf-8") as log:
        log.write(json.dumps({"event": "start", "hours": HOURS, "base": BASE, "conversation": conversation}) + "\n")
        log.flush()
        while time.time() < deadline:
            index += 1
            record = iteration(index, conversation)
            log.write(json.dumps(record, ensure_ascii=False) + "\n")
            log.flush()
            if index % 30 == 0:
                try:
                    login()
                    conversation = conversation or conversation_id()
                except Exception as exc:  # noqa: BLE001
                    log.write(json.dumps({"ts": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "relogin_error": str(exc)}) + "\n")
                    log.flush()
            time.sleep(INTERVAL)
        log.write(json.dumps({"event": "done", "iterations": index, "hours": HOURS}) + "\n")


if __name__ == "__main__":
    main()
