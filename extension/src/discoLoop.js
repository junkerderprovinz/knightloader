// The loop disco walks: the palette in order and back to its first colour,
// drawn in OKLCH so a glide turns round the colour wheel instead of cutting
// across it through grey. The loop is measured, so the walk covers equal
// distances in equal times. The palette's colours sit unevenly on the wheel,
// so a fixed time per colour would speed the glide up and slow it down at
// every colour it passes.
//
// Ported from glimstone's reference/discoLoop.ts with the types stripped. The
// pages load their scripts into one global scope, where phrase.js already
// has a toHex, so the private helpers carry a loop prefix.

// Below this chroma a colour reads as grey, and its hue is noise.
const LOOP_GREY = 0.002;

const LOOP_RAD = Math.PI / 180;

const loopToLinear = (v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
const loopToGamma = (v) => (v <= 0.0031308 ? 12.92 * v : 1.055 * v ** (1 / 2.4) - 0.055);

/** Converts a six-digit hex colour to { l, c, h }, lightness and chroma in
 *  Oklab units and hue in degrees, with Björn Ottosson's Oklab matrices
 *  (https://bottosson.github.io/posts/oklab/). */
function oklch(hex) {
  const n = parseInt(hex.slice(1), 16);
  const [lr, lg, lb] = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => loopToLinear(v / 255));
  const l = Math.cbrt(0.4122214708 * lr + 0.5363325363 * lg + 0.0514459929 * lb);
  const m = Math.cbrt(0.2119034982 * lr + 0.6806995451 * lg + 0.1073969566 * lb);
  const s = Math.cbrt(0.0883024619 * lr + 0.2817188376 * lg + 0.6299787005 * lb);
  const L = 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s;
  const A = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s;
  const B = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s;
  return { l: L, c: Math.hypot(A, B), h: Math.atan2(B, A) / LOOP_RAD };
}

// A colour between two palette entries can fall outside sRGB, so each channel
// is clamped; the loop's own stops always come back as they went in.
function loopHex({ l: L, c, h }) {
  const A = c * Math.cos(h * LOOP_RAD);
  const B = c * Math.sin(h * LOOP_RAD);
  const l = (L + 0.3963377774 * A + 0.2158037573 * B) ** 3;
  const m = (L - 0.1055613458 * A - 0.0638541728 * B) ** 3;
  const s = (L - 0.0894841775 * A - 1.291485548 * B) ** 3;
  const rgb = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ];
  const byte = (v) => Math.round(loopToGamma(Math.min(1, Math.max(0, v))) * 255);
  return `#${rgb.map((v) => byte(v).toString(16).padStart(2, '0')).join('')}`.toUpperCase();
}

// The hue a glide from a to b starts at, and how far it turns the short way
// round. A grey has no hue of its own, so a glide to or from one keeps the
// other colour's instead of sweeping a stray tint in with the chroma.
function loopTurn(a, b) {
  const from = a.c < LOOP_GREY ? b.h : a.h;
  const to = b.c < LOOP_GREY ? from : b.h;
  return [from, ((to - from + 540) % 360) - 180];
}

/** Measures each glide as its length through Oklab: the change in lightness
 *  and chroma, and the arc the hue turns through at the chroma it travels at.
 *  at[k] is the distance from the first colour to colour k, and
 *  at[palette.length] the length of the whole loop. */
function buildLoop(palette) {
  const stops = palette.map(oklch);
  const at = [0];
  stops.forEach((a, k) => {
    const b = stops[(k + 1) % stops.length];
    const arc = ((a.c + b.c) / 2) * loopTurn(a, b)[1] * LOOP_RAD;
    at.push(at[k] + Math.hypot(b.l - a.l, b.c - a.c, arc));
  });
  return { palette, stops, at };
}

/** The colour at distance `s` along the loop, wrapping round in either
 *  direction. A palette of one colour repeated has no length and stands still. */
function colourAt(loop, s) {
  const { stops, at } = loop;
  const length = at[stops.length];
  if (length === 0) return loop.palette[0];
  const d = ((s % length) + length) % length;
  let k = 0;
  while (at[k + 1] <= d) k++;
  const a = stops[k];
  const b = stops[(k + 1) % stops.length];
  const f = (d - at[k]) / (at[k + 1] - at[k]);
  const [from, degrees] = loopTurn(a, b);
  return loopHex({ l: a.l + (b.l - a.l) * f, c: a.c + (b.c - a.c) * f, h: from + degrees * f });
}
