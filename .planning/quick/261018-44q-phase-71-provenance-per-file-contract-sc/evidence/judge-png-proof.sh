set -uo pipefail
D=internal/convert/testdata/cad/render-bg
P=scripts/cad-render-probe.sh
W="$(mktemp -d "${TMPDIR:-/tmp}/judge-png-proof.XXXXXX")"
trap 'rm -rf "$W"' EXIT
bad=0

echo "HDR,head=$(git rev-parse --short HEAD),probe_sha256=$(shasum -a 256 "$P" | cut -c1-12),go=$(GOTOOLCHAIN=go1.26.4 go env GOVERSION)"
echo "== render-bg: size and background from the file name"
for f in "$D"/*.png; do
  b=$(basename "$f" .png)
  bg=${b##*_}
  size=$(printf '%s\n' "$b" | sed -E 's/.*_(256|512|1280|2048)_(opaque|transparent)$/\1/')
  out=$(bash "$P" judge-png --size "$size" --background "$bg" "$f" 2>"$W/err")
  rc=$?
  printf '%s\nRC,frame=%s.png,rc=%s\n' "$out" "$b" "$rc"
  printf '%s\t%s\t%s\n' "$b" "$out" "$rc" >> "$W/table.tsv"
  [ "$rc" = 0 ] || bad=1
done

echo "== cadRealPairs (internal/convert/cadpixelbg_test.go) against the judge-png lines"
awk '/^var cadRealPairs = \[\]cadRealPair\{/{on = 1; next} on && /^\}/{on = 0} on' internal/convert/cadpixelbg_test.go |
  sed -n 's/^[[:space:]]*{"\([^"]*\)", "\([^"]*\)", \([0-9]*\)},$/\1 \2 \3/p' > "$W/pairs.txt"
n=0
while read -r name mode size; do
  for bg in opaque transparent; do
    n=$((n + 1))
    line=$(awk -F'\t' -v k="${name}_$bg" '$1 == k {print $2}' "$W/table.tsv")
    case "$mode" in
      blob|thin) want="ok=1,mode=$mode,rule=none," ;;
      *) want="ok=0,mode=none,rule=$mode," ;;
    esac
    got=$(printf '%s\n' "$line" | sed -E 's/^GOJUDGE,frame=[^,]*,(ok=[^,]*,mode=[^,]*,rule=[^,]*,).*/\1/')
    size_seen=$(printf '%s\n' "$line" | sed -n 's/.*,w=\([0-9]*\),h=\([0-9]*\),.*/\1x\2/p')
    if [ "$got" = "$want" ] && [ "$size_seen" = "${size}x$size" ]; then m=1; else m=0; bad=1; fi
    echo "PAIR,name=$name,background=$bg,cadRealPairs_mode=$mode,size=$size,want=${want%,},got=${got%,},frame=$size_seen,match=$m"
  done
done < "$W/pairs.txt"
echo "PAIRS,compared=$n,expected=10"
[ "$n" = 10 ] || bad=1

echo "== control: the transparent nauo4 frame judged as opaque"
out=$(bash "$P" judge-png --size 1280 --background opaque "$D/nauo4_iso_1280_transparent.png" 2>/dev/null); rc=$?
printf '%s\nRC,rc=%s\n' "$out" "$rc"
case "$out" in GOJUDGE,*,ok=0,*rule=background,*) [ "$rc" = 0 ] || bad=1 ;; *) bad=1 ;; esac

echo "== control: a PNG path that does not exist"
out=$(bash "$P" judge-png --size 256 --background opaque "$W/does-not-exist.png" 2>/dev/null); rc=$?
printf '%s\nRC,rc=%s\n' "$out" "$rc"
case "$out" in GOJUDGE,*,ok=error,*) [ "$rc" = 1 ] || bad=1 ;; *) bad=1 ;; esac

echo "== control: the blank frames of judge mode (blank_png of the probe, 1280)"
python3 - "$W/blank_opaque.png" "$W/blank_transparent.png" <<'PY'
import struct, sys, zlib
def chunk(t, d):
    c = struct.pack(">I", len(d)) + t + d
    return c + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
for path, kind in ((sys.argv[1], "opaque"), (sys.argv[2], "transparent")):
    size = 1280
    if kind == "transparent":
        ctype, row = 6, b"\x00" + b"\x00" * (4 * size)
    else:
        ctype, row = 2, b"\x00" + b"\xff" * (3 * size)
    with open(path, "wb") as fh:
        fh.write(b"\x89PNG\r\n\x1a\n")
        fh.write(chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, ctype, 0, 0, 0)))
        fh.write(chunk(b"IDAT", zlib.compress(row * size, 6)))
        fh.write(chunk(b"IEND", b""))
PY
for bg in opaque transparent; do
  out=$(bash "$P" judge-png --size 1280 --background "$bg" "$W/blank_$bg.png" 2>/dev/null); rc=$?
  printf '%s\nRC,rc=%s\n' "$out" "$rc"
  case "$out" in GOJUDGE,*,ok=0,*rule=J1,*) [ "$rc" = 0 ] || bad=1 ;; *) bad=1 ;; esac
done

echo "== control: judge-png runs no docker (a logging stub docker first on PATH)"
mkdir -p "$W/stub"
printf '#!/bin/sh\necho "docker $*" >> "%s/docker.log"\nexit 99\n' "$W" > "$W/stub/docker"
chmod 0755 "$W/stub/docker"
: > "$W/docker.log"
out=$(PATH="$W/stub:$PATH" bash "$P" judge-png --size 1280 --background transparent "$D/nauo4_iso_1280_transparent.png" 2>/dev/null); rc=$?
PATH="$W/stub:$PATH" docker probe-the-stub >/dev/null 2>&1; stub_rc=$?
printf '%s\nRC,rc=%s\n' "$out" "$rc"
echo "DOCKER_STUB,stub_self_test_rc=$stub_rc,log_lines_after_judge_png=$(grep -vc 'probe-the-stub' "$W/docker.log")"
[ "$stub_rc" = 99 ] && [ "$(grep -vc 'probe-the-stub' "$W/docker.log")" = 0 ] && [ "$rc" = 0 ] || bad=1

echo "== usage"
bash "$P" judge-png --size 256 --background sideways x.png >/dev/null 2>&1; echo "USAGE,bad_background_rc=$?"
bash "$P" judge --size 256 >/dev/null 2>&1; echo "USAGE,size_outside_judge_png_rc=$?"
bash "$P" judge-png --size 256 --background opaque >/dev/null 2>&1; echo "USAGE,no_png_rc=$?"

if [ "$bad" = 0 ]; then echo JUDGE-PNG-OK; else echo JUDGE-PNG-FAILED; exit 1; fi
