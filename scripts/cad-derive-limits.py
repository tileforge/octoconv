#!/usr/bin/env python3
"""cad-derive-limits.py -- Phase 71 (EVAL-03, D-05..D-10): derive the pod memory limit,
per-format input ceilings, cap candidates and the OCP pin from the measurement evidence.

Usage:
    python3 scripts/cad-derive-limits.py EVIDENCE_DIR [--pin 7.9.3.1.1|8.0.1.0.0]
    python3 scripts/cad-derive-limits.py --self-check

--pin records a decision taken outside the formula: PIN_DECISION carries it and PIN_FORMULA keeps
the formula result.

Reads (EVIDENCE_DIR): 71-06-ladder-4g-ocp*.txt, 71-06-ladder-2g.txt, 71-06-ladder-1g.txt,
71-06-go-worker-rss.txt, 71-06-threads.txt, 71-06-quality.txt, 71-03-corpus-inventory.txt.
Prints LIMIT, FIT, CEILING, CAP, MEMORY_LIMIT_DECISION, PIN_DECISION, THREADS_DECISION and
QUALITY lines, each with its formula and the numbers substituted into it. Nothing is taken
from a number outside the evidence; the 40 MiB comparison point is printed as a yardstick.

Stdlib only; runs on the host.
"""

import glob
import os
import statistics
import sys

MIB = 1048576
BUDGET = 0.70
LIMITS = (1024, 2048, 4096)
MEM_NAME = {1024: "1g", 2048: "2g", 4096: "4g"}
YARDSTICK_STEP_MIB = 40
MARGIN = 0.80
KDIVERGE = 0.25
PIN_A = "7.9.3.1.1"
PIN_B = "8.0.1.0.0"
PIN_K_DROP = 0.15
PIN_WALL_DROP = 0.20
LOW_MIB = 1.0
NOISE_FRAC = 0.20
D05_TOP_MIB = 100
COUNTER_SERIES = ("step-nauo", "dxf-minsert")

PIPELINE = {
    "step": ("mesh", "render"),
    "iges": ("mesh", "render"),
    "stl_binary": ("stlstats", "render"),
    "stl_ascii": ("stlstats", "render"),
    "dxf": ("dxf", "rsvg-png", "rsvg-pdf"),
}
CLASSES = ("step", "iges", "stl_binary", "stl_ascii", "dxf")


def counter_series(series):
    return any(series.startswith(c) for c in COUNTER_SERIES)


def name_count(name):
    tail = name.rsplit("-n", 1)[-1].split(".")[0]
    return float(tail) if tail.isdigit() else None


def split_fields(text):
    fields = {}
    for part in text.split(","):
        k, sep, v = part.partition("=")
        if sep:
            fields[k] = v
    return fields


def num(value):
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def parse_out(out):
    result = {}
    for part in out.split(";"):
        k, sep, v = part.partition(":")
        if sep:
            result[k] = v
    return result


def read_lines(path):
    with open(path, encoding="utf-8") as fh:
        return [ln.rstrip("\n") for ln in fh]


def parse_row(line):
    body = line[len("ROW,"):]
    head, sep, out = body.partition(",out=")
    f = split_fields(head)
    f["_out"] = parse_out(out) if sep else {}
    f["bytes"] = int(f["bytes"])
    f["rc"] = int(f["rc"])
    for key in ("wall_s", "utime_s", "stime_s", "child_rss_mib", "cg_peak_mib"):
        f[key] = num(f.get(key))
    vals = [v for v in (f["child_rss_mib"], f["cg_peak_mib"]) if v is not None]
    f["rss"] = max(vals) if vals else None
    f["oom"] = f.get("oom_kill", "0") not in ("0", "na", "")
    f["timeout"] = f.get("timeout") == "1"
    return f


class Cell:
    def __init__(self):
        self.rows = []
        self.skips = []

    def status(self):
        if any(r["oom"] for r in self.rows):
            return "oom"
        if any(r["timeout"] for r in self.rows):
            return "timeout"
        if any(r["rc"] == 137 for r in self.rows):
            return "oom"
        for reason in self.skips:
            if reason.endswith("-oom"):
                return "oom"
            if reason.endswith("-timeout"):
                return "timeout"
        if self.rows and all(r["rc"] == 0 for r in self.rows):
            return "ok"
        if self.rows:
            return "fail"
        return None

    def rss(self):
        vals = [r["rss"] for r in self.rows if r["rc"] == 0 and r["rss"] is not None]
        return max(vals) if vals else None

    def wall(self):
        vals = [r["wall_s"] for r in self.rows if r["rc"] == 0 and r["wall_s"] is not None]
        return statistics.median_low(vals) if vals else None


