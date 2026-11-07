#!/usr/bin/env python3
"""Generates the game's sprite atlas and its TypeScript metadata.

Everything is drawn procedurally, so the art is original and reproducible
under the project's license. Run from the repository root:

    python3 tools/gen_assets/gen_sprites.py

Outputs:
    web/dist/assets/sprites.png   the atlas
    web/src/sprites.ts            sprite rectangles (generated, do not edit)

Requires Pillow.
"""
import math
import random
from pathlib import Path

from PIL import Image, ImageChops, ImageDraw

ROOT = Path(__file__).resolve().parents[2]
ATLAS_PATH = ROOT / "web/dist/assets/sprites.png"
TS_PATH = ROOT / "web/src/sprites.ts"
ATLAS_WIDTH = 1024
SS = 4  # supersampling factor

TANK_FRAMES = 48
EXPLOSION_FRAMES = 32
SMALL_EXPLOSION_FRAMES = 16

# ---------------------------------------------------------------------------
# drawing helpers
# ---------------------------------------------------------------------------


class Canvas:
    """A supersampled RGBA canvas with an anchor point (the tile centre)."""

    def __init__(self, w, h, ax, ay):
        self.w, self.h, self.ax, self.ay = int(w), int(h), ax, ay
        self.img = Image.new("RGBA", (self.w * SS, self.h * SS), (0, 0, 0, 0))
        self.d = ImageDraw.Draw(self.img)

    def pt(self, x, y):
        return ((self.ax + x) * SS, (self.ay + y) * SS)

    def poly(self, pts, fill, outline=None, width=1):
        pp = [self.pt(*p) for p in pts]
        self.d.polygon(pp, fill=fill)
        if outline:
            self.d.line(pp + [pp[0]], fill=outline, width=int(width * SS), joint="curve")

    def line(self, pts, fill, width):
        pp = [self.pt(*p) for p in pts]
        self.d.line(pp, fill=fill, width=int(width * SS), joint="curve")
        r = width * SS / 2
        for p in (pp[0], pp[-1]):
            self.d.ellipse([p[0] - r, p[1] - r, p[0] + r, p[1] + r], fill=fill)

    def ellipse(self, cx, cy, rx, ry, fill, outline=None, width=1):
        x, y = self.pt(cx, cy)
        self.d.ellipse(
            [x - rx * SS, y - ry * SS, x + rx * SS, y + ry * SS],
            fill=fill,
            outline=outline,
            width=int(width * SS),
        )

    def done(self):
        return self.img.resize((self.w, self.h), Image.BOX)


def shade(c, f):
    return tuple(max(0, min(255, int(v * f))) for v in c[:3]) + ((c[3],) if len(c) > 3 else ())


def mix(a, b, t):
    return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(3))


def darken(img):
    """Fog-of-war version of a sprite: dim, desaturated and slightly warm."""
    r, g, b, a = img.split()
    out = Image.merge("RGB", (r, g, b))
    gray = out.convert("L").convert("RGB")
    out = Image.blend(out, gray, 0.55)
    out = ImageChops.multiply(out, Image.new("RGB", out.size, (125, 112, 104)))
    out.putalpha(a)
    return out


# ---------------------------------------------------------------------------
# geometry for one preset
# ---------------------------------------------------------------------------


class Preset:
    def __init__(self, name, width, height, thickness):
        self.name = name
        self.W, self.H, self.T = width, height, thickness
        self.u = width / 64.0
        self.k = height / width  # vertical squash of the ground plane
        self.sprites = []  # (key, Image, anchor)

    def add(self, key, canvas_or_img, ax=None, ay=None):
        if isinstance(canvas_or_img, Canvas):
            img, ax, ay = canvas_or_img.done(), canvas_or_img.ax, canvas_or_img.ay
        else:
            img = canvas_or_img
        self.sprites.append((key, img, ax, ay))

    # screen vector from a tile centre to its neighbour in direction idx;
    # idx order matches IDX_TO_UNIT_VECTOR in web/src/vector.ts
    def dir_vec(self, idx):
        W, H = self.W, self.H
        return [(W, 0), (0.5 * W, 0.75 * H), (-0.5 * W, 0.75 * H), (-W, 0), (-0.5 * W, -0.75 * H), (0.5 * W, -0.75 * H)][idx]

    def edge_mid(self, idx):
        v = self.dir_vec(idx)
        return (v[0] / 2, v[1] / 2)

    def hex_pts(self, inset=0.0):
        W, H = self.W, self.H
        pts = [(0, -H / 2), (W / 2, -H / 4), (W / 2, H / 4), (0, H / 2), (-W / 2, H / 4), (-W / 2, -H / 4)]
        if inset:
            s = 1 - inset
            pts = [(x * s, y * s) for x, y in pts]
        return pts


