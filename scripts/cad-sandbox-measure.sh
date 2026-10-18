#!/usr/bin/env bash
# cad-sandbox-measure.sh -- Phase 71 (EVAL-01, D-16) Landlock path sets of the
# four CAD helpers (cad_mesh, cad_render, cad_dxf, rsvg-convert) on the final
# native arm64 image (pinned OCP, pruned VTK, USER nobody).
#
# Usage:
#   scripts/cad-sandbox-measure.sh [--out FILE]
#   scripts/cad-sandbox-measure.sh --recheck [--out FILE]
#
# Steps: trace each matrix input with strace (scripts/sandbox-paths-measure.sh
# trace) and collapse the paths into candidate sets; prove sufficiency under a
# real ruleset (scripts/cmd/sandbox-probe) by CONTENT against an unconfined run;
# prove effectiveness with an input outside every grant; remove entries one at a
# time to find the minimal set. Every set is a candidate for phase 73, not a
# grant.
#
# The helpers run in production argv form (/opt/venv/bin/python -I -B ...),
# without --dump-maps and without LD_DEBUG, and the container environment is
# only HOME, LANG and PROBE_*.
#
# --recheck    one input per helper with the LANDLOCK_MINIMAL sets of the last
#              evidence file, without and with the ruleset.
#
# Exit codes: 64 usage, 69 docker missing, 1 a result is not identical, a probe
# or measurement failure, or an emulated image.

set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"
. "$REPO_ROOT/scripts/cad-measure/verdicts.sh"

EVID="$REPO_ROOT/.planning/milestones/v1.16-phases/71-evaluation-gate/evidence"
CORPUS="$REPO_ROOT/internal/convert/testdata/cad"
LIMITS_FILE="$EVID/71-06-limits.txt"
MAIN_EVIDENCE="$EVID/71-07-landlock.txt"
PY=(/opt/venv/bin/python -I -B)
LIB=/opt/cad-measure
LIMITS=(--network none --cpus 2 --memory 2g --memory-swap 2g --user nobody)

OUT=""
RECHECK=0
usage() {
  sed -n '/^# Usage:/,/^# Steps:/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 64
}
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT=${2:?}; shift 2 ;;
    --recheck) RECHECK=1; shift ;;
    -h|--help) usage ;;
    *) echo "error: unknown argument $1" >&2; usage ;;
  esac
done
if [ -z "$OUT" ]; then
  if [ "$RECHECK" = 1 ]; then OUT="${TMPDIR:-/tmp}/71-07-recheck.txt"; else OUT=$MAIN_EVIDENCE; fi
fi

command -v docker >/dev/null 2>&1 || { echo "error: docker is required" >&2; exit 69; }
command -v go >/dev/null 2>&1 || { echo "error: go is required to build the probe" >&2; exit 69; }
[ -s "$LIMITS_FILE" ] || { echo "error: $LIMITS_FILE is missing" >&2; exit 1; }
PIN=$(sed -n 's/^PIN_DECISION,choice=\([^,]*\),.*/\1/p' "$LIMITS_FILE")
LP=$(sed -n 's/^THREADS_DECISION,lp_num_threads=\([^,]*\),.*/\1/p' "$LIMITS_FILE")
PAR=$(sed -n 's/^THREADS_DECISION,lp_num_threads=[^,]*,occt_parallel=\([^,]*\),.*/\1/p' "$LIMITS_FILE")
[ -n "$PIN" ] && [ -n "$LP" ] && [ -n "$PAR" ] || { echo "error: PIN_DECISION or THREADS_DECISION missing" >&2; exit 1; }

if [ "$RECHECK" = 1 ]; then
  [ -s "$MAIN_EVIDENCE" ] || { echo "error: $MAIN_EVIDENCE is missing; run the main measurement first" >&2; exit 1; }
  grep '^LANDLOCK_MINIMAL,' "$MAIN_EVIDENCE" > "${TMPDIR:-/tmp}/cad-minimal.$$"
fi

mkdir -p "$(dirname "$OUT")"
: > "$OUT"
emit() { printf '%s\n' "$*" | tee -a "$OUT"; }
note() { printf '%s\n' "$*" >> "$OUT"; printf '%s\n' "$*" >&2; }