class Evidence:
    def __init__(self, directory, config=None):
        self.dir = directory
        self.config = config
        self.cnt = {}
        self.private = []
        self.go = None
        self.threads = []
        self.quality = []
        self.sources = []
        self.meta = {}
        self.cells = {}
        self.skip_lines = []
        self.stops = []
        self.row_configs = {}
        self.cell_configs = {}
        for path in sorted(glob.glob(os.path.join(directory, "71-06-ladder-*.txt"))):
            base = os.path.basename(path)
            file_ocp = PIN_A
            file_cfg = ("on", "default")
            used_here = False
            for line in read_lines(path):
                if line.startswith("HDR,ocp="):
                    file_ocp = line[len("HDR,ocp="):].strip()
                elif line.startswith("HDR,ladder="):
                    h = split_fields(line[len("HDR,"):])
                    file_cfg = (h.get("parallel", "on"), h.get("lp_threads", "default"))
                elif line.startswith("ROW,"):
                    r = parse_row(line)
                    cfg = (r.get("par", "on"), r.get("lp", "default"))
                    key = (r["ocp"], r["mem"], r["sha"], r["op"])
                    self.cell_configs.setdefault(key, set()).add(cfg)
                    ck = (base, r["ocp"], r["mem"], cfg)
                    self.row_configs[ck] = self.row_configs.get(ck, 0) + 1
                    if config is not None and cfg != config:
                        continue
                    used_here = True
                    sha = r["sha"]
                    self.meta.setdefault(sha, {
                        "name": r["name"] if r.get("src") == "public" else "-",
                        "series": r.get("series", "real"), "bytes": r["bytes"], "fmt": r["fmt"],
                        "src": r.get("src"),
                    })
                    self.cells.setdefault(key, Cell()).rows.append(r)
                elif line.startswith("SKIP,") and "reason=prestep-failed-" in line:
                    if config is not None and file_cfg != config:
                        continue
                    f = split_fields(line[len("SKIP,"):])
                    f["_ocp"] = file_ocp
                    self.skip_lines.append(f)
                elif line.startswith("LADDER_STOP,"):
                    if config is not None and file_cfg != config:
                        continue
                    self.stops.append(split_fields(line[len("LADDER_STOP,"):]))
                elif line.startswith("PRIVATE_CORPUS,"):
                    self.private.append(line)
            if used_here:
                self.sources.append(base)
        by_name = {}
        for sha, m in self.meta.items():
            if m["name"] != "-":
                by_name[m["name"]] = sha
        self.unmapped_skips = 0
        for f in self.skip_lines:
            sha = by_name.get(f.get("name"))
            if sha is None:
                self.unmapped_skips += 1
                continue
            self.cells.setdefault((f["_ocp"], f["mem"], sha, f["op"]), Cell()).skips.append(f["reason"])
        inv = os.path.join(directory, "71-03-corpus-inventory.txt")
        if os.path.exists(inv):
            for line in read_lines(inv):
                if line.startswith("CNT,"):
                    f = split_fields(line[len("CNT,"):])
                    self.cnt[f["sha"]] = f
                elif line.startswith("PRIVATE_CORPUS,"):
                    self.private.append(line)
        go_path = os.path.join(directory, "71-06-go-worker-rss.txt")
        if os.path.exists(go_path):
            for line in read_lines(go_path):
                if line.startswith("GO_WORKER,"):
                    self.go = split_fields(line[len("GO_WORKER,"):])
        for name, store, prefix in (("71-06-threads.txt", self.threads, "THREADS_DECISION,"),
                                    ("71-06-quality.txt", self.quality, "QUALITY,")):
            path = os.path.join(directory, name)
            if os.path.exists(path):
                store.extend(ln for ln in read_lines(path) if ln.startswith(prefix))

    def klass(self, sha):
        m = self.meta[sha]
        if m["fmt"] != "stl":
            return m["fmt"]
        kind = self.cnt.get(sha, {}).get("kind")
        if kind is None:
            kind = "ascii" if m["series"].startswith("stl-ascii") else "binary"
        return "stl_ascii" if kind == "ascii" else "stl_binary"

    def private_files(self):
        for line in self.private:
            f = split_fields(line[len("PRIVATE_CORPUS,"):])
            if f.get("files", "0") != "0":
                return int(f["files"])
        return 0

    def point(self, ocp, mem, sha):
        klass = self.klass(sha)
        ops = {}
        for op in PIPELINE[klass]:
            cell = self.cells.get((ocp, mem, sha, op))
            if cell is not None:
                ops[op] = cell
        if not ops:
            return None
        statuses = [c.status() for c in ops.values()]
        complete = len(ops) == len(PIPELINE[klass])
        if "oom" in statuses:
            status = "oom"
        elif "timeout" in statuses:
            status = "timeout"
        elif "fail" in statuses:
            status = "fail"
        elif complete:
            status = "ok"
        else:
            status = "partial"
        rss_vals = [c.rss() for c in ops.values() if c.rss() is not None]
        m = self.meta[sha]
        return {
            "sha": sha, "name": m["name"], "series": m["series"], "bytes": m["bytes"],
            "mib": m["bytes"] / MIB, "klass": klass, "ops": ops, "status": status,
            "complete": complete, "peak": max(rss_vals) if rss_vals else None, "src": m["src"],
        }

    def points(self, ocp, mem, klass):
        out = []
        for sha, m in self.meta.items():
            if self.klass(sha) != klass:
                continue
            p = self.point(ocp, mem, sha)
            if p is not None:
                out.append(p)
        out.sort(key=lambda p: (p["bytes"], p["name"]))
        return out


def fmt1(x):
    return "%.1f" % x


def usable(p):
    return p["status"] in ("ok", "timeout", "partial") and p["peak"] is not None


def derive_class(ev, ocp, klass, mem="4g"):
    pts = ev.points(ocp, mem, klass)
    base = [p for p in pts if not counter_series(p["series"]) and usable(p)]
    low = [p for p in base if p["mib"] < LOW_MIB]
    floor = max((p["peak"] for p in low), default=None)
    size_pts = [p for p in base if p["mib"] >= LOW_MIB]
    return {"klass": klass, "pts": pts, "floor": floor, "low": low, "size_pts": size_pts}


def k_worst(d):
    if d["floor"] is None or not d["size_pts"]:
        return None
    for p in d["size_pts"]:
        p["k"] = (p["peak"] - d["floor"]) / p["mib"]
    best = max(d["size_pts"], key=lambda p: p["k"])
    ks = sorted(p["k"] for p in d["size_pts"])
    return {"k": best["k"], "pt": best, "k_median": statistics.median(ks), "k_min": ks[0], "n": len(ks)}


def fit_rung(ev, klass, limit, go, ocp=PIN_A):
    fit, fit_pt = 0.0, None
    for p in ev.points(ocp, "4g", klass):
        if counter_series(p["series"]) or p["status"] != "ok" or p["peak"] is None:
            continue
        if p["peak"] + go <= BUDGET * limit and p["mib"] > fit:
            fit, fit_pt = p["mib"], p
    return fit, fit_pt


def size_max(limit, floor, go, k):
    return (BUDGET * limit - floor - go) / k