# ---------------------------------------------------------------------------
# ground
# ---------------------------------------------------------------------------

SAND = [(224, 203, 152), (213, 190, 140), (230, 212, 168)]


def draw_hex(p, variant):
    W, H, T, u = p.W, p.H, p.T, p.u
    c = Canvas(W, H + T, W / 2, H / 2)
    top = SAND[variant]
    side_l, side_r = shade(top, 0.62), shade(top, 0.5)
    top_pts = p.hex_pts()
    left = [top_pts[4], top_pts[3], (top_pts[3][0], top_pts[3][1] + T), (top_pts[4][0], top_pts[4][1] + T)]
    right = [top_pts[3], top_pts[2], (top_pts[2][0], top_pts[2][1] + T), (top_pts[3][0], top_pts[3][1] + T)]
    c.poly(left, side_l)
    c.poly(right, side_r)
    c.poly(top_pts, top)
    rng = random.Random(100 + variant)
    for _ in range(int(40 * u * u)):
        x, y = rng.uniform(-W / 2.4, W / 2.4), rng.uniform(-H / 2.6, H / 2.6)
        if abs(x) / (W / 2) + abs(y) / (H / 2) < 0.9:
            c.ellipse(x, y, 0.7 * u, 0.4 * u, shade(top, rng.choice([0.93, 1.06])))
    return c


def draw_outline_hex(p, color, inset=0.06, width=2.0, hatch=None, fill=None):
    c = Canvas(p.W, p.H, p.W / 2, p.H / 2)
    pts = p.hex_pts(inset)
    if fill:
        c.poly(pts, fill)
    if hatch:
        layer = Canvas(p.W, p.H, p.W / 2, p.H / 2)
        step = 6 * p.u
        x = -p.W
        while x < p.W:
            layer.line([(x, p.H / 2), (x + p.H, -p.H / 2)], hatch, 1.6 * p.u)
            x += step
        mask = Canvas(p.W, p.H, p.W / 2, p.H / 2)
        mask.poly(pts, (255, 255, 255, 255))
        hatch_img = layer.img
        m = mask.img.split()[3]
        hatch_img.putalpha(ImageChops.multiply(hatch_img.split()[3], m))
        c.img.alpha_composite(hatch_img)
    c.poly(pts, None, color, width * p.u) if False else c.d.line(
        [c.pt(*q) for q in pts + [pts[0]]], fill=color, width=int(width * p.u * SS), joint="curve"
    )
    return c


# ---------------------------------------------------------------------------
# obstacles ("sites")
# ---------------------------------------------------------------------------


def iso_box(c, cx, cy, hw, hd, ht, top, z=0.0):
    """An axis-aligned isometric box standing on the ground at (cx, cy)."""
    n, e, s, w = (cx, cy - hd - z), (cx + hw, cy - z), (cx, cy + hd - z), (cx - hw, cy - z)
    up = lambda q: (q[0], q[1] - ht)
    c.poly([w, s, up(s), up(w)], shade(top, 0.62), shade(top, 0.38), 0.6)
    c.poly([s, e, up(e), up(s)], shade(top, 0.48), shade(top, 0.3), 0.6)
    c.poly([up(n), up(e), up(s), up(w)], top, shade(top, 0.55), 0.6)


def blob(c, cx, cy, rx, ry, color, rng, bumps=9):
    pts = []
    for i in range(bumps * 2):
        a = i / (bumps * 2) * math.tau
        r = 1 + rng.uniform(-0.18, 0.18)
        pts.append((cx + math.cos(a) * rx * r, cy + math.sin(a) * ry * r))
    c.poly(pts, color, shade(color, 0.5), 0.7)
    c.poly([(cx + (x - cx) * 0.55 - rx * 0.1, cy + (y - cy) * 0.5 - ry * 0.25) for x, y in pts], shade(color, 1.15))


