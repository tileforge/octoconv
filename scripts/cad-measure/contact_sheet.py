#!/usr/bin/env python3
"""contact_sheet.py -- tile PNGs into a labelled rows x columns grid.

Usage:
    contact_sheet.py --out PNG --rows R1,R2 --cols C1,C2 PNG...

The PNG arguments are taken row by row. A path that does not exist is drawn
as an empty cell marked MISSING.
"""

import argparse
import os
import sys

from PIL import Image, ImageDraw, ImageFont

THUMB = 320
GAP = 8
FONT_PATH = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
FONT_SIZE = 14


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--out", required=True)
    ap.add_argument("--rows", required=True)
    ap.add_argument("--cols", required=True)
    ap.add_argument("pngs", nargs="+")
    args = ap.parse_args(argv[1:])
    rows = args.rows.split(",")
    cols = args.cols.split(",")
    if len(args.pngs) != len(rows) * len(cols):
        print("contact_sheet: %d images for a %dx%d grid" % (len(args.pngs), len(rows), len(cols)),
              file=sys.stderr)
        return 64

    font = ImageFont.truetype(FONT_PATH, FONT_SIZE)
    label_w = max(int(font.getlength(r)) for r in rows) + 2 * GAP
    head_h = FONT_SIZE + 2 * GAP
    width = label_w + len(cols) * (THUMB + GAP)
    height = head_h + len(rows) * (THUMB + GAP)
    sheet = Image.new("RGB", (width, height), (255, 255, 255))
    draw = ImageDraw.Draw(sheet)

    for c, name in enumerate(cols):
        draw.text((label_w + c * (THUMB + GAP) + GAP, GAP), name, fill=(0, 0, 0), font=font)
    for r, name in enumerate(rows):
        draw.text((GAP, head_h + r * (THUMB + GAP) + THUMB // 2), name, fill=(0, 0, 0), font=font)

    for i, path in enumerate(args.pngs):
        r, c = divmod(i, len(cols))
        x = label_w + c * (THUMB + GAP)
        y = head_h + r * (THUMB + GAP)
        if os.path.isfile(path):
            im = Image.open(path).convert("RGBA")
            bg = Image.new("RGBA", im.size, (255, 255, 255, 255))
            im = Image.alpha_composite(bg, im).convert("RGB")
            im.thumbnail((THUMB, THUMB), Image.LANCZOS)
            sheet.paste(im, (x, y))
        else:
            draw.text((x + GAP, y + THUMB // 2), "MISSING", fill=(200, 0, 0), font=font)
        draw.rectangle((x, y, x + THUMB - 1, y + THUMB - 1), outline=(210, 210, 210))

    sheet.save(args.out, optimize=False)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