TAG=$(cad_build_image "$PIN" pruned)
cad_require_native "$TAG" || exit 1
IMAGE_ARCH=$(docker image inspect "$TAG" --format '{{.Architecture}}')
if [ "$RECHECK" = 1 ] && ! grep -q "^HDR,pin=$PIN,variant=pruned,lp_threads=$LP,occt_parallel=$PAR,image_arch=$IMAGE_ARCH," "$MAIN_EVIDENCE"; then
  echo "error: $MAIN_EVIDENCE was not measured at pin $PIN, lp_threads $LP, occt_parallel $PAR on $IMAGE_ARCH; its minimal sets say nothing about this configuration" >&2
  exit 1
fi

WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-landlock.XXXXXX")
trap 'rm -rf "$WORK" "${TMPDIR:-/tmp}/cad-minimal.$$"' EXIT
mkdir -p "$WORK/probe" "$WORK/ref" "$WORK/svg" "$WORK/stl" "$WORK/trace"
chmod 0755 "$WORK" "$WORK/probe" "$WORK/svg" "$WORK/stl"
CGO_ENABLED=0 GOOS=linux GOARCH="$IMAGE_ARCH" go build -o "$WORK/probe/sandbox-probe" ./scripts/cmd/sandbox-probe
chmod 0755 "$WORK/probe/sandbox-probe"

newdir() {
  local d
  d=$(mktemp -d "$WORK/job.XXXXXX")
  chmod 0777 "$d"
  printf '%s\n' "$d"
}

SIG_PY='import hashlib, json, os, re, sys
op, job = sys.argv[1], sys.argv[2]
def sha(path):
    with open(path, "rb") as fh:
        return hashlib.sha256(fh.read()).hexdigest()
def present(name):
    path = os.path.join(job, name)
    return os.path.isfile(path) and os.path.getsize(path) > 0
def meas(path):
    with open(path) as fh:
        data = json.load(fh)
    data = {k: v for k, v in data.items() if not k.startswith("t_")}
    return hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest()
if op in ("mesh", "stlstats", "dxf"):
    if not present("measure.json"):
        print("missing"); sys.exit(0)
    sig = "measure=" + meas(os.path.join(job, "measure.json"))
    if op == "mesh":
        sig += ";stl=" + (sha(os.path.join(job, "part.stl")) if present("part.stl") else "none")
    if op == "dxf":
        if not present("out.svg"):
            print("missing"); sys.exit(0)
        sig += ";svg=" + sha(os.path.join(job, "out.svg"))
    print(sig)
elif op == "render":
    print("png=" + sha(os.path.join(job, "out.png")) if present("out.png") else "missing")
elif op == "rsvg-png":
    print("png=" + sha(os.path.join(job, "out.png")) if present("out.png") else "missing")
elif op == "rsvg-pdf":
    if not present("out.pdf"):
        print("missing"); sys.exit(0)
    with open(os.path.join(job, "out.pdf"), "rb") as fh:
        data = fh.read()
    data = re.sub(rb"/(CreationDate|ModDate)\s*\([^)]*\)", rb"/\1()", data)
    print("pdf=" + hashlib.sha256(data).hexdigest())'

COLLAPSE_PY='import re, sys
BROAD = {"/etc", "/usr/bin", "/usr/sbin", "/bin", "/sbin", "/usr/lib", "/lib", "/usr", "/", "/usr/share",
         "/var", "/dev", "/proc", "/sys", "/run", "/usr/local"}
def entry(p):
    parts = p.split("/")[1:]
    if p.startswith("/opt/venv/"): return "/opt/venv"
    if p.startswith("/opt/cad-measure/"): return "/opt/cad-measure"
    if p.startswith("/usr/lib/python3"): return "/usr/lib/" + parts[2]
    if p.startswith("/proc/") or p.startswith("/dev/"): return p
    if p.startswith("/sys/devices/system/cpu/"): return "/sys/devices/system/cpu"
    if p.startswith("/sys/"): return p
    if p.startswith("/usr/share/") and len(parts) > 3: return "/usr/share/" + parts[2]
    if p.startswith("/etc/") and len(parts) > 2: return "/etc/" + parts[1]
    if p.startswith("/usr/lib/") and len(parts) > 3:
        a = parts[2]
        if a.endswith("-linux-gnu"):
            return "/usr/lib/" + a if len(parts) == 4 else "/usr/lib/" + a + "/" + parts[3]
        return "/usr/lib/" + a
    d = p.rsplit("/", 1)[0] or "/"
    return p if d in BROAD else d
