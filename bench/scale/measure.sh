#!/usr/bin/env bash
# End-to-end measurement engine. Runs producer and/or consumer load concurrently
# for WARMUP+WINDOW seconds, samples a cheap monotonic completion counter once a
# second, and reports STEADY-STATE rates (median + p10/p90 over the post-warmup
# window), append rate, lag/backlog trajectory, and correctness counts as one
# JSON line on stdout. Diagnostics go to stderr.
#
# Config via env vars (with defaults):
#   DESIGN   d1|d2|d3|d4|d5      (required) selects the consumer script
#   RUN      both|producer|consumer  (default both)
#   PC       producer clients    (default 4)
#   CC       consumer clients    (default 4)
#   PBATCH   events per producer txn (default 1)
#   BATCH    rows/offsets per consumer claim (default 200)
#   N_GROUPS consumer groups     (default 1)  -> :ngroups for claim-from-log
#   RATE     producer -R offered rate, 0 = unthrottled saturation (default 0)
#   WARMUP   seconds to discard   (default 10)
#   WINDOW   steady-state seconds (default 30)
#   LABEL    free-text tag        (default "")
#
# Assumes the schema is already built (reset.sh) and tables are in the desired
# pre-grown state. measure.sh does NOT reset data.
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"; source "$DIR/env.sh"

DESIGN="${DESIGN:?set DESIGN=d1..d5}"
RUN="${RUN:-both}"
PC="${PC:-4}"; CC="${CC:-4}"
PBATCH="${PBATCH:-1}"; BATCH="${BATCH:-200}"
N_GROUPS="${N_GROUPS:-1}"
RATE="${RATE:-0}"
WARMUP="${WARMUP:-10}"; WINDOW="${WINDOW:-30}"
LABEL="${LABEL:-}"
DURATION=$(( WARMUP + WINDOW ))
# optional projector (designs d2/d3): PROJ=1 launches PROJ_N instances
PROJ="${PROJ:-0}"; PROJ_MODE="${PROJ_MODE:-poll}"; PROJ_BATCH="${PROJ_BATCH:-5000}"
PROJ_N="${PROJ_N:-1}"; PROJ_POLLMS="${PROJ_POLLMS:-50}"
PROJBIN="$DIR/projector/projector"

case "$DESIGN" in
  d1|d2|d3) CLAIM="$DIR/claim_deliveries.sql" ;;
  d4)       CLAIM="$DIR/claim_log.sql" ;;
  d5)       CLAIM="$DIR/claim_rangelease.sql" ;;
  *) echo "bad DESIGN: $DESIGN" >&2; exit 2 ;;
esac
# optional override of the consumer script (e.g. combined-commit / sharded variants)
[[ -n "${CLAIM_OVERRIDE:-}" ]] && CLAIM="$DIR/$CLAIM_OVERRIDE"

jmin() { local a="$1" b="$2"; (( a < b )) && echo "$a" || echo "$b"; }
PJ="$(jmin "$PC" 8)"; CJ="$(jmin "$CC" 8)"

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
SAMPLE="$TMP/sample.csv"; SENTINEL="$TMP/run"; touch "$SENTINEL"