def palm(c, x, y, u, rng, height=22):
    lean = rng.uniform(-3, 3) * u
    top = (x + lean, y - height * u)
    trunk = [(x, y), (x + lean * 0.3, y - height * u * 0.5), top]
    c.line(trunk, (96, 66, 38), 2.2 * u)
    for i in range(7):
        a = i / 7 * math.tau + rng.uniform(-0.2, 0.2)
        ex, ey = top[0] + math.cos(a) * 11 * u, top[1] + math.sin(a) * 5 * u + 3 * u
        c.line([top, ((top[0] + ex) / 2, top[1] - 3 * u), (ex, ey)], (58, 130, 52) if i % 2 else (84, 156, 60), 2.4 * u)


def draw_site(p, variant):
    W, H, u = p.W, p.H, p.u
    ch = int(H * 1.75)
    c = Canvas(W, ch, W / 2, ch - H / 2 - 2 * u)
    rng = random.Random(900 + variant)
    rock = (140, 130, 120)
    adobe = (176, 120, 78)
    if variant == 0:
        for dx, dy, r in [(-12, 2, 9), (8, 5, 11), (-2, -4, 8), (16, -2, 6)]:
            blob(c, dx * u, dy * u, r * u, r * 0.65 * u, shade(rock, rng.uniform(0.9, 1.1)), rng)
    elif variant == 1:
        for i in range(5):
            x = (-16 + i * 8) * u
            for row in range(2):
                iso_box(c, x + row * 3 * u, (3 - row * 5) * u, 5 * u, 2.4 * u, 3.6 * u, (200, 172, 112), z=row * 3.4 * u)
    elif variant == 2:
        iso_box(c, 0, 2 * u, 12 * u, 6 * u, 26 * u, adobe)
        c.poly([(-12 * u, -24 * u), (-6 * u, -30 * u), (-2 * u, -25 * u), (4 * u, -31 * u), (12 * u, -24 * u), (12 * u, -26 * u), (0, -34 * u), (-12 * u, -26 * u)], shade(adobe, 0.7))
        c.poly([(-8 * u, -4 * u), (-4 * u, -2 * u), (-4 * u, -12 * u), (-8 * u, -14 * u)], (50, 30, 24))
    elif variant == 3:
        for dx, dy, r in [(-8, 3, 11), (10, 0, 9)]:
            blob(c, dx * u, dy * u, r * u, r * 0.7 * u, (122, 98, 80), rng, bumps=7)
    elif variant == 4:
        palm(c, 0, 6 * u, u, rng, height=26)
        for dx in (-9, 8):
            blob(c, dx * u, 7 * u, 4 * u, 2 * u, (110, 140, 70), rng, bumps=5)
    elif variant == 5:
        palm(c, -12 * u, 6 * u, u, rng, 24)
        palm(c, 10 * u, 8 * u, u, rng, 28)
        palm(c, -1 * u, 10 * u, u, rng, 20)
    elif variant == 6:
        iso_box(c, 0, 2 * u, 15 * u, 7 * u, 11 * u, adobe)
        iso_box(c, 0, 2 * u, 16 * u, 7.5 * u, 1.6 * u, (140, 92, 62), z=11 * u)
        c.poly([(-10 * u, 5 * u), (-5 * u, 7.5 * u), (-5 * u, -2 * u), (-10 * u, -4 * u)], (60, 36, 28))
        c.poly([(5 * u, 7.5 * u), (10 * u, 5 * u), (10 * u, -1 * u), (5 * u, 1 * u)], (84, 120, 140))
    elif variant == 7:
        pts = [(-27 * u, 6 * u), (-14 * u, -6 * u), (0, -10 * u), (14 * u, -6 * u), (27 * u, 5 * u), (14 * u, 9 * u), (-14 * u, 9 * u)]
        c.poly(pts, (196, 168, 118), (150, 120, 80), 0.8)
        c.poly([(-14 * u, -6 * u), (0, -10 * u), (6 * u, 0), (-4 * u, 6 * u), (-18 * u, 2 * u)], (222, 196, 146))
    else:
        iso_box(c, -8 * u, 4 * u, 14 * u, 6.5 * u, 18 * u, adobe)
        iso_box(c, 12 * u, 0, 10 * u, 5 * u, 11 * u, shade(adobe, 1.12))
        c.poly([(-14 * u, 7 * u), (-9 * u, 9.5 * u), (-9 * u, -1 * u), (-14 * u, -3 * u)], (60, 36, 28))
        palm(c, 22 * u, 6 * u, u, rng, 22)
    return c


