#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT=$(pwd)

IMAGE="${CAD_IMAGE:-octoconv-cad-worker:latest}"
OLD_REF="${CAD_PARTIAL_OLD_REF:-92336e9}"
HELPER=cmd/cad-worker/helper/cad-mesh
SOURCE=internal/convert/testdata/cad/synthetic/units_inch.step
EVID="${CAD_PARTIAL_EVIDENCE:-}"
INTACT_TRIANGLES=12
PLANE_AXIS_OLD="#35 = DIRECTION('',(1.,0.,-0.));"
PLANE_AXIS_NEW="#35 = DIRECTION('',(0.,0.,0.));"

W=$(mktemp -d "${TMPDIR:-/tmp}/cad-partial.XXXXXX")
trap 'rm -rf "$W"' EXIT
mkdir -p "$W/in" "$W/out" "$W/h"
chmod 777 "$W/out"

log() { echo "$*" >&2; }
ev() {
	echo "$*"
	if [ -n "$EVID" ]; then echo "$*" >>"$EVID"; fi
}
fail() {
	ev "SUMMARY,result=FAIL,reason=$*"
	exit 1
}

command -v docker >/dev/null || fail "docker is not available"
docker image inspect "$IMAGE" >/dev/null 2>&1 || fail "image $IMAGE is not present"

if [ -n "$EVID" ]; then : >"$EVID"; fi
ev "HDR,date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ),commit=$(git rev-parse HEAD),tracked_dirt=$(git status --short --untracked-files=no | wc -l | tr -d ' '),old_ref=$OLD_REF"

cp "$SOURCE" "$W/in/intact.step"
python3 - "$SOURCE" "$W/in/partial_face.step" "$PLANE_AXIS_OLD" "$PLANE_AXIS_NEW" <<'PY'
import sys
src, dst, old, new = sys.argv[1:5]
text = open(src).read()
if text.count(old) != 1:
    sys.exit("the plane axis line is not unique in " + src)
open(dst, "w").write(text.replace(old, new))
PY
cmp -s "$W/in/intact.step" "$W/in/partial_face.step" && fail "the partial fixture equals the intact source"

git show "$OLD_REF:$HELPER" >"$W/h/cad-mesh-old"
cp "$HELPER" "$W/h/cad-mesh-new"

run_helper() {
	local which=$1 input=$2 rc=0
	rm -f "$W/out/$which-$input.stl"
	docker run --rm --network none --cpus 2 --memory 2g --user nobody -e HOME=/tmp -e LANG=C.UTF-8 \
		-v "$W/h:/h:ro" -v "$W/in:/in:ro" -v "$W/out:/out" \
		--entrypoint /opt/venv/bin/python "$IMAGE" -I -B "/h/cad-mesh-$which" \
		--format step "/in/$input.step" "/out/$which-$input.stl" >"$W/$which-$input.log" 2>&1 || rc=$?
	local line stl=absent size triangles=none
	line=$(grep '^CADVERDICT ' "$W/$which-$input.log" | tail -1 | sed 's/^CADVERDICT //')
	[ -n "$line" ] || fail "$which on $input printed no verdict: $(tail -3 "$W/$which-$input.log")"
	if [ -f "$W/out/$which-$input.stl" ]; then
		stl=present
		size=$(wc -c <"$W/out/$which-$input.stl" | tr -d ' ')
		triangles=$(((size - 84) / 50))
	fi
	RESULT_RC=$rc
	RESULT_VERDICT=$(printf '%s' "$line" | jq -r .verdict)
	RESULT_REASON=$(printf '%s' "$line" | jq -r .reason)
	RESULT_REPORTED=$(printf '%s' "$line" | jq -r .triangles)
	RESULT_STL=$stl
	RESULT_STL_TRIANGLES=$triangles
	ev "PARTIAL,helper=$which,input=$input,exit=$rc,verdict=$RESULT_VERDICT,reason=$RESULT_REASON,reported_triangles=$RESULT_REPORTED,stl=$stl,stl_triangles=$triangles,intact_triangles=$INTACT_TRIANGLES"
}

run_helper old intact
[ "$RESULT_VERDICT" = ok ] && [ "$RESULT_STL_TRIANGLES" = "$INTACT_TRIANGLES" ] || fail "the old helper did not mesh the intact source to $INTACT_TRIANGLES triangles"
run_helper new intact
[ "$RESULT_RC" = 0 ] && [ "$RESULT_VERDICT" = ok ] && [ "$RESULT_STL_TRIANGLES" = "$INTACT_TRIANGLES" ] && [ "$RESULT_REPORTED" = "$INTACT_TRIANGLES" ] || fail "the new helper did not mesh the intact source to $INTACT_TRIANGLES triangles"

run_helper old partial_face
[ "$RESULT_RC" = 0 ] && [ "$RESULT_VERDICT" = ok ] && [ "$RESULT_STL" = present ] && [ "$RESULT_STL_TRIANGLES" -gt 0 ] && [ "$RESULT_STL_TRIANGLES" -lt "$INTACT_TRIANGLES" ] ||
	fail "the old helper did not reproduce a partial STL on the partial fixture (verdict=$RESULT_VERDICT stl_triangles=$RESULT_STL_TRIANGLES)"
OLD_PARTIAL=$RESULT_STL_TRIANGLES
run_helper new partial_face
[ "$RESULT_RC" = 2 ] && [ "$RESULT_VERDICT" = incomplete ] && [ "$RESULT_STL" = absent ] || fail "the new helper did not refuse the partial fixture before writing (exit=$RESULT_RC verdict=$RESULT_VERDICT stl=$RESULT_STL)"

ev "SUMMARY,result=ALL PASS,old_partial_triangles=$OLD_PARTIAL,intact_triangles=$INTACT_TRIANGLES,new_verdict=incomplete,new_stl=absent,image=$(docker image inspect "$IMAGE" --format '{{.Id}}'),fixture_sha256=$(shasum -a 256 "$W/in/partial_face.step" | cut -d' ' -f1),old_ref=$OLD_REF"