# counts before
DELIV0="$(psql_q "SELECT count(*) FROM deliveries;")"
HEAD0="$(psql_q "SELECT COALESCE(max(\"offset\"),0) FROM events;")"
DONE0="$(psql_q "SELECT COALESCE(sum(n),0) FROM progress;")"

# ---- background sampler: epoch|done|head once per second ----
(
  while [ -f "$SENTINEL" ]; do
    line="$(psql -tAc "SELECT COALESCE(sum(n),0)||'|'||(SELECT COALESCE(max(\"offset\"),0) FROM events) FROM progress;" 2>/dev/null || echo "|" )"
    printf '%s|%s\n' "$(date +%s.%N)" "$line" >> "$SAMPLE"
    sleep 1
  done
) & SAMPLER=$!

# ---- launch load ----
PPID_=""; CPID_=""
if [[ "$RUN" == "both" || "$RUN" == "producer" ]]; then
  RFLAG=(); if (( RATE > 0 )); then RFLAG=(-R "$RATE"); fi
  pgbench -n -P 1 -f "$DIR/append.sql" -D pbatch="$PBATCH" \
    -c "$PC" -j "$PJ" -T "$DURATION" ${RFLAG[@]+"${RFLAG[@]}"} \
    > "$TMP/producer.log" 2>&1 & PPID_=$!
fi
if [[ "$RUN" == "both" || "$RUN" == "consumer" ]]; then
  pgbench -n -P 1 -f "$CLAIM" -D batch="$BATCH" -D nshards="$NSHARDS" -D ngroups="$N_GROUPS" \
    -D kfront="${KFRONT:-1}" \
    -c "$CC" -j "$CJ" -T "$DURATION" \
    > "$TMP/consumer.log" 2>&1 & CPID_=$!
fi
PROJ_PIDS=()
if [[ "$PROJ" == "1" ]]; then
  i=0
  while (( i < PROJ_N )); do
    "$PROJBIN" -mode "$PROJ_MODE" -batch "$PROJ_BATCH" -poll-ms "$PROJ_POLLMS" \
      -shard "$i" -nshards "$PROJ_N" -dur "$DURATION" > "$TMP/proj_$i.log" 2>&1 & PROJ_PIDS+=($!)
    i=$(( i + 1 ))
  done
fi

[[ -n "$PPID_" ]] && wait "$PPID_" || true
[[ -n "$CPID_" ]] && wait "$CPID_" || true
for p in ${PROJ_PIDS[@]+"${PROJ_PIDS[@]}"}; do wait "$p" 2>/dev/null || true; done
rm -f "$SENTINEL"; wait "$SAMPLER" 2>/dev/null || true

# counts after
DELIV1="$(psql_q "SELECT count(*) FROM deliveries;")"
HEAD1="$(psql_q "SELECT COALESCE(max(\"offset\"),0) FROM events;")"
DONE1="$(psql_q "SELECT COALESCE(sum(n),0) FROM progress;")"

# ---- stats helpers ----
pctl() { sort -n | awk -v p="$1" 'NF{a[++n]=$1} END{ if(n==0){print 0; exit} i=int(p/100*(n-1))+1; if(i<1)i=1; print a[i] }'; }

# producer append rate (tps * pbatch) over steady window
APP_MED=0; APP_P10=0; APP_P90=0
if [[ -f "$TMP/producer.log" ]]; then
  awk -v w="$WARMUP" -v pb="$PBATCH" '/^progress:/{ t=$2+0; tps=$4+0; if(t>w) print tps*pb }' "$TMP/producer.log" > "$TMP/app.txt" || true
  if [[ -s "$TMP/app.txt" ]]; then
    APP_MED="$(pctl 50 < "$TMP/app.txt")"; APP_P10="$(pctl 10 < "$TMP/app.txt")"; APP_P90="$(pctl 90 < "$TMP/app.txt")"
  fi
fi

# consumer raw txn rate (reference; includes empty claims)
CTX_MED=0
if [[ -f "$TMP/consumer.log" ]]; then
  awk -v w="$WARMUP" '/^progress:/{ t=$2+0; if(t>w) print $4+0 }' "$TMP/consumer.log" > "$TMP/ctx.txt" || true
  [[ -s "$TMP/ctx.txt" ]] && CTX_MED="$(pctl 50 < "$TMP/ctx.txt")"
fi

# projector materialization rate = sum of per-shard steady-state median rows/s
PROJ_MED=0
if [[ "$PROJ" == "1" ]]; then
  i=0
  while (( i < PROJ_N )); do
    if [[ -f "$TMP/proj_$i.log" ]]; then
      awk -v w="$WARMUP" '/^proj shard=/{ t=0; rr=0; for(j=1;j<=NF;j++){ if($j ~ /^t=/) t=substr($j,3)+0; if($j ~ /^rate=/){ r=substr($j,6); sub(/\/s$/,"",r); rr=r+0 } } if(t>w) print rr }' "$TMP/proj_$i.log" > "$TMP/proj_$i.txt" || true
      if [[ -s "$TMP/proj_$i.txt" ]]; then
        m="$(pctl 50 < "$TMP/proj_$i.txt")"
        PROJ_MED="$(awk -v a="$PROJ_MED" -v b="$m" 'BEGIN{print a+b}')"
      fi
    fi
    i=$(( i + 1 ))
  done
fi

# completion rate (units/sec) from sampler done-deltas over steady window
CMP_MED=0; CMP_P10=0; CMP_P90=0; DECAY=false
if [[ -s "$SAMPLE" ]]; then
  awk -F'|' -v w="$WARMUP" '
    NR==1{ t0=$1; pt=$1; pd=$2; next }
    { rel=$1-t0; if(rel>=w){ dt=$1-pt; if(dt>0) printf "%.2f\n",($2-pd)/dt } pt=$1; pd=$2 }
  ' "$SAMPLE" > "$TMP/cmp.txt" || true
  if [[ -s "$TMP/cmp.txt" ]]; then
    CMP_MED="$(pctl 50 < "$TMP/cmp.txt")"; CMP_P10="$(pctl 10 < "$TMP/cmp.txt")"; CMP_P90="$(pctl 90 < "$TMP/cmp.txt")"
    # decay: second-half median vs first-half median
    n=$(wc -l < "$TMP/cmp.txt"); half=$(( n/2 ))
    if (( half >= 2 )); then
      fh="$(head -n "$half" "$TMP/cmp.txt" | pctl 50)"; sh="$(tail -n "$half" "$TMP/cmp.txt" | pctl 50)"
      DECAY="$(awk -v a="$fh" -v b="$sh" 'BEGIN{print (a>0 && b < 0.85*a)?"true":"false"}')"
    fi
  fi
fi

# backlog trajectory: backlog = head*N_GROUPS - done, at window start vs end; slope/sec
read -r BL_START BL_END BL_SLOPE < <(
  awk -F'|' -v w="$WARMUP" -v ng="$N_GROUPS" '
    NR==1{ t0=$1 }
    { rel=$1-t0; bl=$3*ng-$2; if(rel>=w){ if(bs==""){bs=bl; ts=rel} be=bl; te=rel } }
    END{ printf "%.0f %.0f %.3f\n", (bs==""?0:bs), (be==""?0:be), (te>ts?(be-bs)/(te-ts):0) }
  ' "$SAMPLE" 2>/dev/null || echo "0 0 0"
)
# diverging if backlog grows at >10% of append rate (producer outrunning consumers)
DIVERGING="$(awk -v s="$BL_SLOPE" -v a="$APP_MED" 'BEGIN{print (s > 0 && (a<=0 || s > 0.10*a*'"$N_GROUPS"'))?"true":"false"}')"

EV_TOTAL=$(( HEAD1 - HEAD0 )); DV_TOTAL=$(( DELIV1 - DELIV0 )); DN_TOTAL=$(( DONE1 - DONE0 ))

printf '{"label":"%s","design":"%s","run":"%s","groups":%s,"pc":%s,"cc":%s,"pbatch":%s,"batch":%s,"rate":%s,"warmup":%s,"window":%s,' \
  "$LABEL" "$DESIGN" "$RUN" "$N_GROUPS" "$PC" "$CC" "$PBATCH" "$BATCH" "$RATE" "$WARMUP" "$WINDOW"
printf '"append_med":%s,"append_p10":%s,"append_p90":%s,' "$APP_MED" "$APP_P10" "$APP_P90"
printf '"complete_med":%s,"complete_p10":%s,"complete_p90":%s,"consumer_txn_med":%s,' "$CMP_MED" "$CMP_P10" "$CMP_P90" "$CTX_MED"
printf '"proj_med":%s,"proj_mode":"%s","proj_n":%s,"proj_batch":%s,' "$PROJ_MED" "$PROJ_MODE" "$PROJ_N" "$PROJ_BATCH"
printf '"backlog_start":%s,"backlog_end":%s,"backlog_slope_per_s":%s,"diverging":%s,"decay":%s,' \
  "$BL_START" "$BL_END" "$BL_SLOPE" "$DIVERGING" "$DECAY"
printf '"events_added":%s,"deliveries_added":%s,"done_added":%s,"deliveries_total":%s,"events_head":%s}\n' \
  "$EV_TOTAL" "$DV_TOTAL" "$DN_TOTAL" "$DELIV1" "$HEAD1"