entries, tmp = set(), set()
for line in open(sys.argv[1]):
    line = line.rstrip("\n")
    if not line.startswith("  /"):
        continue
    path = re.sub(r"^/proc/[0-9]+/", "/proc/self/", line.strip().split(" -> ")[-1])
    if path.startswith("/tmp/") or path == "/tmp":
        tmp.add(path); continue
    entries.add(entry(path))
for extra in sys.argv[2:]:
    entries.add(extra)
dirs = sorted(e for e in entries if not any(o != e and e.startswith(o + "/") for o in entries))
for e in dirs: print("ENTRY " + e)
for t in sorted(tmp): print("TMP " + t)'

: > "$WORK/matrix.tsv"
add_case() { printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" "$5" "$6" "$7" >> "$WORK/matrix.tsv"; }

ARGV=()
set_argv() {
  local op=$1 ext=$2 extra=$3 view size bg
  case "$op" in
    mesh) ARGV=("${PY[@]}" "$LIB/cad_mesh.py" mesh "/job/in.$ext" /job --parallel "$PAR") ;;
    stlstats) ARGV=("${PY[@]}" "$LIB/cad_mesh.py" stlstats /job/in.stl /job) ;;
    render)
      IFS=: read -r view size bg <<< "$extra"
      ARGV=("${PY[@]}" "$LIB/cad_render.py" /job/in.stl /job/out.png --view "$view" --size "$size" --background "$bg" --lp-threads "$LP") ;;
    dxf) ARGV=("${PY[@]}" "$LIB/cad_dxf.py" /job/in.dxf /job) ;;
    rsvg-png) ARGV=(rsvg-convert -f png -w 1280 -h 1280 --keep-aspect-ratio -b '#FFFFFF' /job/in.svg -o /job/out.png) ;;
    rsvg-pdf) ARGV=(rsvg-convert -f pdf /job/in.svg -o /job/out.pdf) ;;
  esac
}

JOB=""; RC=0; LOG="$WORK/run.log"
EXTRA=()
exec_case() {
  local ro=$1 infile=$2 ext=$3 job rc=0
  job=$(newdir)
  if [ "$ro" = "-" ]; then
    docker run --rm "${LIMITS[@]}" -e HOME=/job/home -e LANG=C.UTF-8 \
      ${EXTRA[@]+"${EXTRA[@]}"} -v "$job:/job" ${infile:+-v "$infile:/job/in.$ext:ro"} "$TAG" "${ARGV[@]}" > "$LOG" 2>&1 </dev/null || rc=$?
  else
    docker run --rm "${LIMITS[@]}" -e HOME=/job/home -e LANG=C.UTF-8 \
      -e "PROBE_RO=$ro" -e PROBE_RW=/job \
      --entrypoint /probe/sandbox-probe -v "$WORK/probe:/probe:ro" \
      ${EXTRA[@]+"${EXTRA[@]}"} -v "$job:/job" ${infile:+-v "$infile:/job/in.$ext:ro"} "$TAG" "${ARGV[@]}" > "$LOG" 2>&1 </dev/null || rc=$?
  fi
  JOB=$job; RC=$rc
}

probe_failure() {
  if grep -aqE 'the ruleset confines nothing|is not readable before the ruleset|not a permission denial|^apply:' "$LOG"; then
    return 0
  fi
  return 1
}

signature() { python3 -c "$SIG_PY" "$1" "$2"; }
refkey() { printf '%s' "$1|$2" | shasum -a 256 | cut -c1-24; }

PIXEL=""
pixel_ok() {
  local job=$1 line
  line=$(docker run --rm --network none --user nobody -v "$job:/job:ro" "$TAG" \
           "${PY[@]}" "$LIB/pixelcheck.py" single /job/out.png --mode part 2>/dev/null </dev/null | grep -a '^PIX,' | tail -n 1 || true)
  case "$line" in *,ok=1) PIXEL=ok ;; *) PIXEL=fail ;; esac
}

