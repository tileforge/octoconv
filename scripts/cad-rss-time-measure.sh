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
#   scripts/cad-rss-time-measure.sh --check-resume FILE [--ocp V] [--variant NAME] [--memory M]
#       [--cpus N] [--parallel P] [--lp-threads N|default] [--quality Q] [--budget-s N]
#       [--confine-ro PATH:PATH:...] [--expect-image TAG] [--expect-image-id ID]
#   scripts/cad-rss-time-measure.sh --self-test
#   scripts/cad-rss-time-measure.sh --pin-compare EVIDENCE-OCP-A EVIDENCE-OCP-B
#   scripts/cad-rss-time-measure.sh --threads-decision EVIDENCE
#   scripts/cad-rss-time-measure.sh --quality-summary EVIDENCE
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
#              F already stopped stays stopped. F is refused (exit 65) when any
#              header block of F differs from this run in a compared header
#              field (RESUME_COMPARED_FIELDS: image tag, image id, server arch,
#              ocp, cpus, memory, confine_ro, budget_s, quality, parallel,
#              lp_threads), when a block lacks one, or when a block carries a
#              header field that is neither compared nor in RESUME_FREE_FIELDS.
#              The free fields may differ between blocks: date_utc and
#              host_noise are per-run readings, go, kernel, python, vtk, ezdxf,
#              mesa and rsvg are readings of the host or fixed by the image id,
#              repeats is carried by the rep in every row key, ladder does not
#              change what a cell measures, resume is the flag itself. The row
#              keys and the stop keys carry ocp, variant, memory, cpus,
#              parallel, lp, quality and confined as well.
# --check-resume F  parse F and report whether --resume F would be accepted for
#              the configuration given; no docker, no measurement. The image
#              tag expected is the one the current inputs hash to; give
#              --expect-image TAG, and --expect-image-id ID to check the image
#              id too, to ask about another build.
# --self-test  run the resume checks on synthetic and committed evidence files.
# --max-wall-s stop cleanly (exit 75) before the next cell once N seconds have
#              passed, so a long run goes in chunks of a few hours.
# --floor-from skip a cell whose RSS in the 4g rows of FILE is below
#              --floor-frac of the memory limit being run.
# The render of a STEP or IGES file reads an STL meshed once per file, and the
# rsvg operations read an SVG drawn once per file, both at --premesh-mem and
# inside the cell budget; a failed pre-step is a SKIP and, in a ladder, a stop
# when it was an oom or a timeout.
#
# Exit codes: 64 usage, 65 resume file written under another configuration,
# 69 docker missing, 75 chunk ended by --max-wall-s, 1 a cell produced no WRAP
# line or there were no rows.

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
QUALITY_FILE=""
CHECK_RESUME=""
EXPECT_IMAGE=""
EXPECT_IMAGE_ID=""
EXPECT_ARCH=""
CONFINED=0
SELF_TEST=0
INPUTS=()

usage() {
  sed -n '/^# Usage:/,/^# Exit codes/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 64
}

