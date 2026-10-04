#!/usr/bin/env bash
# cad-rss-time-measure.sh -- Phase 71 (EVAL-03) peak RSS and wall time of the
# CAD helpers, one container per (file, operation, memory limit, repeat).
#
# memory.peak is cumulative per container, so a container shared between cells
# reports one peak for all of them. An emulated image is not a measurement:
# cad_require_native refuses it. A render is judged by pixelcheck on the PNG,
# never by the helper's exit code.
#
# Usage:
#   scripts/cad-rss-time-measure.sh [--ocp V] [--variant NAME] [--memory 1g,2g,4g]
#       [--cpus 2] [--repeats N] [--budget-s N] [--confine-ro PATH:PATH:...] [--out FILE]
#       [--ops mesh,render,stlstats,dxf,rsvg-png,rsvg-pdf] [--quality draft|standard|fine]
#       [--parallel on|off|affinity] [--lp-threads N|default]
#       [--ladder] [--include-private] [--resume FILE] [--max-wall-s N]
#       [--floor-from FILE] [--floor-frac 0.40] [--premesh-mem 4g] FILE-OR-DIR...
#   scripts/cad-rss-time-measure.sh --pin-compare EVIDENCE-OCP-A EVIDENCE-OCP-B
#   scripts/cad-rss-time-measure.sh --threads-decision EVIDENCE
#
# Operations by format: step and iges take mesh and render, stl takes stlstats
# and render, dxf takes dxf (SVG through ezdxf), rsvg-png and rsvg-pdf (the SVG
# comes from an unmeasured dxf run). Other pairs print a SKIP line.
#
# --ladder     inside one (format, operation, memory limit, series) the files go
#              by ascending size and the first oom_kill, rc 137 or timeout ends
#              that series with a LADDER_STOP line; reason=top means the top
#              rung ran. A series is a scaled generator family, or "real"
#              (committed and fetched files), or "private".
# --resume F   append to F and skip the cells F already has a ROW for; a series
#              F already stopped stays stopped.
# --max-wall-s stop cleanly (exit 75) before the next cell once N seconds have
#              passed, so a long run goes in chunks of a few hours.
# --floor-from skip a cell whose RSS in the 4g rows of FILE is below
#              --floor-frac of the memory limit being run.
# The render of a STEP or IGES file reads an STL meshed once per file, and the
# rsvg operations read an SVG drawn once per file, both at --premesh-mem and
# inside the cell budget; a failed pre-step is a SKIP and, in a ladder, a stop
# when it was an oom or a timeout.
#
# Exit codes: 64 usage, 69 docker missing, 75 chunk ended by --max-wall-s,
# 1 a cell produced no WRAP line or there were no rows.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

OCP=7.9.3.1.1
VARIANT=full
MEMS=1g,2g,4g
CPUS=2
REPEATS=1
OPS=mesh,render
QUALITY=standard
PARALLEL=on
LP=default
BUDGET=${OCTOCONV_CAD_CELL_BUDGET:-900}
CONFINE=""
OUT=cad-rss-time-measurement.txt
LADDER=0
INCLUDE_PRIVATE=0
RESUME=""
MAX_WALL=0
FLOOR_FROM=""
FLOOR_FRAC=0.40
PREMEM=4g
PIN_A=""
PIN_B=""
THREADS_FILE=""
INPUTS=()

usage() {
  sed -n '/^# Usage:/,/^# Exit codes/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 64
}

pin_compare() {
  local a=$1 b=$2
  [ -s "$a" ] && [ -s "$b" ] || { echo "error: both evidence files are required" >&2; exit 64; }
  {
    echo "PIN_COMPARE_BEGIN,a=$(basename "$a"),b=$(basename "$b")"
    awk -F, -v A="$(basename "$a")" -v B="$(basename "$b")" '
      function kv(s, k,    n, i, p, parts) {
        n = split(s, parts, ",")
        for (i = 1; i <= n; i++) {
          if (index(parts[i], k "=") == 1) return substr(parts[i], length(k) + 2)
        }
        return ""
      }
      function outkv(o, k,    n, i, p, parts) {
        n = split(o, parts, ";")
        for (i = 1; i <= n; i++) {
          if (index(parts[i], k ":") == 1) return substr(parts[i], length(k) + 2)
        }
        return "-"
      }
      function num(x) { return (x ~ /^[0-9.]+$/) ? x + 0 : -1 }
      FNR == 1 { side = (FILENAME == ARGV[1]) ? "a" : "b" }
      /^ROW,/ {
        line = $0
        out = line
        sub(/^.*,out=/, "", out)
        head = line
        sub(/,out=.*$/, "", head)
        if (kv(head, "op") != "mesh") next
        f = kv(head, "fmt")
        if (f != "step" && f != "iges") next
        if (kv(head, "mem") != "4g") next
        sha = kv(head, "sha")
        if (!(sha in seen)) { seen[sha] = 1; order[++n] = sha }
        name[sha] = kv(head, "name"); fmt[sha] = f; bytes[sha] = kv(head, "bytes")
        rc = kv(head, "rc")
        c = num(kv(head, "child_rss_mib")); g = num(kv(head, "cg_peak_mib"))
        r = (c > g) ? c : g
        key = side SUBSEP sha
        runs[key]++
        if (rc == "0") {
          ok[key]++
          w[key, ok[key]] = kv(head, "wall_s") + 0
          if (r > rss[key]) rss[key] = r
          tri[key] = outkv(out, "triangles"); vol[key] = outkv(out, "volume")
          sol[key] = outkv(out, "solids")
        } else {
          why = (kv(head, "oom_kill") != "0" && kv(head, "oom_kill") != "na") ? "oom" : (kv(head, "timeout") == "1" ? "timeout" : "rc" rc)
          fail[key] = why
        }
      }
      function median(key,    i, j, t, m, cnt, arr) {
        cnt = ok[key]
        if (cnt == 0) return "-"
        for (i = 1; i <= cnt; i++) arr[i] = w[key, i]
        for (i = 2; i <= cnt; i++) { t = arr[i]; j = i - 1; while (j > 0 && arr[j] > t) { arr[j+1] = arr[j]; j-- } arr[j+1] = t }
        return arr[int((cnt + 1) / 2)]
      }
      END {
        for (i = 1; i <= n; i++) {
          s = order[i]; ka = "a" SUBSEP s; kb = "b" SUBSEP s
          printf "PIN_COMPARE,name=%s,sha=%s,fmt=%s,bytes=%s,", name[s], s, fmt[s], bytes[s]
          printf "rss_a_mib=%s,wall_a_median=%s,ok_a=%d/%d,fail_a=%s,tri_a=%s,vol_a=%s,solids_a=%s,",
            (ok[ka] ? rss[ka] : "-"), median(ka), ok[ka], runs[ka], (fail[ka] ? fail[ka] : "-"),
            (ka in tri ? tri[ka] : "-"), (ka in vol ? vol[ka] : "-"), (ka in sol ? sol[ka] : "-")
          printf "rss_b_mib=%s,wall_b_median=%s,ok_b=%d/%d,fail_b=%s,tri_b=%s,vol_b=%s,solids_b=%s\n",
            (ok[kb] ? rss[kb] : "-"), median(kb), ok[kb], runs[kb], (fail[kb] ? fail[kb] : "-"),
            (kb in tri ? tri[kb] : "-"), (kb in vol ? vol[kb] : "-"), (kb in sol ? sol[kb] : "-")
        }
        print "PIN_COMPARE_END,files=" n
      }' "$a" "$b"
  } | tee -a "$b"
  exit 0
}