# ---------------------------------------------------------------------------
# tanks
# ---------------------------------------------------------------------------

# keypoints: grid angle (degrees) -> on-screen heading (degrees). A grid
# direction rotated by 60 degrees is not a 60 degree turn on screen because
# the board is squashed, so the sprite heading follows the hex edges.
def screen_heading(p, grid_angle):
    keys = [0, 60, 120, 180, 240, 300, 360]
    heads = []
    for i in range(6):
        v = p.dir_vec(i)
        heads.append(math.degrees(math.atan2(v[1], v[0])) % 360)
    heads = [heads[0], heads[1], heads[2], heads[3], heads[4], heads[5], 360.0]
    a = grid_angle % 360
    for i in range(6):
        if keys[i] <= a <= keys[i + 1]:
            t = (a - keys[i]) / 60.0
            h0, h1 = heads[i], heads[i + 1]
            if i == 5:
                h1 = 360.0
            return h0 + (h1 - h0) * t
    return 0.0


class TankGeom:
    def __init__(self, p, grid_angle):
        self.p = p
        hs = math.radians(screen_heading(p, grid_angle))
        gx, gy = math.cos(hs), math.sin(hs) / p.k
        n = math.hypot(gx, gy)
        self.g = (gx / n, gy / n)  # forward, in ground units
        self.s = (-self.g[1], self.g[0])  # sideways

    def ground(self, fwd, side):
        return (self.g[0] * fwd + self.s[0] * side, self.g[1] * fwd + self.s[1] * side)

    def screen(self, fwd, side, z):
        gx, gy = self.ground(fwd, side)
        return (gx, gy * self.p.k - z)


def prism(c, tg, fwd0, fwd1, side0, side1, z0, z1, top, bevel=0.0):
    """Draw a rectangular prism in tank-local coordinates."""
    corners = [(fwd0, side0), (fwd1, side0), (fwd1, side1), (fwd0, side1)]
    if bevel:
        corners = [(fwd0 + bevel, side0), (fwd1 - bevel, side0), (fwd1, side0 + bevel), (fwd1, side1 - bevel),
                   (fwd1 - bevel, side1), (fwd0 + bevel, side1), (fwd0, side1 - bevel), (fwd0, side0 + bevel)]
    n = len(corners)
    cx = sum(q[0] for q in corners) / n
    cy = sum(q[1] for q in corners) / n
    faces = []
    for i in range(n):
        a, b = corners[i], corners[(i + 1) % n]
        mf, ms = (a[0] + b[0]) / 2 - cx, (a[1] + b[1]) / 2 - cy
        gy = tg.ground(mf, ms)[1]
        gx = tg.ground(mf, ms)[0]
        if gy <= -0.02:
            continue  # facing away from the viewer
        light = 0.62 + 0.28 * max(0.0, -gx / (math.hypot(gx, gy) + 1e-9)) - 0.1 * max(0.0, gx / (math.hypot(gx, gy) + 1e-9))
        faces.append(([tg.screen(a[0], a[1], z0), tg.screen(b[0], b[1], z0), tg.screen(b[0], b[1], z1), tg.screen(a[0], a[1], z1)], shade(top, light)))
    for pts, col in faces:
        c.poly(pts, col, shade(col, 0.55), 0.5)
    c.poly([tg.screen(q[0], q[1], z1) for q in corners], top, shade(top, 0.6), 0.55)


PLAYER = {"hull": (98, 124, 70), "turret": (112, 140, 80), "track": (52, 56, 46), "mark": (232, 226, 190)}
ENEMY = {"hull": (150, 78, 58), "turret": (170, 92, 66), "track": (58, 44, 42), "mark": (232, 196, 120)}


