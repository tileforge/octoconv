#!/usr/bin/env bash
# Doc-vs-Go cross-check for CONF-05: each confined class's measured path set
# is recorded ONCE, in SANDBOX-PATHS.md, between a pair of conf05 marker
# blocks -- and the Go var every converter actually enforces must equal it
# exactly, or the two have drifted apart with nothing to say so.
#
# Usage:
#   scripts/conf05-crosscheck.sh [--doc] <class>...
#
# Seven classes: pandoc, ocr, av, audio, html, probe (probe's Go file lands
# in a later plan -- until then this script exits 3 for it, its documented
# "not there yet" status). html and probe additionally carry a conf05
# readwrite block (CONF-06's shared-writable-path decision) -- the other
# four classes' writable set is the job directory alone and have no such
# block.
#
# --doc prints what the DOCUMENT says (readonly/engines/readwrite counts and
# lists) and exits 0 -- no Go file is read. Without --doc, the doc lists are
# diffed against internal/convert/<class>sandbox.go's var <class>ReadOnly /
# var <class>Engines / (html, probe only) var <class>ReadWrite.
#
# Exit codes:
#   0  doc read cleanly (--doc), or doc and Go agree exactly
#   1  doc and Go disagree (diff printed)
#   2  a conf05 marker block is missing from SANDBOX-PATHS.md for a
#      requested class -- never treated as an empty, passing set
#   3  the Go file exists but the named var was not found in it -- distinct
#      from a mismatch: the var is simply not there yet (e.g. before probe's
#      own plan lands)
set -euo pipefail

die() { echo "error: $*" >&2; exit 1; }

DOC_FILE=".planning/research/SANDBOX-PATHS.md"

DOC_MODE=0
if [ "${1:-}" = "--doc" ]; then
  DOC_MODE=1
  shift
fi
[ $# -gt 0 ] || die "usage: $0 [--doc] <class>..."

[ -f "$DOC_FILE" ] || die "$DOC_FILE not found (run from the repository root)"

class_go_file() {
  case "$1" in
    pandoc) echo "internal/convert/pandocsandbox.go" ;;
    ocr)    echo "internal/convert/ocrsandbox.go" ;;
    av)     echo "internal/convert/avsandbox.go" ;;
    audio)  echo "internal/convert/audiosandbox.go" ;;
    html)   echo "internal/convert/htmlsandbox.go" ;;
    probe)  echo "internal/convert/probesandbox.go" ;;
    *) die "unknown class: $1 (expected pandoc, ocr, av, audio, html or probe)" ;;
  esac
}
class_go_readonly_var() {
  case "$1" in
    pandoc) echo "pandocReadOnly" ;;
    ocr)    echo "ocrReadOnly" ;;
    av)     echo "avReadOnly" ;;
    audio)  echo "audioReadOnly" ;;
    html)   echo "htmlReadOnly" ;;
    probe)  echo "probeReadOnly" ;;
  esac
}
class_go_engines_var() {
  case "$1" in
    pandoc) echo "pandocEngines" ;;
    ocr)    echo "ocrEngines" ;;
    av)     echo "avEngines" ;;
    audio)  echo "audioEngines" ;;
    html)   echo "htmlEngines" ;;
    probe)  echo "probeEngines" ;;
  esac
}
# class_go_readwrite_var prints the Go var naming a class's shared writable
# set BEYOND the job directory (CONF-06) -- html and probe only; empty for
# every class whose writable set is the job directory alone (pandoc/ocr/av/
# audio), which the main loop below reads as "no readwrite block for this
# class", not as an empty passing set.
class_go_readwrite_var() {
  case "$1" in
    html)  echo "htmlReadWrite" ;;
    probe) echo "probeReadWrite" ;;
    *) : ;;
  esac
}

# require_doc_block checks that BOTH the open and close marker for
# class/kind are present in $DOC_FILE -- a missing block must fail loud, not
# read as an empty (and therefore trivially "passing") list.
require_doc_block() {
  local class=$1 kind=$2
  local open="<!-- conf05:${class}:${kind} -->"
  local close="<!-- /conf05:${class}:${kind} -->"
  grep -qF "$open" "$DOC_FILE" && grep -qF "$close" "$DOC_FILE"
}

# extract_doc_block prints the lines strictly between the open and close
# marker for class/kind. Caller must have already checked require_doc_block.
extract_doc_block() {
  local class=$1 kind=$2
  local open="<!-- conf05:${class}:${kind} -->"
  local endmark="<!-- /conf05:${class}:${kind} -->"
  # NOTE: the awk variable is named "endmark", not "close" -- BWK/one-true-awk
  # (macOS /usr/bin/awk) treats "close" as its builtin close() function name
  # and refuses "-v close=..." with a syntax error, not a warning.
  awk -v open="$open" -v endmark="$endmark" '
    $0 == open { f=1; next }
    $0 == endmark { f=0 }
    f { print }
  ' "$DOC_FILE"
}

doc_readonly_lines() {
  extract_doc_block "$1" readonly | grep '^/' || true
}
doc_engine_lines() {
  extract_doc_block "$1" engines | grep -v '^[[:space:]]*$' || true
}
doc_readwrite_lines() {
  extract_doc_block "$1" readwrite | grep '^/' || true
}