pin_compare() {
  local a=$1 b=$2
  local out rc=0
  [ -s "$a" ] && [ -s "$b" ] || { echo "error: both evidence files are required" >&2; exit 64; }
  out=$(
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
        cfg = "variant=" kv(head, "variant") ",cpus=" kv(head, "cpus") ",par=" kv(head, "par") ",lp=" kv(head, "lp") ",quality=" kv(head, "quality") ",confined=" kv(head, "confined")
        if (!(side in sidecfg)) sidecfg[side] = cfg
        else if (sidecfg[side] != cfg) { bad++; printf "PIN_COMPARE_REFUSED,reason=file %s mixes rows of two configurations (%s | %s)\n", (side == "a" ? A : B), sidecfg[side], cfg > "/dev/stderr" }
        if ((side in ocps) && ocps[side] != kv(head, "ocp")) { bad++; printf "PIN_COMPARE_REFUSED,reason=file %s mixes two ocp versions\n", (side == "a" ? A : B) > "/dev/stderr" }
        ocps[side] = kv(head, "ocp")
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
        if (n == 0) { printf "PIN_COMPARE_REFUSED,reason=no step or iges mesh row at 4g in either file\n" > "/dev/stderr"; exit 65 }
        if (!("a" in sidecfg) || !("b" in sidecfg)) { printf "PIN_COMPARE_REFUSED,reason=one file has no step or iges mesh row at 4g\n" > "/dev/stderr"; exit 65 }
        if (sidecfg["a"] != sidecfg["b"]) { printf "PIN_COMPARE_REFUSED,reason=the two files were measured at different configurations (%s | %s)\n", sidecfg["a"], sidecfg["b"] > "/dev/stderr"; exit 65 }
        if (ocps["a"] == ocps["b"]) { printf "PIN_COMPARE_REFUSED,reason=both files carry ocp %s; a pin comparison needs two pins\n", ocps["a"] > "/dev/stderr"; exit 65 }
        if (bad > 0) exit 65
        for (i = 1; i <= n; i++) {
          s = order[i]; ka = "a" SUBSEP s; kb = "b" SUBSEP s
          if (!(ka in runs) || !(kb in runs)) { printf "PIN_COMPARE_REFUSED,reason=file %s has no row on one of the sides\n", name[s] > "/dev/stderr"; exit 65 }
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
  ) || rc=$?
  if [ "$rc" != 0 ]; then
    echo "error: the pin comparison was refused (exit $rc); nothing was appended to $b" >&2
    exit "$rc"
  fi
  printf '%s\n' "$out" | tee -a "$b"
  exit 0
}

quality_summary() {
  local f=$1 out rc=0
  [ -s "$f" ] || { echo "error: the evidence file is empty" >&2; exit 64; }
  out=$(
    awk -F, '
      function kv(s, k,    n, i, parts) {
        n = split(s, parts, ",")
        for (i = 1; i <= n; i++) if (index(parts[i], k "=") == 1) return substr(parts[i], length(k) + 2)
        return ""
      }
      function outkv(o, k,    n, i, parts) {
        n = split(o, parts, ";")
        for (i = 1; i <= n; i++) if (index(parts[i], k ":") == 1) return substr(parts[i], length(k) + 2)
        return ""
      }
      /^ROW,/ {
        head = $0; sub(/,out=.*$/, "", head); out = $0; sub(/^.*,out=/, "", out)
        if (kv(head, "op") != "mesh" || kv(head, "rc") != "0") next
        key = kv(head, "quality") "|" kv(head, "name")
        if (!(key in tri)) { tri[key] = outkv(out, "triangles"); lin[key] = outkv(out, "lin_deflection") }
      }
      /^DIST,/ {
        key = kv($0, "quality") "|" kv($0, "name")
        cfg = "mem=" kv($0, "mem") ",ocp=" kv($0, "ocp") ",par=" kv($0, "par") ",lp=" kv($0, "lp")
        if (dcfg == "") dcfg = cfg
        else if (dcfg != cfg) { printf "QUALITY_REFUSED,reason=DIST lines of two configurations (%s | %s)\n", dcfg, cfg > "/dev/stderr"; bad++ }
        if (kv($0, "n_failed") != "0" || kv($0, "n") + 0 < 1) { printf "QUALITY_REFUSED,reason=DIST line with failed or no repeats (name=%s,op=%s,n=%s,n_failed=%s)\n", kv($0, "name"), kv($0, "op"), kv($0, "n"), kv($0, "n_failed") > "/dev/stderr"; bad++ }
        if (!(key in seen)) { seen[key] = 1; keys[++nk] = key }
        op = kv($0, "op")
        wall[key, op] = kv($0, "wall_median"); rss[key, op] = kv($0, "rss_max_mib")
        bytes[key] = kv($0, "bytes")
      }
      END {
        if (nk == 0) { printf "QUALITY_REFUSED,reason=no DIST line in the evidence file\n" > "/dev/stderr"; exit 65 }
        for (i = 1; i <= nk; i++) {
          k = keys[i]
          if (!((k, "mesh") in wall) || !((k, "render") in wall) || !(k in tri) || tri[k] == "") { printf "QUALITY_REFUSED,reason=%s lacks a mesh DIST, a render DIST or a triangle count\n", k > "/dev/stderr"; bad++ }
        }
        if (bad > 0) exit 65
        for (i = 1; i <= nk; i++) {
          k = keys[i]; split(k, kp, "|")
          t = tri[k]
          printf "QUALITY_ROW,level=%s,name=%s,input_bytes=%s,triangles=%s,stl_bytes=%d,mesh_wall_median_s=%s,mesh_rss_max_mib=%s,render_wall_median_s=%s,render_rss_max_mib=%s\n",
            kp[1], kp[2], bytes[k], t, 84 + 50 * t, wall[k, "mesh"], rss[k, "mesh"], wall[k, "render"], rss[k, "render"]
        }
      }' "$f"
  ) || rc=$?
  if [ "$rc" != 0 ]; then
    echo "error: the quality summary was refused (exit $rc); nothing was appended to $f" >&2
    exit "$rc"
  fi
  printf '%s\n' "$out" | tee -a "$f"
  exit 0
}

threads_decision() {
  local f=$1 out rc=0
  [ -s "$f" ] || { echo "error: the evidence file is empty" >&2; exit 64; }
  out=$(
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
        cfg = "mem=" kv($0, "mem") ",ocp=" kv($0, "ocp") ",quality=" kv($0, "quality")
        if (dcfg == "") dcfg = cfg
        else if (dcfg != cfg) { printf "THREADS_REFUSED,reason=DIST lines of two configurations (%s | %s)\n", dcfg, cfg > "/dev/stderr"; bad++ }
        if ((op == "mesh" && lp != "default") || (op == "render" && par != "on")) { printf "THREADS_REFUSED,reason=%s DIST line varies the other axis too (par=%s,lp=%s)\n", op, par, lp > "/dev/stderr"; bad++ }
        if (kv($0, "n_failed") != "0" || kv($0, "n") + 0 < 1) { printf "THREADS_REFUSED,reason=DIST line with failed or no repeats (name=%s,op=%s,n=%s,n_failed=%s)\n", kv($0, "name"), op, kv($0, "n"), kv($0, "n_failed") > "/dev/stderr"; bad++ }
        sha = kv($0, "sha")
        if (!((key, sha) in inset)) { inset[key, sha] = 1; setn[key]++ }
        opshas[op SUBSEP sha] = 1
        if (!(key in seen)) { seen[key] = 1; keys[++nk] = key }
        w[key] += kv($0, "wall_median"); u[key] += kv($0, "usersys_median")
        t = kv($0, "threads_max") + 0; if (t > th[key]) th[key] = t
        files[key]++
        printf "THREADS_ROW,op=%s,variant=%s,name=%s,bytes=%s,n=%s,wall_median=%s,wall_min=%s,wall_max=%s,usersys_median=%s,threads_max=%s,rss_max_mib=%s\n",
          op, v, kv($0, "name"), kv($0, "bytes"), kv($0, "n"), kv($0, "wall_median"), kv($0, "wall_min"), kv($0, "wall_max"),
          kv($0, "usersys_median"), kv($0, "threads_max"), kv($0, "rss_max_mib")
      }
      END {
        if (nk == 0) { printf "THREADS_REFUSED,reason=no DIST line in the evidence file\n" > "/dev/stderr"; exit 65 }
        for (o = 1; o <= 2; o++) {
          op = (o == 1) ? "mesh" : "render"
          nv = 0; ninp = 0
          for (i = 1; i <= nk; i++) { split(keys[i], kp, "|"); if (kp[1] != op) continue; nv++; if (nv == 1) ninp = setn[keys[i]]; else if (setn[keys[i]] != ninp) { printf "THREADS_REFUSED,reason=%s variants were measured over different input sets (%s has %d inputs, expected %d)\n", op, keys[i], setn[keys[i]], ninp > "/dev/stderr"; bad++ } }
          if (nv < 2) { printf "THREADS_REFUSED,reason=%s has %d variant(s) in the evidence, a decision needs at least two\n", op, nv > "/dev/stderr"; bad++ }
        }
        if (bad > 0) exit 65
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
  ) || rc=$?
  if [ "$rc" != 0 ]; then
    echo "error: the threads decision was refused (exit $rc); nothing was appended to $f" >&2
    exit "$rc"
  fi
  printf '%s\n' "$out" | tee -a "$f"
  exit 0
}

floor_rows() {
  awk -F, -v OCP="$OCP" -v VAR="$VARIANT" -v CPUS="$CPUS" -v PAR="$PARALLEL" -v LP="$LP" -v Q="$QUALITY" -v CONF="$CONFINED" -v COUNTS="$2" '
    function kv(s, k,    n, i, parts) {
      n = split(s, parts, ",")
      for (i = 1; i <= n; i++) if (index(parts[i], k "=") == 1) return substr(parts[i], length(k) + 2)
      return ""
    }
    function num(x) { return (x ~ /^[0-9.]+$/) ? x + 0 : -1 }
    /^ROW,/ {
      head = $0; sub(/,out=.*$/, "", head)
      if (kv(head, "mem") != "4g") next
      if (kv(head, "ocp") != OCP || kv(head, "variant") != VAR || kv(head, "cpus") != CPUS || kv(head, "par") != PAR || kv(head, "lp") != LP || kv(head, "quality") != Q || kv(head, "confined") != CONF) { other++; next }
      used++
      key = kv(head, "sha") "|" kv(head, "op")
      if (kv(head, "rc") != "0") { r = 99999999 } else {
        c = num(kv(head, "child_rss_mib")); g = num(kv(head, "cg_peak_mib")); r = (c > g) ? c : g
      }
      if (!(key in best) || r > best[key]) best[key] = r
    }
    END {
      for (k in best) printf "%s|%s\n", k, best[k]
      printf "%d %d\n", used + 0, other + 0 > COUNTS
    }' "$1"
}

RESUME_COMPARED_FIELDS="image image_id server_arch ocp cpus memory confine_ro budget_s quality parallel lp_threads"
RESUME_FREE_FIELDS="date_utc host_noise go kernel python vtk ezdxf mesa rsvg ladder repeats resume"

resume_expect() {
  printf 'image=%s\n' "$EXPECT_IMAGE"
  [ -z "$EXPECT_IMAGE_ID" ] || printf 'image_id=%s\n' "$EXPECT_IMAGE_ID"
  [ -z "$EXPECT_ARCH" ] || printf 'server_arch=%s\n' "$EXPECT_ARCH"
  printf 'ocp=%s\ncpus=%s\nmemory=%s\nconfine_ro=%s\n' "$OCP" "$CPUS" "$MEMS" "$CONFINE"
  printf 'budget_s=%s\nquality=%s\nparallel=%s\nlp_threads=%s\n' "$BUDGET" "$QUALITY" "$PARALLEL" "$LP"
}

resume_scan() {
  local mode=$1 f=$2
  RESUME_EXPECT=$(resume_expect) RESUME_FREE="$RESUME_COMPARED_FIELDS $RESUME_FREE_FIELDS" \
  awk -F, -v MODE="$mode" -v DK="${DONE_KEYS:-/dev/null}" -v SK="${STOPPED:-/dev/null}" '
    function kv(s, k,    n, i, parts) {
      n = split(s, parts, ",")
      for (i = 1; i <= n; i++) if (index(parts[i], k "=") == 1) return substr(parts[i], length(k) + 2)
      return ""
    }
    function note(k, got) {
      got_k[k] = got
      if (MODE == "check" && (k in want) && got != want[k] && !((k SUBSEP got) in said)) {
        said[k SUBSEP got] = 1
        bad++
        printf "RESUME_CONFIG_MISMATCH,key=%s,file=%s,current=%s\n", k, got, want[k] > "/dev/stderr"
      }
    }
    function field(piece, k, v, p) {
      p = index(piece, "=")
      k = substr(piece, 1, p - 1); v = substr(piece, p + 1)
      if (!(k in want) && !(k in free) && MODE == "check" && !((k SUBSEP "unclassified") in said)) {
        said[k SUBSEP "unclassified"] = 1
        bad++
        printf "RESUME_CONFIG_MISMATCH,key=%s,file=%s,current=unclassified-header-field\n", k, v > "/dev/stderr"
      }
      note(k, v)
    }
    function endblock(   i, k) {
      if (!inblock) return
      for (i = 1; i <= nord; i++) {
        k = order[i]
        if (!(k in got_k) && want[k] != "" && MODE == "check") {
          bad++
          printf "RESUME_CONFIG_MISMATCH,key=%s,file=missing,current=%s\n", k, want[k] > "/dev/stderr"
        }
      }
      delete got_k
    }
    function hdrline(line,    n, i, parts, cur, curk) {
      sub(/^HDR,/, "", line)
      n = split(line, parts, ",")
      cur = ""
      for (i = 1; i <= n; i++) {
        if (parts[i] ~ /^[A-Za-z_][A-Za-z0-9_]*=/) {
          if (cur != "") field(cur)
          cur = parts[i]
        } else {
          cur = cur "," parts[i]
        }
      }
      if (cur != "") field(cur)
    }
    function orblock(v, b) { return (v == "") ? b : v }
    BEGIN {
      n = split(ENVIRON["RESUME_EXPECT"], lines, "\n")
      for (i = 1; i <= n; i++) {
        if (lines[i] == "") continue
        p = index(lines[i], "=")
        k = substr(lines[i], 1, p - 1)
        want[k] = substr(lines[i], p + 1)
        order[++nord] = k
      }
      n = split(ENVIRON["RESUME_FREE"], fr, " ")
      for (i = 1; i <= n; i++) free[fr[i]] = 1
    }
    /^HDR,date_utc=/ { endblock(); inblock = 1; blocks++; b_conf = 0 }
    /^HDR,/ {
      hdrline($0)
      if ($0 ~ /^HDR,confine_ro=/) b_conf = 1
      if ($0 ~ /^HDR,image=/) { v = $0; sub(/^HDR,image=/, "", v); sub(/^[^:]*:ocp[^-]*-/, "", v); sub(/-[0-9a-f]+$/, "", v); b_var = v }
      if ($0 ~ /^HDR,ocp=/) { v = $0; sub(/^HDR,ocp=/, "", v); b_ocp = v }
      if ($0 ~ /^HDR,cpus=/) { v = $0; sub(/^HDR,cpus=/, "", v); b_cpus = v }
      if ($0 ~ /^HDR,ladder=/) { b_par = kv($0, "parallel"); b_lp = kv($0, "lp_threads"); b_q = kv($0, "quality") }
    }
    MODE == "load" && /^ROW,/ {
      head = $0; sub(/,out=.*$/, "", head)
      printf "%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s\n", kv(head,"sha"), kv(head,"op"), kv(head,"ocp"), kv(head,"variant"), kv(head,"mem"), kv(head,"cpus"), kv(head,"rep"), kv(head,"par"), kv(head,"lp"), kv(head,"quality"), kv(head,"confined") >> DK
      oom = kv(head, "oom_kill"); to = kv(head, "timeout"); rc = kv(head, "rc")
      if ((oom != "0" && oom != "na" && oom != "") || to == "1" || rc == "137")
        printf "%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s\n", kv(head,"fmt"), kv(head,"op"), kv(head,"mem"), kv(head,"ocp"), kv(head,"variant"), kv(head,"cpus"), kv(head,"par"), kv(head,"lp"), kv(head,"quality"), kv(head,"confined"), kv(head,"series"), kv(head,"bytes") >> SK
    }
    MODE == "load" && /^LADDER_STOP,/ {
      if (kv($0, "reason") == "oom" || kv($0, "reason") == "timeout")
        printf "%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s\n", kv($0,"fmt"), kv($0,"op"), kv($0,"mem"), orblock(kv($0,"ocp"), b_ocp), orblock(kv($0,"variant"), b_var), orblock(kv($0,"cpus"), b_cpus), orblock(kv($0,"par"), b_par), orblock(kv($0,"lp"), b_lp), orblock(kv($0,"quality"), b_q), orblock(kv($0,"confined"), b_conf), kv($0,"series"), kv($0,"at_bytes") >> SK
    }
    END {
      if (MODE != "check") exit 0
      endblock()
      if (blocks == 0) { bad++; printf "RESUME_CONFIG_MISMATCH,key=header,file=missing,current=present\n" > "/dev/stderr" }
      exit (bad > 0)
    }' "$f"
}

stop_prefix() {
  printf '%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s' "$1" "$2" "$3" "$OCP" "$VARIANT" "$CPUS" "$PARALLEL" "$LP" "$QUALITY" "$CONFINED" "$4"
}
is_stopped_any() { grep -Fq "$(stop_prefix "$1" "$2" "$3" "$4")|" "$STOPPED"; }
is_stopped() {
  awk -F'|' -v k="$(stop_prefix "$1" "$2" "$3" "$4")" -v b="$5" '
    { key = $1; for (i = 2; i <= 11; i++) key = key "|" $i }
    key == k && b + 0 >= $12 + 0 { found = 1 }
    END { exit !found }' "$STOPPED"
}
mark_stopped() { printf '%s|%s\n' "$(stop_prefix "$1" "$2" "$3" "$4")" "$5" >> "$STOPPED"; }
is_done() { grep -Fxq "$1|$2|$OCP|$VARIANT|$3|$CPUS|$4|$PARALLEL|$LP|$QUALITY|$CONFINED" "$DONE_KEYS"; }

check_resume_file() {
  local f=$1
  [ -f "$f" ] || { echo "error: no such resume file" >&2; return 64; }
  resume_scan check "$f" || return 65
}

self_test() {
  local d fails=0 n=0 evid a b
  d=$(mktemp -d "${TMPDIR:-/tmp}/cad-resume-selftest.XXXXXX")
  DONE_KEYS="$d/done.keys"
  STOPPED="$d/stopped.keys"
  evid="$REPO_ROOT/.planning/phases/71-evaluation-gate/evidence"
  OCP=7.9.3.1.1; VARIANT=full; MEMS=4g; CPUS=2; PARALLEL=on; LP=default; QUALITY=standard; BUDGET=900
  CONFINE=""; CONFINED=0
  EXPECT_IMAGE=octoconv-cad-measure:ocp7.9.3.1.1-full-aaaaaaaaaaaa; EXPECT_IMAGE_ID=""; EXPECT_ARCH=""

  expect() {
    local want=$1 label=$2 got=0
    shift 2
    "$@" >/dev/null 2>&1 || got=$?
    n=$((n + 1))
    if { [ "$want" = ok ] && [ "$got" = 0 ]; } || { [ "$want" = no ] && [ "$got" != 0 ]; }; then
      return 0
    fi
    echo "SELF_TEST_FAIL,case=$label,want=$want,rc=$got" >&2
    fails=$((fails + 1))
  }
  in_cfg() { ( eval "$1"; shift; "$@" ); }
  mismatch_keys() {
    local err rc=0
    err=$("$@" 2>&1 >/dev/null) || rc=$?
    printf '%s\n' "$err" | sed -n 's/^RESUME_CONFIG_MISMATCH,key=\([^,]*\),.*/\1/p' | LC_ALL=C sort -u | tr '\n' ' '
  }
  expect_keys() {
    local want=$1 label=$2 got
    shift 2
    got=$(mismatch_keys "$@")
    n=$((n + 1))
    [ "$got" = "$want " ] && return 0
    echo "SELF_TEST_FAIL,case=$label,want_keys=$want,got_keys=$got" >&2
    fails=$((fails + 1))
  }
  hdr_fields_classified() {
    local f missing=0 known
    known=" $RESUME_COMPARED_FIELDS $RESUME_FREE_FIELDS "
    for f in $({ grep -hE '(printf|emit)[^#]*HDR,' "$REPO_ROOT/scripts/cad-measure/common.sh" "$REPO_ROOT/scripts/cad-rss-time-measure.sh" | grep -oE '(HDR|,)[a-z_]+=' | sed -E 's/^(HDR)?,?//; s/=$//'
                grep -hE 'for k, p in' "$REPO_ROOT/scripts/cad-measure/common.sh" | grep -oE '\("[a-z]+"' | tr -d '("'; } | LC_ALL=C sort -u); do
      case "$known" in *" $f "*) ;; *) echo "HDR field $f is neither compared nor allow-listed" >&2; missing=1 ;; esac
    done
    [ "$missing" = 0 ]
  }

  cat > "$d/synthetic.txt" <<SYN
