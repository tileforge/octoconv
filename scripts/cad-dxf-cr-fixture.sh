#!/usr/bin/env bash

set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"

OUT_DIR="$REPO_ROOT/internal/convert/testdata/cad/dxf-cr"
DOCKERFILE="$REPO_ROOT/Dockerfile.cad-worker"
LIMITS=(--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody)
IMAGE=""
BUILT=""
WORK=""

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/cad-dxf-cr-fixture.sh [--image TAG]

Writes internal/convert/testdata/cad/dxf-cr from the engine image of Dockerfile.cad-worker:
  cr_carriers.dxf        ezdxf 1.4.4, R2000, fixed metadata: every DXF way to spell a carriage
                         return that survives a readable file (caret ^M in TEXT and MTEXT, the
                         \U+000D escape in TEXT and MTEXT)
  cr_carriers.{svg,render.svg,texts.json,verdict}
                         what the production cad-dxf makes of it
  cr_injected.{svg,render.svg,texts.json,verdict}
                         the same document with CR, CRLF and CR CR written into three texts by
                         Python's ElementTree, the serializer cad-dxf uses, with covered and the
                         U+FFFD substitution recomputed from the DejaVu Sans cmap
Without --image the engine stage is built and its tag removed at exit. One FACT line per
cr_carriers text and one FILE line per written file go to stdout.
USAGE
  exit 64
}

while [ $# -gt 0 ]; do
  case "$1" in
    --image) [ $# -ge 2 ] || usage; IMAGE=$2; shift 2 ;;
    *) usage ;;
  esac
done

cleanup() {
  [ -z "$BUILT" ] || docker rmi "$BUILT" >/dev/null 2>&1 || true
  [ -z "$WORK" ] || rm -rf "$WORK"
}
trap cleanup EXIT

if [ -z "$IMAGE" ]; then
  stage=$(mktemp -d "${TMPDIR:-/tmp}/cad-dxf-cr.XXXXXX")
  mkdir -p "$stage/cmd/cad-worker/helper"
  cp "$DOCKERFILE" "$stage/Dockerfile.cad-worker"
  cp "$REPO_ROOT"/cmd/cad-worker/requirements-*.txt "$REPO_ROOT"/cmd/cad-worker/vtk_keeplist.py \
    "$REPO_ROOT"/cmd/cad-worker/vtk-keeplist.txt "$stage/cmd/cad-worker/"
  cp "$REPO_ROOT"/cmd/cad-worker/helper/* "$stage/cmd/cad-worker/helper/"
  BUILT="octoconv-cad-worker:engine75-crfx"
  docker build -f "$stage/Dockerfile.cad-worker" --target engine -t "$BUILT" "$stage" >&2
  rm -rf "$stage"
  IMAGE=$BUILT
fi

WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-dxf-cr-work.XXXXXX")
chmod 0777 "$WORK"
mkdir -p "$WORK/.home/.cache" "$WORK/.home/.config"
chmod -R 0777 "$WORK/.home"

docker run --rm -i "${LIMITS[@]}" -e HOME=/tmp -e PYTHONHASHSEED=0 -v "$WORK:/job" \
  --entrypoint /opt/venv/bin/python "$IMAGE" -B - /job/cr_carriers.dxf <<'PY'
import sys
import ezdxf
if ezdxf.__version__ != "1.4.4":
    raise SystemExit("ezdxf 1.4.4 is required, found %s" % ezdxf.__version__)
ezdxf.options.write_fixed_meta_data_for_testing = True
doc = ezdxf.new("R2000")
doc.header["$INSUNITS"] = 4
m = doc.modelspace()
m.add_line((0, -10), (200, -10))
m.add_text("E^MF", dxfattribs={"height": 5, "insert": (0, 0)})
m.add_text("J\\U+000DK", dxfattribs={"height": 5, "insert": (0, 10)})
m.add_mtext("G^MH\\PI", dxfattribs={"char_height": 5, "insert": (60, 20)})
m.add_mtext("L\\U+000DM", dxfattribs={"char_height": 5, "insert": (120, 20)})
doc.saveas(sys.argv[1])
PY

docker run --rm "${LIMITS[@]}" -e HOME=/job/.home -e XDG_CACHE_HOME=/job/.home/.cache \
  -e XDG_CONFIG_HOME=/job/.home/.config -e TMPDIR=/job -v "$WORK:/job" \
  --entrypoint /usr/local/bin/cad-dxf "$IMAGE" --max-drawn-entities 850155 \
  --texts /job/cr_carriers.texts.json --render-svg /job/cr_carriers.render.svg \
  /job/cr_carriers.dxf /job/cr_carriers.svg > "$WORK/cr_carriers.verdict"

mkdir -p "$WORK/raw"
chmod 0777 "$WORK/raw"
LC_ALL=C sed $'s/^E\\^MF$/E\rF/' "$WORK/cr_carriers.dxf" > "$WORK/raw/cr_raw.dxf"
grep -q $'^E\rF' "$WORK/raw/cr_raw.dxf" || { echo "FAIL: cr_raw.dxf carries no raw CR" >&2; exit 1; }
rc=0
docker run --rm "${LIMITS[@]}" -e HOME=/job/.home -e XDG_CACHE_HOME=/job/.home/.cache \
  -e XDG_CONFIG_HOME=/job/.home/.config -e TMPDIR=/job -v "$WORK:/job" \
  --entrypoint /usr/local/bin/cad-dxf "$IMAGE" --max-drawn-entities 850155 \
  --texts /job/raw/texts.json --render-svg /job/raw/r.svg /job/raw/cr_raw.dxf /job/raw/c.svg > "$WORK/raw/verdict" 2>/dev/null || rc=$?
printf 'FACT,file=cr_raw,carrier=raw_cr_byte_in_a_TEXT_value,exit=%s,verdict=%s\n' "$rc" \
  "$(sed -n 's/^CADVERDICT .*"reason": *"\([^"]*\)".*"verdict": *"\([^"]*\)".*/\2\/\1/p' "$WORK/raw/verdict")"