def tank_canvas(p):
    u = p.u
    w, h = int(round(46 * u)), int(round(36 * u))
    return Canvas(w, h, w / 2, h - 11 * u)


def draw_tank_body(p, angle, pal):
    u = p.u
    c = tank_canvas(p)
    tg = TankGeom(p, angle)
    L, Wd = 13.5 * u, 6.5 * u
    # shadow
    pts = [tg.screen(f, s, 0) for f, s in [(-L - 1, -Wd - 1), (L + 1, -Wd - 1), (L + 1, Wd + 1), (-L - 1, Wd + 1)]]
    c.poly(pts, (0, 0, 0, 60))
    # tracks (two prisms), hull on top; draw far side first
    sides = sorted([-1, 1], key=lambda sgn: tg.ground(0, sgn * Wd)[1])
    for sgn in sides[:1]:
        prism(c, tg, -L, L, min(sgn * Wd, sgn * (Wd - 3.4 * u)), max(sgn * Wd, sgn * (Wd - 3.4 * u)), 0, 4.2 * u, pal["track"], bevel=2.2 * u)
    prism(c, tg, -L + 1.2 * u, L - 1.2 * u, -Wd + 2.2 * u, Wd - 2.2 * u, 2.4 * u, 6.8 * u, pal["hull"], bevel=1.6 * u)
    for sgn in sides[1:]:
        prism(c, tg, -L, L, min(sgn * Wd, sgn * (Wd - 3.4 * u)), max(sgn * Wd, sgn * (Wd - 3.4 * u)), 0, 4.2 * u, pal["track"], bevel=2.2 * u)
    # hull details: front plate stripe and engine vents
    for f in (-L * 0.55, -L * 0.4):
        c.line([tg.screen(f, -Wd * 0.45, 6.9 * u), tg.screen(f, Wd * 0.45, 6.9 * u)], shade(pal["hull"], 0.7), 0.9 * u)
    c.line([tg.screen(L * 0.62, -Wd * 0.5, 6.9 * u), tg.screen(L * 0.62, Wd * 0.5, 6.9 * u)], pal["mark"], 1.0 * u)
    return c


def draw_tank_turret(p, angle, pal):
    u = p.u
    c = tank_canvas(p)
    tg = TankGeom(p, angle)
    z0 = 6.8 * u
    barrel = lambda: prism(c, tg, 2 * u, 15.5 * u, -1.1 * u, 1.1 * u, z0 + 2.0 * u, z0 + 4.2 * u, shade(pal["turret"], 0.62))
    if tg.g[1] < 0:
        barrel()
    prism(c, tg, -5.2 * u, 5.8 * u, -4.4 * u, 4.4 * u, z0, z0 + 4.8 * u, pal["turret"], bevel=2.2 * u)
    hx, hy = tg.screen(-1.5 * u, 0, z0 + 4.8 * u)
    c.ellipse(hx, hy, 1.9 * u, 1.0 * u, shade(pal["turret"], 0.6), shade(pal["turret"], 0.4), 0.4)
    if tg.g[1] >= 0:
        barrel()
    mx, my = tg.screen(15.5 * u, 0, z0 + 3.1 * u)
    c.ellipse(mx, my, 1.1 * u, 0.8 * u, (30, 28, 26))
    return c


# ---------------------------------------------------------------------------
# explosions
# ---------------------------------------------------------------------------


def ease(t):
    return 1 - (1 - t) ** 2.2