HDR,date_utc=2026-01-01T00:00:00Z
HDR,server_arch=arm64
HDR,image=octoconv-cad-measure:ocp7.9.3.1.1-full-aaaaaaaaaaaa
HDR,image_id=sha256:aaaa
HDR,ocp=7.9.3.1.1
HDR,cpus=2
HDR,memory=4g
HDR,ladder=1,repeats=3,budget_s=900,quality=standard,parallel=on,lp_threads=default,resume=0
ROW,src=public,name=a.step,sha=s1,fmt=step,bytes=100,op=mesh,ocp=7.9.3.1.1,variant=full,mem=4g,cpus=2,confined=0,rep=1,rc=0,oom_kill=0,timeout=0,pix=-,par=on,lp=default,quality=standard,series=real,out=-
ROW,src=public,name=b.step,sha=s2,fmt=step,bytes=200,op=mesh,ocp=7.9.3.1.1,variant=full,mem=4g,cpus=2,confined=0,rep=1,rc=137,oom_kill=0,timeout=1,pix=-,par=on,lp=default,quality=standard,series=real,out=-
LADDER_STOP,fmt=iges,op=mesh,mem=4g,ocp=7.9.3.1.1,series=real,reason=oom,at_bytes=300
SYN
  expect ok same-config check_resume_file "$d/synthetic.txt"
  expect no parallel-differs in_cfg 'PARALLEL=off' check_resume_file "$d/synthetic.txt"
  expect no lp-differs in_cfg 'LP=2' check_resume_file "$d/synthetic.txt"
  expect no quality-differs in_cfg 'QUALITY=fine' check_resume_file "$d/synthetic.txt"
  expect no cpus-differs in_cfg 'CPUS=4' check_resume_file "$d/synthetic.txt"
  expect no memory-differs in_cfg 'MEMS=1g,4g' check_resume_file "$d/synthetic.txt"
  expect no ocp-differs in_cfg 'OCP=8.0.1.0.0' check_resume_file "$d/synthetic.txt"
  expect no variant-differs in_cfg 'VARIANT=pruned; EXPECT_IMAGE=octoconv-cad-measure:ocp7.9.3.1.1-pruned-aaaaaaaaaaaa' check_resume_file "$d/synthetic.txt"
  expect no budget-differs in_cfg 'BUDGET=600' check_resume_file "$d/synthetic.txt"

  { cat "$d/synthetic.txt"; grep '^HDR,' "$d/synthetic.txt" | sed 's/^HDR,cpus=2/HDR,cpus=4/'; } > "$d/two-blocks.txt"
  expect no second-block-differs check_resume_file "$d/two-blocks.txt"
  grep -v '^HDR,ladder=' "$d/synthetic.txt" > "$d/no-ladder-hdr.txt"
  expect no header-missing check_resume_file "$d/no-ladder-hdr.txt"

  expect ok image-id-same in_cfg 'EXPECT_IMAGE_ID=sha256:aaaa' check_resume_file "$d/synthetic.txt"
  expect no image-id-differs in_cfg 'EXPECT_IMAGE_ID=sha256:bbbb' check_resume_file "$d/synthetic.txt"
  expect no image-tag-differs in_cfg 'EXPECT_IMAGE=octoconv-cad-measure:ocp7.9.3.1.1-full-bbbbbbbbbbbb' check_resume_file "$d/synthetic.txt"
  expect no arch-differs in_cfg 'EXPECT_ARCH=amd64' check_resume_file "$d/synthetic.txt"
  expect ok arch-same in_cfg 'EXPECT_ARCH=arm64' check_resume_file "$d/synthetic.txt"
  expect no confine-added-on-unconfined-file in_cfg 'CONFINE=/opt/venv:/lib; CONFINED=1' check_resume_file "$d/synthetic.txt"
  expect_keys confine_ro confine-added-names-confine_ro in_cfg 'CONFINE=/opt/venv:/lib; CONFINED=1' check_resume_file "$d/synthetic.txt"
  sed 's|^HDR,cpus=2|HDR,confine_ro=/opt/venv:/lib\nHDR,cpus=2|' "$d/synthetic.txt" > "$d/confined.txt"
  grep -q '^HDR,confine_ro=/opt/venv:/lib$' "$d/confined.txt" || { echo "SELF_TEST_FAIL,case=confined-fixture" >&2; fails=$((fails + 1)); }
  expect ok confined-file-same-confinement in_cfg 'CONFINE=/opt/venv:/lib; CONFINED=1' check_resume_file "$d/confined.txt"
  expect no confined-file-resumed-unconfined check_resume_file "$d/confined.txt"
  expect no confined-file-other-paths in_cfg 'CONFINE=/opt/venv; CONFINED=1' check_resume_file "$d/confined.txt"
  { cat "$d/confined.txt"; grep '^HDR,' "$d/synthetic.txt"; } > "$d/confined-then-bare.txt"
  expect no second-block-lost-confinement in_cfg 'CONFINE=/opt/venv:/lib; CONFINED=1' check_resume_file "$d/confined-then-bare.txt"
  sed 's|^HDR,cpus=2|HDR,newfield=1\nHDR,cpus=2|' "$d/synthetic.txt" > "$d/unclassified.txt"
  expect no unclassified-header-field check_resume_file "$d/unclassified.txt"
  sed 's|^HDR,date_utc=.*|HDR,date_utc=2027-02-02T00:00:00Z\nHDR,host_noise=99\nHDR,go=go version go9|; s|repeats=3|repeats=9|; s|resume=0|resume=1|; s|ladder=1|ladder=0|' "$d/synthetic.txt" > "$d/free.txt"
  expect ok allow-listed-fields-may-differ check_resume_file "$d/free.txt"
  expect ok hdr-fields-all-classified hdr_fields_classified

  resume_scan load "$d/synthetic.txt"
  expect ok done-same-config is_done s1 mesh 4g 1
  expect no done-other-confinement in_cfg 'CONFINE=/opt/venv:/lib; CONFINED=1' is_done s1 mesh 4g 1
  expect no stop-other-confinement in_cfg 'CONFINE=/opt/venv:/lib; CONFINED=1' is_stopped step mesh 4g real 200
  expect no done-other-parallel in_cfg 'PARALLEL=off' is_done s1 mesh 4g 1
  expect no done-other-cpus in_cfg 'CPUS=4' is_done s1 mesh 4g 1
  expect no done-other-lp in_cfg 'LP=2' is_done s1 mesh 4g 1
  expect no done-other-quality in_cfg 'QUALITY=fine' is_done s1 mesh 4g 1
  expect no done-other-variant in_cfg 'VARIANT=pruned' is_done s1 mesh 4g 1
  in_sub() { ( "$@" ); }
  floor_row() {
    printf 'ROW,src=public,name=a.step,sha=s1,fmt=step,bytes=100,op=mesh,ocp=%s,variant=%s,mem=%s,cpus=%s,confined=%s,rep=1,rc=0,child_rss_mib=%s,cg_peak_mib=10,par=%s,lp=%s,quality=%s,series=real,out=-\n' "$1" "$2" "$3" "$4" "$5" "$6" "$7" "$8" "$9"
  }
  {
    floor_row 7.9.3.1.1 full 4g 2 0 300 on default standard
    floor_row 8.0.1.0.0 full 4g 2 0 900 on default standard
    floor_row 7.9.3.1.1 pruned 4g 2 0 901 on default standard
    floor_row 7.9.3.1.1 full 4g 2 1 902 on default standard
    floor_row 7.9.3.1.1 full 4g 2 0 903 off default standard
    floor_row 7.9.3.1.1 full 4g 2 0 904 on default fine
    floor_row 7.9.3.1.1 full 4g 4 0 905 on default standard
    floor_row 7.9.3.1.1 full 4g 2 0 906 on 2 standard
    floor_row 7.9.3.1.1 full 2g 2 0 907 on default standard
  } > "$d/floor-src.txt"
  floor_rows "$d/floor-src.txt" "$d/floor.counts" > "$d/floor.tsv"
  [ "$(cat "$d/floor.tsv")" = "s1|mesh|300" ] || { echo "SELF_TEST_FAIL,case=floor-uses-only-rows-of-this-configuration,got=$(cat "$d/floor.tsv")" >&2; fails=$((fails + 1)); }
  [ "$(cat "$d/floor.counts")" = "1 7" ] || { echo "SELF_TEST_FAIL,case=floor-counts,got=$(cat "$d/floor.counts")" >&2; fails=$((fails + 1)); }
  ( OCP=9.9; floor_rows "$d/floor-src.txt" "$d/floor9.counts" > "$d/floor9.tsv" )
  [ "$(cat "$d/floor9.counts")" = "0 8" ] || { echo "SELF_TEST_FAIL,case=floor-no-row-of-this-ocp,got=$(cat "$d/floor9.counts")" >&2; fails=$((fails + 1)); }
  n=$((n + 3))
  dist() {
    printf 'DIST,sha=%s,name=%s,fmt=step,bytes=100,op=%s,mem=%s,ocp=7.9.3.1.1,n=%s,n_failed=%s,wall_min=1,wall_median=%s,wall_p95=1,wall_max=1,rss_max_mib=100,usersys_median=%s,threads_max=2,par=%s,lp=%s,quality=%s,series=real\n' "$1" "$2" "$3" "$4" "$5" "$6" "$7" "$7" "$8" "$9" "${10}"
  }
  threads_fixture() {
    local f=$1 mem=${2:-4g} nfail=${3:-0}
    {
      printf 'HDR,date_utc=2026-01-01T00:00:00Z\n'
      for sha in s1 s2; do
        dist $sha $sha.step mesh "$mem" 3 "$nfail" 20 on default standard
        dist $sha $sha.step mesh "$mem" 3 0 22 off default standard
        dist $sha $sha.stl render "$mem" 3 0 4 on 1 standard
        dist $sha $sha.stl render "$mem" 3 0 3 on 2 standard
      done
    } > "$f"
  }
  threads_fixture "$d/th-ok.txt"
  expect ok threads-decision-good-fixture in_sub threads_decision "$d/th-ok.txt"
  grep -q '^THREADS_DECISION,lp_num_threads=2,occt_parallel=on,' "$d/th-ok.txt" || { echo "SELF_TEST_FAIL,case=threads-decision-value" >&2; fails=$((fails + 1)); }
  printf 'HDR,date_utc=2026-01-01T00:00:00Z\n' > "$d/th-empty.txt"
  expect no threads-decision-no-dist-lines in_sub threads_decision "$d/th-empty.txt"
  [ "$(grep -c '^THREADS_DECISION' "$d/th-empty.txt")" = 0 ] || { echo "SELF_TEST_FAIL,case=threads-decision-empty-appended" >&2; fails=$((fails + 1)); }
  threads_fixture "$d/th-fail.txt" 4g 1
  expect no threads-decision-failed-repeat in_sub threads_decision "$d/th-fail.txt"
  threads_fixture "$d/th-mem.txt"
  dist s3 s3.step mesh 2g 3 0 20 on default standard >> "$d/th-mem.txt"
  expect no threads-decision-two-memory-limits in_sub threads_decision "$d/th-mem.txt"
  threads_fixture "$d/th-mem2.txt"
  sed 's/^\(DIST,sha=s2,.*op=render,\)mem=4g/\1mem=2g/' "$d/th-mem2.txt" > "$d/th-mem3.txt"
  grep -q 'op=render,mem=2g' "$d/th-mem3.txt" || { echo "SELF_TEST_FAIL,case=threads-mem-fixture" >&2; fails=$((fails + 1)); }
  expect no threads-decision-same-sets-other-memory in_sub threads_decision "$d/th-mem3.txt"
  threads_fixture "$d/th-sets.txt"
  grep -v 'sha=s2,.*op=mesh.*par=off' "$d/th-sets.txt" > "$d/th-sets2.txt"
  expect no threads-decision-different-input-sets in_sub threads_decision "$d/th-sets2.txt"
  threads_fixture "$d/th-axis.txt"
  dist s1 s1.step mesh 4g 3 0 20 on 2 standard >> "$d/th-axis.txt"
  expect no threads-decision-mesh-row-varies-lp in_sub threads_decision "$d/th-axis.txt"
  grep -v 'op=render' "$d/th-ok.txt" | grep '^DIST' > "$d/th-onlymesh.txt"
  expect no threads-decision-one-op-only in_sub threads_decision "$d/th-onlymesh.txt"

  quality_fixture() {
    local f=$1 nfail=${2:-0} lvl
    {
      printf 'HDR,date_utc=2026-01-01T00:00:00Z\n'
      for lvl in draft standard fine; do
        printf 'ROW,src=public,name=a.step,sha=s1,fmt=step,bytes=100,op=mesh,ocp=7.9.3.1.1,variant=full,mem=4g,cpus=2,confined=0,rep=1,rc=0,quality=%s,par=on,lp=default,series=real,out=triangles:10;lin_deflection:0.1\n' "$lvl"
        dist s1 a.step mesh 4g 3 "$nfail" 2 on default "$lvl"
        dist s1 a.step render 4g 3 0 2 on default "$lvl"
      done
    } > "$f"
  }
  quality_fixture "$d/q-ok.txt"
  expect ok quality-summary-good-fixture in_sub quality_summary "$d/q-ok.txt"
  [ "$(grep -c '^QUALITY_ROW' "$d/q-ok.txt")" = 3 ] || { echo "SELF_TEST_FAIL,case=quality-summary-rows" >&2; fails=$((fails + 1)); }
  printf 'HDR,date_utc=2026-01-01T00:00:00Z\n' > "$d/q-empty.txt"
  expect no quality-summary-no-dist-lines in_sub quality_summary "$d/q-empty.txt"
  quality_fixture "$d/q-fail.txt" 1
  expect no quality-summary-failed-repeat in_sub quality_summary "$d/q-fail.txt"
  quality_fixture "$d/q-norender.txt"
  grep -v 'op=render' "$d/q-norender.txt" > "$d/q-norender2.txt"
  expect no quality-summary-no-render-dist in_sub quality_summary "$d/q-norender2.txt"

  pin_row() {
    printf 'ROW,src=public,name=%s,sha=%s,fmt=step,bytes=100,op=mesh,ocp=%s,variant=full,mem=4g,cpus=2,confined=%s,rep=1,rc=0,wall_s=1.0,child_rss_mib=100,cg_peak_mib=90,par=%s,lp=default,quality=standard,series=real,out=triangles:10;volume:1;solids:1\n' "$1" "$2" "$3" "$4" "$5"
  }
  { pin_row a.step s1 7.9.3.1.1 0 on; pin_row b.step s2 7.9.3.1.1 0 on; } > "$d/pin-a.txt"
  { pin_row a.step s1 8.0.1.0.0 0 on; pin_row b.step s2 8.0.1.0.0 0 on; } > "$d/pin-b.txt"
  cp "$d/pin-b.txt" "$d/pin-b-run.txt"
  expect ok pin-compare-good-fixture in_sub pin_compare "$d/pin-a.txt" "$d/pin-b-run.txt"
  grep -q '^PIN_COMPARE_END,files=2$' "$d/pin-b-run.txt" || { echo "SELF_TEST_FAIL,case=pin-compare-files" >&2; fails=$((fails + 1)); }
  { pin_row a.step s1 8.0.1.0.0 1 on; pin_row b.step s2 8.0.1.0.0 1 on; } > "$d/pin-b-conf.txt"
  expect no pin-compare-confined-against-unconfined in_sub pin_compare "$d/pin-a.txt" "$d/pin-b-conf.txt"
  { pin_row a.step s1 8.0.1.0.0 0 off; pin_row b.step s2 8.0.1.0.0 0 off; } > "$d/pin-b-par.txt"
  expect no pin-compare-other-parallel in_sub pin_compare "$d/pin-a.txt" "$d/pin-b-par.txt"
  { pin_row a.step s1 8.0.1.0.0 0 on; pin_row b.step s2 8.0.1.0.0 0 off; } > "$d/pin-b-mix.txt"
  expect no pin-compare-file-mixes-configs in_sub pin_compare "$d/pin-a.txt" "$d/pin-b-mix.txt"
  cp "$d/pin-a.txt" "$d/pin-a2.txt"
  expect no pin-compare-same-pin-twice in_sub pin_compare "$d/pin-a.txt" "$d/pin-a2.txt"
  printf 'HDR,date_utc=2026-01-01T00:00:00Z\n' > "$d/pin-none.txt"
  expect no pin-compare-no-rows in_sub pin_compare "$d/pin-none.txt" "$d/pin-none.txt"
  expect no pin-compare-one-side-empty in_sub pin_compare "$d/pin-a.txt" "$d/pin-none.txt"
  { pin_row a.step s1 8.0.1.0.0 0 on; } > "$d/pin-b-short.txt"
  expect no pin-compare-file-missing-on-one-side in_sub pin_compare "$d/pin-a.txt" "$d/pin-b-short.txt"

  : > "$DONE_KEYS"; : > "$STOPPED"
  sed 's/confined=0/confined=1/' "$d/confined.txt" > "$d/confined-rows.txt"
  resume_scan load "$d/confined-rows.txt"
  expect ok confined-row-done-when-confined in_cfg 'CONFINE=/opt/venv:/lib; CONFINED=1' is_done s1 mesh 4g 1
  expect no confined-row-not-done-when-unconfined is_done s1 mesh 4g 1
  expect ok confined-stop-row-when-confined in_cfg 'CONFINE=/opt/venv:/lib; CONFINED=1' is_stopped step mesh 4g real 200
  expect no confined-stop-row-not-stop-when-unconfined is_stopped step mesh 4g real 200
  expect ok confined-block-stop-line-inherits-confinement in_cfg 'CONFINE=/opt/venv:/lib; CONFINED=1' is_stopped iges mesh 4g real 400
  expect no confined-block-stop-line-not-stop-when-unconfined is_stopped iges mesh 4g real 400
  : > "$DONE_KEYS"; : > "$STOPPED"
  resume_scan load "$d/synthetic.txt"
  expect ok stop-same-config is_stopped step mesh 4g real 200
  expect ok stop-larger-rung is_stopped step mesh 4g real 250
  expect no stop-smaller-rung is_stopped step mesh 4g real 150
  expect ok stop-ladder-line-inherits-header is_stopped iges mesh 4g real 400
  expect no stop-other-parallel in_cfg 'PARALLEL=off' is_stopped step mesh 4g real 200
  expect no stop-other-cpus in_cfg 'CPUS=4' is_stopped step mesh 4g real 200
  expect no stop-other-lp in_cfg 'LP=2' is_stopped step mesh 4g real 200
  expect no stop-other-quality in_cfg 'QUALITY=fine' is_stopped step mesh 4g real 200
  expect no stop-other-variant in_cfg 'VARIANT=pruned' is_stopped step mesh 4g real 200
  expect no stop-any-other-parallel in_cfg 'PARALLEL=off' is_stopped_any step mesh 4g real
  expect ok stop-any-same-config is_stopped_any step mesh 4g real

  a="$evid/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt"
  b="$evid/71-06-ladder-4g-ocp7.9.3.1.1.txt"
  if [ -s "$a" ] && [ -s "$b" ]; then
    local ta tb1 tb2
    ta=$(sed -n 's/^HDR,image=//p' "$a" | sed -n 1p)
    tb1=$(sed -n 's/^HDR,image=//p' "$b" | sed -n 1p)
    tb2=$(sed -n 's/^HDR,image=//p' "$b" | tail -n 1)
    expect ok committed-par-off-lp2-own-config in_cfg "PARALLEL=off; LP=2; EXPECT_IMAGE=$ta" check_resume_file "$a"
    expect no committed-par-off-lp2-as-par-on in_cfg "PARALLEL=on; LP=default; EXPECT_IMAGE=$ta" check_resume_file "$a"
    expect no committed-par-off-lp2-current-image in_cfg "PARALLEL=off; LP=2; EXPECT_IMAGE=$(cad_image_tag 7.9.3.1.1 full)" check_resume_file "$a"
    expect no committed-par-on-as-par-off in_cfg "PARALLEL=off; LP=2; EXPECT_IMAGE=$tb2" check_resume_file "$b"
    if [ "$tb1" != "$tb2" ]; then
      expect_keys image committed-par-on-spans-two-image-builds in_cfg "EXPECT_IMAGE=$tb2" check_resume_file "$b"
    else
      expect ok committed-par-on-own-config in_cfg "EXPECT_IMAGE=$tb1" check_resume_file "$b"
    fi
  else
    echo "SELF_TEST_NOTE,committed_evidence=absent,skipped=5"
  fi

  rm -rf "$d"
  echo "SELF_TEST,cases=$n,failed=$fails"
  [ "$fails" = 0 ]
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
    --quality-summary) QUALITY_FILE=${2:?}; shift 2 ;;
    --check-resume) CHECK_RESUME=${2:?}; shift 2 ;;
    --expect-image) EXPECT_IMAGE=${2:?}; shift 2 ;;
    --expect-image-id) EXPECT_IMAGE_ID=${2:?}; shift 2 ;;
    --self-test) SELF_TEST=1; shift ;;
    -h|--help) usage ;;
    --) shift; while [ $# -gt 0 ]; do INPUTS+=("$1"); shift; done ;;
    -*) echo "error: unknown option $1" >&2; usage ;;
    *) INPUTS+=("$1"); shift ;;
  esac