CONTENT=""; CHECK=""; CONF_RC=0; ATTEMPTS=1
judge_case() {
  local ro=$1 helper=$2 op=$3 label=$4 ext=$5 infile=$6 extra=$7 ref sig attempt=1
  ref=$(cat "$WORK/ref/$(refkey "$helper" "$label")")
  set_argv "$op" "$ext" "$extra"
  while [ "$attempt" -le 3 ]; do
    exec_case "$ro" "$infile" "$ext"
    if probe_failure; then
      note "PROBE_FAIL,helper=$helper,input=$label,rc=$RC,message=$(grep -a -m1 -E 'the ruleset confines nothing|is not readable before|not a permission denial|^apply:' "$LOG" | tr ',' ' ')"
      CONTENT=probe_failure; CONF_RC=$RC; ATTEMPTS=$attempt
      rm -rf "$JOB"
      return 0
    fi
    sig=$(signature "$op" "$JOB")
    CONF_RC=$RC
    if [ "$sig" = "$ref" ]; then
      CONTENT=identical
      CHECK="sig_equal"
      if [ "$op" = render ]; then
        pixel_ok "$JOB"
        CHECK="sig_equal+pixelcheck_$PIXEL"
        [ "$PIXEL" = ok ] || CONTENT=DIFFERENT
      fi
      ATTEMPTS=$attempt
      rm -rf "$JOB"
      return 0
    fi
    if [ "$sig" = missing ]; then CONTENT=missing; CHECK="output_absent"; else CONTENT=DIFFERENT; CHECK="sig_differs"; fi
    if grep -aqiE 'permission|Fatal Python error|ModuleNotFoundError|ImportError|cannot open shared object' "$LOG"; then
      ATTEMPTS=$attempt
      rm -rf "$JOB"
      return 0
    fi
    note "RETRY,helper=$helper,input=$label,attempt=$attempt,rc=$RC,content=$CONTENT"
    rm -rf "$JOB"
    attempt=$((attempt + 1))
  done
  ATTEMPTS=3
}

make_ref() {
  local helper=$1 op=$2 label=$3 ext=$4 infile=$5 extra=$6 sig attempt=1
  set_argv "$op" "$ext" "$extra"
  while [ "$attempt" -le 3 ]; do
    exec_case - "$infile" "$ext"
    sig=$(signature "$op" "$JOB")
    if [ "$sig" != missing ]; then
      printf '%s' "$sig" > "$WORK/ref/$(refkey "$helper" "$label")"
      if [ "$op" = mesh ] && [ -s "$JOB/part.stl" ] && [ "$label" = slide ]; then cp "$JOB/part.stl" "$WORK/stl/slide.stl"; fi
      if [ "$op" = dxf ] && [ -s "$JOB/out.svg" ]; then cp "$JOB/out.svg" "$WORK/svg/$label.svg"; fi
      emit "REF,helper=$helper,input=$label,op=$op,rc=$RC,sig=${sig:0:60},attempts=$attempt"
      rm -rf "$JOB"
      return 0
    fi
    emit "RETRY,helper=$helper,input=$label,attempt=$attempt,rc=$RC,reason=unconfined-reference-has-no-output"
    rm -rf "$JOB"
    attempt=$((attempt + 1))
  done
  emit "FAIL,helper=$helper,input=$label,reason=unconfined reference run produced no output after 3 attempts"
  exit 1
}

emit "# Plan 71-07 Landlock matrix of the four CAD helpers: candidate sets for phase 73, not grants"
cad_evidence_header "$TAG" 2 2g | tee -a "$OUT"
emit "HDR,pin=$PIN,variant=pruned,lp_threads=$LP,occt_parallel=$PAR,image_arch=$IMAGE_ARCH,user=$(docker image inspect "$TAG" --format '{{.Config.User}}')"
emit "HDR,probe=scripts/cmd/sandbox-probe,env_in_container=HOME=/job/home;LANG=C.UTF-8;PROBE_RO;PROBE_RW,rw=/job"

PY_TARGET=$(docker run --rm --network none --entrypoint sh "$TAG" -c 'readlink -f /opt/venv/bin/python')
emit "HDR,venv_python_target=$PY_TARGET"

