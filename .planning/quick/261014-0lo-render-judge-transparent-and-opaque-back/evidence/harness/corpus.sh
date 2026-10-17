#!/usr/bin/env bash
set -euo pipefail
LO=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad/lo
ROOT=/Users/apaderin/dev/octoconv
C=$ROOT/internal/convert/testdata/cad
X="$LO/exp.sh"
free_gib() { docker run --rm --entrypoint df octoconv-cad-worker:latest -k / | awk 'NR==2 { printf "%d", $4 / 1048576 }'; }
[ "$(free_gib)" -ge 6 ] || { echo "VM free below 6 GiB"; exit 1; }

stats() {
	local src=$1 name=$2
	docker run --rm --network none --cpus 2 --memory 4g --memory-swap 4g --user nobody -e HOME=/tmp -v "$(dirname "$src"):/in:ro" -v "$LO/stl:/job" \
		--entrypoint /usr/local/bin/cad-mesh octoconv-cad-worker:latest stats --measure "/job/$name.json" "/in/$(basename "$src")" "/job/$name.stl" >/dev/null
}

for p in LinearSlide-MGN15x-Block LM8UU SC8UU V-Slot-20x20-100mm 2020_corner_bracket-Corner; do
	[ -s "$LO/stl/$p.stl" ] || "$X" mesh "$C/public/$p.step" "$p" step standard
done
for p in degenerate_rod degenerate_plate units_inch; do
	[ -s "$LO/stl/$p.stl" ] || "$X" mesh "$C/synthetic/$p.step" "$p" step standard
done
[ -s "$LO/stl/stl_ascii_box.stl" ] || stats "$C/synthetic/stl_ascii_box.stl" stl_ascii_box
[ -s "$LO/stl/w15.stl" ] || stats "$LO/out/stl-ascii-nasa-sls-block1-1MiB.stl" w15

pair() {
	local name=$1 view=$2 size=$3
	for bg in opaque transparent; do
		[ -s "$LO/png/$name.$view.$size.$bg.png" ] || "$X" render "$name" "$view" "$size" "$bg"
	done
	echo "$name $view $size" >> "$LO/pairs.list"
}

: > "$LO/pairs.list"
for p in LinearSlide-MGN15x-Block LM8UU SC8UU V-Slot-20x20-100mm 2020_corner_bracket-Corner; do
	for v in iso front back left right top bottom; do pair "$p" "$v" 1280; done
done
for p in LM8UU V-Slot-20x20-100mm; do
	for s in 256 512 2048; do pair "$p" iso "$s"; done
done
for p in degenerate_rod degenerate_plate units_inch stl_ascii_box; do
	for s in 256 1280; do pair "$p" iso "$s"; done
	pair "$p" front 1280
done
for n in n4 n16 n64; do
	for s in 1280 2048; do pair "$n" iso "$s"; done
done
for s in 256 1280 2048; do pair w15 iso "$s"; done
pair LinearSlide-MGN15x-Block iso 256
echo "free_gib=$(free_gib)"