docker run --rm -i "${LIMITS[@]}" -e HOME=/tmp -v "$WORK:/job" \
  --entrypoint /opt/venv/bin/python "$IMAGE" -B - /job <<'PY'
import json
import sys
import xml.etree.ElementTree as ET
from fontTools.ttLib import TTFont

NS = "http://www.w3.org/2000/svg"
ET.register_namespace("", NS)
d = sys.argv[1]
with open(d + "/cr_carriers.texts.json", encoding="ascii") as fh:
    carriers = json.load(fh)["texts"]
for i, t in enumerate(carriers):
    print("FACT,file=cr_carriers,text=%d,value=%s,has_cr=%d,has_lf=%d" % (
        i, json.dumps(t["text"], ensure_ascii=True).replace(",", "%2C"), int("\r" in t["text"]), int("\n" in t["text"])))
cmap = TTFont("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf").getBestCmap()
tree = ET.parse(d + "/cr_carriers.svg")
els = tree.getroot().findall(".//{%s}text" % NS)
forms = ["E\rF", "J\r\nK", "G\r\rH"]
if len(els) < len(forms):
    raise SystemExit("cr_carriers.svg has %d texts, want at least %d" % (len(els), len(forms)))
for el in els:
    if el.get("font-family") != "DejaVu Sans" or el.get("font-weight"):
        raise SystemExit("text %r is not drawn in DejaVu Sans" % el.text)
texts = []
for i, el in enumerate(els):
    if i < len(forms):
        el.text = forms[i]
    t = el.text
    texts.append({"text": t, "covered": "".join(c for c in t if ord(c) in cmap),
                  "render": "".join(c if ord(c) in cmap else "�" for c in t)})
tree.write(d + "/cr_injected.svg", encoding="utf-8", xml_declaration=True)
for el, t in zip(els, texts):
    el.text = t["render"]
tree.write(d + "/cr_injected.render.svg", encoding="utf-8", xml_declaration=True)
with open(d + "/cr_injected.texts.json", "w", encoding="ascii") as fh:
    fh.write(json.dumps({"texts": [{"text": t["text"], "covered": t["covered"]} for t in texts]},
                        separators=(",", ":"), ensure_ascii=True))
with open(d + "/cr_carriers.verdict", encoding="ascii") as fh:
    line = fh.read().strip()
if not line.startswith("CADVERDICT "):
    raise SystemExit("cr_carriers.verdict: %r" % line)
v = json.loads(line[len("CADVERDICT "):])
if v.get("verdict") != "ok":
    raise SystemExit("cr_carriers: verdict %r" % v.get("verdict"))
v["texts"] = len(texts)
v["uncovered"] = sum(len(t["text"]) - len(t["covered"]) for t in texts)
v["render_substituted"] = sum(1 for t in texts for a, b in zip(t["text"], t["render"]) if a != b)
with open(d + "/cr_injected.verdict", "w", encoding="ascii") as fh:
    fh.write("CADVERDICT " + json.dumps(v, sort_keys=True) + "\n")
PY

grep -q $'\r' "$WORK/cr_injected.svg" || { echo "FAIL: cr_injected.svg carries no raw CR" >&2; exit 1; }
mkdir -p "$OUT_DIR"
for f in cr_carriers.dxf cr_carriers.svg cr_carriers.render.svg cr_carriers.texts.json cr_carriers.verdict \
  cr_injected.svg cr_injected.render.svg cr_injected.texts.json cr_injected.verdict; do
  cp "$WORK/$f" "$OUT_DIR/$f"
  chmod 0644 "$OUT_DIR/$f"
  printf 'FILE,name=%s,bytes=%s,sha256=%s\n' "$f" "$(wc -c < "$OUT_DIR/$f" | tr -d ' ')" "$(shasum -a 256 "$OUT_DIR/$f" | awk '{print $1}')"
done
printf 'IMAGE,id=%s\n' "$(docker image inspect --format '{{.Id}}' "$IMAGE")"