threads_decision() {
  local f=$1
  [ -s "$f" ] || { echo "error: the evidence file is empty" >&2; exit 64; }
  {
    awk -F, -v TOL=0.10 '
      function kv(s, k,    n, i, parts) {
        n = split(s, parts, ",")
        for (i = 1; i <= n; i++) if (index(parts[i], k "=") == 1) return substr(parts[i], length(k) + 2)
        return ""
      }
      /^DIST,/ {
        op = kv($0, "op"); par = kv($0, "par"); lp = kv($0, "lp")
        v = (op == "mesh") ? par : lp
        key = op "|" v
        if (!(key in seen)) { seen[key] = 1; keys[++nk] = key }
        w[key] += kv($0, "wall_median"); u[key] += kv($0, "usersys_median")
        t = kv($0, "threads_max") + 0; if (t > th[key]) th[key] = t
        files[key]++
        printf "THREADS_ROW,op=%s,variant=%s,name=%s,bytes=%s,n=%s,wall_median=%s,wall_min=%s,wall_max=%s,usersys_median=%s,threads_max=%s,rss_max_mib=%s\n",
          op, v, kv($0, "name"), kv($0, "bytes"), kv($0, "n"), kv($0, "wall_median"), kv($0, "wall_min"), kv($0, "wall_max"),
          kv($0, "usersys_median"), kv($0, "threads_max"), kv($0, "rss_max_mib")
      }
      END {
        for (o = 1; o <= 2; o++) {
          op = (o == 1) ? "mesh" : "render"
          best = ""; bw = -1
          for (i = 1; i <= nk; i++) { split(keys[i], kp, "|"); if (kp[1] != op) continue; if (bw < 0 || w[keys[i]] < bw) { bw = w[keys[i]]; best = keys[i] } }
          if (best == "") continue
          pick = best
          for (i = 1; i <= nk; i++) {
            split(keys[i], kp, "|"); if (kp[1] != op) continue
            k = keys[i]
            if (w[k] <= bw * (1 + TOL)) {
              if (u[k] < u[pick] || (u[k] == u[pick] && th[k] < th[pick])) pick = k
            }
          }
          line = ""
          for (i = 1; i <= nk; i++) {
            split(keys[i], kp, "|"); if (kp[1] != op) continue
            k = keys[i]
            line = line sprintf("%s:wall_sum=%.2f/usersys_sum=%.2f/threads_max=%d/inputs=%d ", kp[2], w[k], u[k], th[k], files[k])
          }
          split(pick, pp, "|")
          choice[op] = pp[2]; why[op] = sprintf("%s fastest=%s within %d%% of the fastest: tie broken by lower user+sys then threads; picked %s", line, best, TOL * 100, pick)
        }
        split(choice["render"] "", dummy)
        printf "THREADS_DECISION,lp_num_threads=%s,occt_parallel=%s,reason=cpus=2 4g, sums of per-input median wall over the heavy inputs; mesh: %s; render: %s\n",
          choice["render"], choice["mesh"], why["mesh"], why["render"]
      }' "$f"
  } | tee -a "$f"
  exit 0
}

