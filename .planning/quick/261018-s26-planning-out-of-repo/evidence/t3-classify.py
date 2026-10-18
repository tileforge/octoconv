import collections
import re
import sys

E = sys.argv[1]

A_PATTERNS = [
    '"../../.planning/milestones/v1.16-phases/',
    'DOC_FILE=".planning/research/SANDBOX-PATHS.md"',
]
COPY_PATTERNS = [
    ".planning/research/SANDBOX-PATHS.md",
    ".planning/milestones/v1.16-phases/76-measured-bounds-keda-proof-client-docs/evidence/",
]
SCRIPT_COMMENT_AS_B = {("scripts/keda-load-proof.sh", 28)}
SCRIPT_C = {("scripts/ci-helm-checks.sh", 10)}


def parse(path):
    rows = []
    with open(path, encoding="utf-8", errors="replace") as fh:
        for raw in fh:
            raw = raw.rstrip("\n")
            m = re.match(r"^([^:]+):(\d+):(.*)$", raw)
            if m:
                rows.append((m.group(1), int(m.group(2)), m.group(3)))
    return rows


def is_script(f):
    return (f.startswith("scripts/") and f.endswith(".sh")) or (f.startswith("scripts/fixtures/") and f.endswith(".py"))


def classify_pre_removed(f, text):
    if any(p in text for p in A_PATTERNS):
        return "a"
    if any(p in text for p in COPY_PATTERNS):
        return "c->copy"
    return "?"


def classify_post(f, n, text):
    if f == ".gitignore":
        return "config"
    if (f, n) in SCRIPT_C:
        return "c"
    if f == "scripts/ci-local.sh":
        return "b"
    if is_script(f):
        if text.lstrip().startswith("#") and (f, n) not in SCRIPT_COMMENT_AS_B:
            return "c"
        return "b"
    return "c"


ACTION = {
    "a": "fixed: reader repointed to the tracked copy",
    "c->copy": "repointed to the tracked copy (M5/M6)",
    "b": "kept: operator-local measurement/transcript path",
    "c": "kept: prose/comment/message citation",
    "config": "kept: ignores /.planning/",
}


def main():
    pre = parse(E + "/t3-refs-pre-lines.txt")
    post = parse(E + "/t3-refs-post-lines.txt")
    pre_by = collections.defaultdict(list)
    post_by = collections.defaultdict(list)
    for f, n, t in pre:
        pre_by[f].append((n, t))
    for f, n, t in post:
        post_by[f].append((n, t))
    files = sorted(set(pre_by) | set(post_by))
    totals = collections.Counter()
    unknown = 0
    print("| File | Pre | Post | Class | Action |")
    print("|------|-----|------|-------|--------|")
    for f in files:
        post_texts = collections.Counter(t for _, t in post_by[f])
        classes = collections.Counter()
        for n, t in post_by[f]:
            classes[classify_post(f, n, t)] += 1
        pre_texts = collections.Counter(t for _, t in pre_by[f])
        removed = pre_texts - post_texts
        for t, k in removed.items():
            c = classify_pre_removed(f, t)
            if c == "?":
                unknown += k
            classes[c] += k
        npre = len(pre_by[f])
        npost = len(post_by[f])
        if f == "docs/SANDBOX-PATHS.md":
            note = "; new file, byte copy of .planning/research/SANDBOX-PATHS.md, its internal citations"
        elif f == "internal/convert/cad_test.go":
            note = "; the c line is the D-06 71-DECISION.md message constant"
        else:
            note = ""
        cls = ", ".join("%s:%d" % (c, classes[c]) for c in ["a", "c->copy", "b", "c", "config", "?"] if classes[c])
        act = "; ".join(ACTION.get(c, "UNCLASSIFIED") for c in ["a", "c->copy", "b", "c", "config", "?"] if classes[c])
        print("| %s | %d | %d | %s | %s%s |" % (f, npre, npost, cls, act, note))
        totals.update(classes)
    print()
    print("TOTAL files=%d pre_refs=%d post_refs=%d classes=%s unclassified=%d"
          % (len(files), len(pre), len(post), dict(totals), unknown))
    return 1 if unknown else 0


if __name__ == "__main__":
    sys.exit(main())