def cliffs(ev, klass, limit_mib, ocp=PIN_A):
    mem = MEM_NAME[limit_mib]
    out = {"oom": None, "timeout": None}
    top = 0
    for p in ev.points(ocp, mem, klass):
        if counter_series(p["series"]):
            continue
        if p["status"] in ("oom", "timeout"):
            cur = out[p["status"]]
            if cur is None or p["bytes"] < cur["bytes"]:
                out[p["status"]] = p
        elif p["status"] == "ok":
            top = max(top, p["bytes"])
    return out, top


def top_rung_from_stops(ev, klass, limit_mib, ocp=PIN_A):
    mem = MEM_NAME[limit_mib]
    reached = {}
    for s in ev.stops:
        if s.get("ocp") != ocp or s.get("mem") != mem or s.get("reason") != "top":
            continue
        series = s.get("series", "")
        if counter_series(series):
            continue
        fmt = s["fmt"]
        c = ("stl_ascii" if series.startswith("stl-ascii") else "stl_binary") if fmt == "stl" else fmt
        if c == klass:
            ops = reached.setdefault(series, {})
            ops[s["op"]] = max(ops.get(s["op"], 0), int(s["at_bytes"]))
    top = 0
    for ops in reached.values():
        if all(op in ops for op in PIPELINE[klass]):
            top = max(top, min(ops[op] for op in PIPELINE[klass]))
    return top


def biggest_real(ev, klass, ocp=PIN_A):
    best = 0
    for sha, m in ev.meta.items():
        if ev.klass(sha) != klass or m["series"] not in ("real", "private"):
            continue
        p = ev.point(ocp, "4g", sha)
        if p is not None and p["status"] == "ok":
            best = max(best, m["bytes"])
    return best


def wall_at(d, ceiling_mib):
    best = {}
    for p in d["pts"]:
        if p["mib"] > ceiling_mib or counter_series(p["series"]) or p["status"] != "ok":
            continue
        for op, cell in p["ops"].items():
            w = cell.wall()
            if w is not None and (op not in best or w > best[op][0]):
                best[op] = (w, p)
    return best


def cap_line(label, pts, floor, go, limit, unit, scale, extra=""):
    """pts: dicts with n (counter value), status, peak. Memory-derived; timeouts are listed, not used."""
    if floor is None:
        return "CAP,counter=%s,status=not-derivable,reason=no floor" % label
    fit = [p for p in pts if p["status"] == "ok" and p["peak"] is not None
           and p["peak"] - floor >= NOISE_FRAC * floor and p["n"] > 0]
    if not fit:
        return "CAP,counter=%s,status=not-derivable,reason=no measured point above the floor noise (%d points)" % (label, len(pts))
    kp = max(fit, key=lambda p: (p["peak"] - floor) / (p["n"] / scale))
    k = (kp["peak"] - floor) / (kp["n"] / scale)
    linear = (BUDGET * limit - floor - go) / k * scale
    breaches = [p for p in pts if p["status"] == "oom"]
    timeouts = [p for p in pts if p["status"] == "timeout"]
    top = max((p["n"] for p in pts if p["status"] == "ok"), default=0)
    bounds = [("linear", linear)]
    breach = min(breaches, key=lambda p: p["n"]) if breaches else None
    if breach is not None:
        bounds.append(("cliff%d" % (MARGIN * 100), MARGIN * breach["n"]))
        last_fit = max((p["n"] for p in pts if p["status"] == "ok" and p["n"] < breach["n"]), default=0)
    else:
        bounds.append(("top-measured", top))
        last_fit = top
    which, value = min(bounds, key=lambda b: b[1])
    tmo = ""
    if timeouts:
        first = min(timeouts, key=lambda p: p["n"])
        tmo = ",time_break_first_n=%d,time_break_note=timeouts at the cell budget are reported, not used in the value" % int(first["n"])
    spread = ";".join("%d:%s:%s" % (int(p["n"]), p["status"], fmt1(p["peak"]) if p["peak"] is not None else "-")
                      for p in sorted(pts, key=lambda p: p["n"]))
    return ("CAP,counter=%s,value=%d,limit_mib=%d,bound=%s,derivation=k_worst=(peak-floor)/%s=(%s-%s)/%.4f=%.2f MiB per %s at sha %s;"
            "linear=(%.2f*%d-%s-%s)/%.2f=%.0f;last_fit_n=%d;first_oom_n=%s;cliff%d=%s;top_measured_n=%d%s%s,points_n:status:peak_mib=%s" % (
                label, int(value), limit, which, unit, fmt1(kp["peak"]), fmt1(floor), kp["n"] / scale, k, unit, kp["sha"],
                BUDGET, limit, fmt1(floor), fmt1(go), k, linear, int(last_fit),
                "%d" % breach["n"] if breach else "none", MARGIN * 100,
                "%d" % (MARGIN * breach["n"]) if breach else "-", int(top), tmo, extra, spread))


def selected_config(directory):
    path = os.path.join(directory, "71-06-threads.txt")
    if not os.path.exists(path):
        return None, "no 71-06-threads.txt; the selected thread settings are not known"
    chosen = None
    for line in read_lines(path):
        if line.startswith("THREADS_DECISION,"):
            chosen = split_fields(line[len("THREADS_DECISION,"):])
    if chosen is None or "lp_num_threads" not in chosen or "occt_parallel" not in chosen:
        return None, "no THREADS_DECISION line with lp_num_threads and occt_parallel in 71-06-threads.txt"
    return (chosen["occt_parallel"], chosen["lp_num_threads"]), None


