"""Generate the README's download buttons from one template.

From a template, because six hand-drawn buttons are six chances to type one
number differently, and the whole point of a row of them is that they look like
one control repeated. Two rows: the desktop builds, then the container, the
Android app and the browser extension.

THE GEOMETRY. Height and corner radius are the Buy Me a Coffee button's own
(245.3 tall, rx 38.2), so a download button and the coffee button rendered at
the same width stand the same height. The WIDTH is 720 rather than that button's
841.9, measured on screen rather than guessed: at 841.9 a third of the face sat
empty to the right of the longest word and the button read as lopsided.

THE COLOUR is the platform's own, and the button has no outline (jdp: "die
butotns sollen keine rahmenliniehaben und farbig sein"). A filled shape in a
colour somebody already associates with the platform does the work an outline
was doing, and does it faster: the eye finds "the blue one" before it reads the
word. macOS has no brand colour of its own, so it takes Apple's own space grey,
which is the one value that stays visible against GitHub's light theme and its
dark one - a black button disappears into the dark theme, and this row has no
outline to save it.

THE LOGOS are the platforms' own marks, from Font Awesome Free (CC BY 4.0 for
the icons; see scripts/brand-paths/). Each mark is a trademark of its owner and
is used here the one way a trademark may be used without permission: to name the
thing it refers to. Each button links to a download FOR that platform, the marks
are unmodified, and nothing here claims endorsement by or affiliation with
Microsoft, Apple, the Linux Foundation, Docker or Google. The extension's puzzle
piece is Font Awesome's too, and no one's trademark.

Run from anywhere:  python scripts/gen_download_buttons.py
Writes .github/assets/download-buttons/*.svg, which are committed, and the
button rows in README.md between their markers, all of them showing one
sprite, .github/assets/download-buttons/buttons.svg.
"""

import http.client
import io
import math
import os
import re
import time
import urllib.error
import urllib.request
from html import escape

HERE = os.path.dirname(os.path.abspath(__file__))
# Relative to this file, so the generator works from any working directory and
# in any repo it is copied into.
OUT = os.path.join(HERE, "..", ".github", "assets", "download-buttons")
BRANDS = os.path.join(HERE, "brand-paths")

W, H, R = 720.0, 245.3, 38.2

# The mark is drawn into a square this tall, centred vertically, inset from the
# left. Its own viewBox decides the horizontal centring, because the three marks
# are not equally wide: Apple's is 384 units against Windows' and Tux's 448.
GLYPH = 112.0
GX, GY = 78.0, (H - GLYPH) / 2

# A system stack, because an SVG loaded through <img> cannot fetch a webfont:
# whatever is named here has to already be on the reader's machine. The layout
# leaves room to the right of the longest word for a face wider than the one
# this was measured with.
FONT = "-apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif"

TEMPLATE = """<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w} {h}" width="{w}" height="{h}" role="img" aria-label="{alt}">
  <title>{alt}</title>
  <defs>
    <clipPath id="edge">
      <rect x="0" y="0" width="{w}" height="{h}" rx="{r}" ry="{r}"/>
    </clipPath>
    <linearGradient id="sheen" x1="0" y1="0" x2="1" y2="0">
      <stop offset="0"    stop-color="#fff" stop-opacity="0"/>
      <stop offset="0.45" stop-color="#fff" stop-opacity="0.28"/>
      <stop offset="0.55" stop-color="#fff" stop-opacity="0.28"/>
      <stop offset="1"    stop-color="#fff" stop-opacity="0"/>
    </linearGradient>
  </defs>
  <style>
    @keyframes pass {{
      0%       {{ transform: translateX({band_start}px); }}
      {pass_pct}%   {{ transform: translateX({band_end}px); }}
      100%     {{ transform: translateX({band_end}px); }}
    }}
    /* linear, not eased: an eased pass varies the speed inside each button, so
       the hand-off at the seam arrives early or late and the row stops reading
       as one band.

       The fill mode is not decoration, it is the second half of the delay. An
       animation that has not started yet leaves its element wherever the
       document put it, which for this band is x=0 - INSIDE the button, against
       its left edge. Without it the stagger that makes the row read as one band
       also parks a motionless band on every button but the first, for as long
       as that button's delay, every time the page loads. */
    .band {{ animation: pass {cycle}s linear {delay}s infinite backwards; }}
    @media (prefers-reduced-motion: reduce) {{
      .band {{ animation: none; opacity: 0; }}
    }}
  </style>
  <rect width="{w}" height="{h}" rx="{r}" ry="{r}" fill="{bg}"/>
  <g transform="translate({gx} {gy}) scale({scale})" fill="{ink}">
    <path d="{path}"/>
  </g>
  <text x="238" y="110" font-family="{font}" font-size="82" font-weight="700" fill="{ink}">{head}</text>
  <text x="240" y="180" font-family="{font}" font-size="50" font-weight="400" fill="{ink}" fill-opacity="0.72">{sub_text}</text>
  <g clip-path="url(#edge)">
    <g class="band">
      <!-- Taller than the canvas and started off its left edge, so the tilt
           never exposes a corner. skewX rather than rotate: the band stays
           axis-aligned for the translate, so the motion is one transform. -->
      <rect x="0" y="-60" width="{band_w}" height="{band_h}"
            fill="url(#sheen)" transform="skewX(-16)"/>
    </g>
  </g>
</svg>
"""