def draw_explosion(p, i):
    u = p.u
    w, h = int(round(72 * u)), int(round(64 * u))
    t = i / (EXPLOSION_FRAMES - 1)
    base = Canvas(w, h, w / 2, h - 18 * u)
    layer = Image.new("RGBA", base.img.size, (0, 0, 0, 0))
    rng = random.Random(4242)
    blobs = [(rng.uniform(0, math.tau), rng.uniform(0.2, 1.0), rng.uniform(0.55, 1.0)) for _ in range(11)]
    grow = ease(min(1.0, t * 1.5 + 0.03))
    fade = 1.0 if t < 0.55 else max(0.0, 1 - (t - 0.55) / 0.45)
    for pass_i, (scale, colors) in enumerate([
        (1.0, [(205, 54, 24), (92, 78, 72)]),
        (0.78, [(244, 124, 28), (118, 104, 96)]),
        (0.5, [(255, 214, 70), (150, 138, 128)]),
    ]):
        smoke = min(1.0, max(0.0, (t - 0.35 - pass_i * 0.08) / 0.4))
        col = mix(colors[0], colors[1], smoke)
        lc = Canvas(w, h, w / 2, h - 18 * u)
        for a, d, r in blobs:
            rr = 14 * u * r * grow * scale
            cx = math.cos(a) * 16 * u * d * grow
            cy = math.sin(a) * 11 * u * d * grow - (6 * u + 10 * u * t) * grow
            lc.ellipse(cx, cy, rr, rr * 0.92, col + (int(255 * fade),), shade(col, 0.6) + (int(255 * fade),), 0.6)
        if pass_i == 2 and t > 0.6:
            continue
        layer.alpha_composite(lc.img)
    base.img.alpha_composite(layer)
    return base


def draw_small_explosion(p, i):
    u = p.u
    w, h = int(round(26 * u)), int(round(26 * u))
    t = i / (SMALL_EXPLOSION_FRAMES - 1)
    c = Canvas(w, h, w / 2, h - 7 * u)
    r = (3 + 9 * math.sin(min(1, t * 1.15) * math.pi * 0.85)) * u
    alpha = int(255 * (1 - t ** 3))
    rng = random.Random(77)
    for k in range(8):
        a = k / 8 * math.tau + rng.uniform(-0.2, 0.2)
        rr = r * (1.0 if k % 2 == 0 else 0.55)
        c.poly([(0, 0), (math.cos(a - 0.35) * rr * 0.5, math.sin(a - 0.35) * rr * 0.5 - 4 * u), (math.cos(a) * rr, math.sin(a) * rr * 0.7 - 4 * u),
                (math.cos(a + 0.35) * rr * 0.5, math.sin(a + 0.35) * rr * 0.5 - 4 * u)], (255, 190, 50, alpha))
    c.ellipse(0, -4 * u, r * 0.45, r * 0.38, (255, 245, 190, alpha))
    return c


# ---------------------------------------------------------------------------
# overlays
# ---------------------------------------------------------------------------

PATH_COLORS = [(84, 190, 240), (30, 140, 210), (20, 104, 170)]


def bezier(a, b, c_, n=20):
    return [((1 - t) ** 2 * a[0] + 2 * (1 - t) * t * b[0] + t * t * c_[0], (1 - t) ** 2 * a[1] + 2 * (1 - t) * t * b[1] + t * t * c_[1])
            for t in [i / n for i in range(n + 1)]]


def draw_path_piece(p, color, key):
    u = p.u
    c = Canvas(p.W, p.H, p.W / 2, p.H / 2)
    width = 6.5 * u
    if len(key) == 1:
        c.line([(0, 0), p.edge_mid(int(key))], color, width)
    else:
        a, b = p.edge_mid(int(key[0])), p.edge_mid(int(key[1]))
        c.line(bezier(a, (0, 0), b), color, width)
    return c


def draw_arrow(p, color, right):
    u = p.u
    w, h = int(round(28 * u)), int(round(18 * u))
    ax = 10 * u if right else w - 10 * u
    c = Canvas(w, h, ax, h / 2)
    sgn = 1 if right else -1
    c.poly([(-7 * u * sgn, -7 * u), (15 * u * sgn, 0), (-7 * u * sgn, 7 * u)], color, shade(color, 0.7), 0.8)
    return c


def draw_aim(p, idx):
    u = p.u
    c = Canvas(p.W, p.H, p.W / 2, p.H / 2)
    mx, my = p.edge_mid(idx)
    d = math.hypot(mx, my)
    ux, uy = mx / d, my / d
    px, py = -uy, ux
    tip = (mx * 0.92, my * 0.92)
    base = (mx * 0.5, my * 0.5)
    c.line([(mx * 0.25, my * 0.25), base], (220, 30, 60), 2.2 * u)
    c.poly([tip, (base[0] + px * 6 * u, base[1] + py * 6 * u), (base[0] - px * 6 * u, base[1] - py * 6 * u)], (220, 30, 60), (130, 10, 30), 0.8)
    return c


# ---------------------------------------------------------------------------
# assemble
# ---------------------------------------------------------------------------