# extract_go_list prints, one per line, the quoted string elements of
# `var <varname> = []string{...}` in file -- the gofmt shape every class's
# sandbox.go var takes, whether the whole declaration is on one line
# (`[]string{}`, `[]string{"pandoc"}`) or the items are one per line ending
# in a lone `}`. Prints __NOTFOUND__ (only) when the var is not present in
# file at all, or when file itself does not exist.
extract_go_list() {
  local file=$1 varname=$2
  [ -f "$file" ] || { echo "__NOTFOUND__"; return; }
  awk -v var="$varname" '
    BEGIN { found = 0; inblock = 0 }
    {
      if (!found) {
        if ($0 ~ ("^var " var " = \\[\\]string\\{")) {
          found = 1
          rest = $0
          sub(/^.*\[\]string\{/, "", rest)
        } else {
          next
        }
      } else if (inblock) {
        rest = $0
      } else {
        next
      }

      trimmed = rest
      gsub(/^[ \t]+/, "", trimmed)
      gsub(/[ \t]+$/, "", trimmed)
      if (trimmed == "}") { done = 1; exit }

      s = rest
      while (match(s, /"[^"]*"/)) {
        print substr(s, RSTART + 1, RLENGTH - 2)
        s = substr(s, RSTART + RLENGTH)
      }

      tail = rest
      gsub(/"[^"]*"/, "", tail)
      gsub(/[ \t,]/, "", tail)
      if (tail == "}") { done = 1; exit }

      inblock = 1
    }
    END { if (!found) print "__NOTFOUND__" }
  ' "$file"
}

status=0
for class in "$@"; do
  require_doc_block "$class" readonly \
    || { echo "error: missing conf05 readonly marker block for class: $class" >&2; exit 2; }
  require_doc_block "$class" engines \
    || { echo "error: missing conf05 engines marker block for class: $class" >&2; exit 2; }

  ro_lines=$(doc_readonly_lines "$class")
  eng_lines=$(doc_engine_lines "$class")
  ro_count=0
  [ -n "$ro_lines" ] && ro_count=$(printf '%s\n' "$ro_lines" | grep -c '^/')
  eng_count=0
  [ -n "$eng_lines" ] && eng_count=$(printf '%s\n' "$eng_lines" | grep -vc '^[[:space:]]*$')

  # A class with a readwrite var (html, probe) MUST carry a conf05 readwrite
  # block -- exit 2 when missing, never read as an empty passing set. A
  # class without one (pandoc/ocr/av/audio) keeps its current output
  # byte-for-byte: has_rw stays 0 and every readwrite-shaped branch below is
  # skipped entirely.
  rw_var=$(class_go_readwrite_var "$class")
  has_rw=0
  rw_lines=""
  rw_count=0
  if [ -n "$rw_var" ]; then
    has_rw=1
    require_doc_block "$class" readwrite \
      || { echo "error: missing conf05 readwrite marker block for class: $class" >&2; exit 2; }
    rw_lines=$(doc_readwrite_lines "$class")
    [ -n "$rw_lines" ] && rw_count=$(printf '%s\n' "$rw_lines" | grep -c '^/')
  fi

  if [ "$DOC_MODE" = 1 ]; then
    if [ "$has_rw" = 1 ]; then
      echo "DOC: $class readonly ($ro_count) engines ($eng_count) readwrite ($rw_count)"
    else
      echo "DOC: $class readonly ($ro_count) engines ($eng_count)"
    fi
    [ -n "$ro_lines" ] && printf '%s\n' "$ro_lines" | sed 's/^/  ro: /'
    [ -n "$eng_lines" ] && printf '%s\n' "$eng_lines" | sed 's/^/  engine: /'
    [ "$has_rw" = 1 ] && [ -n "$rw_lines" ] && printf '%s\n' "$rw_lines" | sed 's/^/  rw: /'
    continue
  fi

  go_file=$(class_go_file "$class")
  ro_var=$(class_go_readonly_var "$class")
  eng_var=$(class_go_engines_var "$class")

  go_ro_raw=$(extract_go_list "$go_file" "$ro_var")
  if [ "$go_ro_raw" = "__NOTFOUND__" ]; then
    echo "error: var $ro_var not found in $go_file" >&2
    exit 3
  fi
  go_eng_raw=$(extract_go_list "$go_file" "$eng_var")
  if [ "$go_eng_raw" = "__NOTFOUND__" ]; then
    echo "error: var $eng_var not found in $go_file" >&2
    exit 3
  fi

  ro_diff=$(diff <(printf '%s\n' "$ro_lines") <(printf '%s\n' "$go_ro_raw") || true)
  eng_diff=$(diff <(printf '%s\n' "$eng_lines") <(printf '%s\n' "$go_eng_raw") || true)

  rw_diff=""
  if [ "$has_rw" = 1 ]; then
    go_rw_raw=$(extract_go_list "$go_file" "$rw_var")
    if [ "$go_rw_raw" = "__NOTFOUND__" ]; then
      echo "error: var $rw_var not found in $go_file" >&2
      exit 3
    fi
    rw_diff=$(diff <(printf '%s\n' "$rw_lines") <(printf '%s\n' "$go_rw_raw") || true)
  fi

  if [ -n "$ro_diff" ] || [ -n "$eng_diff" ] || [ -n "$rw_diff" ]; then
    echo "MISMATCH: $class"
    if [ -n "$ro_diff" ]; then
      echo "readonly diff (doc vs go, - doc / + go):"
      echo "$ro_diff"
    fi
    if [ -n "$eng_diff" ]; then
      echo "engines diff (doc vs go, - doc / + go):"
      echo "$eng_diff"
    fi
    if [ -n "$rw_diff" ]; then
      echo "readwrite diff (doc vs go, - doc / + go):"
      echo "$rw_diff"
    fi
    status=1
    continue
  fi

  if [ "$has_rw" = 1 ]; then
    echo "MATCH: $class readonly ($ro_count) engines ($eng_count) readwrite ($rw_count)"
  else
    echo "MATCH: $class readonly ($ro_count) engines ($eng_count)"
  fi
done

exit "$status"