# THE SHEEN, and it is DEFINED ON SCREEN rather than on this canvas.
#
# A tilted white band, clipped to the button, crossing once per loop. It is the
# donation row's own band, and the point is that it is the SAME band there and
# here: a row of house buttons carries one band that appears to travel the whole
# row, and three rows on one page have to look like one effect rather than three.
#
# That is why these numbers are in SCREEN pixels (see the GitHub style guide,
# "Der Schein"). Described in canvas units they come out different in
# every row, because the canvases differ (720 here, 841.9 for the donation row)
# and so do the widths the READMEs render them at.
#
# THE GAP IS MEASURED, not assumed: the row is `<img width="195">` with a
# newline, two spaces and a `&nbsp;` between the images, which HTML collapses to
# space-nbsp-space, 13.16px at GitHub's 16px body text. A `&nbsp;` glued to the
# closing `</a>` instead measures 8.77px, so the separator is part of the rule.
BAND_PX = 33.0     # the band's width on screen
SPEED = 250.0      # screen pixels per second
GAP_PX = 13.16     # measured, see above
RENDER_PX = 195.0  # the width the README asks for

SCALE = W / RENDER_PX              # canvas units per screen pixel
SHEEN_W = BAND_PX * SCALE
# The band is skewed, so its horizontal extent is wider than the rect: skewX
# shifts every point by tan(16 degrees) times its own y, and the rect is taller
# than the canvas on both sides. Clearing the edge by the rect's width alone
# would leave the tilted corner showing.
SHEEN_H = H + 120.0
CLEAR = SHEEN_W + math.tan(math.radians(16)) * SHEEN_H
SHEEN_FROM = -CLEAR
SHEEN_TO = W + CLEAR
# How long the band needs to cross one button, and how long to travel from one
# button's left edge to the next one's. Both come from one speed, so the band
# leaves button n at the moment it enters button n+1.
PASS = (SHEEN_TO - SHEEN_FROM) / SCALE / SPEED
STEP = (RENDER_PX + GAP_PX) / SPEED

