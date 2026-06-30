#!/usr/bin/env python3
# Pretty-print + sanity-check a results JSONL file.
#   Usage: analyze.py results/<file>.jsonl [substr-filter]
import sys, json

path = sys.argv[1]
filt = sys.argv[2] if len(sys.argv) > 2 else ""
rows = []
with open(path) as f:
    for line in f:
        line = line.strip()
        if not line or not line.startswith("{"):
            continue
        try:
            rows.append(json.loads(line))
        except Exception as e:
            print("PARSE-ERR:", e, line[:80])

hdr = "%-44s %10s %12s %10s %4s %4s %9s %10s" % (
    "label", "append/s", "complete/s", "bklog/s", "div", "dec", "ev_added", "dlv_added")
print(hdr); print("-" * len(hdr))
for d in rows:
    if filt and filt not in d.get("label", ""):
        continue
    note = ""
    if d.get("design") == "d1" and d.get("run") == "producer":
        ev, dv, g = d["events_added"], d["deliveries_added"], d["groups"]
        ok = "OK" if dv == ev * g else "MISMATCH(exp %d)" % (ev * g)
        note = " fanout=" + ok
    print("%-44s %10s %12s %10s %4s %4s %9d %10d%s" % (
        d.get("label", "")[:44],
        d.get("append_med", 0), d.get("complete_med", 0),
        d.get("backlog_slope_per_s", 0),
        "Y" if d.get("diverging") else ".",
        "Y" if d.get("decay") else ".",
        d.get("events_added", 0), d.get("deliveries_added", 0), note))
