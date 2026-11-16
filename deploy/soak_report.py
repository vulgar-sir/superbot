#!/usr/bin/env python3
"""Summarise a soak.jsonl written by deploy/soak.py.

Usage: python3 deploy/soak_report.py [/srv/superbot/soak.jsonl]

Reports coverage, per-check success rates and latency percentiles, and lists
failure windows so they can be matched against deploy times.
"""
import json
import statistics
import sys
from datetime import datetime

path = sys.argv[1] if len(sys.argv) > 1 else "/srv/superbot/soak.jsonl"
CHECKS = ["web", "api", "memory_create", "memory_delete", "sse"]


def ok(check, value):
    if not isinstance(value, dict) or "error" in value:
        return False
    if check == "sse":
        return value.get("chunks", 0) > 0
    if check == "memory_create":
        return value.get("status") in (200, 201)
    if check == "memory_delete":
        return value.get("status") in (200, 204)
    return value.get("status") == 200


def parse_ts(raw):
    return datetime.strptime(raw, "%Y-%m-%dT%H:%M:%S%z")


records, starts = [], []
for line in open(path, encoding="utf-8"):
    line = line.strip()
    if not line:
        continue
    item = json.loads(line)
    if item.get("event") == "start":
        starts.append(item)
    elif "iteration" in item:
        records.append(item)

if not records:
    sys.exit("no iterations recorded")
first, last = parse_ts(records[0]["ts"]), parse_ts(records[-1]["ts"])
hours = (last - first).total_seconds() / 3600
print(f"iterations: {len(records)}  from {first}  to {last}  ({hours:.1f} h)  restarts of the soak: {len(starts)}")
expected = int((last - first).total_seconds() / 60) + 1
print(f"coverage: {len(records)}/{expected} expected one-minute iterations ({100 * len(records) / max(expected, 1):.1f}%)")

for check in CHECKS:
    values = [r.get(check) for r in records if check in r]
    if not values:
        continue
    good = [v for v in values if ok(check, v)]
    latency = sorted(v["ms"] for v in good if "ms" in v)
    line = f"{check:<14} {len(good)}/{len(values)} ok ({100 * len(good) / len(values):.2f}%)"
    if latency:
        p50 = statistics.median(latency)
        p99 = latency[min(len(latency) - 1, int(len(latency) * 0.99))]
        line += f"  p50={p50:.0f}ms  p99={p99:.0f}ms  max={latency[-1]:.0f}ms"
    print(line)

windows, current = [], None
for record in records:
    failed = [c for c in CHECKS if c in record and not ok(c, record[c])]
    ts = parse_ts(record["ts"])
    if failed:
        if current is None:
            current = {"from": ts, "to": ts, "checks": set(failed), "count": 0, "sample": record}
        current["to"] = ts
        current["checks"].update(failed)
        current["count"] += 1
    elif current is not None:
        windows.append(current)
        current = None
if current is not None:
    windows.append(current)

print(f"failure windows: {len(windows)}")
for window in windows:
    sample = {c: window["sample"].get(c) for c in sorted(window["checks"])}
    print(f"  {window['from']} -> {window['to']}  iterations={window['count']}  checks={sorted(window['checks'])}  "
          f"sample={json.dumps(sample, ensure_ascii=False)[:220]}")