done

[ -z "$PIN_A" ] || pin_compare "$PIN_A" "$PIN_B"
[ -z "$THREADS_FILE" ] || threads_decision "$THREADS_FILE"
[ -z "$QUALITY_FILE" ] || quality_summary "$QUALITY_FILE"
[ "$SELF_TEST" = 0 ] || { self_test; exit $?; }

[ -z "$CONFINE" ] || CONFINED=1
if [ -z "$CHECK_RESUME" ] && { [ -n "$EXPECT_IMAGE" ] || [ -n "$EXPECT_IMAGE_ID" ]; }; then
  echo "error: --expect-image and --expect-image-id go with --check-resume only" >&2
  exit 64
fi
[ -n "$EXPECT_IMAGE" ] || EXPECT_IMAGE=$(cad_image_tag "$OCP" "$VARIANT")
case "$QUALITY" in draft|standard|fine) ;; *) echo "error: --quality must be draft, standard or fine" >&2; exit 64 ;; esac
case "$PARALLEL" in on|off|affinity) ;; *) echo "error: --parallel must be on, off or affinity" >&2; exit 64 ;; esac
case "$LP" in default) ;; *[!0-9]*|'') echo "error: --lp-threads must be a positive integer or default" >&2; exit 64 ;; esac
if [ -n "$CHECK_RESUME" ]; then
  rc=0
  check_resume_file "$CHECK_RESUME" || rc=$?
  if [ "$rc" = 0 ]; then echo "RESUME_CONFIG,file=$(basename "$CHECK_RESUME"),status=accepted"
  else echo "RESUME_CONFIG,file=$(basename "$CHECK_RESUME"),status=rejected,rc=$rc"; fi
  exit "$rc"
