// A browser that has joined no group, as after a fresh install, has nowhere to
// send to. The popup and the options page say so at the top and lead to the
// phrase, and both go quiet once a phrase is stored.
//
//   popup     Without a phrase the notice shows with its button to the options
//             page, and the send pane, whose button could only fail, goes.
//             With a phrase and an instance online the notice stays hidden.
//   options   The notice is the first card, hidden until the page knows, and
//             renderGroup shows it exactly while no phrase is stored. Its
//             button takes the keyboard to the phrase field.
//
// popup.js runs whole against a stand-in DOM; from options.js only renderGroup
// and the notice's click run, since the rest of the page needs a browser.
//
// Run by CI and by hand: `node extension/check-pair-hint.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');
const problems = [];
const fail = (why) => problems.push(why);
const settle = () => new Promise((r) => setTimeout(r, 30));

class El {
  constructor(id = '') {
    this.id = id;
    this.hidden = false;
    this.text = '';
    this.attrs = {};
    this.listeners = {};
    this.children = [];
    this.dataset = {};
    this.classList = { add() {}, remove() {}, contains: () => false };
    this.style = { setProperty() {} };
    this.focused = false;
    this.scrolled = false;
  }
  get textContent() { return this.text; }
  set textContent(v) { this.text = String(v); this.children = []; }
  set innerHTML(_) { this.children = []; }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return this.attrs[k] ?? null; }
  removeAttribute(k) { delete this.attrs[k]; }
  addEventListener(type, f) { (this.listeners[type] ??= []).push(f); }
  click() { for (const f of this.listeners.click ?? []) f({ preventDefault() {} }); this.onclick?.({}); }
  replaceChildren(...nodes) { this.children = nodes; }
  append(...nodes) { this.children.push(...nodes); }
  appendChild(n) { this.children.push(n); return n; }
  querySelector() { return new El(); }
  querySelectorAll() { return []; }
  cloneNode() { return new El(); }
  focus() { this.focused = true; }
  scrollIntoView() { this.scrolled = true; }
  /** The words a button shows, from its text or the text node beside its glyph. */
  get words() { return this.text || this.children.map((c) => c.text ?? '').join(''); }
}

/** A page whose elements are made on first ask, hidden where the markup says. */
function makeDocument(html) {
  const els = new Map();
  return {
    hidden: false,
    els,
    getElementById(id) {
      if (!els.has(id)) {
        const el = new El(id);
        el.hidden = new RegExp(`id="${id}"[^>]* hidden[ >]`).test(html);
        els.set(id, el);
      }
      return els.get(id);
    },
    createElement: () => new El(),
    createElementNS: () => new El(),
    createTextNode: (text) => ({ text }),
    querySelector: () => new El(),
  };
}

/** Opens the popup over a stored phrase (or none) and a group of `siblings`. */
async function openPopup({ phrase, siblings = [] }) {
  const html = read('src', 'popup.html');
  const doc = makeDocument(html);
  let optionsOpened = 0;
  let groupAsked = 0;
  const ctx = vm.createContext({
    document: doc, console, setTimeout, clearTimeout, setInterval: () => 0, clearInterval() {}, addEventListener() {},
    chrome: {
      runtime: { openOptionsPage: () => optionsOpened++, connect: () => ({ postMessage() {} }), sendMessage: async () => undefined },
      storage: { session: { get: async () => ({}), remove: async () => {} }, onChanged: { addListener() {} } },
      action: {},
      tabs: { query: async () => [], create: async () => {} },
    },
    t: (key) => key, glimGlyph: () => new El(), sendLabelKey: () => 'popup.send',
    applyAppearance: async () => ({ disco: false }), refreshFollowedLook: async () => false,
    setHues() {}, setHue() {}, listenForPageHue() {}, applyDisco() {}, loadLanguage: async () => {}, wireTooltips() {},
    cnlState: async () => ({ wanted: false, on: false }),
    readPhrase: async () => phrase,
    groupInstances: async () => { groupAsked++; return siblings; },
    groupStatus: async () => siblings,
    readDefaultTarget: async () => '', defaultOf: (s) => s[0]?.instanceId ?? '',
    instanceCard: () => new El(), staggerRows() {}, fitSegments() {}, replay() {}, shake() {},
    CLIP_LINK_RULE: /^https?:\/\//,
  });
  vm.runInContext(read('src', 'popup.js'), ctx, { filename: 'popup.js' });
  await settle();
  return { el: (id) => doc.getElementById(id), opened: () => optionsOpened, asked: () => groupAsked };
}