# slug, brand file, background, ink, heading, second line, accessible name, and
# where the button leads. The last one lives here with the rest of the button
# because this file writes the README rows too, see write_readme(). One list per
# row, top to bottom.
RELEASE = "https://github.com/junkerderprovinz/knightloader/releases/latest/download/"
# The app and the extension are released on tags of their own, so the
# product's /releases/latest/ never carries them. Their workflows copy the
# newest build into a standing release each, under a name without a version.
NEWEST = "https://github.com/junkerderprovinz/knightloader/releases/download/%s/latest/"
DESKTOP = [
    ("windows", "windows", "#0078d4", "#ffffff", "Windows", "amd64", "Download for Windows",
     RELEASE + "knightloader-windows-amd64.zip"),
    # Apple's own space grey. Black is the usual answer and the wrong one here:
    # with no outline it vanishes against GitHub's dark theme.
    ("macos", "apple", "#6e6e73", "#ffffff", "macOS", "Universal", "Download for macOS",
     RELEASE + "knightloader-macos-universal.zip"),
    # The yellow Tux is drawn in, dark ink on it for the same reason road signs
    # do that.
    ("linux", "linux", "#fcc624", "#1b1b1b", "Linux", "amd64", "Download for Linux",
     RELEASE + "knightloader-linux-amd64.zip"),
]
SERVER_PHONE_BROWSER = [
    # The container has no file to download, so this one leads to the image's
    # own page, which carries the pull command and every tag.
    ("docker", "docker", "#1d63ed", "#ffffff", "Docker", "Container", "Run it with Docker",
     "https://github.com/junkerderprovinz/knightloader/pkgs/container/knightloader"),
    ("android", "android", "#3ddc84", "#1b1b1b", "Android", "App", "Download the Android app",
     NEWEST % "mobile" + "knightloader-android.apk"),
    # No browser's own colour, because the extension is not one browser's: an
    # orange that none of the other five buttons wears, dark ink on it.
    ("extension", "puzzle-piece", "#ff7139", "#1b1b1b", "Browser", "Extension", "Download the browser extension",
     NEWEST % "extension" + "knightloader-extension.zip"),
]
ROWS = [DESKTOP, SERVER_PHONE_BROWSER]
BUTTONS = [button for buttons in ROWS for button in buttons]

# THE README ROWS are written here as well, between markers, so a button added
# to ROWS reaches the page by running this file and nothing else: both download
# rows (one marked block), and every donation row (the one above them and the
# one in Support).
#
# ALL OF THEM SHOW ONE FILE, buttons.svg, each button through its own
# #svgView fragment inside its own link. The shine is a CSS animation, and a
# browser runs it on a clock that starts when that <img> gets its file. Separate
# files arrive at separate moments, so the band jumped between buttons; and
# Firefox reuses an image it already has when GitHub swaps the page without a
# reload, starting a new clock on it. One file arrives once for every button on
# the page and all of its <img> are inserted together, so all clocks start
# together: the donation row, then the download rows below it, in order. That is
# also why the donation buttons are copied into this file rather than linked
# from the profile repository's give.svg: two files would be two arrivals again.
# Measured on github.com in Firefox, loaded fresh and after in-page navigation.
# The layout of a sprite is explained in
# junkerderprovinz/junkerderprovinz, donate/buttons/sprite.mjs.
#
# The donation buttons are read from the profile repository when this runs, so
# after they change there, run this again. The sprite is read from main, so a
# branch's README preview shows main's buttons.
REPO = "knightloader"
SPRITE = os.path.join(OUT, "buttons.svg")
SPRITE_URL = "https://raw.githubusercontent.com/junkerderprovinz/%s/main/.github/assets/download-buttons/buttons.svg" % REPO
GIVE_URL = "https://raw.githubusercontent.com/junkerderprovinz/junkerderprovinz/main/donate/buttons/button-%s-live.svg"
GIVE_RENDER_PX = 160.0
# slug, where it leads, accessible name. The same three as in every README.
GIVE = [
    ("buy-me-a-coffee", "https://buymeacoffee.com/junkerderprovinz", "Buy me a coffee"),
    ("paypal", "https://www.paypal.com/donate/?hosted_button_id=76FVV52TKXTUS", "PayPal"),
    ("crypto", "https://junkerderprovinz.github.io/junkerderprovinz/", "Donate with crypto"),
]
README = os.path.join(HERE, "..", "README.md")
ROW_OPEN = "<!-- download-buttons: written by scripts/gen_download_buttons.py -->"
ROW_CLOSE = "<!-- /download-buttons -->"
GIVE_OPEN = "<!-- give-buttons: written by scripts/gen_download_buttons.py -->"
GIVE_CLOSE = "<!-- /give-buttons -->"

