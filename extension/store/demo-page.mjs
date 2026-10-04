/**
 * The film page shown inside the store screenshots: a made-up site with a real
 * Click'n'Load form, so the button is the kind the extension catches on the web
 * (addcrypted2: AES-128-CBC, the key doubling as the IV, zero padding).
 *
 * The poster and the still are the Blender Foundation's, CC BY 3.0, credited
 * on the page.
 */
import crypto from "node:crypto";

const PEACH = "https://download.blender.org/peach";
export const FILMS = {
  trailer: ["Trailer", "480p · MOV", "11 MB", `${PEACH}/trailer/trailer_480p.mov`],
  hd: ["1080p", "MOV in ZIP", "692 MB", `${PEACH}/bigbuckbunny_movies/big_buck_bunny_1080p_h264.mov.zip`],
  sd: ["720p", "MOV in ZIP", "397 MB", `${PEACH}/bigbuckbunny_movies/big_buck_bunny_720p_h264.mov.zip`],
};

function clickNLoad(urls) {
  const key = crypto.randomBytes(16);
  const plain = Buffer.from(urls.join("\r\n"), "utf8");
  const padded = Buffer.concat([plain, Buffer.alloc((16 - (plain.length % 16)) % 16)]);
  const cipher = crypto.createCipheriv("aes-128-cbc", key, key);
  cipher.setAutoPadding(false);
  const crypted = Buffer.concat([cipher.update(padded), cipher.final()]).toString("base64");
  return { jk: `function f(){ return '${key.toString("hex")}';}`, crypted };
}

const ICON_DL = '<svg viewBox="0 0 16 16" width="16" height="16" fill="currentColor"><path d="M7.25 1.5h1.5v7.19l2.47-2.47 1.06 1.06L8 11.56 3.72 7.28l1.06-1.06 2.47 2.47zM2.5 12.5h11V14h-11z"/></svg>';
const ICON_PLAY = '<svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor"><path d="M4 2.5v11l9-5.5z"/></svg>';

/** hover is the id of the element to draw in its hover state, "trailer" or "cnl"; data-point marks where the pointer goes. */
export function demoPage({ poster, still, hover }) {
  const { jk, crypted } = clickNLoad([FILMS.hd[3], FILMS.sd[3]]);
  const row = (id, [label, format, size, url]) => `
    <li${hover === id ? ' class="hover"' : ""}>
      <a id="${id}" href="${url}">${id === "trailer" ? ICON_PLAY : ICON_DL}<span data-point>${label}</span></a>
      <span class="fmt">${format}</span><span class="size">${size}</span>
    </li>`;
  return `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<title>Big Buck Bunny · Open Movies</title>
<style>
  :root { color-scheme: dark; --accent: #5cb8e8; }
  * { box-sizing: border-box; }
  body { margin: 0; min-height: 100vh; font: 14px/1.5 "Segoe UI", system-ui, sans-serif; background: #1d1e21; color: #e8e8e8; }
  header { height: 52px; display: flex; align-items: center; gap: 26px; padding: 0 28px; background: #16171a; border-bottom: 1px solid #2a2b2f; }
  .brand { display: flex; align-items: center; gap: 8px; font-weight: 700; font-size: 16px; }
  .brand i { width: 22px; height: 22px; border-radius: 6px; background: #0b8fd0; display: grid; place-items: center; color: #fff; }
  nav { display: flex; gap: 18px; color: #8c8f95; } nav b { color: #e8e8e8; font-weight: 600; }
  .banner { height: 150px; background: url(${still}) 30% 30% / cover; position: relative; }
  .banner::after { content: ""; position: absolute; inset: 0; background: linear-gradient(rgba(29,30,33,.1) 40%, #1d1e21); }
  main { padding: 0 28px 24px; }
  .top { display: flex; gap: 20px; margin-top: -76px; position: relative; }
  .poster { width: 128px; flex: none; }
  .poster img { width: 128px; height: 181px; object-fit: cover; border-radius: 8px; display: block; border: 3px solid #2c2d31; box-shadow: 0 12px 28px rgba(0,0,0,.5); }
  .poster small { display: block; margin-top: 6px; font-size: 10px; color: #7d8086; line-height: 1.3; }
  .info { padding-top: 84px; }
  h1 { margin: 0 0 2px; font-size: 27px; line-height: 1.15; font-weight: 700; }
  .meta { color: #9a9da3; font-size: 13px; }
  .tags { display: flex; gap: 6px; margin: 8px 0 8px; }
  .tags span { font-size: 11px; padding: 2px 9px; border-radius: 999px; background: rgba(92,184,232,.16); color: var(--accent); }
  .about { color: #c3c6cb; max-width: 240px; margin: 0; font-size: 13px; }
  h2 { margin: 16px 0 6px; font-size: 12px; text-transform: uppercase; letter-spacing: .08em; color: #8c8f95; font-weight: 600; }
  ul { list-style: none; margin: 0; padding: 0; max-width: 370px; border-top: 1px solid #2e3035; }
  li { display: grid; grid-template-columns: 1fr 80px 54px; gap: 10px; align-items: center; padding: 7px 6px; border-bottom: 1px solid #2e3035; border-radius: 6px; }
  li a { display: flex; align-items: center; gap: 8px; color: var(--accent); text-decoration: none; font-weight: 600; }
  li.hover { background: rgba(92,184,232,.12); } li.hover a span { text-decoration: underline; }
  .fmt, .size { color: #8c8f95; font-size: 12px; } .size { text-align: right; }
  form { margin-top: 14px; }
  .cnl { height: 40px; padding: 0 20px; border-radius: 6px; cursor: pointer;
    font: 600 14px "Segoe UI", system-ui, sans-serif; color: #4ade80; background: #15803d; border: 1px solid #16a34a; }
  .cnl.hover { background: #14532d; color: #bbf7d0; }
</style>
</head><body>
<header>
  <div class="brand"><i>${ICON_PLAY}</i>Open Movies</div>
  <nav><b>Films</b><span>Shorts</span><span>About</span></nav>
</header>
<div class="banner"></div>
<main>
  <div class="top">
    <div class="poster"><img src="${poster}" alt=""><small>Images &copy; Blender Foundation, CC BY 3.0</small></div>
    <div class="info">
      <h1>Big Buck Bunny</h1>
      <div class="meta">2008 · 10 min · Blender Foundation</div>
      <div class="tags"><span>Comedy</span><span>Animation</span><span>CC BY</span></div>
      <p class="about">A big, gentle rabbit gets his own back on three bullying rodents.</p>
    </div>
  </div>
  <h2>Downloads</h2>
  <ul>${row("trailer", FILMS.trailer)}${row("hd", FILMS.hd)}${row("sd", FILMS.sd)}
  </ul>
  <form action="http://127.0.0.1:9666/flash/addcrypted2" method="POST" target="cnl">
    <input type="hidden" name="passwords" value="">
    <input type="hidden" name="jk" value="${jk}">
    <input type="hidden" name="crypted" value="${crypted}">
    <button id="cnl" data-point class="cnl${hover === "cnl" ? " hover" : ""}" type="submit">Click 'n Load</button>
  </form>
  <iframe name="cnl" hidden></iframe>
</main>
</body></html>`;
}