build_matrix() {
  local f ext
  for spec in "slide|public/LinearSlide-MGN15x-Block.step" "assembly|public/SC8UU.step" \
              "units_inch|synthetic/units_inch.step" "legacy_cp1251_step|synthetic/legacy_cp1251.step" \
              "surfaces_only|synthetic/surfaces_only.iges"; do
    IFS='|' read -r name rel <<< "$spec"
    f="$CORPUS/$rel"; ext=$(printf '%s' "${f##*.}" | tr '[:upper:]' '[:lower:]')
    small=0; case "$name" in units_inch|surfaces_only) small=1 ;; esac
    add_case cad_mesh mesh "$name" "$f" "$ext" "-" "$small"
  done
  if [ ! -f "$CAD_CACHE_DIR/fetched/freecad-mk8-hotend.iges" ]; then
    emit "FAIL,helper=cad_mesh,input=freecad-mk8-hotend,reason=not in the fetched corpus; run scripts/cad-corpus-fetch.sh, the matrix is not run without it"
    exit 1
  fi
  add_case cad_mesh mesh freecad-mk8-hotend "$CAD_CACHE_DIR/fetched/freecad-mk8-hotend.iges" iges "-" 0
  add_case cad_mesh stlstats nasa-hubble "$CORPUS/public/nasa-hubble-all-pieces.stl" stl "-" 0
  add_case cad_mesh stlstats stl_ascii_box "$CORPUS/synthetic/stl_ascii_box.stl" stl "-" 1
  add_case cad_mesh stlstats stl_binary_solid_header "$CORPUS/synthetic/stl_binary_solid_header.stl" stl "-" 0
  for stl in "slide|$WORK/stl/slide.stl" "nasa-hubble|$CORPUS/public/nasa-hubble-all-pieces.stl"; do
    IFS='|' read -r name f <<< "$stl"
    for v in iso:1280:white front:1280:white top:1280:white iso:1280:transparent iso:2048:white; do
      small=0
      case "$name:$v" in slide:iso:1280:white|nasa-hubble:top:1280:white) small=1 ;; esac
      add_case cad_render render "$name/$v" "$f" stl "$v" "$small"
    done
  done
  for spec in "hatches_2|public/hatches_2.dxf" "blocks_minsert|synthetic/blocks_minsert.dxf" \
              "legacy_cp1251_dxf|synthetic/legacy_cp1251.dxf" "columns_R2007|public/columns_R2007.dxf" \
              "dimension_nested|public/dimension_in_nested_blocks.dxf"; do
    IFS='|' read -r name rel <<< "$spec"
    small=0; case "$name" in hatches_2|legacy_cp1251_dxf) small=1 ;; esac
    add_case cad_dxf dxf "$name" "$CORPUS/$rel" dxf "-" "$small"
    add_case rsvg-convert rsvg-png "png/$name" "$WORK/svg/$name.svg" svg "-" "$small"
    add_case rsvg-convert rsvg-pdf "pdf/$name" "$WORK/svg/$name.svg" svg "-" "$small"
  done
}

prep_slide() {
  local f="$CORPUS/public/LinearSlide-MGN15x-Block.step"
  make_ref cad_mesh mesh slide step "$f" "-" >/dev/null
}

if [ "$RECHECK" = 1 ]; then
  prep_slide
  RECHECKED=0
  while IFS= read -r spec; do
    helper=$(printf '%s' "$spec" | sed -n 's/^LANDLOCK_MINIMAL,helper=\([^,]*\),.*/\1/p')
    ro=$(printf '%s' "$spec" | sed -n 's/^.*,ro=\([^,]*\)\(,note=.*\)\{0,1\}$/\1/p')
    case "$helper" in
      cad_mesh) op=mesh; label=units_inch; ext=step; infile="$CORPUS/synthetic/units_inch.step"; extra="-" ;;
      cad_render) op=render; label="slide/iso:1280:white"; ext=stl; infile="$WORK/stl/slide.stl"; extra="iso:1280:white" ;;
      cad_dxf) op=dxf; label=hatches_2; ext=dxf; infile="$CORPUS/public/hatches_2.dxf"; extra="-" ;;
      rsvg-convert) op=rsvg-png; label=png/hatches_2; ext=svg; extra="-"
        make_ref cad_dxf dxf hatches_2 dxf "$CORPUS/public/hatches_2.dxf" "-" >/dev/null
        infile="$WORK/svg/hatches_2.svg" ;;
      *) continue ;;
    esac
    make_ref "$helper" "$op" "$label" "$ext" "$infile" "$extra" >/dev/null
    judge_case "$ro" "$helper" "$op" "$label" "$ext" "$infile" "$extra"
    emit "RECHECK,helper=$helper,input=$label,content=$CONTENT,confined_rc=$CONF_RC"
    [ "$CONTENT" = identical ] || exit 1
    RECHECKED=$((RECHECKED + 1))
  done < "${TMPDIR:-/tmp}/cad-minimal.$$"
  [ "$RECHECKED" = 4 ] || { emit "FAIL,reason=the recheck covered $RECHECKED helpers, expected 4"; exit 1; }
  exit 0
fi

build_matrix

ro_of() { paste -sd: "$1"; }
without() { grep -vxF "$2" "$1" || true; }

