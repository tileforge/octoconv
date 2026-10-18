set -u
Q=.planning/quick/261018-44q-phase-71-provenance-per-file-contract-sc
P=internal/convert/testdata/cad/PROVENANCE.md
G="$TMPDIR/PROVENANCE.green.md"
cp "$P" "$G"

mutate() {
  python3 - "$P" "$1" <<'PY'
import sys
path, which = sys.argv[1], sys.argv[2]
lines = open(path, encoding="utf-8").read().split("\n")
def heading(rel):
    idx = [i for i, l in enumerate(lines) if l.startswith("## `" + rel + "` — ")]
    assert len(idx) == 1, rel
    return idx[0]
if which == "M1":
    i = heading("dxf-out/frames.tsv")
    j = next(k for k in range(i + 1, len(lines)) if lines[k].startswith("- sha256: "))
    v = lines[j][len("- sha256: "):]
    lines[j] = "- sha256: " + ("1" if v[0] != "1" else "2") + v[1:]
elif which == "M2":
    i = heading("dxf/all_hidden.dxf")
    j = next(k for k in range(i + 1, len(lines)) if lines[k].startswith("- Licence: "))
    lines[j] = lines[j] + "."
elif which == "M3":
    i = heading("render-bg/nauo4_iso_1280_transparent.png")
    j = next(k for k in range(i + 1, len(lines)) if lines[k].startswith("## ") or lines[k] == "---")
    del lines[i:j]
open(path, "w", encoding="utf-8").write("\n".join(lines))
PY
}

run() {
  local name=$1 gate=$2 rc
  echo "== $name"
  go test -count=1 -run '^TestCADProvenanceCoversEveryCommittedFile$' ./internal/convert/ > "$TMPDIR/mut.out" 2>&1
  rc=$?
  echo "gotest_rc=$rc"
  grep -E 'provenance problems:' "$TMPDIR/mut.out" | sed 's/^[[:space:]]*//'
  grep -vE 'provenance problems:|^(---|FAIL|ok|PASS)' "$TMPDIR/mut.out" | sed 's/^[[:space:]]*//'
  bash "$Q/evidence/$gate" > "$TMPDIR/gate.out" 2>&1
  rc=$?
  echo "gate=$gate rc=$rc"
  tail -1 "$TMPDIR/gate.out" | tr ' ' '\n' | grep -E 'testdata|MISSING|files=|CORPUS-OK|PROVENANCE-COMPLETE' | head -5
}

restore() {
  cp "$G" "$P"
  cmp "$G" "$P" && echo "restored=1"
}

mutate M1
run M1-dxf-out-sha256-digit gate-provenance-complete.sh
restore

mutate M2
run M2-licence-period gate-corpus-ok.sh
restore

mutate M3
run M3-render-bg-section-removed gate-provenance-complete.sh
restore

cp internal/convert/testdata/cad/dxf/all_hidden.dxf internal/convert/testdata/cad/dxf/zz_mutation.dxf
run M4-extra-file gate-provenance-complete.sh
rm internal/convert/testdata/cad/dxf/zz_mutation.dxf
restore

echo "porcelain=[$(git status --porcelain internal/convert/testdata/cad | grep -v " $P\$")]"
