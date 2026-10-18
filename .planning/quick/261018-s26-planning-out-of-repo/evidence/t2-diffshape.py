import subprocess
import sys

BASE = "31c35171"
EXPECTED_FILES = 28
EXPECTED_LINES = 39

M = {
    "M1": ("../../.planning/milestones/v1.16-phases/76-measured-bounds-keda-proof-client-docs/evidence/",
           "testdata/evidence/76-measured-bounds-keda-proof-client-docs/"),
    "M2": ("../../.planning/milestones/v1.16-phases/75-dxf-2d-pipeline/evidence/",
           "testdata/evidence/75-dxf-2d-pipeline/"),
    "M3": ("../../.planning/milestones/v1.16-phases/74-3d-render-measurements/evidence/",
           "testdata/evidence/74-3d-render-measurements/"),
    "M4": ("../../.planning/milestones/v1.16-phases/76-measured-bounds-keda-proof-client-docs/evidence/",
           "../convert/testdata/evidence/76-measured-bounds-keda-proof-client-docs/"),
    "M5": (".planning/milestones/v1.16-phases/76-measured-bounds-keda-proof-client-docs/evidence/",
           "internal/convert/testdata/evidence/76-measured-bounds-keda-proof-client-docs/"),
    "M6": (".planning/research/SANDBOX-PATHS.md", "docs/SANDBOX-PATHS.md"),
}

S1 = [
    ".env.example",
    "cmd/audio-worker/main.go",
    "cmd/av-worker/main.go",
    "cmd/chromium-worker/main.go",
    "cmd/document-worker/main.go",
    "cmd/ocr-worker/main.go",
    "cmd/pandoc-worker/main.go",
    "cmd/probe-worker/main.go",
    "cmd/worker/main.go",
    "deploy/chart/octoconv/templates/deployment-chromium-worker.yaml",
    "docker-compose.yml",
    "internal/convert/audiosandbox_test.go",
    "internal/convert/avsandbox_test.go",
    "internal/convert/htmlsandbox_test.go",
    "internal/convert/libvips.go",
    "internal/convert/libvipssandbox_test.go",
    "internal/convert/ocrsandbox_test.go",
    "internal/convert/pandocsandbox.go",
    "internal/convert/pandocsandbox_test.go",
    "internal/convert/probesandbox_test.go",
    "scripts/conf05-crosscheck.sh",
    "scripts/sandbox-liveproof.sh",
]

FILE_MAPS = {
    "internal/convert/cad_test.go": ["M1", "M2"],
    "internal/convert/cad_doc_test.go": ["M1"],
    "internal/convert/caddxfpdffloor_test.go": ["M2"],
    "internal/convert/cadrender_test.go": ["M3"],
    "internal/api/cad_ceiling_test.go": ["M4"],
    "docs/CAD.md": ["M5"],
}
for f in S1:
    FILE_MAPS.setdefault(f, [])
    FILE_MAPS[f].append("M6")
FILE_MAPS[".env.example"] = ["M5", "M6"]


def apply(line, maps):
    for m in maps:
        old, new = M[m]
        line = line.replace(old, new)
    return line


def main():
    out = subprocess.run(
        ["git", "diff", "-U0", "--no-color", BASE, "--", ".", ":!.planning"],
        check=True, capture_output=True, text=True,
    ).stdout
    files = {}
    order = []
    cur = None
    added = False
    hunk = None
    for raw in out.split("\n"):
        if raw.startswith("diff --git "):
            cur = None
            added = False
            hunk = None
            continue
        if raw.startswith("new file mode") or raw.startswith("deleted file mode") or raw == "--- /dev/null":
            added = True
            continue
        if raw.startswith("+++ "):
            if added:
                cur = None
                continue
            cur = raw[len("+++ b/"):]
            files[cur] = []
            order.append(cur)
            continue
        if raw.startswith("--- "):
            continue
        if cur is None:
            continue
        if raw.startswith("@@"):
            hunk = {"minus": [], "plus": []}
            files[cur].append(hunk)
            continue
        if raw.startswith("-") and hunk is not None:
            hunk["minus"].append(raw[1:])
        elif raw.startswith("+") and hunk is not None:
            hunk["plus"].append(raw[1:])
    bad = 0
    total = 0
    for f in order:
        maps = FILE_MAPS.get(f)
        reason = ""
        n = 0
        if maps is None:
            reason = "file not in mapping table"
        else:
            for h in files[f]:
                if len(h["minus"]) != len(h["plus"]):
                    reason = "unequal -/+ counts in a hunk"
                    break
                for a, b in zip(h["minus"], h["plus"]):
                    mapped = apply(a, maps)
                    if mapped == a:
                        reason = "a - line does not change under " + "+".join(maps)
                        break
                    if mapped != b:
                        reason = "a + line is not the mapped - line"
                        break
                    n += 1
                if reason:
                    break
        if reason:
            bad += 1
            print("FAIL %s: %s" % (f, reason))
        else:
            print("OK %s %s lines=%d" % (f, "+".join(maps), n))
        total += n
    print("TOTAL modified_files=%d changed_lines=%d expected_files=%d expected_lines=%d"
          % (len(order), total, EXPECTED_FILES, EXPECTED_LINES))
    if len(order) != EXPECTED_FILES or total != EXPECTED_LINES:
        print("FAIL totals differ from the expected totals")
        bad += 1
    print("RESULT %s" % ("PASS" if bad == 0 else "FAIL"))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
