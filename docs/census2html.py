# -*- coding: utf-8 -*-
"""Turn the JD feature census markdown into one filterable page jdp can click through.

The census is 928 rows. As markdown in a private repo it is unreadable in
practice, which is exactly the complaint. This produces a single self-contained
HTML file: filter by area/weight/status/effort, search, and click a verdict per
row. Verdicts live in localStorage and can be exported back as the markdown
column, so going through it once is not thrown away.
"""
import io, json, os, re, html

SRC = r"d:\nextcloud\it\github\knightloader\docs\jd-feature-census.md"
OUT = r"d:\nextcloud\it\github\knightloader-features.html"

src = io.open(SRC, encoding='utf-8').read()

area = None
rows = []
for line in src.splitlines():
    m = re.match(r'^## (\d+)\. (.+)$', line)
    if m:
        area = m.group(2).strip()
        continue
    if not line.startswith('|') or area is None:
        continue
    # Split on a pipe that is NOT escaped: two rows describe proxy URL schemes
    # and a dropdown using "\|" inside a cell, and a naive split silently loses
    # exactly those rows from a list whose whole job is to be complete.
    body = line.strip()
    body = body[1:] if body.startswith('|') else body
    body = body[:-1] if body.endswith('|') and not body.endswith('\\|') else body
    cells = [c.strip().replace('\\|', '|') for c in re.split(r'(?<!\\)\|', body)]
    if len(cells) < 7:
        continue
    if set(cells[0]) <= set('-: ') or cells[0].lower() in ('feature', 'status'):
        continue
    name = re.sub(r'\*\*', '', cells[0]).strip()
    weight, status, effort = cells[3], cells[4], cells[5]
    if status not in ('have', 'partial', 'missing'):
        continue
    rows.append({
        'a': area,
        'n': name,
        'd': cells[1],
        'w': cells[2],          # where it lives in JD
        'g': weight,            # common / niche
        's': status,
        'e': effort,
        'b': cells[6],          # blocker / what is missing
    })

areas = sorted({r['a'] for r in rows})
print('rows: %d  areas: %d' % (len(rows), len(areas)))