def build_preset(p):
    # ground
    for v in range(3):
        c = draw_hex(p, v)
        p.add(f"hexes.light.{v}", c)
        p.add(f"hexes.dark.{v}", darken(c.done()), c.ax, c.ay)
    # tanks
    for i in range(TANK_FRAMES):
        a = i / TANK_FRAMES * 360
        p.add(f"tanksBodies.{i}", draw_tank_body(p, a, PLAYER))
        p.add(f"tanksTurrets.{i}", draw_tank_turret(p, a, PLAYER))
        p.add(f"enemyTanksBodies.{i}", draw_tank_body(p, a, ENEMY))
        p.add(f"enemyTanksTurrets.{i}", draw_tank_turret(p, a, ENEMY))
    # obstacles
    for v in range(9):
        c = draw_site(p, v)
        p.add(f"sites.light.{v}", c)
        p.add(f"sites.dark.{v}", darken(c.done()), c.ax, c.ay)
    # overlays
    keys = ["01", "02", "04", "05", "12", "13", "15", "23", "24", "34", "35", "45", "0", "1", "2", "3", "4", "5"]
    for pi, color in enumerate(PATH_COLORS):
        for key in keys:
            p.add(f"overlays.paths.{pi}.{key}", draw_path_piece(p, color, key))
        p.add(f"overlays.paths.{pi}.arrowL", draw_arrow(p, color, False))
        p.add(f"overlays.paths.{pi}.arrowR", draw_arrow(p, color, True))
    p.add("overlays.highlights.0", draw_outline_hex(p, (40, 190, 80), 0.04, 2.4))
    p.add("overlays.highlights.1", draw_outline_hex(p, (244, 220, 20), 0.04, 2.4))
    p.add("overlays.highlights.2", draw_outline_hex(p, (230, 40, 70), 0.04, 2.4))
    for i in range(6):
        p.add(f"overlays.aim.{i}", draw_aim(p, i))
    mark0 = draw_outline_hex(p, (220, 36, 70), 0.06, 2.0)
    mark1 = draw_outline_hex(p, (160, 24, 60), 0.06, 2.0, hatch=(170, 30, 66, 255))
    p.add("overlays.markers.light.0", mark0)
    p.add("overlays.markers.dark.0", darken(mark0.done()), mark0.ax, mark0.ay)
    p.add("overlays.markers.light.1", mark1)
    p.add("overlays.markers.dark.1", darken(mark1.done()), mark1.ax, mark1.ay)
    for i in range(EXPLOSION_FRAMES):
        p.add(f"explosion.{i}", draw_explosion(p, i))
    for i in range(SMALL_EXPLOSION_FRAMES):
        p.add(f"smallExplosion.{i}", draw_small_explosion(p, i))


def pack(items):
    """Shelf packing. items: list of (id, w, h). Returns {id: (x, y)}, height."""
    order = sorted(items, key=lambda it: (-it[2], -it[1]))
    x = y = shelf_h = 0
    pos = {}
    for ident, w, h in order:
        if x + w + 1 > ATLAS_WIDTH:
            x, y, shelf_h = 0, y + shelf_h + 1, 0
        pos[ident] = (x, y)
        x += w + 1
        shelf_h = max(shelf_h, h)
    return pos, y + shelf_h


def sprite_expr(rect):
    x, y, w, h, ox, oy = rect
    return f"s({x}, {y}, {w}, {h}, {ox}, {oy})"