while [ $# -gt 0 ]; do
  case "$1" in
    --ocp) OCP=${2:?}; shift 2 ;;
    --variant) VARIANT=${2:?}; shift 2 ;;
    --memory) MEMS=${2:?}; shift 2 ;;
    --cpus) CPUS=${2:?}; shift 2 ;;
    --repeats) REPEATS=${2:?}; shift 2 ;;
    --ops) OPS=${2:?}; shift 2 ;;
    --quality) QUALITY=${2:?}; shift 2 ;;
    --parallel) PARALLEL=${2:?}; shift 2 ;;
    --lp-threads) LP=${2:?}; shift 2 ;;
    --budget-s) BUDGET=${2:?}; shift 2 ;;
    --confine-ro) CONFINE=${2:?}; shift 2 ;;
    --out) OUT=${2:?}; shift 2 ;;
    --ladder) LADDER=1; shift ;;
    --include-private) INCLUDE_PRIVATE=1; shift ;;
    --resume) RESUME=${2:?}; shift 2 ;;
    --max-wall-s) MAX_WALL=${2:?}; shift 2 ;;
    --floor-from) FLOOR_FROM=${2:?}; shift 2 ;;
    --floor-frac) FLOOR_FRAC=${2:?}; shift 2 ;;
    --premesh-mem) PREMEM=${2:?}; shift 2 ;;
    --pin-compare) PIN_A=${2:?}; PIN_B=${3:?}; shift 3 ;;
    --threads-decision) THREADS_FILE=${2:?}; shift 2 ;;
    -h|--help) usage ;;
    --) shift; while [ $# -gt 0 ]; do INPUTS+=("$1"); shift; done ;;
    -*) echo "error: unknown option $1" >&2; usage ;;
    *) INPUTS+=("$1"); shift ;;
  esac
done

[ -z "$PIN_A" ] || pin_compare "$PIN_A" "$PIN_B"
[ -z "$THREADS_FILE" ] || threads_decision "$THREADS_FILE"

[ ${#INPUTS[@]} -gt 0 ] || [ "$INCLUDE_PRIVATE" = 1 ] || usage
case "$QUALITY" in draft|standard|fine) ;; *) echo "error: --quality must be draft, standard or fine" >&2; exit 64 ;; esac
case "$PARALLEL" in on|off|affinity) ;; *) echo "error: --parallel must be on, off or affinity" >&2; exit 64 ;; esac
case "$LP" in default) ;; *[!0-9]*|'') echo "error: --lp-threads must be a positive integer or default" >&2; exit 64 ;; esac
[ -z "$FLOOR_FROM" ] || [ -s "$FLOOR_FROM" ] || { echo "error: --floor-from file is empty or missing" >&2; exit 64; }

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is required" >&2
  exit 69
fi

abspath() {
  local p=$1
  if [ -d "$p" ]; then (cd "$p" && pwd -P)
  else printf '%s/%s\n' "$(cd "$(dirname "$p")" && pwd -P)" "$(basename "$p")"; fi
}

list_cad_files() {
  find "$1" -type f \( -iname '*.step' -o -iname '*.stp' -o -iname '*.iges' \
    -o -iname '*.igs' -o -iname '*.stl' -o -iname '*.dxf' \)
}