DOC = u"""<meta charset="utf-8">
<title>KnightLoader vs JDownloader \u2014 %d Funktionen</title>
<style>
  :root {
    --bg:#131110; --surface:#1c1917; --surface2:#242020; --line:#2e2926;
    --text:#ece7e1; --sub:#b9b0a6; --muted:#8b8177; --accent:#e9a83c;
    --ok:#5ca271; --warn:#e2703a; --fail:#d9534f; --radius:10px;
  }
  @media (prefers-color-scheme: light) {
    :root { --bg:#f7f4f0; --surface:#fff; --surface2:#f0ece7; --line:#e0d9d1;
            --text:#231f1c; --sub:#5c544c; --muted:#877d73; }
  }
  :root[data-theme="dark"] {
    --bg:#131110; --surface:#1c1917; --surface2:#242020; --line:#2e2926;
    --text:#ece7e1; --sub:#b9b0a6; --muted:#8b8177;
  }
  :root[data-theme="light"] {
    --bg:#f7f4f0; --surface:#fff; --surface2:#f0ece7; --line:#e0d9d1;
    --text:#231f1c; --sub:#5c544c; --muted:#877d73;
  }
  * { box-sizing:border-box; }
  body { margin:0; background:var(--bg); color:var(--text);
         font:14px/1.5 "Segoe UI Variable Text","Segoe UI",system-ui,sans-serif; }
  header { padding:22px 20px 12px; max-width:1400px; margin:0 auto; }
  h1 { margin:0 0 4px; font-size:20px; letter-spacing:-.01em; }
  .lede { color:var(--sub); font-size:13px; margin:0 0 14px; max-width:70ch; }
  .bars { display:flex; flex-wrap:wrap; gap:8px; margin-bottom:14px; }
  .stat { background:var(--surface); border-radius:var(--radius); padding:8px 12px; }
  .stat b { font-variant-numeric:tabular-nums; font-size:17px; }
  .stat span { color:var(--muted); font-size:11px; display:block; }
  .tools { display:flex; flex-wrap:wrap; gap:8px; align-items:center;
           position:sticky; top:0; z-index:5; background:var(--bg);
           padding:10px 20px; max-width:1400px; margin:0 auto; border-bottom:1px solid var(--line); }
  select, input[type=search] { background:var(--surface2); color:var(--text); border:0;
           border-radius:var(--radius); padding:7px 10px; font:inherit; font-size:13px; outline:0; }
  input[type=search] { min-width:200px; flex:1; }
  button.seg { background:transparent; color:var(--muted); border:0; border-radius:var(--radius);
           padding:6px 11px; font:inherit; font-size:12px; font-weight:600; cursor:pointer; }
  button.seg.on { background:var(--accent); color:#17130e; }
  .group { background:var(--surface2); border-radius:var(--radius); padding:3px; display:flex; gap:2px; }
  main { max-width:1400px; margin:0 auto; padding:14px 20px 60px; }
  .wrap { overflow-x:auto; }
  table { border-collapse:collapse; width:100%%; min-width:1180px; }
  /* The column header is deliberately NOT sticky. The table sits inside an
     overflow-x wrapper, which is its own scroll container, so a sticky header
     sticks to THAT box rather than to the viewport and lands 95px down on top
     of the first row. The toolbar above is the thing worth keeping in view
     anyway, and it is sticky against the page where that works. */
  th { text-align:left; font-size:10px; letter-spacing:.12em; text-transform:uppercase;
       color:var(--muted); font-weight:600; padding:8px 10px; background:var(--bg); }
  td:last-child, th:last-child { white-space:nowrap; }
  td { padding:9px 10px; border-top:1px solid var(--line); vertical-align:top; font-size:13px; }
  tr:hover td { background:var(--surface); }
  .nm { font-weight:600; }
  .ds { color:var(--sub); font-size:12px; max-width:52ch; }
  .wh { color:var(--muted); font-size:11px; max-width:30ch; }
  .bk { color:var(--muted); font-size:11px; max-width:38ch; font-style:italic; }
  .pill { display:inline-block; border-radius:999px; padding:2px 8px; font-size:11px; font-weight:600; white-space:nowrap; }
  .have { background:color-mix(in srgb,var(--ok) 20%%,transparent); color:var(--ok); }
  .partial { background:color-mix(in srgb,var(--warn) 20%%,transparent); color:var(--warn); }
  .missing { background:color-mix(in srgb,var(--fail) 18%%,transparent); color:var(--fail); }
  .common { color:var(--text); font-weight:600; }
  .niche { color:var(--muted); }
  .vd { display:flex; gap:2px; }
  .vd button { border:0; border-radius:6px; padding:3px 7px; font:inherit; font-size:11px;
       font-weight:600; cursor:pointer; background:var(--surface2); color:var(--muted); }
  .vd button.on { background:var(--accent); color:#17130e; }
  .none { padding:40px; text-align:center; color:var(--muted); }
  footer { max-width:1400px; margin:0 auto; padding:0 20px 40px; color:var(--muted); font-size:12px; }
  code { background:var(--surface2); padding:1px 5px; border-radius:5px; font-size:12px; }
</style>

<header>
  <h1>KnightLoader gegen JDownloader</h1>
  <p class="lede">%d Funktionen aus JDownloader 2, jede gegen unseren Quellcode gepr\u00fcft.
     Filtere auf <b>verbreitet + fehlt</b>, um die Liste zu sehen, die wirklich z\u00e4hlt \u2014
     und klicke pro Zeile <b>bauen</b>, <b>sp\u00e4ter</b> oder <b>nein</b>.
     Die Entscheidungen bleiben im Browser gespeichert; <b>Exportieren</b> gibt sie
     als Markdown-Spalte zur\u00fcck.</p>
  <div class="bars" id="bars"></div>
</header>

<div class="tools">
  <div class="group">
    <button class="seg" data-f="s" data-v="">alle</button>
    <button class="seg" data-f="s" data-v="missing">fehlt</button>
    <button class="seg" data-f="s" data-v="partial">teilweise</button>
    <button class="seg" data-f="s" data-v="have">haben wir</button>
  </div>
  <div class="group">
    <button class="seg" data-f="g" data-v="">alle</button>
    <button class="seg on" data-f="g" data-v="common">verbreitet</button>
    <button class="seg" data-f="g" data-v="niche">Nische</button>
  </div>
  <div class="group">
    <button class="seg on" data-f="v" data-v="">alle</button>
    <button class="seg" data-f="v" data-v="none">offen</button>
    <button class="seg" data-f="v" data-v="build">bauen</button>
    <button class="seg" data-f="v" data-v="later">sp\u00e4ter</button>
    <button class="seg" data-f="v" data-v="no">nein</button>
  </div>
  <select id="area"><option value="">alle Bereiche</option>%s</select>
  <select id="effort"><option value="">jeder Aufwand</option><option>S</option><option>M</option><option>L</option><option>XL</option></select>
  <input type="search" id="q" placeholder="suchen\u2026">
  <button class="seg" id="export">exportieren</button>
</div>

<main><div class="wrap"><table>
  <thead><tr><th>Funktion</th><th>Was sie tut</th><th>Wo in JD</th><th>Gewicht</th><th>Status</th><th>Aufwand</th><th>Was fehlt</th><th>Entscheidung</th></tr></thead>
  <tbody id="rows"></tbody>
</table></div><div class="none" id="none" hidden>Nichts passt zu diesem Filter.</div></main>

<footer>Quelle: <code>knightloader/docs/jd-feature-census.md</code>. Aufwand ist f\u00fcr <i>diese</i>
Architektur gesch\u00e4tzt: S unter einem Tag, M ein bis zwei, L mehrere, XL ein Projekt.</footer>

<script>
const DATA = %s;
const KEY = 'kl-verdicts';
let verdicts = {};
try { verdicts = JSON.parse(localStorage.getItem(KEY) || '{}'); } catch (e) {}
const filt = { s:'', g:'common', v:'', a:'', e:'', q:'' };

function id(r) { return r.a + '|' + r.n; }

function stats() {
  const c = { have:0, partial:0, missing:0, common:0, decided:0 };
  for (const r of DATA) {
    c[r.s]++;
    if (r.g === 'common' && r.s !== 'have') c.common++;
    if (verdicts[id(r)]) c.decided++;
  }
  document.getElementById('bars').innerHTML =
    '<div class="stat"><b>' + DATA.length + '</b><span>Funktionen</span></div>' +
    '<div class="stat"><b style="color:var(--ok)">' + c.have + '</b><span>haben wir</span></div>' +
    '<div class="stat"><b style="color:var(--warn)">' + c.partial + '</b><span>teilweise</span></div>' +
    '<div class="stat"><b style="color:var(--fail)">' + c.missing + '</b><span>fehlt</span></div>' +
    '<div class="stat"><b>' + c.common + '</b><span>verbreitet + offen</span></div>' +
    '<div class="stat"><b style="color:var(--accent)">' + c.decided + '</b><span>entschieden</span></div>';
}

function esc(s) { const d = document.createElement('div'); d.textContent = s || ''; return d.innerHTML; }

function render() {
  const q = filt.q.toLowerCase();
  const list = DATA.filter(r =>
    (!filt.s || r.s === filt.s) && (!filt.g || r.g === filt.g) &&
    (!filt.a || r.a === filt.a) && (!filt.e || r.e === filt.e) &&
    (!filt.v || (filt.v === 'none' ? !verdicts[id(r)] : verdicts[id(r)] === filt.v)) &&
    (!q || (r.n + ' ' + r.d + ' ' + r.w).toLowerCase().includes(q)));
  document.getElementById('none').hidden = list.length > 0;
  document.getElementById('rows').innerHTML = list.map(r => {
    const v = verdicts[id(r)] || '';
    const btn = (k, l) => '<button data-id="' + esc(id(r)) + '" data-v="' + k + '"' +
      (v === k ? ' class="on"' : '') + '>' + l + '</button>';
    return '<tr><td class="nm">' + esc(r.n) + '</td><td class="ds">' + esc(r.d) +
      '</td><td class="wh">' + esc(r.w) + '</td><td class="' + r.g + '">' + esc(r.g) +
      '</td><td><span class="pill ' + r.s + '">' + esc(r.s) + '</span></td><td>' + esc(r.e) +
      '</td><td class="bk">' + esc(r.b) + '</td><td><div class="vd">' +
      btn('build','bauen') + btn('later','sp\\u00e4ter') + btn('no','nein') + '</div></td></tr>';
  }).join('');
  stats();
}

document.addEventListener('click', e => {
  const seg = e.target.closest('button.seg');
  if (seg && seg.dataset.f) {
    filt[seg.dataset.f] = seg.dataset.v;
    seg.parentElement.querySelectorAll('.seg').forEach(b => b.classList.toggle('on', b === seg));
    render();
    return;
  }
  const vd = e.target.closest('.vd button');
  if (vd) {
    const k = vd.dataset.id;
    verdicts[k] = verdicts[k] === vd.dataset.v ? '' : vd.dataset.v;
    if (!verdicts[k]) delete verdicts[k];
    try { localStorage.setItem(KEY, JSON.stringify(verdicts)); } catch (e2) {}
    render();
  }
});
document.getElementById('area').onchange = e => { filt.a = e.target.value; render(); };
document.getElementById('effort').onchange = e => { filt.e = e.target.value; render(); };
document.getElementById('q').oninput = e => { filt.q = e.target.value; render(); };
document.getElementById('export').onclick = () => {
  const lines = DATA.filter(r => verdicts[id(r)])
    .map(r => '| ' + r.a + ' | ' + r.n + ' | ' + verdicts[id(r)] + ' |');
  const md = '| Bereich | Funktion | Entscheidung |\\n|---|---|---|\\n' + lines.join('\\n');
  navigator.clipboard.writeText(md).then(
    () => alert(lines.length + ' Entscheidungen als Markdown in die Zwischenablage kopiert.'),
    () => prompt('Kopieren:', md));
};
render();
</script>
"""

opts = ''.join('<option>%s</option>' % html.escape(a) for a in areas)
io.open(OUT, 'w', encoding='utf-8').write(
    DOC % (len(rows), len(rows), opts, json.dumps(rows, ensure_ascii=False)))
print('wrote %s' % OUT)