IDX=0
INPUTS=0
while IFS="$(printf '\t')" read -r -u 3 helper op label file ext extra small; do
  IDX=$((IDX + 1))
  INPUTS=$((INPUTS + 1))
  make_ref "$helper" "$op" "$label" "$ext" "$file" "$extra"
  set_argv "$op" "$ext" "$extra"
  emit "ARGV,helper=$helper,input=$label,argv=$(printf '%s ' "${ARGV[@]}" | sed 's/ $//' | tr ',' ';')"
done 3< "$WORK/matrix.tsv"

HELPERS="cad_mesh cad_render cad_dxf rsvg-convert"

trace_helper() {
  local helper=$1 idx=0 n h op label file ext extra small
  : > "$WORK/trace/$helper.all"
  while IFS="$(printf '\t')" read -r -u 3 h op label file ext extra small; do
    [ "$h" = "$helper" ] || continue
    idx=$((idx + 1))
    local jobd
    jobd=$(newdir)
    cp "$file" "$jobd/in.$ext"
    set_argv "$op" "$ext" "$extra"
    if ! scripts/sandbox-paths-measure.sh trace "$TAG" "$jobd" "${ARGV[@]}" > "$WORK/trace/$helper.$idx" 2>>"$WORK/trace.err" </dev/null; then
      emit "FAIL,helper=$helper,input=$label,reason=trace failed: $(tail -n 2 "$WORK/trace.err" | tr '\n,' '  ')"
      exit 1
    fi
    rm -rf "$jobd"
    n=$(grep -c '^  /' "$WORK/trace/$helper.$idx" || true)
    if [ "$n" -lt 1 ]; then
      emit "FAIL,helper=$helper,input=$label,reason=the trace recorded no path"
      exit 1
    fi
    cat "$WORK/trace/$helper.$idx" >> "$WORK/trace/$helper.all"
    local tree
    tree=$(grep -cE '^  (/opt/venv/|/opt/cad-measure/|/usr/lib/python3)' "$WORK/trace/$helper.$idx" || true)
    emit "TRACE,helper=$helper,input=$label,paths=$n,python_tree_paths=$tree,list=$(grep '^  /' "$WORK/trace/$helper.$idx" | grep -vE '^  (/opt/venv/|/opt/cad-measure/|/usr/lib/python3)' | sed 's/^  //; s/ -> /=>/' | paste -sd: -)"
  done 3< "$WORK/matrix.tsv"
}

collapse_helper() {
  local helper=$1 manual tmpn
  case "$helper" in
    rsvg-convert) manual=/usr/bin/rsvg-convert ;;
    *) manual="$PY_TARGET" ;;
  esac
  python3 -c "$COLLAPSE_PY" "$WORK/trace/$helper.all" $manual > "$WORK/collapse.$helper"
  grep '^ENTRY ' "$WORK/collapse.$helper" | sed 's/^ENTRY //' > "$WORK/set.cand.$helper"
  tmpn=$(grep -c '^TMP ' "$WORK/collapse.$helper" || true)
  if [ "$tmpn" -gt 0 ]; then
    emit "TMP_NEED,helper=$helper,paths=$(grep '^TMP ' "$WORK/collapse.$helper" | sed 's/^TMP //' | paste -sd: -)"
  else
    emit "TMP_NEED,helper=$helper,none"
  fi
  emit "LANDLOCK_CANDIDATE,helper=$helper,rw=/job,ro=$(ro_of "$WORK/set.cand.$helper"),note=candidate for phase 73 not a grant"
}

SUFF=0; DIFF=0; LEAKED=0; REMROWS=0
sufficiency() {
  local helper=$1 setname=$2 setfile=$3 ro h op label file ext extra small
  ro=$(ro_of "$setfile")
  while IFS="$(printf '\t')" read -r -u 3 h op label file ext extra small; do
    [ "$h" = "$helper" ] || continue
    judge_case "$ro" "$helper" "$op" "$label" "$ext" "$file" "$extra"
    [ "$CONTENT" != probe_failure ] || exit 1
    emit "SUFFICIENT,helper=$helper,input=$label,op=$op,set=$setname,confined_rc=$CONF_RC,content=$CONTENT,check=$CHECK,attempts=$ATTEMPTS"
    if [ "$CONTENT" = identical ]; then
      [ "$setname" != minimal ] || SUFF=$((SUFF + 1))
    else
      DIFF=$((DIFF + 1))
    fi
  done 3< "$WORK/matrix.tsv"
}