def brand(name):
    """One mark: its path, and the scale and offset that centre it in GLYPH."""
    path = io.open(os.path.join(BRANDS, name + ".txt"), encoding="utf-8").read().strip()
    box = io.open(os.path.join(BRANDS, name + ".box.txt"), encoding="utf-8").read().strip()
    _, _, width, height = (float(n) for n in box.split())
    # Scaled by HEIGHT so the three marks share an optical size, then nudged
    # right by half the width they do not use. Apple's mark is narrower than the
    # other two, and without this it would sit left of them in the row.
    scale = GLYPH / height
    return path, scale, (GLYPH - width * scale) / 2


def num(x):
    """A coordinate as the fragment carries it: no trailing zeros, no float noise."""
    return ("%.3f" % x).rstrip("0").rstrip(".")


# The names a button document defines. Every one of them is prefixed per button
# in the sprite, and the sprite is refused if any is left without a prefix, so a
# button template that starts using another name fails here instead of quietly
# handing one button's delay or clip to all of them.
UNPREFIXED = re.compile(r'id="(?!b\d+-)|url\(#(?!b\d+-)|href="#(?!b\d+-)|class="(?!b\d+-)|@keyframes (?!b\d+-)|animation: (?!b\d+-|none)')


def sprite(parts):
    """Several button documents as one SVG, laid out left to right.

    Every part keeps its own document as a nested <svg> at its own x, with its
    ids, class and keyframes prefixed, because the CSS inside one SVG document
    is shared: unprefixed, the last button's delay would win for all of them.
    Returns the sprite and each part's x.
    """
    x, xs, body = 0.0, [], []
    for i, (svg, width, _height) in enumerate(parts):
        pre = "b%d-" % i
        s = re.sub(r"<\?xml[^>]*>\s*", "", svg, count=1)
        s = re.sub(r"<!--.*?-->\s*", "", s, flags=re.S)
        s = re.sub(r'id="(edge|sheen)"', lambda m: 'id="%s%s"' % (pre, m.group(1)), s)
        s = re.sub(r"url\(#(edge|sheen)\)", lambda m: "url(#%s%s)" % (pre, m.group(1)), s)
        s = s.replace('class="band"', 'class="%sband"' % pre)
        s = re.sub(r"\.band\b", ".%sband" % pre, s)
        s = re.sub(r"@keyframes pass\b", "@keyframes %spass" % pre, s)
        s = s.replace("animation: pass ", "animation: %spass " % pre)
        s = re.sub(r"<svg\b", '<svg x="%s" y="0"' % num(x), s, count=1)
        left = UNPREFIXED.search(s)
        if left:
            raise SystemExit("button %d still has an unprefixed name near %r" % (i, s[left.start():left.start() + 40]))
        xs.append(x)
        body.append(s.strip())
        x = round(x + width, 3)
    height = max(p[2] for p in parts)
    head = ('<?xml version="1.0" encoding="UTF-8"?>\n'
            '<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" '
            'viewBox="0 0 %s %s">\n' % (num(x), num(height)))
    return head + "\n".join(body) + "\n</svg>\n", xs


def give_buttons():
    """The donation buttons' own files and sizes, as the profile repository publishes them.

    The size is read from each file's viewBox rather than assumed, because the
    other repository decides it. The query string gets past raw.githubusercontent's
    five-minute cache, so a run right after a change there sees the change.
    """
    out = []
    for slug, _href, _alt in GIVE:
        url = GIVE_URL % slug + "?t=%d" % time.time()
        for attempt in range(3):
            try:
                with urllib.request.urlopen(url, timeout=30) as r:
                    svg = r.read().decode("utf-8")
                break
            except urllib.error.HTTPError as err:
                if err.code < 500 or attempt == 2:
                    raise SystemExit("%s: HTTP %d" % (url, err.code))
            except (OSError, http.client.HTTPException):
                # A dropped connection, also halfway through the body. Nothing
                # has been written yet, so trying again is safe.
                if attempt == 2:
                    raise
            time.sleep(2)
        box = re.search(r'viewBox="0 0 ([\d.]+) ([\d.]+)"', svg)
        if not box:
            raise SystemExit("%s has no viewBox" % url)
        out.append((svg, float(box.group(1)), float(box.group(2))))
    return out


