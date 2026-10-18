import sys

E = ".planning/quick/261018-44q-phase-71-provenance-per-file-contract-sc/evidence"
DOC = "internal/convert/testdata/cad/PROVENANCE.md"
ANCHORS = {
    "dxf": "| Section scope |",
    "dxf-out": "| `frames.tsv` |",
    "dxf-cr": "| `cr_injected.svg`",
    "render-bg": "| `degenerate_rod_iso_256` |",
}
RENDER = (
    "- Evidence: no sha256 was recorded when the file was committed in 056a8d2 (quick 261014-0lo); "
    "this sha256 is that of the committed file, computed by quick 261018-44q; "
    "the file has not changed since 056a8d2."
)

rows = [line.rstrip("\n").split("\t") for line in open(E + "/provenance-map.tsv", encoding="utf-8")]
blocks = {}
for rel, size, digest, evidence, kind, _ in rows:
    group = rel.split("/", 1)[0]
    out = ["## `" + rel + "` — " + size + " bytes", "", "- Licence: generated", "- sha256: " + digest]
    if group == "render-bg":
        out.append("- Command: see the pair table of this group above")
        out.append(RENDER)
    else:
        out.append("- Command: see the generation command of this group above")
        out.append("- Evidence: `" + evidence + "`, the " + kind + " line of this file (same sha256 and bytes)")
    out.append("")
    blocks.setdefault(group, []).append("\n".join(out) + "\n")

for group in blocks:
    rels = [r[0] for r in rows if r[0].startswith(group + "/")]
    if rels != sorted(rels):
        sys.exit("not sorted: " + group)
    with open(E + "/sections-" + group + ".md", "w", encoding="utf-8") as f:
        f.write("".join(blocks[group]))

if len(sys.argv) > 1 and sys.argv[1] == "--insert":
    lines = open(DOC, encoding="utf-8").read().split("\n")
    for group, anchor in ANCHORS.items():
        idx = [i for i, l in enumerate(lines) if l.startswith(anchor)]
        if len(idx) != 1:
            sys.exit("anchor " + repr(anchor) + " found " + str(len(idx)) + " times")
        i = idx[0]
        if lines[i + 1] != "":
            sys.exit("anchor " + repr(anchor) + " is not followed by a blank line")
        text = open(E + "/sections-" + group + ".md", encoding="utf-8").read()
        lines[i + 2:i + 2] = text.rstrip("\n").split("\n") + [""]
    with open(DOC, "w", encoding="utf-8") as f:
        f.write("\n".join(lines))
    print("INSERTED," + ",".join(g + "=" + str(len(blocks[g])) for g in ANCHORS))
else:
    print("SECTIONS," + ",".join(g + "=" + str(len(blocks[g])) for g in ANCHORS))