fi
[ ${#INPUTS[@]} -gt 0 ] || [ "$INCLUDE_PRIVATE" = 1 ] || usage
[ -z "$FLOOR_FROM" ] || [ -s "$FLOOR_FROM" ] || { echo "error: --floor-from file is empty or missing" >&2; exit 64; }

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is required" >&2
  exit 69
fi

if [ -n "$RESUME" ] && [ -s "$RESUME" ]; then
  resume_rc=0
  check_resume_file "$RESUME" || resume_rc=$?
  if [ "$resume_rc" != 0 ]; then
    echo "error: $RESUME was measured under a different configuration than this run; resume only with the same image build, ocp, variant, memory, cpus, confine-ro, parallel, lp-threads, quality and budget-s" >&2
    exit 65
  fi
fi

abspath() {
  local p=$1
  if [ -d "$p" ]; then (cd "$p" && pwd -P)
  else printf '%s/%s\n' "$(cd "$(dirname "$p")" && pwd -P)" "$(basename "$p")"; fi
}

list_cad_files() {
  find "$1" -type f ! -path '*/reject/*' \( -iname '*.step' -o -iname '*.stp' -o -iname '*.iges' \
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
EXPECT_IMAGE=$TAG
EXPECT_IMAGE_ID=$(docker image inspect "$TAG" --format '{{.Id}}')
EXPECT_ARCH=$(docker version --format '{{.Server.Arch}}')
if [ -n "$RESUME" ] && [ -s "$RESUME" ]; then
  resume_rc=0
  check_resume_file "$RESUME" || resume_rc=$?
  if [ "$resume_rc" != 0 ]; then
    echo "error: $RESUME was measured on another image id or architecture than the image this run uses" >&2
    exit 65
  fi
fi

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
  resume_scan load "$OUT"
fi

FLOOR_TSV="$WORK/floor.tsv"
: > "$FLOOR_TSV"
if [ -n "$FLOOR_FROM" ]; then
  floor_rows "$FLOOR_FROM" "$WORK/floor.counts" > "$FLOOR_TSV"
  read -r FLOOR_USED FLOOR_OTHER < "$WORK/floor.counts"
  emit "FLOOR,from=$(basename "$FLOOR_FROM"),frac=$FLOOR_FRAC,rows_used=$FLOOR_USED,rows_of_another_configuration=$FLOOR_OTHER"
  if [ "$FLOOR_USED" -lt 1 ]; then
    echo "error: $FLOOR_FROM has no 4g row at this run's ocp, variant, cpus, parallel, lp-threads, quality and confinement; a floor from nothing would skip nothing and say so nowhere" >&2
    exit 64
  fi
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

CFG_TAIL="variant=$VARIANT,cpus=$CPUS,par=$PARALLEL,lp=$LP,quality=$QUALITY,confined=$CONFINED"
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
        emit "LADDER_STOP,fmt=$fmt,op=$op,mem=$mem,ocp=$OCP,series=$series,reason=top,at_bytes=$top,$CFG_TAIL"
      else
        emit "LADDER_STOP,fmt=$fmt,op=$op,mem=$mem,ocp=$OCP,series=$series,reason=top,at_bytes=$top,floor_skipped=$skipped,$CFG_TAIL"
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
            emit "LADDER_STOP,fmt=$fmt,op=$op,mem=$mem,ocp=$OCP,series=$series,reason=$PRE_REASON,at_bytes=$bytes,via=prestep,$CFG_TAIL"
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
          emit "LADDER_STOP,fmt=$fmt,op=$op,mem=$mem,ocp=$OCP,series=$series,reason=$CELL_STOP,at_bytes=$bytes,$CFG_TAIL"
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