def blocks(text, opener, closer):
    """Every opener ... closer span in the text, in order.

    An opener whose closer is missing is refused rather than paired with the
    next block's closer, which would replace everything in between.
    """
    spans, at = [], 0
    while True:
        start = text.find(opener, at)
        if start < 0:
            return spans
        end = text.find(closer, start)
        following = text.find(opener, start + len(opener))
        if end < 0 or (0 <= following < end):
            raise SystemExit("README.md has %s without its %s" % (opener, closer))
        spans.append((start, end))
        at = end + len(closer)


def read_readme():
    """README.md, checked before anything is written.

    Read after the donation buttons are fetched, so an edit saved meanwhile is
    not overwritten with the text from before it. Checked before any file is
    written, so a README without its markers stops the run while the buttons are
    still untouched. REPO is checked against the links for the same reason:
    copied into another repository and left unchanged, it would quietly show this
    repository's buttons there.
    """
    text = io.open(README, encoding="utf-8", newline="").read()
    if len(blocks(text, ROW_OPEN, ROW_CLOSE)) != 1:
        raise SystemExit("README.md needs exactly one %s ... %s" % (ROW_OPEN, ROW_CLOSE))
    if not blocks(text, GIVE_OPEN, GIVE_CLOSE):
        raise SystemExit("README.md has no %s ... %s" % (GIVE_OPEN, GIVE_CLOSE))
    for slug, *_, href in BUTTONS:
        if "/%s/" % REPO not in href:
            raise SystemExit("REPO is %r, but %s leads to %s" % (REPO, slug, href))
    return text


def row(items, nl):
    """One centred row: a link per button, the separator on its own line, two
    spaces in, because that is the gap GAP_PX was measured on."""
    lines = ['<p align="center">']
    for index, (href, alt, x, width, height, render) in enumerate(items):
        if index:
            lines.append("  &nbsp;")
        lines.append('  <a href="%s"><img src="%s#svgView(viewBox(%s,0,%s,%s))" alt="%s" width="%s" height="%s"></a>'
                     % (escape(href), SPRITE_URL, num(x), num(width), num(height), escape(alt), num(render), num(render * height / width)))
    lines.append("</p>")
    return nl.join(lines) + nl


def write_readme(text, xs, gives):
    """Replace every marked block, each taking the line ending of its own marker.

    width AND height are both set, because the image's own proportions are the
    whole sprite's, not the button's.
    """
    downloads, at = [], 0
    for buttons in ROWS:
        downloads.append([(href, alt, xs[at + i], W, H, RENDER_PX) for i, (_s, *_, alt, href) in enumerate(buttons)])
        at += len(buttons)
    donations = [[(href, alt, xs[len(BUTTONS) + i], gives[i][1], gives[i][2], GIVE_RENDER_PX)
                  for i, (_s, href, alt) in enumerate(GIVE)]]
    for opener, closer, rows in ((ROW_OPEN, ROW_CLOSE, downloads), (GIVE_OPEN, GIVE_CLOSE, donations)):
        for start, end in reversed(blocks(text, opener, closer)):
            nl = "\r\n" if text[start:].split("\n", 1)[0].endswith("\r") else "\n"
            text = text[:start] + opener + nl + "".join(row(items, nl) for items in rows) + text[end:]
    io.open(README, "w", encoding="utf-8", newline="").write(text)
    print("README.md  download rows of %s, donation rows of %d" % ("+".join(str(len(r)) for r in ROWS), len(GIVE)))


ANIMATION = re.compile(r"animation: pass (\d+(?:\.\d+)?)s linear (\d+(?:\.\d+)?)s infinite backwards;")
PASS_STOP = re.compile(r"^(\s*)(\d+(?:\.\d+)?)%(\s+\{ transform: translateX\()", re.M)