effect_case() {
  local helper=$1 setname=$2 setfile=$3 op label file ext extra h o l f e x s ro sig outside_unconf outside_conf
  ro=$(ro_of "$setfile")
  case "$helper" in
    cad_mesh) label=units_inch ;;
    cad_render) label="slide/iso:1280:white" ;;
    cad_dxf) label=hatches_2 ;;
    rsvg-convert) label=png/hatches_2 ;;
  esac
  while IFS="$(printf '\t')" read -r -u 3 h o l f e x s; do
    if [ "$h" = "$helper" ] && [ "$l" = "$label" ]; then op=$o; file=$f; ext=$e; extra=$x; break; fi
  done 3< "$WORK/matrix.tsv"
  rm -rf "$WORK/outside"; mkdir -p "$WORK/outside"; chmod 0755 "$WORK/outside"
  cp "$file" "$WORK/outside/in.$ext"
  chmod 0644 "$WORK/outside/in.$ext"
  EXTRA=(-v "$WORK/outside:/outside:ro")
  set_argv "$op" "$ext" "$extra"
  local -a moved=()
  for a in "${ARGV[@]}"; do moved+=("$(printf '%s' "$a" | sed "s#/job/in\\.#/outside/in.#")"); done
  ARGV=("${moved[@]}")
  emit "ARGV,helper=$helper,input=outside/$label,argv=$(printf '%s ' "${ARGV[@]}" | sed 's/ $//' | tr ',' ';')"
  exec_case - ""  "$ext"
  sig=$(signature "$op" "$JOB")
  if [ "$sig" != missing ]; then outside_unconf=ok; else outside_unconf=fail; fi
  rm -rf "$JOB"
  exec_case "$ro" "" "$ext"
  if probe_failure; then
    emit "PROBE_FAIL,helper=$helper,input=outside/$label,rc=$RC,message=$(grep -a -m1 -E 'the ruleset confines nothing|is not readable before|not a permission denial|^apply:' "$LOG" | tr ',' ' ')"
    exit 1
  fi
  sig=$(signature "$op" "$JOB")
  if [ "$sig" = missing ]; then outside_conf=denied; else outside_conf=LEAKED; LEAKED=$((LEAKED + 1)); fi
  local why=no_permission_text_in_log
  grep -aqi 'permission' "$LOG" && why=permission_error_in_log
  rm -rf "$JOB"
  EXTRA=()
  emit "EFFECT,helper=$helper,outside_unconfined=$outside_unconf,outside_confined=$outside_conf,set=$setname,confined_rc=$RC,probe_canary=/etc/hostname,evidence=$why"
  [ "$outside_unconf" = ok ] || exit 1
}

subset_pass() {
  local helper=$1 entry=$2 setfile=$3 ro h op label file ext extra small detail=""
  ro=$(ro_of "$setfile")
  while IFS="$(printf '\t')" read -r -u 3 h op label file ext extra small; do
    [ "$h" = "$helper" ] && [ "$small" = 1 ] || continue
    judge_case "$ro" "$helper" "$op" "$label" "$ext" "$file" "$extra"
    [ "$CONTENT" != probe_failure ] || exit 1
    [ "$CONTENT" = identical ] || detail="$detail$label:$CONTENT/rc$CONF_RC "
  done 3< "$WORK/matrix.tsv"
  printf '%s' "$detail"
}

full_pass() {
  local helper=$1 setfile=$2 stop=$3 ro h op label file ext extra small detail=""
  ro=$(ro_of "$setfile")
  while IFS="$(printf '\t')" read -r -u 3 h op label file ext extra small; do
    [ "$h" = "$helper" ] || continue
    judge_case "$ro" "$helper" "$op" "$label" "$ext" "$file" "$extra"
    [ "$CONTENT" != probe_failure ] || exit 1
    if [ "$CONTENT" != identical ]; then
      detail="$detail$label:$CONTENT/rc$CONF_RC "
      [ "$stop" = 1 ] && break
    fi
  done 3< "$WORK/matrix.tsv"
  printf '%s' "$detail"
}

