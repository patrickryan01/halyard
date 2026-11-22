#!/usr/bin/env python3
"""Generates every logo file for the three name options from the mark paths below.
Usage: make-brand.py OUTDIR    (needs rsvg-convert for the PNGs)"""
import os, subprocess, sys

# the mark is drawn on a 64 x 64 grid as one filled shape group so it reads at 16 px
BRANDS = {
 "halyard": dict(name="Halyard", light="#0f766e", dark="#5fd0c3", icon_bg="#0f766e",
   # a sloop: mast, mainsail, jib and a hull line; the halyard is the mast's line to the sail head
   mark='<rect x="29" y="4" width="4" height="47" rx="2"/><path d="M36 8L36 46L57 46Z"/><path d="M26 15L26 46L10 46Z"/><path d="M5 51H59L52 59H12Z"/>'),
}
out = sys.argv[1]

def svg(w, h, body, vb=None):
    return f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="{vb or f"0 0 {w} {h}"}">{body}</svg>\n'

def lockup(b, fill, text):
    # horizontal lockup: mark then wordmark; the wordmark is live text in a system font stack
    return svg(260, 64, f'<g fill="{fill}">{b["mark"]}</g><text x="76" y="44" font-family="Inter,Segoe UI,Roboto,Helvetica,Arial,sans-serif" font-size="38" font-weight="600" letter-spacing="-0.5" fill="{text}">{b["name"]}</text>', "0 0 260 64")

def png(src, dst, size):
    subprocess.check_call(["rsvg-convert", "-w", str(size), "-h", str(size), src, "-o", dst])

for key, b in BRANDS.items():
    d = os.path.join(out, key); os.makedirs(d, exist_ok=True)
    w = lambda n, s: open(os.path.join(d, n), "w").write(s)
    w("mark-light.svg", svg(64, 64, f'<g fill="{b["light"]}">{b["mark"]}</g>'))
    w("mark-dark.svg", svg(64, 64, f'<g fill="{b["dark"]}">{b["mark"]}</g>'))
    w("mark-mono.svg", svg(64, 64, f'<g fill="#000000">{b["mark"]}</g>'))
    w("logo-light.svg", lockup(b, b["light"], "#1c2230"))
    w("logo-dark.svg", lockup(b, b["dark"], "#e8ebf2"))
    w("logo-mono.svg", lockup(b, "#000000", "#000000"))
    # app icon: rounded square in the brand color, the mark in white at 58%
    icon = svg(1024, 1024, f'<rect width="1024" height="1024" rx="230" fill="{b["icon_bg"]}"/><g transform="translate(212 212) scale(9.5)" fill="#ffffff">{b["mark"]}</g>')
    w("app-icon.svg", icon)
    for n in (1024, 512, 256):
        png(os.path.join(d, "app-icon.svg"), os.path.join(d, f"app-icon-{n}.png"), n)
    # favicon: the mark alone, larger inside its square so it survives 16 px
    fav = svg(64, 64, f'<rect width="64" height="64" rx="14" fill="{b["icon_bg"]}"/><g transform="translate(6 6) scale(.81)" fill="#ffffff">{b["mark"]}</g>')
    w("favicon.svg", fav)
    png(os.path.join(d, "favicon.svg"), os.path.join(d, "favicon-16.png"), 16)
    png(os.path.join(d, "favicon.svg"), os.path.join(d, "favicon-32.png"), 32)
print("ok")
