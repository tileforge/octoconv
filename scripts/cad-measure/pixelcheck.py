#!/usr/bin/env python3
"""pixelcheck.py -- judge rendered PNGs by their pixels, never by an exit code.

Usage:
    pixelcheck.py single PNG [--mode part|drawing]
    pixelcheck.py views PNG PNG [PNG...]
    pixelcheck.py same PNG PNG
    pixelcheck.py transparent PNG
"""

import argparse
import hashlib
import itertools
import json
import os
import sys

import numpy as np
from PIL import Image

FRAC_MIN = 0.02
FRAC_MAX = 0.90
DARK_MIN = 0.001
MARGIN_MIN = 0.03

SLIVER_ASPECT = 0.02
SLIVER_EXTENT_MIN = 0.80

DRAW_FRAC_MIN = 0.0003
DRAW_DARK_MIN = 0.0002

VIEW_IOU_MAX = 0.97
VIEW_MAD_MIN = 0.01

TRANSPARENT_MIN = 0.05
TRANSPARENT_MAX = 0.95


def load_rgb(path):
    return np.asarray(Image.open(path).convert("RGB"), dtype=np.uint8)


def sidecar(path):
    try:
        with open(path + ".json") as fh:
            return json.load(fh)
    except (OSError, ValueError):
        return {}


def stats(img):
    h, w = img.shape[:2]
    mask = (img < 250).any(axis=2)
    dark = float((img < 80).all(axis=2).mean())
    frac = float(mask.mean())
    ys, xs = np.nonzero(mask)
    if xs.size == 0:
        return h, w, mask, dark, frac, None
    box = (int(xs.min()), int(ys.min()), int(xs.max()), int(ys.max()))
    return h, w, mask, dark, frac, box


def single(path, mode):
    img = load_rgb(path)
    h, w, mask, dark, frac, box = stats(img)
    if box is None:
        print("PIX,frac=0.0000,margin_px=-1,dark=%.4f,bbox=-,w=%d,h=%d,mode=%s,ok=0" % (dark, w, h, mode))
        return 1
    x0, y0, x1, y1 = box
    margin = min(x0, y0, w - 1 - x1, h - 1 - y1)
    margin_ok = margin >= int(MARGIN_MIN * min(w, h))
    if mode == "drawing":
        ok = frac >= DRAW_FRAC_MIN and dark >= DRAW_DARK_MIN and margin_ok
    else:
        info = sidecar(path)
        aspect = info.get("proj_aspect")
        if aspect is not None and aspect < SLIVER_ASPECT:
            mode = "sliver"
            extent = max(x1 - x0 + 1, y1 - y0 + 1)
            ok = extent >= SLIVER_EXTENT_MIN * min(w, h) and int(mask.sum()) >= extent and margin_ok
        else:
            ok = FRAC_MIN <= frac <= FRAC_MAX and dark >= DARK_MIN and margin_ok
    print("PIX,frac=%.4f,margin_px=%d,dark=%.4f,bbox=%d:%d:%d:%d,w=%d,h=%d,mode=%s,ok=%d" % (
        frac, margin, dark, x0, y0, x1, y1, w, h, mode, 1 if ok else 0))
    return 0 if ok else 1


def views(paths):
    imgs = [load_rgb(p) for p in paths]
    masks = [(im < 250).any(axis=2) for im in imgs]
    grays = [im.astype(np.float32).mean(axis=2) for im in imgs]
    ious = []
    mads = []
    ok = True
    for i, j in itertools.combinations(range(len(paths)), 2):
        if imgs[i].shape != imgs[j].shape:
            print("VIEWS,n=%d,error=size_mismatch,ok=0" % len(paths))
            return 1
        union = int((masks[i] | masks[j]).sum())
        iou = 1.0 if union == 0 else float((masks[i] & masks[j]).sum()) / union
        mad = float(np.abs(grays[i] - grays[j]).mean()) / 255.0
        ious.append(iou)
        mads.append(mad)
        if not (iou < VIEW_IOU_MAX or mad > VIEW_MAD_MIN):
            ok = False
    print("VIEWS,n=%d,pairs=%d,min_pair_iou=%.4f,max_pair_iou=%.4f,min_mad=%.4f,ok=%d" % (
        len(paths), len(ious), min(ious), max(ious), min(mads), 1 if ok else 0))
    return 0 if ok else 1


def digest(path):
    with open(path, "rb") as fh:
        return hashlib.sha256(fh.read()).hexdigest()


def same(a, b):
    sha_equal = digest(a) == digest(b)
    ia = np.asarray(Image.open(a).convert("RGBA"))
    ib = np.asarray(Image.open(b).convert("RGBA"))
    pixel_equal = ia.shape == ib.shape and bool((ia == ib).all())
    diff = int((ia != ib).any(axis=2).sum()) if ia.shape == ib.shape else -1
    ok = sha_equal and pixel_equal
    print("SAME,sha_equal=%d,pixel_equal=%d,diff_pixels=%d,w=%d,h=%d,ok=%d" % (
        1 if sha_equal else 0, 1 if pixel_equal else 0, diff, ia.shape[1], ia.shape[0], 1 if ok else 0))
    return 0 if ok else 1


def transparent(path):
    im = Image.open(path)
    has_alpha = im.mode in ("RGBA", "LA") or "transparency" in im.info
    if not has_alpha:
        print("TRANSPARENT,alpha=0,frac_transparent=0.0000,frac_opaque=0.0000,ok=0")
        return 1
    alpha = np.asarray(im.convert("RGBA"))[:, :, 3]
    frac_t = float((alpha == 0).mean())
    frac_o = float((alpha == 255).mean())
    ok = TRANSPARENT_MIN <= frac_t <= TRANSPARENT_MAX and frac_o > 0
    print("TRANSPARENT,alpha=1,frac_transparent=%.4f,frac_opaque=%.4f,ok=%d" % (frac_t, frac_o, 1 if ok else 0))
    return 0 if ok else 1


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("cmd", choices=("single", "views", "same", "transparent"))
    ap.add_argument("pngs", nargs="+")
    ap.add_argument("--mode", choices=("part", "drawing"), default="part")
    args = ap.parse_args(argv[1:])
    for p in args.pngs:
        if not os.path.isfile(p):
            print("pixelcheck: missing file", file=sys.stderr)
            return 2
    if args.cmd == "single" and len(args.pngs) == 1:
        return single(args.pngs[0], args.mode)
    if args.cmd == "views" and len(args.pngs) >= 2:
        return views(args.pngs)
    if args.cmd == "same" and len(args.pngs) == 2:
        return same(*args.pngs)
    if args.cmd == "transparent" and len(args.pngs) == 1:
        return transparent(args.pngs[0])
    print(__doc__, file=sys.stderr)
    return 64


if __name__ == "__main__":
    sys.exit(main(sys.argv))