removal_tables() {
  local helper=$1 e detail cur
  : > "$WORK/removable.$helper"
  while IFS= read -r e; do
    without "$WORK/set.cand.$helper" "$e" > "$WORK/set.try"
    detail=$(subset_pass "$helper" "$e" "$WORK/set.try")
    REMROWS=$((REMROWS + 1))
    if [ -z "$detail" ]; then
      emit "REMOVAL,helper=$helper,entry=$e,result=removable,detail=every subset input identical without it"
      printf '%s\n' "$e" >> "$WORK/removable.$helper"
    else
      emit "REMOVAL,helper=$helper,entry=$e,result=load-bearing,detail=$(printf '%s' "$detail" | sed 's/ $//' | tr ',' ';')"
    fi
  done < "$WORK/set.cand.$helper"

  cp "$WORK/set.cand.$helper" "$WORK/set.min.$helper"
  while IFS= read -r e; do
    [ -n "$e" ] || continue
    without "$WORK/set.min.$helper" "$e" > "$WORK/set.min.tmp"; cp "$WORK/set.min.tmp" "$WORK/set.min.$helper"
  done < "$WORK/removable.$helper"

  detail=$(full_pass "$helper" "$WORK/set.min.$helper" 1)
  if [ -n "$detail" ]; then
    emit "INTERACTION,helper=$helper,detail=the candidate without every removable entry fails the whole matrix ($(printf '%s' "$detail" | sed 's/ $//' | tr ',' ';')); entries are restored one at a time"
    while IFS= read -r e && [ -n "$detail" ]; do
      printf '%s\n' "$e" >> "$WORK/set.min.$helper"
      detail=$(full_pass "$helper" "$WORK/set.min.$helper" 1)
      emit "RESTORED,helper=$helper,entry=$e,matrix_identical=$([ -z "$detail" ] && echo 1 || echo 0)"
    done < "$WORK/removable.$helper"
  fi

  local changed=1
  while [ "$changed" = 1 ]; do
    changed=0
    while IFS= read -r e; do
      without "$WORK/set.min.$helper" "$e" > "$WORK/set.try"
      detail=$(full_pass "$helper" "$WORK/set.try" 1)
      if [ -z "$detail" ]; then
        emit "REMOVAL2,helper=$helper,entry=$e,result=removable,detail=whole matrix identical without it; dropped and the pass restarts"
        REMROWS=$((REMROWS + 1))
        cp "$WORK/set.try" "$WORK/set.min.$helper"
        changed=1
        break
      fi
    done < "$WORK/set.min.$helper"
  done
  cp "$WORK/set.min.$helper" "$WORK/set.min.final.$helper"
  while IFS= read -r e; do
    without "$WORK/set.min.final.$helper" "$e" > "$WORK/set.try"
    detail=$(full_pass "$helper" "$WORK/set.try" 1)
    REMROWS=$((REMROWS + 1))
    emit "REMOVAL2,helper=$helper,entry=$e,result=load-bearing,detail=$(printf '%s' "$detail" | sed 's/ $//' | tr ',' ';')"
  done < "$WORK/set.min.final.$helper"
  emit "LANDLOCK_MINIMAL,helper=$helper,rw=/job,ro=$(ro_of "$WORK/set.min.final.$helper"),note=candidate for phase 73 not a grant"
  local ps
  ps=$(grep -E '^/(proc|sys)' "$WORK/set.min.final.$helper" | paste -sd: - || true)
  emit "PROC_SYS,helper=$helper,entries=${ps:-none},note=/proc/self resolves to the pid at Apply time; the probe execs in the same pid as the production helper"
}

for hh in $HELPERS; do
  trace_helper "$hh"
  collapse_helper "$hh"
done
for hh in $HELPERS; do
  sufficiency "$hh" candidate "$WORK/set.cand.$hh"
  effect_case "$hh" candidate "$WORK/set.cand.$hh"
done
for hh in $HELPERS; do
  removal_tables "$hh"
  sufficiency "$hh" minimal "$WORK/set.min.final.$hh"
  effect_case "$hh" minimal "$WORK/set.min.final.$hh"
done

HELPERS_SEEN=$(cut -f1 "$WORK/matrix.tsv" | LC_ALL=C sort -u | wc -l | tr -d ' ')
HELPERS_WANT=$(printf '%s\n' $HELPERS | wc -l | tr -d ' ')
emit "LANDLOCK_SUMMARY,helpers=$HELPERS_SEEN,inputs=$INPUTS,sufficient=$SUFF,different=$DIFF,leaked=$LEAKED,removal_rows=$REMROWS"
emit "NOTE,every set above is a candidate for phase 73, not a grant"
landlock_verdict "$INPUTS" "$SUFF" "$DIFF" "$LEAKED" "$HELPERS_SEEN" "$HELPERS_WANT"
