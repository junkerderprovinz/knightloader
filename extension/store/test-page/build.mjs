/**
 * Builds index.html beside this file: the test page for store reviewers, and
 * the page the reviewer video was recorded on. The store screenshots show an
 * earlier version of it.
 *
 * It carries the two things a reviewer has to try:
 *
 *   - a plain download link, to right-click and send, and
 *   - a real Click'n'Load button (addcrypted2: AES-128-CBC, the key doubling
 *     as the IV, zero padding), aimed at http://127.0.0.1:9666 like every
 *     Click'n'Load button on the web, with two other files in its batch.
 *
 * The batch leaves out the right-click link. Sent twice, it would be folded in
 * as a duplicate and the instance would report "1 link(s) were not added",
 * which reads like a failure in a review.
 *
 * Every file is a Blender open movie, CC BY, checked online when this was
 * written. The key is random per build, so the committed index.html changes on
 * every run; that is expected.
 *
 * Run: node extension/store/test-page/build.mjs
 */
import crypto from "node:crypto";
import { writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const TRAILER = ["Sintel (trailer)", "480p, MP4", "4 MB", "https://download.blender.org/durian/trailer/sintel_trailer-480p.mp4"];
const FILMS = [
  ["Big Buck Bunny", "480p, MOV in ZIP", "238 MB", "https://download.blender.org/peach/bigbuckbunny_movies/big_buck_bunny_480p_h264.mov.zip"],
  ["Tears of Steel", "720p, MOV", "355 MB", "https://download.blender.org/demo/movies/ToS/tears_of_steel_720p.mov"],
];

const key = crypto.randomBytes(16);
const plain = Buffer.from(FILMS.map((f) => f[3]).join("\r\n"), "utf8");
const padded = Buffer.concat([plain, Buffer.alloc((16 - (plain.length % 16)) % 16)]);
const cipher = crypto.createCipheriv("aes-128-cbc", key, key);
cipher.setAutoPadding(false);
const crypted = Buffer.concat([cipher.update(padded), cipher.final()]).toString("base64");
const jk = `function f(){ return '${key.toString("hex")}';}`;

const row = ([name, format, size, url], id) =>
  `<li><a${id ? ` id="${id}"` : ""} href="${url}">${name}</a><span>${format}</span><span>${size}</span></li>`;

const html = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<title>Open Movies</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  :root { color-scheme: dark; }
  body { margin: 0; font: 16px/1.5 system-ui, "Segoe UI", Roboto, sans-serif; background: #1b1d22; color: #e6e6e6; }
  main { max-width: 760px; margin: 48px auto; padding: 0 24px; }
  h1 { font-size: 30px; margin: 0 0 6px; }
  h2 { font-size: 15px; margin: 28px 0 8px; color: #a8adb7; font-weight: 600; text-transform: uppercase; letter-spacing: .06em; }
  p.lead { margin: 0; color: #a8adb7; }
  ul { list-style: none; margin: 0; padding: 0; border-top: 1px solid #30333b; }
  li { display: grid; grid-template-columns: 1fr 150px 80px; gap: 12px; padding: 13px 4px; border-bottom: 1px solid #30333b; }
  li span { color: #a8adb7; text-align: right; }
  a { color: #8ab4f8; text-decoration: none; } a:hover { text-decoration: underline; }
  form { margin-top: 16px; }
  button { font: 600 15px system-ui, sans-serif; padding: 11px 20px; border-radius: 8px; border: 0; background: #3b82f6; color: #fff; cursor: pointer; }
  small { display: block; margin-top: 36px; color: #7d828c; }
</style>
<script src="http://127.0.0.1:9666/jdcheck.js"></script>
</head><body><main>
  <h1>Open Movies</h1>
  <p class="lead">A test page for the KnightLoader browser extension: a link to right-click, and a Click'n'Load button.</p>
  <h2>Trailer</h2>
  <ul>
    ${row(TRAILER, "trailer-link")}
  </ul>
  <h2>Full films</h2>
  <ul>
    ${FILMS.map((f) => row(f)).join("\n    ")}
  </ul>
  <form action="http://127.0.0.1:9666/flash/addcrypted2" method="POST" target="cnl">
    <input type="hidden" name="passwords" value="">
    <input type="hidden" name="jk" value="${jk}">
    <input type="hidden" name="crypted" value="${crypted}">
    <button id="cnl-button" type="submit">Click'n'Load: both films</button>
  </form>
  <iframe name="cnl" hidden></iframe>
  <small>Films &copy; Blender Foundation, CC BY.</small>
</main></body></html>
`;
writeFileSync(join(dirname(fileURLToPath(import.meta.url)), "index.html"), html);
console.log(`wrote index.html (${html.length} bytes)`);