series_of() {
  local base
  base=$(basename "$1")
  case "$1" in
    "$CAD_CACHE_DIR"/scaled/*)
      printf '%s\n' "$base" | sed -E 's/-n?[0-9]+(MiB)?\.[^.]+$//' ;;
    *) printf 'real\n' ;;
  esac
}

LIST=$(mktemp "${TMPDIR:-/tmp}/cad-list.XXXXXX")
add_file() {
  local f=$1 origin=$2 series size
  size=$(wc -c < "$f" | tr -d ' ')
  if [ "$origin" = private ]; then series=private; else series=$(series_of "$f"); fi
  printf '%s\t%s\t%s\t%s\n' "$series" "$size" "$origin" "$f" >> "$LIST"
}

for item in ${INPUTS[@]+"${INPUTS[@]}"}; do
  if [ -d "$item" ]; then
    while IFS= read -r f; do add_file "$f" public; done < <(list_cad_files "$(abspath "$item")")
  elif [ -f "$item" ]; then
    add_file "$(abspath "$item")" public
  else
    echo "error: no such file or directory in the argument list" >&2
    rm -f "$LIST"
    exit 64
  fi
done

PRIVATE_FILES=0
if [ "$INCLUDE_PRIVATE" = 1 ]; then
  if [ -n "$CAD_PRIVATE_DIR" ] && [ -d "$CAD_PRIVATE_DIR" ]; then
    while IFS= read -r f; do add_file "$f" private; PRIVATE_FILES=$((PRIVATE_FILES + 1)); done \
      < <(list_cad_files "$(abspath "$CAD_PRIVATE_DIR")")
  fi
fi

SORTED=$(mktemp "${TMPDIR:-/tmp}/cad-sorted.XXXXXX")
LC_ALL=C sort -t "$(printf '\t')" -k1,1 -k2,2n -k4,4 "$LIST" > "$SORTED"
rm -f "$LIST"

if [ -n "$RESUME" ]; then
  OUT=$RESUME
  [ -f "$OUT" ] || : > "$OUT"
else
  : > "$OUT"
fi
emit() { printf '%s\n' "$*" | tee -a "$OUT"; }

TAG=$(cad_build_image "$OCP" "$VARIANT")
cad_require_native "$TAG" || exit 1
IMAGE_ARCH=$(docker image inspect "$TAG" --format '{{.Architecture}}')

WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-measure.XXXXXX")
trap 'rm -rf "$WORK" "$SORTED"' EXIT

PROBE_MOUNT=()
PROBE_ENV=()
if [ -n "$CONFINE" ]; then
  mkdir -p "$WORK/probe"
  CGO_ENABLED=0 GOOS=linux GOARCH="$IMAGE_ARCH" go build -o "$WORK/probe/sandbox-probe" ./scripts/cmd/sandbox-probe
  chmod 0755 "$WORK/probe" "$WORK/probe/sandbox-probe"
  PROBE_MOUNT=(-v "$WORK/probe:/probe:ro")
  PROBE_ENV=(-e "PROBE_RO=$CONFINE" -e PROBE_RW=/job)
fi

cad_evidence_header "$TAG" "$CPUS" "$MEMS" | tee -a "$OUT"
[ -z "$CONFINE" ] || emit "HDR,confine_ro=$CONFINE"
emit "HDR,ladder=$LADDER,repeats=$REPEATS,budget_s=$BUDGET,quality=$QUALITY,parallel=$PARALLEL,lp_threads=$LP,resume=$([ -n "$RESUME" ] && echo 1 || echo 0)"
[ "$INCLUDE_PRIVATE" = 0 ] || {
  if [ "$PRIVATE_FILES" -eq 0 ]; then
    emit "PRIVATE_CORPUS,files=0,status=реальный поток не замерен"
  else
    emit "PRIVATE_CORPUS,files=$PRIVATE_FILES,status=measured"
  fi
}

DONE_KEYS="$WORK/done.keys"
STOPPED="$WORK/stopped.keys"
: > "$DONE_KEYS"
: > "$STOPPED"
if [ -n "$RESUME" ] && [ -s "$OUT" ]; then
  awk -F, '
    function kv(s, k,    n, i, parts) {
      n = split(s, parts, ",")
      for (i = 1; i <= n; i++) if (index(parts[i], k "=") == 1) return substr(parts[i], length(k) + 2)
      return ""
    }
    /^ROW,/ {
      head = $0; sub(/,out=.*$/, "", head)
      printf "%s|%s|%s|%s|%s|%s|%s|%s|%s\n", kv(head,"sha"), kv(head,"op"), kv(head,"ocp"), kv(head,"variant"), kv(head,"mem"), kv(head,"rep"), kv(head,"par"), kv(head,"lp"), kv(head,"quality") > "'"$DONE_KEYS"'"
      oom = kv(head, "oom_kill"); to = kv(head, "timeout"); rc = kv(head, "rc")
      if ((oom != "0" && oom != "na" && oom != "") || to == "1" || rc == "137")
        printf "%s|%s|%s|%s|%s|%s\n", kv(head,"fmt"), kv(head,"op"), kv(head,"mem"), kv(head,"ocp"), kv(head,"series"), kv(head,"bytes") > "'"$STOPPED"'"
    }
    /^LADDER_STOP,/ {
      if (kv($0, "reason") == "oom" || kv($0, "reason") == "timeout")
        printf "%s|%s|%s|%s|%s|%s\n", kv($0,"fmt"), kv($0,"op"), kv($0,"mem"), kv($0,"ocp"), kv($0,"series"), kv($0,"at_bytes") > "'"$STOPPED"'"
    }' "$OUT"
fi

FLOOR_TSV="$WORK/floor.tsv"
: > "$FLOOR_TSV"
if [ -n "$FLOOR_FROM" ]; then
  awk -F, '
    function kv(s, k,    n, i, parts) {
      n = split(s, parts, ",")
      for (i = 1; i <= n; i++) if (index(parts[i], k "=") == 1) return substr(parts[i], length(k) + 2)
      return ""
    }
    function num(x) { return (x ~ /^[0-9.]+$/) ? x + 0 : -1 }
    /^ROW,/ {
      head = $0; sub(/,out=.*$/, "", head)
      if (kv(head, "mem") != "4g") next
      key = kv(head, "sha") "|" kv(head, "op")
      if (kv(head, "rc") != "0") { r = 99999999 } else {
        c = num(kv(head, "child_rss_mib")); g = num(kv(head, "cg_peak_mib")); r = (c > g) ? c : g
      }
      if (!(key in best) || r > best[key]) best[key] = r
    }
    END { for (k in best) printf "%s|%s\n", k, best[k] }' "$FLOOR_FROM" > "$FLOOR_TSV"
fi

PY=(/opt/venv/bin/python -I -B)
LIB=/opt/cad-measure

normalize_fmt() {
  case "$1" in
    stp|step) echo step ;;
    igs|iges) echo iges ;;
    *) echo "$1" ;;
  esac
}

mem_mib() {
  case "$1" in
    *g) echo $(( ${1%g} * 1024 )) ;;
    *m) echo "${1%m}" ;;
    *) echo "$1" ;;
  esac
}

newdir() {
  local d
  d=$(mktemp -d "$WORK/cell.XXXXXX")
  chmod 0777 "$d"
  printf '%s\n' "$d"
}

docker_cell() {
  local mem=$1 jobdir=$2 file=$3 ext=$4 emit_text=$5 name st
  shift 5
  name="cadm.$$.$RANDOM.$RANDOM"
  docker run --name "$name" --network none --cpus "$CPUS" --memory "$mem" --memory-swap "$mem" --user nobody \
    -v "$jobdir:/job" -v "$file:/job/in.$ext:ro" \
    ${PROBE_MOUNT[@]+"${PROBE_MOUNT[@]}"} ${PROBE_ENV[@]+"${PROBE_ENV[@]}"} \
    -e "CAD_EMIT_TEXT=$emit_text" \
    "$TAG" "$@" || true
  st=$(docker inspect "$name" --format 'exit{{.State.ExitCode}}_oom{{.State.OOMKilled}}' 2>/dev/null || echo exitna_oomna)
  printf '%s\n' "$st" > "$WORK/last_state"
  docker rm -f "$name" >/dev/null 2>&1 || true
  return 0
}

field() {
  local h=",$1," v
  v=${h#*,$2=}
  [ "$v" != "$h" ] || { echo ""; return 0; }
  printf '%s\n' "${v%%,*}"
}

rss_eff() {
  awk -v c="$1" -v g="$2" 'BEGIN { c = (c ~ /^[0-9.]+$/) ? c + 0 : -1; g = (g ~ /^[0-9.]+$/) ? g + 0 : -1; print (c > g) ? c : g }'
}

stop_reason() {
  local head=$1 rc oom to
  rc=$(field "$head" rc); oom=$(field "$head" oom_kill); to=$(field "$head" timeout)
  if [ "$oom" != 0 ] && [ "$oom" != na ] && [ -n "$oom" ]; then echo oom
  elif [ "$to" = 1 ]; then echo timeout
  fi
}

is_stopped_any() { grep -Fq "$1|$2|$3|$OCP|$4|" "$STOPPED"; }
is_stopped() {
  awk -F'|' -v k="$1|$2|$3|$OCP|$4" -v b="$5" '
    ($1 "|" $2 "|" $3 "|" $4 "|" $5) == k && b + 0 >= $6 + 0 { found = 1 }
    END { exit !found }' "$STOPPED"
}
mark_stopped() { printf '%s|%s|%s|%s|%s|%s\n' "$1" "$2" "$3" "$OCP" "$4" "$5" >> "$STOPPED"; }

is_done() { grep -Fxq "$1|$2|$OCP|$VARIANT|$3|$4|$PARALLEL|$LP|$QUALITY" "$DONE_KEYS"; }

below_floor() {
  local sha=$1 op=$2 mem=$3 rss lim
  [ -n "$FLOOR_FROM" ] || return 1
  rss=$(grep -F "$sha|$op|" "$FLOOR_TSV" | head -n 1 | cut -d'|' -f3)
  [ -n "$rss" ] || return 1
  lim=$(mem_mib "$mem")
  awk -v r="$rss" -v l="$lim" -v f="$FLOOR_FRAC" 'BEGIN { exit !(r < f * l) }'
}

dist_line() {
  awk -F, -v SHA="$1" -v OP="$2" -v MEM="$3" -v OCP="$OCP" -v VAR="$VARIANT" -v PAR="$PARALLEL" -v LP="$LP" -v Q="$QUALITY" '
    function kv(s, k,    n, i, parts) {
      n = split(s, parts, ",")
      for (i = 1; i <= n; i++) if (index(parts[i], k "=") == 1) return substr(parts[i], length(k) + 2)
      return ""
    }
    function num(x) { return (x ~ /^[0-9.]+$/) ? x + 0 : -1 }
    function sortn(a, n,    i, j, t) {
      for (i = 2; i <= n; i++) { t = a[i]; j = i - 1; while (j > 0 && a[j] > t) { a[j+1] = a[j]; j-- } a[j+1] = t }
    }
    /^ROW,/ {
      head = $0; sub(/,out=.*$/, "", head)
      if (kv(head,"sha") != SHA || kv(head,"op") != OP || kv(head,"mem") != MEM || kv(head,"ocp") != OCP) next
      if (kv(head,"variant") != VAR || kv(head,"par") != PAR || kv(head,"lp") != LP || kv(head,"quality") != Q) next
      if (kv(head,"rc") != "0") { bad++; next }
      n++
      wall[n] = kv(head,"wall_s") + 0
      us[n] = kv(head,"utime_s") + kv(head,"stime_s")
      c = num(kv(head,"child_rss_mib")); g = num(kv(head,"cg_peak_mib"))
      rss[n] = (c > g) ? c : g
      thr[n] = num(kv(head,"threads_max"))
    }
    END {
      if (n == 0) exit
      sortn(wall, n); sortn(us, n); sortn(rss, n); sortn(thr, n)
      rank = int((n * 95 + 99) / 100); if (rank < 1) rank = 1
      med = int((n + 1) / 2)
      printf "n=%d,n_failed=%d,wall_min=%.2f,wall_median=%.2f,wall_p95=%.2f,wall_max=%.2f,rss_max_mib=%d,usersys_median=%.2f,threads_max=%d",
        n, bad, wall[1], wall[med], wall[rank], wall[n], rss[n], us[med], thr[n]
    }' "$OUT"
}

COMBOS="$WORK/combos.tsv"
: > "$COMBOS"

flush_series() {
  local series=$1
  [ -n "$series" ] || return 0
  awk -F'|' -v S="$series" '
    { k = $1 "|" $2 "|" $3; if ($4 + 0 > top[k] + 0) top[k] = $4; ran[k] += $5; skipped[k] += $6 }
    END { for (k in top) printf "%s|%s|%s|%s\n", k, top[k], ran[k], skipped[k] }' "$COMBOS" | LC_ALL=C sort |
  while IFS='|' read -r fmt op mem top ran skipped; do
    if ! is_stopped_any "$fmt" "$op" "$mem" "$series"; then
      if [ "$ran" -gt 0 ]; then
        emit "LADDER_STOP,fmt=$fmt,op=$op,mem=$mem,ocp=$OCP,series=$series,reason=top,at_bytes=$top"
      else
        emit "LADDER_STOP,fmt=$fmt,op=$op,mem=$mem,ocp=$OCP,series=$series,reason=top,at_bytes=$top,floor_skipped=$skipped"
      fi
    fi
  done
  : > "$COMBOS"
}

combo_note() {
  printf '%s|%s|%s|%s|%s|%s\n' "$1" "$2" "$3" "$4" "$5" "$6" >> "$COMBOS"
}

START_EPOCH=$(date +%s)
ROWS=0
FAILED=0
OOM=0
TIMEOUTS=0
NOWRAP=0
FLAKES=0
RESUMED=0
ACCOUNTED=0
CELL=0
PRE_FILE=""
PRE_DIR=""
PRE_STATUS=""
CUR_SERIES=""
CHUNK_END=0

premesh() {
  local file=$1 ext=$2 sha=$3 wrap rc head attempt
  if [ "$PRE_FILE" = "$sha" ] && [ "$PRE_STATUS" != flake ]; then return 0; fi
  [ -z "$PRE_DIR" ] || rm -rf "$PRE_DIR"
  PRE_FILE=$sha
  PRE_DIR=$(newdir)
  PRE_STATUS=fail
  attempt=1
  while [ "$attempt" -le 3 ]; do
    rm -rf "${PRE_DIR:?}"/* 2>/dev/null || true
    wrap=$(docker_cell "$PREMEM" "$PRE_DIR" "$file" "$ext" 0 \
             "${PY[@]}" "$LIB/measure_wrap.py" --budget-s "$BUDGET" --outdir /job -- \
             "${PY[@]}" "$LIB/cad_mesh.py" mesh "/job/in.$ext" /job --quality "$QUALITY" --parallel "$PARALLEL" \
           | grep -a '^WRAP,' | tail -n 1 || true)
    if [ -n "$wrap" ]; then
      head=${wrap#WRAP,}; head=${head%%,out=*}
      rc=$(field "$head" rc)
      if [ "$rc" = 0 ] && [ -f "$PRE_DIR/part.stl" ]; then PRE_STATUS=ok; return 0; fi
      PRE_STATUS=$(stop_reason "$head")
      [ -n "$PRE_STATUS" ] || PRE_STATUS="rc$rc"
      return 0
    fi
    case "$(cat "$WORK/last_state" 2>/dev/null)" in
      *_oomtrue) PRE_STATUS=oom; return 0 ;;
    esac
    attempt=$((attempt + 1))
  done
  PRE_STATUS=flake
  return 0
}

PRESVG_FILE=""
PRESVG_DIR=""
PRESVG_STATUS=""

presvg() {
  local file=$1 ext=$2 sha=$3 wrap rc head attempt
  if [ "$PRESVG_FILE" = "$sha" ] && [ "$PRESVG_STATUS" != flake ]; then return 0; fi
  [ -z "$PRESVG_DIR" ] || rm -rf "$PRESVG_DIR"
  PRESVG_FILE=$sha
  PRESVG_DIR=$(newdir)
  PRESVG_STATUS=fail
  attempt=1
  while [ "$attempt" -le 3 ]; do
    rm -rf "${PRESVG_DIR:?}"/* 2>/dev/null || true
    wrap=$(docker_cell "$PREMEM" "$PRESVG_DIR" "$file" "$ext" 0 \
             "${PY[@]}" "$LIB/measure_wrap.py" --budget-s "$BUDGET" --outdir /job -- \
             "${PY[@]}" "$LIB/cad_dxf.py" "/job/in.$ext" /job \
           | grep -a '^WRAP,' | tail -n 1 || true)
    if [ -n "$wrap" ]; then
      head=${wrap#WRAP,}; head=${head%%,out=*}
      rc=$(field "$head" rc)
      if [ "$rc" = 0 ] && [ -f "$PRESVG_DIR/out.svg" ]; then PRESVG_STATUS=ok; return 0; fi
      PRESVG_STATUS=$(stop_reason "$head")
      [ -n "$PRESVG_STATUS" ] || PRESVG_STATUS="rc$rc"
      return 0
    fi
    case "$(cat "$WORK/last_state" 2>/dev/null)" in
      *_oomtrue) PRESVG_STATUS=oom; return 0 ;;
    esac
    attempt=$((attempt + 1))
  done
  PRESVG_STATUS=flake
  return 0
}

measure_cell() {
  local op=$1 mem=$2 rep=$3
  local attempt=1 wrap="" jobdir="" pix pre_ok inner stl_in confined=0 state=na cstate=ok
  CELL_STOP=""
  CELL_FLAKE=0
  CELL_ROW_RC=""
  while [ "$attempt" -le 3 ] && [ -z "$wrap" ]; do
    jobdir=$(newdir)
    pix=-
    pre_ok=1
    case "$op" in
      mesh)
        inner=("${PY[@]}" "$LIB/cad_mesh.py" mesh "/job/in.$ext" /job --quality "$QUALITY" --parallel "$PARALLEL") ;;
      stlstats)
        inner=("${PY[@]}" "$LIB/cad_mesh.py" stlstats "/job/in.$ext" /job) ;;
      dxf)
        inner=("${PY[@]}" "$LIB/cad_dxf.py" "/job/in.$ext" /job) ;;
      rsvg-png|rsvg-pdf)
        presvg "$file" "$ext" "$sha"
        if [ "$PRESVG_STATUS" = ok ]; then
          ln "$PRESVG_DIR/out.svg" "$jobdir/out.svg" 2>/dev/null || cp "$PRESVG_DIR/out.svg" "$jobdir/out.svg"
        else
          pre_ok=0
          PRE_REASON=$PRESVG_STATUS
        fi
        if [ "$op" = rsvg-png ]; then
          inner=(rsvg-convert -f png -w 1280 -h 1280 --keep-aspect-ratio -b '#FFFFFF' /job/out.svg -o /job/out.png)
        else
          inner=(rsvg-convert -f pdf /job/out.svg -o /job/out.pdf)
        fi ;;
      render)
        if [ "$fmt" = step ] || [ "$fmt" = iges ]; then
          premesh "$file" "$ext" "$sha"
          if [ "$PRE_STATUS" = ok ]; then
            ln "$PRE_DIR/part.stl" "$jobdir/part.stl" 2>/dev/null || cp "$PRE_DIR/part.stl" "$jobdir/part.stl"
          else
            pre_ok=0
            PRE_REASON=$PRE_STATUS
          fi
          stl_in=/job/part.stl
        else
          stl_in="/job/in.$ext"
        fi
        inner=("${PY[@]}" "$LIB/cad_render.py" "$stl_in" /job/render.png --lp-threads "$LP") ;;
    esac
    if [ -n "$CONFINE" ]; then
      inner=(/probe/sandbox-probe "${inner[@]}")
      confined=1
    else
      confined=0
    fi

    if [ "$pre_ok" = 0 ]; then
      rm -rf "$jobdir"
      break
    fi
    if [ "$pre_ok" = 1 ]; then
      wrap=$(docker_cell "$mem" "$jobdir" "$file" "$ext" "$emit_text" \
               "${PY[@]}" "$LIB/measure_wrap.py" --budget-s "$BUDGET" --outdir /job -- "${inner[@]}" \
             | grep -a '^WRAP,' | tail -n 1 || true)
      state=$(cat "$WORK/last_state" 2>/dev/null || echo exitna_oomna)
      if [ -n "$wrap" ]; then
        local h rc0 oom0 to0
        h=${wrap#WRAP,}; h=${h%%,out=*}
        rc0=$(field "$h" rc); oom0=$(field "$h" oom_kill); to0=$(field "$h" timeout)
        if [ "$rc0" = 137 ] && [ "$oom0" = 0 ] && [ "$to0" = 0 ] && [ "$attempt" -lt 3 ]; then
          echo "retry: cell $CELL (op=$op mem=$mem rep=$rep) rc=137 with oom_kill=0 and no timeout, attempt $attempt" >&2
          wrap=""
        fi
      else
        case "$state" in
          *_oomtrue)
            wrap="WRAP,rc=137,wall_s=na,utime_s=na,stime_s=na,child_rss_mib=na,cg_peak_mib=na,oom_kill=1,timeout=0,threads_max=na,out=-"
            cstate=wraplost-$state ;;
        esac
      fi
    fi
    if [ -z "$wrap" ]; then
      echo "retry: cell $CELL (op=$op mem=$mem rep=$rep) attempt $attempt produced no WRAP line ($state)" >&2
      rm -rf "$jobdir"
      attempt=$((attempt + 1))
    fi
  done

  if [ -z "$wrap" ] && [ "$pre_ok" = 0 ]; then
    CELL_STOP=prestep
    return 0
  fi
  if [ -z "$wrap" ]; then
    CELL_FLAKE=1
    return 0
  fi

  wrap=${wrap#WRAP,}
  local head out rc
  head=${wrap%%,out=*}
  out=${wrap#*,out=}
  rc=$(field "$head" rc)
  CELL_ROW_RC=$rc

  case "$op" in
    render|rsvg-png)
      pix=fail
      local png pmode pixline
      if [ "$op" = render ]; then png=/job/render.png; pmode=part; else png=/job/out.png; pmode=drawing; fi
      if [ "$rc" = 0 ] && [ -f "$jobdir/${png#/job/}" ]; then
        pixline=$(docker run --rm --network none --user nobody -v "$jobdir:/job:ro" "$TAG" \
                    "${PY[@]}" "$LIB/pixelcheck.py" single "$png" --mode "$pmode" 2>/dev/null | grep -a '^PIX,' | tail -n 1 || true)
        if [ -n "$pixline" ]; then
          emit "$pixline"
          case "$pixline" in *,ok=1) pix=ok ;; esac
        fi
      fi ;;
    rsvg-pdf)
      pix=fail
      if [ "$rc" = 0 ] && [ -s "$jobdir/out.pdf" ]; then
        local magic
        magic=$(head -c 5 "$jobdir/out.pdf")
        emit "PDFCHK,bytes=$(wc -c < "$jobdir/out.pdf" | tr -d ' '),magic=$([ "$magic" = "%PDF-" ] && echo ok || echo bad)"
        [ "$magic" = "%PDF-" ] && pix=ok
      fi ;;
  esac

  emit "ROW,src=$src,name=$name,sha=$sha,fmt=$fmt,bytes=$bytes,op=$op,ocp=$OCP,variant=$VARIANT,mem=$mem,cpus=$CPUS,confined=$confined,rep=$rep,$head,pix=$pix,cstate=$cstate,par=$PARALLEL,lp=$LP,quality=$QUALITY,series=$series,out=$out"

  ROWS=$((ROWS + 1))
  if [ "$rc" != 0 ] || [ "$pix" = fail ]; then FAILED=$((FAILED + 1)); fi
  case "$head" in *,oom_kill=0,*|*,oom_kill=na,*) ;; *) OOM=$((OOM + 1)) ;; esac
  case "$head" in *,timeout=1,*) TIMEOUTS=$((TIMEOUTS + 1)) ;; esac

  CELL_STOP=$(stop_reason "$head")
  if [ -z "$CELL_STOP" ] && [ "$rc" = 137 ]; then CELL_STOP=oom; fi
  rm -rf "$jobdir"
  return 0
}

PRE_REASON=""

while IFS="$(printf '\t')" read -r -u 3 series bytes origin file; do
  if [ "$series" != "$CUR_SERIES" ]; then
    [ "$LADDER" = 0 ] || flush_series "$CUR_SERIES"
    CUR_SERIES=$series
  fi
  ext=$(printf '%s' "${file##*.}" | tr '[:upper:]' '[:lower:]')
  fmt=$(normalize_fmt "$ext")
  if [ "$origin" = public ]; then
    src=public
    name=$(basename "$file" | tr ',' '_')
    emit_text=1
  else
    src=private
    name=-
    emit_text=0
  fi
  sha=$(shasum -a 256 "$file" | awk '{print $1}')

  IFS=, read -r -a ops <<< "$OPS"
  IFS=, read -r -a mems <<< "$MEMS"

  applicable=()
  for op in "${ops[@]}"; do
    case "$fmt:$op" in
      step:mesh|step:render|iges:mesh|iges:render|stl:render|stl:stlstats|dxf:dxf|dxf:rsvg-png|dxf:rsvg-pdf) applicable+=("$op") ;;
      *) [ "$LADDER" = 1 ] || emit "SKIP,src=$src,name=$name,fmt=$fmt,op=$op,reason=op-not-for-format" ;;
    esac
  done
  [ ${#applicable[@]} -gt 0 ] || continue

  noise_n=$(docker ps -q | wc -l | tr -d ' ')
  noise_cpu=$(docker stats --no-stream --format '{{.CPUPerc}}' 2>/dev/null | tr -d '%' | sort -n | tail -n 1)
  emit "NOISE,containers=${noise_n},top_cpu=${noise_cpu:-na}"

  for op in "${applicable[@]}"; do
    for mem in "${mems[@]}"; do
      if [ "$LADDER" = 1 ] && is_stopped "$fmt" "$op" "$mem" "$series" "$bytes"; then
        ACCOUNTED=$((ACCOUNTED + 1))
        continue
      fi
      if [ "$LADDER" = 1 ] && below_floor "$sha" "$op" "$mem"; then
        combo_note "$fmt" "$op" "$mem" "$bytes" 0 1
        ACCOUNTED=$((ACCOUNTED + 1))
        continue
      fi
      rep=1
      measured=0
      fresh=0
      while [ "$rep" -le "$REPEATS" ]; do
        if is_done "$sha" "$op" "$mem" "$rep"; then
          RESUMED=$((RESUMED + 1))
          measured=1
          rep=$((rep + 1))
          continue
        fi
        if [ "$MAX_WALL" -gt 0 ] && [ $(( $(date +%s) - START_EPOCH )) -ge "$MAX_WALL" ]; then
          CHUNK_END=1
          break 4
        fi
        CELL=$((CELL + 1))
        measure_cell "$op" "$mem" "$rep"
        measured=1
        fresh=1
        if [ "$CELL_STOP" = prestep ]; then
          emit "SKIP,src=$src,name=$name,fmt=$fmt,op=$op,mem=$mem,rep=$rep,reason=prestep-failed-$PRE_REASON"
          if [ "$LADDER" = 1 ] && { [ "$PRE_REASON" = oom ] || [ "$PRE_REASON" = timeout ]; }; then
            emit "LADDER_STOP,fmt=$fmt,op=$op,mem=$mem,ocp=$OCP,series=$series,reason=$PRE_REASON,at_bytes=$bytes,via=prestep"
            mark_stopped "$fmt" "$op" "$mem" "$series" "$bytes"
          fi
          break
        fi
        if [ "$CELL_FLAKE" = 1 ]; then
          FLAKES=$((FLAKES + 1))
          emit "FLAKE,src=$src,name=$name,sha=$sha,fmt=$fmt,op=$op,mem=$mem,rep=$rep,reason=no-wrap-after-3-attempts,not_a_breaking_point=1"
          NOWRAP=$((NOWRAP + 1))
          rep=$((rep + 1))
          continue
        fi
        if [ -n "$CELL_STOP" ] && [ "$LADDER" = 1 ]; then
          emit "LADDER_STOP,fmt=$fmt,op=$op,mem=$mem,ocp=$OCP,series=$series,reason=$CELL_STOP,at_bytes=$bytes"
          mark_stopped "$fmt" "$op" "$mem" "$series" "$bytes"
          break
        fi
        rep=$((rep + 1))
      done
      if [ "$REPEATS" -gt 1 ] && [ "$fresh" = 1 ]; then
        dist=$(dist_line "$sha" "$op" "$mem")
        [ -z "$dist" ] || emit "DIST,sha=$sha,name=$name,fmt=$fmt,bytes=$bytes,op=$op,mem=$mem,ocp=$OCP,$dist,par=$PARALLEL,lp=$LP,quality=$QUALITY,series=$series"
      fi
      if [ "$LADDER" = 1 ] && [ "$measured" = 1 ]; then
        combo_note "$fmt" "$op" "$mem" "$bytes" 1 0
      fi
    done
  done
done 3< "$SORTED"

if [ "$CHUNK_END" = 0 ]; then
  [ "$LADDER" = 0 ] || flush_series "$CUR_SERIES"
fi

[ -z "$PRE_DIR" ] || rm -rf "$PRE_DIR"
[ -z "$PRESVG_DIR" ] || rm -rf "$PRESVG_DIR"

emit "SUMMARY,rows=$ROWS,failed=$FAILED,oom=$OOM,timeout=$TIMEOUTS,flakes=$FLAKES,resumed_skips=$RESUMED"

if [ "$CHUNK_END" = 1 ]; then
  emit "CHUNK_END,reason=max-wall-s,elapsed_s=$(( $(date +%s) - START_EPOCH ))"
  exit 75
fi

if [ "$NOWRAP" -gt 0 ]; then
  exit 1
fi
if [ "$ROWS" -eq 0 ] && [ "$RESUMED" -eq 0 ] && [ "$ACCOUNTED" -eq 0 ]; then
  exit 1
fi
