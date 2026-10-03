#!/usr/bin/env python3
"""measure_wrap.py -- run one command and print a single WRAP measurement line.

Usage:
    measure_wrap.py [--budget-s N] [--outdir DIR] -- CMD...
"""

import argparse
import json
import os
import re
import resource
import signal
import subprocess
import sys
import time

OUT_KEYS = (
    "solids", "closed_solids", "volume", "volume_null_reason", "unit_text",
    "unit_fileunits", "triangles", "bbox_diag", "text0", "t_render_s",
    "t_total_s",
)


def read_int(path):
    try:
        with open(path) as fh:
            return int(fh.read().strip())
    except (OSError, ValueError):
        return None


def oom_kill():
    try:
        with open("/sys/fs/cgroup/memory.events") as fh:
            for line in fh:
                k, _, v = line.partition(" ")
                if k == "oom_kill":
                    return int(v)
    except (OSError, ValueError):
        pass
    return None


def threads(pid):
    try:
        with open("/proc/%d/status" % pid) as fh:
            for line in fh:
                if line.startswith("Threads:"):
                    return int(line.split()[1])
    except (OSError, ValueError):
        pass
    return 0


def compact(outdir):
    if not outdir:
        return "-"
    for name in ("measure.json", "render.png.json"):
        path = os.path.join(outdir, name)
        if not os.path.isfile(path):
            continue
        try:
            with open(path) as fh:
                data = json.load(fh)
        except (OSError, ValueError):
            continue
        parts = []
        for k in OUT_KEYS:
            if k in data and data[k] is not None:
                v = re.sub(r"[,;:=\s]", "_", str(data[k]))
                parts.append("%s:%s" % (k, v))
        if parts:
            return ";".join(parts)
    return "-"


def mib(n):
    return "na" if n is None else str(n // 2**20)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--budget-s", type=float,
                    default=float(os.environ.get("OCTOCONV_CAD_CELL_BUDGET", "900")))
    ap.add_argument("--outdir", default="")
    ap.add_argument("cmd", nargs=argparse.REMAINDER)
    args = ap.parse_args()
    cmd = args.cmd[1:] if args.cmd and args.cmd[0] == "--" else args.cmd
    if not cmd:
        ap.error("command required after --")

    log_path = os.path.join(args.outdir, "child.log") if args.outdir else None
    log = open(log_path, "wb") if log_path else subprocess.DEVNULL

    t0 = time.monotonic()
    proc = subprocess.Popen(cmd, stdout=log, stderr=subprocess.STDOUT,
                            start_new_session=True)
    threads_max = 0
    timed_out = 0
    while proc.poll() is None:
        threads_max = max(threads_max, threads(proc.pid))
        if time.monotonic() - t0 > args.budget_s:
            timed_out = 1
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            break
        time.sleep(0.05)
    proc.wait()
    wall = time.monotonic() - t0
    if log_path:
        log.close()

    rc = proc.returncode
    if rc < 0:
        rc = 128 - rc

    ru = resource.getrusage(resource.RUSAGE_CHILDREN)
    peak = read_int("/sys/fs/cgroup/memory.peak")
    oom = oom_kill()

    if rc != 0 and log_path and os.path.isfile(log_path):
        with open(log_path, "rb") as fh:
            tail = fh.read().decode("utf-8", "replace").splitlines()[-20:]
        for line in tail:
            print("CHILD: " + line, file=sys.stderr)

    print("WRAP,rc=%d,wall_s=%.2f,utime_s=%.2f,stime_s=%.2f,child_rss_mib=%d,"
          "cg_peak_mib=%s,oom_kill=%s,timeout=%d,threads_max=%d,out=%s" % (
              rc, wall, ru.ru_utime, ru.ru_stime, ru.ru_maxrss // 1024,
              mib(peak), "na" if oom is None else oom, timed_out, threads_max,
              compact(args.outdir)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