// popup
{
  const popup = await openPopup({ phrase: '' });
  const hint = popup.el('pairHint');
  if (hint.hidden) fail('without a phrase the popup shows no notice that an instance has to be connected first');
  if (popup.el('pairHintTitle').text !== 'common.pairTitle' || popup.el('pairHintReason').text !== 'popup.pairBody') {
    fail(`the popup's notice says "${popup.el('pairHintTitle').text}" and "${popup.el('pairHintReason').text}", want common.pairTitle and popup.pairBody`);
  }
  if (popup.el('pairHintOpen').words !== 'common.pairOpen') fail(`the notice's button says "${popup.el('pairHintOpen').words}", want common.pairOpen`);
  if (!popup.el('paneSend').hidden) fail('without a phrase the popup still shows its send pane, whose button can only fail');
  if (popup.asked()) fail('without a phrase the popup still asks the relay for a group');
  popup.el('pairHintOpen').click();
  if (popup.opened() !== 1) fail(`the notice's button opens the options page ${popup.opened()} time(s), want once`);
  const html = read('src', 'popup.html');
  if (html.indexOf('id="pairHint"') > html.indexOf('id="paneSend"')) fail('the popup\'s notice is not at the top, above the send pane');
  if (!/id="pairHint" class="glim-unavailable"/.test(html)) fail('the popup\'s notice is not the GlimStone notice (glim-unavailable) the Click\'n\'Load one is');

  const joined = await openPopup({ phrase: 'words', siblings: [{ instanceId: 'inst-1', name: 'Tower' }] });
  if (!joined.el('pairHint').hidden) fail('with a phrase and an instance online the popup still says to connect one');
  if (joined.el('paneSend').hidden) fail('with a phrase and an instance online the popup hides its send pane');
}

// options
{
  const html = read('src', 'options.html');
  const options = read('src', 'options.js');
  const sections = [...html.matchAll(/<section [^>]*>/g)].map((m) => m[0]);
  if (!/id="pairNotice"/.test(sections[0] ?? '')) fail(`the options page opens with ${sections[0]}, want the notice to connect an instance first`);
  else if (!/ hidden>$/.test(sections[0])) fail('the options page shows the notice before it knows whether a phrase is stored');
  for (const id of ['pairNoticeHeading', 'pairNoticeBody', 'pairNoticeOpen']) if (!html.includes(`id="${id}"`)) fail(`the options page's notice has no #${id}`);

  const renderGroup = options.match(/\nasync function renderGroup\([\s\S]*?\n\}\n/)?.[0];
  const onOpen = options.match(/\npairNoticeOpenEl\.addEventListener\('click',[\s\S]*?\n\}\);\n/)?.[0];
  if (!renderGroup) fail('could not find renderGroup in options.js');
  if (!onOpen) fail('the notice\'s button on the options page does nothing');
  if (renderGroup && onOpen) {
    const doc = makeDocument(html);
    let phrase = '';
    const el = (id) => doc.getElementById(id);
    const ctx = vm.createContext({
      document: doc, console, setTimeout, clearTimeout,
      pairNoticeEl: el('pairNotice'), pairNoticeOpenEl: el('pairNoticeOpen'), phraseInput: el('phrase'),
      leaveBtn: el('leave'), refreshBtn: el('refreshGroup'), joinBtn: el('join'), list: el('list'), ownKeyEl: el('ownKey'), status: el('status'),
      t: (key) => key, glimGlyph: () => new El(), say() {}, recheckGroup() {}, showOwnKey: async () => {}, instancesGlyph: () => new El(),
      readPhrase: async () => phrase, readDefaultTarget: async () => '', groupView: async () => ({ instances: [], apps: [], away: [] }),
    });
    vm.runInContext(`let groupTurn = 0, recheckTimer = 0, recheckDue = false, refusalNotice = '';\n${renderGroup}\n${onOpen}`, ctx, { filename: 'options.js' });
    await ctx.renderGroup();
    if (el('pairNotice').hidden) fail('without a phrase the options page does not show the notice to connect an instance first');
    el('pairNoticeOpen').click();
    if (!el('phrase').focused || !el('phrase').scrolled) fail('the notice\'s button does not bring the phrase field into view with the keyboard on it');
    phrase = 'words';
    await ctx.renderGroup();
    if (!el('pairNotice').hidden) fail('with a phrase stored the options page still says to connect an instance');
    phrase = '';
    await ctx.renderGroup();
    if (el('pairNotice').hidden) fail('after leaving the group the options page does not say to connect an instance again');
  }
}

if (problems.length) {
  console.error(`${problems.length} problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: without a group the popup and the options page say so at the top and lead to the phrase, and with one they stay quiet');
