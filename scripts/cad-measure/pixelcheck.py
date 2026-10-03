#!/usr/bin/env python3
"""pixelcheck.py -- judge a rendered PNG by its pixels, never by an exit code.

Usage:
    pixelcheck.py single PNG
"""

import sys

import numpy as np
from PIL import Image

FRAC_MIN = 0.02
FRAC_MAX = 0.90
DARK_MIN = 0.001
MARGIN_MIN = 0.03


def single(path):
    img = np.asarray(Image.open(path).convert("RGB"), dtype=np.uint8)
    h, w = img.shape[:2]
    mask = (img < 250).any(axis=2)
    dark = float((img < 80).all(axis=2).mean())
    frac = float(mask.mean())
    ys, xs = np.nonzero(mask)
    if xs.size == 0:
        print("PIX,frac=0.0000,margin_px=-1,dark=%.4f,bbox=-,w=%d,h=%d,ok=0" % (dark, w, h))
        return 1
    x0, x1, y0, y1 = int(xs.min()), int(xs.max()), int(ys.min()), int(ys.max())
    margin = min(x0, y0, w - 1 - x1, h - 1 - y1)
    ok = (
        FRAC_MIN <= frac <= FRAC_MAX
        and dark >= DARK_MIN
        and margin >= int(MARGIN_MIN * min(w, h))
    )
    print("PIX,frac=%.4f,margin_px=%d,dark=%.4f,bbox=%d:%d:%d:%d,w=%d,h=%d,ok=%d" % (
        frac, margin, dark, x0, y0, x1, y1, w, h, 1 if ok else 0))
    return 0 if ok else 1


def main(argv):
    if len(argv) != 3 or argv[1] != "single":
        print(__doc__, file=sys.stderr)
        return 64
    return single(argv[2])


if __name__ == "__main__":
    sys.exit(main(sys.argv))