def self_check():
    import tempfile

    def row(sha, name, mib, op, rc=0, rss=100.0, timeout=0, series="step-copies-x", par="on", lp="default"):
        return ("ROW,src=public,name=%s,sha=%s,fmt=step,bytes=%d,op=%s,ocp=%s,variant=full,mem=4g,cpus=2,confined=0,rep=1,"
                "rc=%d,wall_s=1.0,utime_s=1.0,stime_s=0.1,child_rss_mib=%.1f,cg_peak_mib=%.1f,oom_kill=0,timeout=%d,"
                "threads_max=2,pix=-,cstate=ok,par=%s,lp=%s,quality=standard,series=%s,out=-" % (
                    name, sha, int(mib * MIB), op, PIN_A, rc, rss, rss, timeout, par, lp, series))

    lines = []
    for sha, name, mib, ops in (
            ("a" * 64, "tiny", 0.4, (("mesh", 0, 100.0, 0), ("render", 0, 120.0, 0))),
            ("b" * 64, "mid", 40, (("mesh", 0, 800.0, 0), ("render", 0, 600.0, 0))),
            ("c" * 64, "slow", 80, (("mesh", 0, 1500.0, 0), ("render", 137, 900.0, 1))),
            ("d" * 64, "mesh-only", 150, (("mesh", 0, 2000.0, 0),))):
        for op, rc, rss, to in ops:
            lines.append(row(sha, name, mib, op, rc, rss, to, "real" if mib < 1 else "step-copies-x"))
    failures = []
    with tempfile.TemporaryDirectory() as tmp:
        with open(os.path.join(tmp, "71-06-ladder-4g-selfcheck.txt"), "w", encoding="utf-8") as fh:
            fh.write("\n".join(["HDR,ocp=%s" % PIN_A] + lines) + "\n")
        ev = Evidence(tmp)
        by = {p["name"]: p for p in ev.points(PIN_A, "4g", "step")}
        if by["mid"]["status"] != "ok" or not by["mid"]["complete"]:
            failures.append("a point with mesh and render must be ok")
        if by["mesh-only"]["status"] != "partial" or by["mesh-only"]["complete"]:
            failures.append("a point with mesh but no render must be partial, got %s" % by["mesh-only"]["status"])
        if by["slow"]["status"] != "timeout":
            failures.append("a point whose render timed out must be timeout, got %s" % by["slow"]["status"])
        fit, fit_pt = fit_rung(ev, "step", 4096, 0.0)
        if fit_pt is None or fit_pt["name"] != "mid":
            failures.append("FIT must stop at the last complete pipeline (mid), got %s" % (fit_pt["name"] if fit_pt else None))
        _cl, top = cliffs(ev, "step", 4096)
        if top != int(40 * MIB):
            failures.append("cliff top rung must be the last complete pipeline, got %d" % top)
        with open(os.path.join(tmp, "71-06-ladder-4g-selfcheck.txt"), "a", encoding="utf-8") as fh:
            fh.write("LADDER_STOP,fmt=step,op=mesh,mem=4g,ocp=%s,series=step-copies-x,reason=top,at_bytes=%d\n" % (PIN_A, 150 * MIB))
            fh.write("LADDER_STOP,fmt=step,op=render,mem=4g,ocp=%s,series=step-copies-x,reason=timeout,at_bytes=%d\n" % (PIN_A, 80 * MIB))
        if top_rung_from_stops(Evidence(tmp), "step", 4096) != 0:
            failures.append("a top stop of one operation must not make the whole pipeline reach the top rung")
        with open(os.path.join(tmp, "71-06-ladder-4g-selfcheck.txt"), "a", encoding="utf-8") as fh:
            fh.write(row("d" * 64, "mesh-only", 150, "render", 0, 700.0) + "\n")
        done = {p["name"]: p for p in Evidence(tmp).points(PIN_A, "4g", "step")}
        if done["mesh-only"]["status"] != "ok":
            failures.append("the same point with its render measured must become ok")
    with tempfile.TemporaryDirectory() as tmp:
        for fname, par, lp, rss in (("71-06-ladder-4g-old.txt", "on", "default", 800.0),
                                    ("71-06-ladder-4g-new.txt", "off", "2", 1000.0)):
            with open(os.path.join(tmp, fname), "w", encoding="utf-8") as fh:
                fh.write("HDR,ocp=%s\nHDR,ladder=1,repeats=1,budget_s=900,quality=standard,parallel=%s,lp_threads=%s,resume=0\n" % (
                    PIN_A, par, lp))
                for op in ("mesh", "render"):
                    fh.write(row("b" * 64, "mid", 40, op, 0, rss, 0, "step-copies-x", par, lp) + "\n")
                fh.write("LADDER_STOP,fmt=step,op=mesh,mem=4g,ocp=%s,series=step-copies-x,reason=top,at_bytes=%d\n" % (PIN_A, 40 * MIB))
                fh.write("LADDER_STOP,fmt=step,op=render,mem=4g,ocp=%s,series=step-copies-x,reason=top,at_bytes=%d\n" % (
                    PIN_A, (40 if par == "on" else 30) * MIB))
        sel = Evidence(tmp, ("off", "2"))
        pt = sel.points(PIN_A, "4g", "step")
        if len(pt) != 1 or pt[0]["peak"] != 1000.0:
            failures.append("rows of another configuration must not enter the selected view")
        if top_rung_from_stops(sel, "step", 4096) != 30 * MIB:
            failures.append("stop lines of another configuration must not enter the selected view")
        if sel.sources != ["71-06-ladder-4g-new.txt"]:
            failures.append("a file with no row of the selected configuration must not be listed as a source")
        if not any(len(v) > 1 for v in sel.cell_configs.values()):
            failures.append("a cell measured at two configurations must be reported")
    for msg in failures:
        print("SELF-CHECK-FAIL,%s" % msg)
    if not failures:
        print("SELF-CHECK-OK,partial_pipeline_not_counted_as_ok=1,other_configuration_rows_excluded=1")
    return 1 if failures else 0


