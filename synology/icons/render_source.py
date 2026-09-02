#!/usr/bin/env python3
"""Render a transparent PilotServer glyph (no tile, no shadow)."""
import math
import os
import struct
import zlib

SIZE = 640
BLUE = (62, 106, 225)  # Tesla Electric Blue #3E6AE1
HUB = (52, 87, 196)  # same hue, slightly darker for hub/chevron contrast
CHEVRON = (255, 255, 255)


def png_chunk(tag, data):
    return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)


def write_png(path, size, pixels):
    raw = bytearray()
    for y in range(size):
        raw.append(0)
        for x in range(size):
            raw += bytes(pixels[y * size + x])
    ihdr = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    blob = (
        b"\x89PNG\r\n\x1a\n"
        + png_chunk(b"IHDR", ihdr)
        + png_chunk(b"IDAT", zlib.compress(bytes(raw), 9))
        + png_chunk(b"IEND", b"")
    )
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    with open(path, "wb") as f:
        f.write(blob)


def cover_disk(x, y, cx, cy, r):
    d = math.hypot(x - cx, y - cy)
    return max(0.0, min(1.0, r + 0.55 - d))


def dist_seg(px, py, x1, y1, x2, y2):
    vx, vy = x2 - x1, y2 - y1
    l2 = vx * vx + vy * vy
    if l2 == 0:
        return math.hypot(px - x1, py - y1)
    t = max(0.0, min(1.0, ((px - x1) * vx + (py - y1) * vy) / l2))
    return math.hypot(px - (x1 + t * vx), py - (y1 + t * vy))


def cover_capsule(x, y, x1, y1, x2, y2, r):
    return max(0.0, min(1.0, r + 0.55 - dist_seg(x, y, x1, y1, x2, y2)))


def sign(p1, p2, p3):
    return (p1[0] - p3[0]) * (p2[1] - p3[1]) - (p2[0] - p3[0]) * (p1[1] - p3[1])


def point_in_tri(p, a, b, c):
    d1, d2, d3 = sign(p, a, b), sign(p, b, c), sign(p, c, a)
    has_neg = (d1 < 0) or (d2 < 0) or (d3 < 0)
    has_pos = (d1 > 0) or (d2 > 0) or (d3 > 0)
    return not (has_neg and has_pos)


def dist_tri(px, py, a, b, c):
    p = (px, py)
    inside = point_in_tri(p, a, b, c)
    d = min(dist_seg(px, py, *a, *b), dist_seg(px, py, *b, *c), dist_seg(px, py, *c, *a))
    return -d if inside else d


def cover_tri(x, y, a, b, c):
    return max(0.0, min(1.0, 0.55 - dist_tri(x, y, a, b, c)))


def over(dst, color, a):
    if a <= 0:
        return dst
    r, g, b, da = dst
    ca = int(round(a * 255))
    if ca <= 0:
        return dst
    out_a = ca + da * (255 - ca) // 255
    if out_a == 0:
        return (0, 0, 0, 0)
    inv = 255 - ca
    or_ = (color[0] * ca + r * da * inv // 255) // out_a
    og = (color[1] * ca + g * da * inv // 255) // out_a
    ob = (color[2] * ca + b * da * inv // 255) // out_a
    return (or_, og, ob, out_a)


def render():
    cx = cy = SIZE / 2.0
    r_out, r_in = 248.0, 176.0
    hub_r = 62.0
    spoke_r = 30.0
    spoke_inner = hub_r - 8.0
    spoke_outer = r_in + 8.0
    # Classic 3-spoke: down, upper-left, upper-right.
    angles = (math.pi / 2.0, math.pi / 2.0 + 2.0 * math.pi / 3.0, math.pi / 2.0 + 4.0 * math.pi / 3.0)
    chevron = ((cx, cy - 34.0), (cx - 28.0, cy + 18.0), (cx + 28.0, cy + 18.0))
    px = []
    for y in range(SIZE):
        py = y + 0.5
        for x in range(SIZE):
            sx = x + 0.5
            pixel = (0, 0, 0, 0)
            ring = min(cover_disk(sx, py, cx, cy, r_out), 1.0 - cover_disk(sx, py, cx, cy, r_in))
            pixel = over(pixel, BLUE, ring)
            for ang in angles:
                x1 = cx + math.cos(ang) * spoke_inner
                y1 = cy + math.sin(ang) * spoke_inner
                x2 = cx + math.cos(ang) * spoke_outer
                y2 = cy + math.sin(ang) * spoke_outer
                pixel = over(pixel, BLUE, cover_capsule(sx, py, x1, y1, x2, y2, spoke_r))
            pixel = over(pixel, HUB, cover_disk(sx, py, cx, cy, hub_r))
            pixel = over(pixel, CHEVRON, cover_tri(sx, py, *chevron))
            px.append(pixel)
    return px


def main():
    out = os.path.join(os.path.dirname(__file__), "source.png")
    write_png(out, SIZE, render())
    print("wrote", out)


if __name__ == "__main__":
    main()