def retime(svg, delay, cycle):
    """A donation button moved to another place in the loop, and to a longer loop.

    The profile repository bakes 3.8 s and a seven second loop into these files
    (see below for why that does not fit here). The band's own time across the
    button is read from the file rather than recomputed, because its geometry is
    that repository's business: the percentage at which it reaches the far edge,
    times the loop it was written for. Anything that does not look exactly like
    the one animation and the one keyframe expected stops the run, rather than
    leaving a button on its old clock in a sprite where every other button moved.
    """
    found = ANIMATION.findall(svg)
    stops = [m for m in PASS_STOP.finditer(svg) if float(m.group(2)) not in (0.0, 100.0)]
    if len(found) != 1 or len(stops) != 1:
        raise SystemExit("a donation button no longer has the one animation and keyframe this retimes")
    crossing = float(stops[0].group(2)) / 100.0 * float(found[0][0])
    stop = stops[0]
    svg = svg[:stop.start()] + "%s%.2f%%%s" % (stop.group(1), crossing / cycle * 100.0, stop.group(3)) + svg[stop.end():]
    return ANIMATION.sub("animation: pass %gs linear %.3fs infinite backwards;" % (cycle, delay), svg)


# THE SCHEDULE. One band works its way down the page: the whole donation row,
# then the desktop row, then the row below it. Each row starts where a button
# after the last one of the row above would have started, and each button one
# STEP after its neighbour, at that row's own rendered width plus the measured
# gap. Computed rather than written into the tables above: a hand-kept column of
# seconds is a column somebody reorders the row without touching, and then the
# band hands off into nothing.
#
# THE DONATION ROW IS FIRST because in this README it stands ABOVE the download
# rows, and so it is retimed here. Everywhere else those three files sit at 3.8 s
# into a seven second loop, after at most one download row; three rows need
# 7.07 s of travel before any rest, which a seven second loop cannot hold. So on
# this page the loop is as long as the three rows plus the rest the house
# schedule leaves on every page with a download row: 7 s minus the end of the
# donation row there (3.8 s plus three of its steps), 1.12 s. The speed, the
# band and the order stay the house's own; only the rest before the band returns
# to the top is measured out afresh.
#
# EVERY DELAY STAYS INSIDE THE LOOP. All clocks in the sprite start together, so
# a delay past the loop's length would not be the same phase as its remainder in
# the first loop, and that button would shine before everything above it.
GIVE_STEP = (GIVE_RENDER_PX + GAP_PX) / SPEED
HOUSE_CYCLE, HOUSE_GIVE_START = 7.0, 3.8
REST = HOUSE_CYCLE - (HOUSE_GIVE_START + len(GIVE) * GIVE_STEP)
DELAYS, at = [], len(GIVE) * GIVE_STEP
GIVE_DELAYS = [GIVE_STEP * i for i in range(len(GIVE))]
for buttons in ROWS:
    DELAYS += [at + STEP * i for i in range(len(buttons))]
    at += len(buttons) * STEP
CYCLE = round(max(HOUSE_CYCLE, at + REST), 3)
PASS_PCT = PASS / CYCLE * 100.0

gives = give_buttons()
readme = read_readme()
svgs = []
for index, (slug, mark, bg, ink, head, sub_text, alt, _href) in enumerate(BUTTONS):
    path, scale, inset = brand(mark)
    svgs.append((slug, TEMPLATE.format(
        w=W, h=H, r=R, bg=bg, ink=ink, gx=round(GX + inset, 2), gy=round(GY, 2),
        scale=round(scale, 5), path=path, font=FONT,
        head=head, sub_text=sub_text, alt=escape(alt),
        delay="%.3f" % DELAYS[index], cycle="%g" % CYCLE,
        pass_pct="%.2f" % PASS_PCT, band_w="%.1f" % SHEEN_W,
        band_h="%g" % SHEEN_H, band_start="%.1f" % SHEEN_FROM,
        band_end="%.1f" % SHEEN_TO,
    )))
gives = [(retime(svg, GIVE_DELAYS[i], CYCLE), width, height) for i, (svg, width, height) in enumerate(gives)]
# Built, and checked, before anything is written.
whole, xs = sprite([(svg, W, H) for _slug, svg in svgs] + gives)
os.makedirs(OUT, exist_ok=True)
for slug, svg in svgs:
    out = os.path.join(OUT, "button-" + slug + ".svg")
    io.open(out, "w", encoding="utf-8", newline="\n").write(svg)
    print("wrote", os.path.normpath(out), len(svg), "bytes")
io.open(SPRITE, "w", encoding="utf-8", newline="\n").write(whole)
print("wrote", os.path.normpath(SPRITE), len(whole), "bytes,", len(xs), "buttons")
write_readme(readme, xs, gives)