def main():
    args = sys.argv[1:]
    if args == ["--self-check"]:
        return self_check()
    override = None
    if len(args) == 3 and args[1] == "--pin" and args[2] in (PIN_A, PIN_B):
        override = args[2]
        args = args[:1]
    if len(args) != 1 or not os.path.isdir(args[0]):
        print(__doc__.strip().split("\n\n")[1], file=sys.stderr)
        return 64
    config, cfg_err = selected_config(args[0])
    if config is None:
        print("error: %s" % cfg_err, file=sys.stderr)
        return 1
    ev = Evidence(args[0], config)
    if not any(k[1] == "4g" and k[0] == PIN_A for k in ev.cells):
        print("error: no %s 4g ladder rows with par=%s lp=%s; the limits are not derived from rows of another "
              "configuration (run the 4g ladder with --parallel %s --lp-threads %s)" % (
                  PIN_A, config[0], config[1], config[0], config[1]), file=sys.stderr)
        return 1
    mixed = sorted(k for k, v in ev.cell_configs.items() if len(v) > 1 and config in v)
    if ev.go is None:
        print("error: no GO_WORKER line; the Go worker RSS is not measured", file=sys.stderr)
        return 1
    go = float(ev.go["vmhwm_after_job_mib"])
    private_n = ev.private_files()
    private_note = "реальный поток не замерен" if private_n == 0 else "private files=%d" % private_n

    print("HDR,tool=cad-derive-limits.py,budget=%.2f,limits_mib=%s,ocp_curve=%s,mem_curve=4g,rss_eff=max(child_rss_mib;cg_peak_mib),sources=%s" % (
        BUDGET, "/".join(map(str, LIMITS)), PIN_A, "+".join(ev.sources)))
    used_rows = sum(n for (_f, _o, _m, c), n in ev.row_configs.items() if c == config)
    other_rows = sum(n for (_f, _o, _m, c), n in ev.row_configs.items() if c != config)
    print("CONFIG,selected=par=%s;lp=%s,source=THREADS_DECISION in 71-06-threads.txt,rows_used=%d,rows_excluded_other_config=%d,"
          "cells_measured_at_two_configs_kept_apart=%d,rule=a number is derived only from rows tagged with the selected configuration" % (
              config[0], config[1], used_rows, other_rows, len(mixed)))
    for (fname, ocp, mem, cfg), n in sorted(ev.row_configs.items()):
        print("CONFIG_ROWS,file=%s,ocp=%s,mem=%s,par=%s,lp=%s,rows=%d,used=%s" % (
            fname, ocp, mem, cfg[0], cfg[1], n, "yes" if cfg == config else "no"))
    print("PRIVATE_CORPUS,files=%d,status=%s" % (private_n, "measured" if private_n else private_note))
    print("GO_WORKER,vmhwm_after_job_mib=%s,vmrss_idle_mib=%s,analogue=image-class Go worker not cad-worker" % (
        ev.go["vmhwm_after_job_mib"], ev.go["vmrss_idle_mib"]))
    print("YARDSTICK,step_mib=%d,source=PreviewGenerator,use=comparison point for D-06 only and not a ceiling source" % YARDSTICK_STEP_MIB)
    if ev.unmapped_skips:
        print("NOTE,unmapped_prestep_skips=%d,reason=private or unnamed file, not attributable to a size" % ev.unmapped_skips)

    stacking = ""
    st_path = os.path.join(ev.dir, "71-06-render-stacking.txt")
    if os.path.exists(st_path):
        rows = [split_fields(ln[len("STACKING,"):]) for ln in read_lines(st_path) if ln.startswith("STACKING,")]
        by = {r["variant"]: r for r in rows}
        if "stacked" in by and "unstacked" in by:
            stacking = "71-06-render-stacking.txt: the same %s triangles render in %ss stacked and %ss unstacked" % (
                by["stacked"]["triangles"], by["stacked"]["wall_s"], by["unstacked"]["wall_s"])
    derived = {}
    for klass in CLASSES:
        d = derive_class(ev, PIN_A, klass)
        kw = k_worst(d)
        derived[klass] = (d, kw)
        if kw is None:
            print("LIMIT,fmt=%s,status=not-derivable,reason=no floor (no file below 1 MiB) or no file of 1 MiB or more at 4g,points=%d" % (
                klass, len(d["pts"])))
            continue
        pt = kw["pt"]
        sizes = ",".join("size_max_%dm=%s" % (L, fmt1(size_max(L, d["floor"], go, kw["k"]))) for L in LIMITS)
        incomplete = sum(1 for p in d["size_pts"] if not p["complete"] or p["status"] != "ok")
        print("LIMIT,fmt=%s,floor_mib=%s,go_mib=%s,k_worst=%.3f,k_median=%.3f,k_min=%.3f,k_points=%d,"
              "worst_sha=%s,worst_name=%s,worst_bytes=%d,worst_peak_mib=%s,floor_points=%d,partial_pipelines=%d,"
              "formula=size_max=(%.2f*L-floor-go)/k_worst with floor=%s go=%s k_worst=%.3f,%s,"
              "worst_point_governs=%s" % (
                  klass, fmt1(d["floor"]), fmt1(go), kw["k"], kw["k_median"], kw["k_min"], kw["n"],
                  pt["sha"], pt["name"], pt["bytes"], fmt1(pt["peak"]), len(d["low"]), incomplete,
                  BUDGET, fmt1(d["floor"]), fmt1(go), kw["k"], sizes,
                  "yes" if kw["k"] > 1.5 * kw["k_median"] else "no"))

    for klass in CLASSES:
        d, kw = derived[klass]
        if kw is None:
            continue
        for L in LIMITS:
            fit, fit_pt = fit_rung(ev, klass, L, go)
            print("FIT,fmt=%s,limit_mib=%d,largest_fitting_rung_mib=%s,rung_peak_mib=%s,rule=peak+go<=%.2f*L at the 4g curve,measured_not_modelled=1" % (
                klass, L, fmt1(fit), fmt1(fit_pt["peak"]) if fit_pt else "-", BUDGET))

    k_stl_b = derived["stl_binary"][1]
    k_stl_a = derived["stl_ascii"][1]
    stl_shared = False
    if k_stl_b and k_stl_a:
        diverge = abs(k_stl_b["k"] - k_stl_a["k"]) / max(k_stl_b["k"], k_stl_a["k"])
        stl_shared = diverge <= KDIVERGE
        print("STL_K,k_binary=%.3f,k_ascii=%.3f,divergence=%.1f%%,threshold=%d%%,separate_ceilings=%s" % (
            k_stl_b["k"], k_stl_a["k"], diverge * 100, KDIVERGE * 100, "no" if stl_shared else "yes"))

    largest = {k: biggest_real(ev, k) for k in CLASSES}
    for klass in CLASSES:
        print("REAL_MAX,fmt=%s,bytes=%d,mib=%s,source=committed and fetched files (and private when present) read at rc=0 at 4g" % (
            klass, largest[klass], fmt1(largest[klass] / MIB)))

    chosen = None
    trace = []
    for L in LIMITS:
        ok = True
        why = []
        kw = derived["step"][1]
        if kw is None:
            ok = False
            why.append("step not derivable")
        else:
            s = size_max(L, derived["step"][0]["floor"], go, kw["k"])
            if s < YARDSTICK_STEP_MIB:
                ok = False
                why.append("step size_max %.1f < %d yardstick" % (s, YARDSTICK_STEP_MIB))
        for klass in CLASSES:
            d, kw = derived[klass]
            if kw is None or largest[klass] == 0:
                continue
            s = size_max(L, d["floor"], go, kw["k"])
            if s < largest[klass] / MIB:
                ok = False
                why.append("%s size_max %.1f < largest real %.1f" % (klass, s, largest[klass] / MIB))
        trace.append("L=%d:%s" % (L, "ok" if ok else "no(" + "; ".join(why) + ")"))
        if ok and chosen is None:
            chosen = L
    if chosen is None:
        chosen = 4096
        reason = "no L of 1/2/4 GiB satisfies D-06; L=4096 and the measured ceilings are accepted (D-09); " + " | ".join(trace)
    else:
        reason = ("smallest L satisfying D-06 (step size_max >= %d MiB yardstick and every format >= its largest real file); %s" % (
            YARDSTICK_STEP_MIB, " | ".join(trace)))
    reason += "; fleet default is 1Gi (2Gi for one service) so this value is a recorded deviation from it (D-06)"
    print("MEMORY_LIMIT_DECISION,limit_mib=%d,reason=%s" % (chosen, reason))
    L = chosen
    kw_step = derived["step"][1]
    if kw_step is not None:
        for smaller in LIMITS:
            if smaller >= chosen:
                continue
            need = (BUDGET * smaller - derived["step"][0]["floor"] - go) / YARDSTICK_STEP_MIB
            print("L_SENSITIVITY,limit_mib=%d,step_size_max=%.1f,yardstick_mib=%d,step_k_worst_must_be_at_most=%.3f,"
                  "measured_k_worst=%.3f,measured_over_needed=%.2f,floor_mib=%s,basis=4g rows at the selected configuration; "
                  "the 1g and 2g ladders were measured at par=on lp=default and take no part in this number" % (
                      smaller, size_max(smaller, derived["step"][0]["floor"], go, kw_step["k"]), YARDSTICK_STEP_MIB, need,
                      kw_step["k"], kw_step["k"] / need, fmt1(derived["step"][0]["floor"])))

    for klass in CLASSES:
        d, kw = derived[klass]
        if kw is None:
            continue
        sm = size_max(L, d["floor"], go, kw["k"])
        ceiling = max(0, int(sm))
        cl, top_measured = cliffs(ev, klass, L)
        top_stop = top_rung_from_stops(ev, klass, L)
        cliff_cfg = ""
        if not any(k[0] == PIN_A and k[1] == MEM_NAME[L] for k in ev.cells):
            cliff_cfg = ",cliff_evidence=none-at-the-selected-configuration-at-%s" % MEM_NAME[L]
        top = max(top_measured, top_stop)
        parts = []
        if cliff_cfg:
            parts.append(cliff_cfg.lstrip(","))
        co, ct = cl["oom"], cl["timeout"]
        if co is not None:
            ratio = ceiling * MIB / co["bytes"]
            parts.append("oom_cliff_mib=%s,oom_cliff_sha=%s,ceiling_over_oom_cliff=%.2f,oom_margin_%d=%s" % (
                fmt1(co["bytes"] / MIB), co["sha"], ratio, MARGIN * 100, "ok" if ratio <= MARGIN else "FLAG"))
            if ratio > MARGIN:
                parts.append("recommended_ceiling_mib=%d" % int(MARGIN * co["bytes"] / MIB))
        else:
            parts.append("oom_cliff=none-up-to-top-rung,top_rung_mib=%s" % fmt1(top / MIB))
            if top and ceiling * MIB > top:
                parts.append("beyond_top_rung=1,measured_ceiling_mib=%d" % int(top / MIB))
            if top / MIB < D05_TOP_MIB:
                parts.append("d05=top rung below %d MiB and no cliff" % D05_TOP_MIB)
        if ct is not None:
            ratio = ceiling * MIB / ct["bytes"]
            parts.append("timeout_cliff_mib=%s,timeout_cliff_series=%s,timeout_cliff_sha=%s,ceiling_over_timeout_cliff=%.2f,timeout_margin_%d=%s" % (
                fmt1(ct["bytes"] / MIB), ct["series"], ct["sha"], ratio, MARGIN * 100, "ok" if ratio <= MARGIN else "FLAG"))
        w = wall_at(d, ceiling)
        wall_txt = "wall_at_ceiling=-"
        if w:
            wall_txt = "wall_sum_of_per_op_max_s=%.2f,wall_per_op_max=%s,cpus=2" % (
                sum(v[0] for v in w.values()),
                "/".join("%s:%.2fs@%sMiB:%s" % (op, v[0], fmt1(v[1]["mib"]), v[1]["sha"][:12]) for op, v in sorted(w.items())))
        if ct is not None and stacking and ct["series"].startswith(("step-copies", "iges-step-copies")):
            parts.append("timeout_cliff_cause=stacked coincident copies of the generator (%s); "
                         "a time guard is needed, a size or triangle cap does not prevent it" % stacking)
            parts.append("recommended_ceiling_mib_if_the_timeout_cliff_applies=%d" % int(MARGIN * ct["bytes"] / MIB))
        label = klass
        if klass in ("stl_binary", "stl_ascii") and stl_shared:
            if klass == "stl_ascii":
                continue
            label = "stl"
        if private_n:
            private_txt = "private=%d files measured" % private_n
        else:
            private_txt = "private=%s" % private_note
        print("CEILING,fmt=%s,limit_mib=%d,ceiling_mib=%d,derivation=size_max=(%.2f*%d-%s-%s)/%.3f=%.1f MiB rounded down;"
              "floor and k_worst from the 4g rows of sha %s (pipeline %s) and the floor rows below 1 MiB;go from GO_WORKER,%s,%s,%s" % (
                  label, L, ceiling, BUDGET, L, fmt1(d["floor"]), fmt1(go), kw["k"], sm,
                  kw["pt"]["sha"], "+".join(PIPELINE[klass]), ",".join(parts), wall_txt, private_txt))

    d_step, kw_step = derived["step"]
    caps = []
    if kw_step is not None:
        ent = []
        for p in d_step["size_pts"]:
            e = num(ev.cnt.get(p["sha"], {}).get("entities"))
            if e:
                ent.append({"n": e, "status": p["status"], "peak": p["peak"], "sha": p["sha"]})
        caps.append(cap_line("step_entities", ent, d_step["floor"], go, L, "million entities", 1e6,
                             ",basis=step pipeline peak at 4g, real and size-scaled files"))

        nauo = []
        for p in ev.points(PIN_A, "4g", "step"):
            if not p["series"].startswith("step-nauo"):
                continue
            n = name_count(p["name"]) or num(ev.cnt.get(p["sha"], {}).get("nauo"))
            if n:
                nauo.append({"n": n, "status": p["status"], "peak": p["peak"], "sha": p["sha"]})
        mesh_wall = []
        for p in ev.points(PIN_A, "4g", "step"):
            if p["series"].startswith("step-nauo") and "mesh" in p["ops"] and p["ops"]["mesh"].wall():
                n = name_count(p["name"]) or num(ev.cnt.get(p["sha"], {}).get("nauo"))
                mesh_wall.append("%d:%.1f" % (int(n or 0), p["ops"]["mesh"].wall()))
        caps.append(cap_line("nauo", nauo, d_step["floor"], go, L, "instance", 1.0,
                             ",basis=step-nauo series: one prototype, n instances; mesh_wall_s by n=%s" % ";".join(mesh_wall)))

    render = []
    for sha, m in ev.meta.items():
        cell = ev.cells.get((PIN_A, "4g", sha, "render"))
        if cell is None:
            continue
        tri = None
        for r in cell.rows:
            tri = num(r["_out"].get("triangles")) or tri
        if tri is None:
            mesh = ev.cells.get((PIN_A, "4g", sha, "mesh"))
            if mesh is not None:
                for r in mesh.rows:
                    tri = num(r["_out"].get("triangles")) or tri
        if tri is None:
            tri = num(ev.cnt.get(sha, {}).get("triangles"))
        if tri:
            render.append({"n": tri, "status": cell.status() or "fail", "peak": cell.rss(), "sha": sha})
    floor_render = max((p["peak"] for p in render if p["status"] == "ok" and p["peak"] and p["n"] < 20000), default=None)
    caps.append(cap_line("render_triangles", render, floor_render, go, L, "million triangles", 1e6,
                         ",basis=render cell only; floor_render_mib=%s" % (fmt1(floor_render) if floor_render else "-")))

    d_dxf = derived["dxf"][0]
    mi = []
    for p in ev.points(PIN_A, "4g", "dxf"):
        if not p["series"].startswith("dxf-minsert"):
            continue
        cells = name_count(p["name"])
        if cells:
            mi.append({"n": cells, "status": p["status"], "peak": p["peak"], "sha": p["sha"], "p": p})
    walls = ";".join("%d:%.1f" % (int(x["n"]), x["p"]["ops"]["dxf"].wall()) for x in sorted(mi, key=lambda x: x["n"])
                     if "dxf" in x["p"]["ops"] and x["p"]["ops"]["dxf"].wall())
    real_ins = max((num(ev.cnt.get(sha, {}).get("insert")) or 0 for sha, m in ev.meta.items()
                    if ev.klass(sha) == "dxf" and m["series"] == "real"), default=0)
    caps.append(cap_line("dxf_expanded_inserts", mi, d_dxf["floor"], go, L, "expanded insert", 1.0,
                         ",basis=dxf-minsert series (one MINSERT, columns x rows cells of a 5-entity block); dxf_wall_s by n=%s;"
                         "real_corpus_max_INSERT_count=%d and none of the real files has a MINSERT" % (walls, int(real_ins))))
    for c in caps:
        print(c)

    ev_formula = Evidence(args[0], ("on", "default"))
    print("PIN_CONFIG,rows=par=on;lp=default,reason=the 8.0.1.0.0 ladder exists only at this configuration; "
          "both pins are compared on rows of the same configuration and no row of another is mixed in")
    for line in pin_decision(ev_formula, override):
        print(line)
    for line in ev.threads:
        print(line)
    for line in ev.quality:
        print(line)
    return 0


