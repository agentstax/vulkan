#!/usr/bin/env bash
# Empirically confirm/refute the v2-hybrid correctness review's load-bearing findings
# against the waterline_v2.sql harness. Each test asserts PASS/FAIL.
#   T1 (R3) stale-token Commit: safe wl_commit -> no phantom; doc wl_commit_doc -> phantom row
#   T2 (R1) sharded Claim is lane-blind: all K lanes claim the SAME (0,batch] -> K-fold dup
#   T3 (R2) Advance exception blocker is group-wide: lane-1 exception freezes lane-0; lanefix fixes
#   T4 (R6) maxAttempts->dead backstop: all-failing exceptions reach dead, committed reaches head
#   T6 (R5) reclaim FOR UPDATE SKIP LOCKED prevents double-grab of one expired lease
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"; SCALE="$(cd "$HERE/.." && pwd)"; source "$SCALE/env.sh"
export PGPASSWORD=bench
P(){ psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDATABASE" -tAc "$1"; }
reset(){ P "TRUNCATE events,cursors,leases,deliveries,processed RESTART IDENTITY;" >/dev/null; }
pass=0; fail=0
assert(){ if [[ "$2" == "$3" ]]; then echo "  PASS $1 ($2)"; pass=$((pass+1)); else echo "  FAIL $1 (got '$2' want '$3')"; fail=$((fail+1)); fi; }

echo "== reload harness =="; P "SELECT 1" >/dev/null; psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDATABASE" -q -f "$SCALE/waterline_v2.sql" >/dev/null 2>&1 && echo "loaded"

echo "== T1 (R3) stale-token Commit: safe vs doc variant =="
# --- safe variant ---
reset; P "SELECT wl_seed('s',1,100);" >/dev/null
read lo hi ta < <(P "SELECT lo||' '||hi||' '||token FROM wl_claim('s',0,10,30);")
P "UPDATE leases SET lease_until=now()-interval '1 min' WHERE consumer_group='s';" >/dev/null
read rl rlo rhi tb < <(P "SELECT lane||' '||lo||' '||hi||' '||token FROM wl_reclaim('s',30);")
P "SELECT wl_commit('s',0,0,'$tb','{}'::bigint[]);" >/dev/null            # reclaimer commits, lease freed
saferes=$(P "SELECT wl_commit('s',0,0,'$ta',ARRAY[5]::bigint[]);")        # original stale worker
safecnt=$(P "SELECT count(*) FROM deliveries WHERE consumer_group='s';")
assert "T1 safe stale-commit returns false" "$saferes" "f"
assert "T1 safe leaves NO phantom row" "$safecnt" "0"
# --- doc (unsafe) variant ---
reset; P "SELECT wl_seed('d',1,100);" >/dev/null
read lo hi ta < <(P "SELECT lo||' '||hi||' '||token FROM wl_claim('d',0,10,30);")
P "UPDATE leases SET lease_until=now()-interval '1 min' WHERE consumer_group='d';" >/dev/null
read rl rlo rhi tb < <(P "SELECT lane||' '||lo||' '||hi||' '||token FROM wl_reclaim('d',30);")
P "SELECT wl_commit('d',0,0,'$tb','{}'::bigint[]);" >/dev/null
P "SELECT wl_commit_doc('d',0,0,'$ta',ARRAY[5]::bigint[]);" >/dev/null    # stale worker, DOC variant
doccnt=$(P "SELECT count(*) FROM deliveries WHERE consumer_group='d' AND \"offset\"=5 AND state='ready';")
assert "T1 doc variant INJECTS a phantom ready row (bug)" "$doccnt" "1"

echo "== T2 (R1) sharded Claim is lane-blind (K=4) =="
reset; P "SELECT wl_seed('q',4,1000);" >/dev/null
ranges=""
for ln in 0 1 2 3; do r=$(P "SELECT lo||'-'||hi FROM wl_claim('q',$ln,200,30);"); ranges="$ranges $r"; done
distinct=$(echo $ranges | tr ' ' '\n' | sort -u | grep -c .)
echo "  lane ranges:$ranges"
assert "T2 all 4 lanes claim the SAME range (not disjoint => dup)" "$distinct" "1"

echo "== T3 (R2) Advance exception blocker group-wide vs lane-fixed =="
reset; P "SELECT wl_seed('w',2,1000);" >/dev/null
P "UPDATE cursors SET claimed=400, committed=0 WHERE consumer_group='w' AND lane=0;" >/dev/null
P "INSERT INTO deliveries(consumer_group,\"offset\",state) VALUES ('w',301,'ready');" >/dev/null  # odd => lane 1
gw=$(P "SELECT wl_advance('w',0);")
P "UPDATE cursors SET committed=0 WHERE consumer_group='w' AND lane=0;" >/dev/null
lf=$(P "SELECT wl_advance_lanefix('w',0,2);")
assert "T3 group-wide advance is cross-floored by lane-1 exception" "$gw" "300"
assert "T3 lane-fixed advance reaches lane-0 true frontier" "$lf" "400"

echo "== T4 (R6) maxAttempts->dead backstop unblocks the waterline =="
reset; P "SELECT wl_seed('m',1,50);" >/dev/null
read lo hi tok < <(P "SELECT lo||' '||hi||' '||token FROM wl_claim('m',0,50,30);")
P "SELECT wl_commit('m',0,0,'$tok',(SELECT array_agg(g)::bigint[] FROM generate_series(1,50) g));" >/dev/null
for r in 1 2 3 4 5 6; do
  P "UPDATE deliveries SET available_at=now() WHERE consumer_group='m' AND state='ready';" >/dev/null
  P "UPDATE deliveries SET state='inflight', attempts=attempts+1 WHERE consumer_group='m' AND state='ready' AND available_at<=now();" >/dev/null
  P "UPDATE deliveries SET state=(CASE WHEN attempts>=3 THEN 'dead' ELSE 'ready' END)::delivery_state WHERE consumer_group='m' AND state='inflight';" >/dev/null
done
dead=$(P "SELECT count(*) FROM deliveries WHERE consumer_group='m' AND state='dead';")
open=$(P "SELECT count(*) FROM deliveries WHERE consumer_group='m' AND state IN ('ready','inflight');")
comm=$(P "SELECT wl_advance('m',0);")
assert "T4 all 50 exceptions reach dead" "$dead" "50"
assert "T4 none left ready/inflight" "$open" "0"
assert "T4 dead unblocks committed to head" "$comm" "50"

echo "== T6 (R5) reclaim SKIP LOCKED prevents double-grab =="
reset; P "SELECT wl_seed('k',1,100);" >/dev/null
read lo hi tok < <(P "SELECT lo||' '||hi||' '||token FROM wl_claim('k',0,10,30);")
P "UPDATE leases SET lease_until=now()-interval '1 min' WHERE consumer_group='k';" >/dev/null
# session A holds a row lock on the only expired lease
psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDATABASE" -c \
  "BEGIN; SELECT 1 FROM leases WHERE consumer_group='k' AND lease_until<now() FOR UPDATE; SELECT pg_sleep(2); COMMIT;" >/dev/null 2>&1 &
sleep 0.6
n=$(P "SELECT count(*) FROM wl_reclaim('k',30);")   # the only expired lease is locked => SKIP LOCKED => 0
assert "T6 concurrent reclaim skips the locked lease" "$n" "0"
wait

echo "== SUMMARY: $pass passed, $fail failed =="
exit "$fail"
