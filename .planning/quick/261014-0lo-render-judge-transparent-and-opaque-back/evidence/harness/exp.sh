#!/usr/bin/env bash
set -euo pipefail
ROOT=/Users/apaderin/dev/octoconv
LO=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad/lo
IMG=octoconv-cad-worker:latest
LIM=(--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody -e HOME=/tmp -e LANG=C.UTF-8)
chmod 777 "$LO/out" "$LO/stl" "$LO/png"

gen() {
	docker run --rm "${LIM[@]}" -v "$ROOT:/src:ro" -v "$ROOT/internal/convert/testdata/cad/public:/base:ro" -v "$LO/out:/out" -w /src \
		--entrypoint /opt/venv/bin/python "$IMG" -I -B scripts/cad-corpus-gen.py --scaled-from /base/LinearSlide-MGN15x-Block.step --kind step-nauo --targets "$1" --scaled-dir /out >/dev/null
}

mesh() {
	local src=$1 name=$2 fmt=$3 q=$4
	docker run --rm "${LIM[@]}" -v "$(dirname "$src"):/in:ro" -v "$LO/stl:/job" --entrypoint /usr/local/bin/cad-mesh "$IMG" \
		--format "$fmt" --quality "$q" --measure "/job/$name.json" "/in/$(basename "$src")" "/job/$name.stl" >/dev/null
}

render() {
	local name=$1 view=$2 size=$3 bg=$4
	docker run --rm "${LIM[@]}" -v "$LO/stl:/in:ro" -v "$LO/png:/job" --entrypoint /usr/local/bin/cad-render "$IMG" \
		--view "$view" --size "$size" --background "$bg" "/in/$name.stl" "/job/$name.$view.$size.$bg.png" >/dev/null
}

"$@"