def rss_of(cell, mode):
    rows = [r for r in cell.rows if r["rc"] == 0]
    if mode == "child":
        vals = [r["child_rss_mib"] for r in rows if r["child_rss_mib"] is not None]
    elif mode == "cg":
        vals = [r["cg_peak_mib"] for r in rows if r["cg_peak_mib"] is not None]
    else:
        vals = [r["rss"] for r in rows if r["rss"] is not None]
    return max(vals) if vals else None


def pin_k(ev, common, pin_idx, mode):
    ocp = (PIN_A, PIN_B)[pin_idx]
    low, ks = [], []
    for sha, m in common:
        if counter_series(m["series"]):
            continue
        v = rss_of(ev.cells[(ocp, "4g", sha, "mesh")], mode)
        if v is None:
            continue
        if m["bytes"] < MIB * LOW_MIB:
            low.append(v)
    if not low:
        return None, None
    floor = max(low)
    for sha, m in common:
        if counter_series(m["series"]) or m["bytes"] < MIB * LOW_MIB:
            continue
        v = rss_of(ev.cells[(ocp, "4g", sha, "mesh")], mode)
        if v is not None:
            ks.append((v - floor) / (m["bytes"] / MIB))
    return floor, (max(ks) if ks else None)


def pin_decision(ev, override=None):
    def mesh_cells(ocp):
        out = {}
        fails = set()
        for sha, m in ev.meta.items():
            if m["fmt"] not in ("step", "iges"):
                continue
            cell = ev.cells.get((ocp, "4g", sha, "mesh"))
            if cell is None:
                continue
            if cell.status() == "ok":
                out[sha] = (cell.rss(), cell.wall(), m["bytes"])
            else:
                fails.add(sha)
        return out, fails

    a, fail_a = mesh_cells(PIN_A)
    b, fail_b = mesh_cells(PIN_B)
    if not b and not fail_b:
        return ["PIN_DECISION,choice=%s,reason=no 8.0.1.0.0 rows in the evidence; thresholds a/b/c not evaluated" % (override or PIN_A)]
    only_b = sorted(s for s in b if s in fail_a)
    only_a = sorted(s for s in a if s in fail_b)
    common_sha = sorted(set(a) & set(b))
    common = [(s, ev.meta[s]) for s in common_sha]
    modes = {}
    for mode in ("eff", "child", "cg"):
        fa, ka = pin_k(ev, common, 0, mode)
        fb, kb = pin_k(ev, common, 1, mode)
        change = (ka - kb) / ka if ka and kb is not None else None
        modes[mode] = (fa, fb, ka, kb, change)
    kA, kB, k_change = modes["eff"][2], modes["eff"][3], modes["eff"][4]
    wall_a = sum(a[s][1] for s in common_sha)
    wall_b = sum(b[s][1] for s in common_sha)
    heavy = [s for s in common_sha if a[s][1] >= 2.0]
    med_ratio = statistics.median([b[s][1] / a[s][1] for s in heavy]) if heavy else None
    cond_a = len(only_b) > 0
    cond_b = k_change is not None and k_change >= PIN_K_DROP
    w_change = (wall_a - wall_b) / wall_a if wall_a > 0 else None
    cond_c = w_change is not None and w_change >= PIN_WALL_DROP
    formula = PIN_B if (cond_a or cond_b or cond_c) else PIN_A
    flips = {m: (v[4] is not None and v[4] >= PIN_K_DROP) for m, v in modes.items()}
    robust_b = len(set(flips.values())) == 1
    pending = (formula == PIN_B and not cond_a and not cond_c and not robust_b)
    steps = [(ev.meta[s]["bytes"] / MIB, a[s][0], b[s][0]) for s in common_sha
             if ev.meta[s]["series"].startswith("step-copies-freecad") and ev.meta[s]["bytes"] >= 40 * MIB]
    steps.sort()
    slope = ""
    if len(steps) >= 2:
        sa = (steps[-1][1] - steps[0][1]) / (steps[-1][0] - steps[0][0])
        sb = (steps[-1][2] - steps[0][2]) / (steps[-1][0] - steps[0][0])
        slope = "heavy_slope_mib_per_mib 7.9.3.1.1=%.2f 8.0.1.0.0=%.2f (step-copies-freecad rungs of 40 MiB and above)" % (sa, sb)
    reason = (
        "thresholds: (a) 8.0.1.0.0 reads a corpus file that 7.9.3.1.1 fails: %d file(s), reverse %d file(s); "
        "(b) k_worst mesh at least %d%% lower: 7.9.3.1.1=%s 8.0.1.0.0=%s change=%s; "
        "(c) mesh wall sum at least %d%% lower over %d common files: 7.9.3.1.1=%.2fs 8.0.1.0.0=%.2fs change=%s "
        "(median per-file ratio over %d files with wall >= 2s: %s); host noise applies to (c); "
        "switch only if (a) or (b) or (c); formula result %s%s" % (
            len(only_b), len(only_a), PIN_K_DROP * 100,
            "%.3f" % kA if kA is not None else "-", "%.3f" % kB if kB is not None else "-",
            "%.1f%%" % (k_change * 100) if k_change is not None else "-",
            PIN_WALL_DROP * 100, len(common_sha), wall_a, wall_b,
            "%.1f%%" % (w_change * 100) if w_change is not None else "-",
            len(heavy), "%.3f" % med_ratio if med_ratio is not None else "-",
            formula,
            ("; needs_user_confirmation=1 because only (b) is met and it is not robust to the RSS definition (see PIN_ROBUSTNESS)"
             if pending else "")))
    lines = []
    if override and override != formula:
        lines.append("PIN_FORMULA,choice=%s,reason=%s" % (formula, reason))
        lines.append("PIN_DECISION,choice=%s,reason=user decision overrides the formula result %s (formula line above)" % (override, formula))
    else:
        lines.append("PIN_DECISION,choice=%s,reason=%s" % (override or formula, reason))
    lines.append("PIN_ROBUSTNESS,k_worst_change_by_rss_definition=%s,threshold=%d%%,b_met_by_definition=%s,%s,licence_note=8.0.1.0.0 adds libjasper libbz2 Imath (V3/V4)" % (
        " ".join("%s:%s(7.9=%s 8.0=%s floor 7.9=%s 8.0=%s)" % (
            m, "%.1f%%" % (v[4] * 100) if v[4] is not None else "-",
            "%.2f" % v[2] if v[2] is not None else "-", "%.2f" % v[3] if v[3] is not None else "-",
            "%.0f" % v[0] if v[0] is not None else "-", "%.0f" % v[1] if v[1] is not None else "-")
            for m, v in modes.items()),
        PIN_K_DROP * 100, ",".join("%s=%s" % (m, "yes" if f else "no") for m, f in flips.items()), slope or "heavy_slope=-"))
    return lines


if __name__ == "__main__":
    sys.exit(main())
