import hashlib
import os
import subprocess
import sys

ROOT = "internal/convert/testdata/cad"
OUT = ".planning/quick/261018-44q-phase-71-provenance-per-file-contract-sc/evidence/provenance-map.tsv"
QUICK = [
    ("o2d", ".planning/quick/261012-o2d-codex-pr36-p2-preserve-dxf-text-opacity-/evidence"),
    ("qds", ".planning/quick/261012-qds-codex-pr36-round2-oblique-font-style-dxf/evidence"),
    ("u2s", ".planning/quick/261012-u2s-codex-pr36-round3-fully-transparent-dxf/evidence"),
    ("0og", ".planning/quick/261013-0og-codex-pr36-round4-dxf-emptiness-judged-b/evidence"),
]
EVIDENCE = [
    ("75-01", ".planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt"),
    ("75-02", ".planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt"),
    ("75-07", ".planning/phases/75-dxf-2d-pipeline/evidence/75-07-cr.txt"),
]
for tag, d in QUICK:
    EVIDENCE.append((tag, d + "/probe-fixtures.txt"))
    EVIDENCE.append((tag, d + "/probe-helper.txt"))

GROUPS = {
    "dxf": ("FIXTURE", "name", lambda base: base[:-4] if base.endswith(".dxf") else None),
    "dxf-out": ("OUT", "file", lambda base: base),
    "dxf-cr": ("FILE", "name", lambda base: base),
}

EXPECTED = {
    "dxf": {"75-01": 41, "o2d": 2, "qds": 1, "u2s": 4, "0og": 6},
    "dxf-out": {"75-02": 30, "o2d": 11, "qds": 4, "u2s": 4, "0og": 4},
    "dxf-cr": {"75-07": 9},
}

RENDER_COMMIT = "056a8d2"


def fail(msg):
    print("MAP-STOP: " + msg)
    sys.exit(1)


def sha(path):
    with open(path, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()


def fields(line):
    out = {}
    for part in line.split(",")[1:]:
        k, sep, v = part.partition("=")
        if sep:
            out.setdefault(k, v)
    return out


def git(*args):
    return subprocess.run(["git", *args], check=True, capture_output=True, text=True).stdout.strip()


records = []
for tag, path in EVIDENCE:
    if not os.path.isfile(path):
        fail("missing evidence file " + path)
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.rstrip("\n")
            kind = line.split(",", 1)[0]
            if kind in ("FIXTURE", "OUT", "FILE"):
                records.append((tag, path, kind, fields(line)))

rows = []
dist = {}
multi = []
for group in sorted(GROUPS):
    kind, key, name_of = GROUPS[group]
    gdir = os.path.join(ROOT, group)
    for base in sorted(os.listdir(gdir)):
        p = os.path.join(gdir, base)
        if not os.path.isfile(p):
            continue
        rel = group + "/" + base
        name = name_of(base)
        if name is None:
            fail(rel + " has no evidence name")
        size = os.path.getsize(p)
        digest = sha(p)
        same = [r for r in records if r[2] == kind and r[3].get(key) == name]
        distinct = sorted({r[3].get("sha256") for r in same})
        hit = next((r for r in same if r[3].get("sha256") == digest and r[3].get("bytes") == str(size)), None)
        if hit is None:
            fail(rel + " has no evidence line with its sha256 and bytes")
        rows.append((rel, str(size), digest, hit[1], kind, str(len(distinct))))
        dist.setdefault(group, {}).setdefault(hit[0], 0)
        dist[group][hit[0]] += 1
        if len(distinct) > 1:
            multi.append((rel, hit[0], len(distinct)))

for group, want in EXPECTED.items():
    if dist.get(group) != want:
        fail("distribution of " + group + " is " + repr(dist.get(group)) + ", expected " + repr(want))

rdir = os.path.join(ROOT, "render-bg")
for base in sorted(os.listdir(rdir)):
    rel = "render-bg/" + base
    p = os.path.join(rdir, base)
    log = git("log", "--format=%h", "--", p).split()
    if log != [RENDER_COMMIT]:
        fail(rel + " git log is " + repr(log))
    if git("rev-parse", RENDER_COMMIT + ":" + p) != git("rev-parse", "HEAD:" + p):
        fail(rel + " blob differs between " + RENDER_COMMIT + " and HEAD")
    rows.append((rel, str(os.path.getsize(p)), sha(p), "git:" + RENDER_COMMIT, "BLOB", "0"))

if len(rows) != 126:
    fail("expected 126 rows, got " + str(len(rows)))

with open(OUT, "w", encoding="utf-8") as f:
    for r in rows:
        f.write("\t".join(r) + "\n")

for group in sorted(dist):
    print("DIST,group=" + group + "," + ",".join(k + "=" + str(v) for k, v in dist[group].items()))
print("DIST,group=render-bg,git_" + RENDER_COMMIT + "=10,sha256_recorded_before=0")
for rel, tag, n in multi:
    print("MULTI,file=" + rel + ",matched_in=" + tag + ",distinct_sha256=" + str(n))
print("MAP-OK,rows=" + str(len(rows)) + ",multi=" + str(len(multi)))