def emit_preset(const_name, p, rects):
    def one(key):
        return sprite_expr(rects[(p.name, key)])

    def arr(prefix, n):
        return "[" + ", ".join(one(f"{prefix}.{i}") for i in range(n)) + "]"

    lines = [f"export const {const_name}: SpritePreset = {{", f"  hexSize: new Vector({p.W}, {p.H}),"]
    lines.append(f"  hexes: {{ light: {arr('hexes.light', 3)}, dark: {arr('hexes.dark', 3)} }},")
    lines.append(f"  tanksBodies: {arr('tanksBodies', TANK_FRAMES)},")
    lines.append(f"  tanksTurrets: {arr('tanksTurrets', TANK_FRAMES)},")
    lines.append(f"  enemyTanksBodies: {arr('enemyTanksBodies', TANK_FRAMES)},")
    lines.append(f"  enemyTanksTurrets: {arr('enemyTanksTurrets', TANK_FRAMES)},")
    lines.append(f"  sites: {{ light: {arr('sites.light', 9)}, dark: {arr('sites.dark', 9)} }},")
    lines.append("  overlays: {")
    lines.append("    paths: [")
    keys = ["01", "02", "04", "05", "12", "13", "15", "23", "24", "34", "35", "45", "0", "1", "2", "3", "4", "5", "arrowL", "arrowR"]
    for pi in range(3):
        body = ", ".join(f'"{k}": {one(f"overlays.paths.{pi}.{k}")}' for k in keys)
        lines.append(f"      {{ {body} }},")
    lines.append("    ],")
    lines.append(f"    highlights: {arr('overlays.highlights', 3)},")
    lines.append(f"    aim: {arr('overlays.aim', 6)},")
    lines.append(f"    markers: {{ light: {arr('overlays.markers.light', 2)}, dark: {arr('overlays.markers.dark', 2)} }},")
    lines.append("  },")
    lines.append(f"  explosion: {arr('explosion', EXPLOSION_FRAMES)},")
    lines.append(f"  smallExplosion: {arr('smallExplosion', SMALL_EXPLOSION_FRAMES)},")
    lines.append("};")
    return "\n".join(lines)


TS_HEADER = '''// GENERATED by tools/gen_assets/gen_sprites.py - do not edit by hand.
import { Vector } from "./vector.js";

export type Sprite = {
  start: Vector;
  size: Vector;
  offset: Vector;
};

type DarkenableSprite = { light: Sprite[]; dark: Sprite[] };
type PathsSprites = {
  "01": Sprite;
  "02": Sprite;
  "04": Sprite;
  "05": Sprite;
  "12": Sprite;
  "13": Sprite;
  "15": Sprite;
  "23": Sprite;
  "24": Sprite;
  "34": Sprite;
  "35": Sprite;
  "45": Sprite;
  "0": Sprite;
  "1": Sprite;
  "2": Sprite;
  "3": Sprite;
  "4": Sprite;
  "5": Sprite;
  arrowL: Sprite;
  arrowR: Sprite;
};
export type ValidPathsKeys = keyof PathsSprites;

export type SpritePreset = {
  hexSize: Vector;
  hexes: DarkenableSprite;
  tanksBodies: Sprite[];
  tanksTurrets: Sprite[];
  enemyTanksBodies: Sprite[];
  enemyTanksTurrets: Sprite[];
  sites: DarkenableSprite;
  overlays: {
    paths: PathsSprites[];
    highlights: Sprite[];
    aim: Sprite[];
    markers: DarkenableSprite;
  };
  explosion: Sprite[];
  smallExplosion: Sprite[];
};

const s = (x: number, y: number, w: number, h: number, ox: number, oy: number): Sprite => ({
  start: new Vector(x, y),
  size: new Vector(w, h),
  offset: new Vector(ox, oy),
});

'''


def main():
    presets = [Preset("64", 64, 36, 6), Preset("96", 96, 56, 9)]
    for p in presets:
        build_preset(p)

    items = []
    for p in presets:
        for key, img, ax, ay in p.sprites:
            items.append(((p.name, key), img.width, img.height))
    pos, height = pack(items)
    atlas = Image.new("RGBA", (ATLAS_WIDTH, height), (0, 0, 0, 0))
    rects = {}
    for p in presets:
        for key, img, ax, ay in p.sprites:
            x, y = pos[(p.name, key)]
            atlas.alpha_composite(img, (x, y))
            rects[(p.name, key)] = (x, y, img.width, img.height, -int(round(ax)), -int(round(ay)))
    ATLAS_PATH.parent.mkdir(parents=True, exist_ok=True)
    atlas.save(ATLAS_PATH, optimize=True)

    out = TS_HEADER + emit_preset("SPRITES_64", presets[0], rects) + "\n\n" + emit_preset("SPRITES_96", presets[1], rects) + "\n"
    TS_PATH.write_text(out)
    print(f"atlas {atlas.size}, {len(items)} sprites -> {ATLAS_PATH.relative_to(ROOT)}, {TS_PATH.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
