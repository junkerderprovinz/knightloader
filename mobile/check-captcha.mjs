// The captcha screen answers a challenge the way the web UI does.
//
// src/api/captcha.ts holds the rules the screen follows and imports nothing at
// run time, so node strips its types and these checks call the real functions:
// the order the cards come in, the countdown, the shape of a click answer JD
// accepts, the widget page's address and the vendors it runs, which of the
// page's messages reach the app, who is to blame when the widget does not load,
// and what the banner says about a captcha that came or went.
//
// The bridge script is run against a stand-in window, because a WebView is
// where it goes wrong without a sound: the page posts to its own origin, and a
// listener that forwards too much hands the vendor's frames to the app while
// one that forwards too little leaves a solved captcha waiting until it
// expires.
//
// Run by hand and by CI, from mobile/: `node check-captcha.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const cap = await import(pathToFileURL(join(here, 'src', 'api', 'captcha.ts')).href);

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const at = (iso) => ({ expiresAt: iso });
const ch = (id, expiresAt, extra = {}) => ({ id, source: 'jd', host: 'example.net', kind: 'image', expiresAt, ...extra });

// Go writes an unknown deadline as the zero time, year 1.
const ZERO = '0001-01-01T00:00:00Z';
expect('a zero deadline is unknown', cap.expiryMs(at(ZERO)), null);
expect('a missing deadline is unknown', cap.expiryMs(at('')), null);
expect('a real deadline is read', cap.expiryMs(at('2026-01-01T00:00:10Z')), Date.UTC(2026, 0, 1, 0, 0, 10));

const now = Date.UTC(2026, 0, 1);
expect('seconds left', cap.secondsLeft(at('2026-01-01T00:01:23Z'), now), 83);
expect('a passed deadline stops at zero', cap.secondsLeft(at('2025-12-31T23:59:00Z'), now), 0);
expect('no deadline, no countdown', cap.secondsLeft(at(ZERO), now), null);
expect('countdown', cap.fmtCountdown(83), '1:23');
expect('countdown under a minute', cap.fmtCountdown(5), '0:05');

expect(
  'nearest deadline first, unknown last, ties by id',
  cap
    .byDeadline([
      ch('d', ZERO),
      ch('c', '2026-01-01T00:02:00Z'),
      ch('b', '2026-01-01T00:01:00Z'),
      ch('a', '2026-01-01T00:02:00Z'),
      ch('e', ZERO),
    ])
    .map((x) => x.id),
  ['b', 'a', 'c', 'd', 'e'],
);

const list = [ch('1', ZERO), ch('2', ZERO)];
expect('the first look at an instance announces nothing', cap.arrivals(null, list), []);
expect('a captcha seen before is not news', cap.arrivals(list, list), []);
expect('a new captcha is news', cap.arrivals([list[0]], list).map((x) => x.id), ['2']);

// The watch's banner, one look after another. The watch starts from null after
// a switch to another instance and after Android has closed the app, and keeps
// the last look while the app is in the background, so a look after coming back
// compares with what was waiting when it went.
const none = new Set();
const early = ch('early', '2026-01-01T00:00:30Z');
const late = ch('late', '2026-01-01T00:05:00Z');
const open = ch('open', ZERO);
const notice = (n) => n && { kind: n.kind, id: n.challenge.id };
expect('a first look says nothing', cap.noticeFor(null, [early], none, now, false), null);
expect('nothing changed, nothing said', cap.noticeFor([early], [early], none, now, false), null);
expect('back from the background, a new captcha', notice(cap.noticeFor([early], [early, late], none, now, false)), {
  kind: 'arrived',
  id: 'late',
});
expect(
  'gone after its deadline timed out',
  notice(cap.noticeFor([early, late], [late], none, now + 60_000, false)),
  { kind: 'timedOut', id: 'early' },
);
expect('gone before its deadline was answered elsewhere', notice(cap.noticeFor([early, late], [early], none, now, false)), {
  kind: 'resolved',
  id: 'late',
});
expect('gone without a deadline was answered elsewhere', notice(cap.noticeFor([open], [], none, now, false)), {
  kind: 'resolved',
  id: 'open',
});
expect('one this phone answered leaves quietly', cap.noticeFor([early, late], [late], new Set(['early']), now, false), null);
expect('the list on screen shows a new one itself', cap.noticeFor([early], [early, late], none, now, true), null);
expect('but not a card that vanished from it', notice(cap.noticeFor([early, late], [early], none, now, true)), {
  kind: 'resolved',
  id: 'late',
});
expect('a new captcha before one that left', notice(cap.noticeFor([early], [late], none, now, false)), {
  kind: 'arrived',
  id: 'late',
});

// JD's ClickedPoint for one point, MultiClickedPoint for several, in the
// picture's own pixels whatever size the phone drew it at.
expect('one point', JSON.parse(cap.clickAnswer([{ x: 0.5, y: 0.25 }], 300, 100)), { x: 150, y: 25 });
expect(
  'several points',
  JSON.parse(cap.clickAnswer([{ x: 0.1, y: 0.2 }, { x: 0.333, y: 0.999 }], 300, 100)),
  { x: [30, 100], y: [20, 100] },
);

// The widget page reads every field from the query string (see
// parseCaptchaWidgetRequest in internal/api/routes_captcha_widget.go).
const widget = ch('17/a b', ZERO, {
  kind: 'widget',
  host: 'files.example',
  prompt: 'Prove it & go',
  payload: { vendor: 'recaptcha', siteKey: 'k+ey', siteUrl: 'https://files.example/x', contextUrl: '', type: 'INVISIBLE', enterprise: true, v3Action: '{"action":"login"}', secureToken: 's' },
});
const path = cap.widgetPath(widget, 'de');
const [route, query] = path.split('?');
expect('the id is one path segment', route, '/captcha/17%2Fa%20b/widget');
expect('the query carries what the page renders from', Object.fromEntries(new URLSearchParams(query)), {
  vendor: 'recaptcha',
  siteKey: 'k+ey',
  type: 'INVISIBLE',
  enterprise: '1',
  v3Action: '{"action":"login"}',
  secureToken: 's',
  lang: 'de',
  host: 'files.example',
  prompt: 'Prove it & go',
});
expect(
  'an empty field stays out of the query',
  new URLSearchParams(cap.widgetPath(ch('1', ZERO, { kind: 'widget', host: '', payload: { vendor: 'hcaptcha', siteKey: 'k' } }), '').split('?')[1]).toString(),
  'vendor=hcaptcha&siteKey=k',
);

// The vendors captchaWidgetVendor renders, and a missing one it decides itself.
const vendorOf = (vendor) => ch('1', ZERO, { kind: 'widget', payload: { vendor, siteKey: 'k' } });
expect(
  'the page runs reCAPTCHA, hCaptcha and whatever it works out itself',
  ['recaptcha', 'hcaptcha', 'HCaptcha', ''].map((v) => cap.widgetRuns(vendorOf(v))),
  [true, true, true, true],
);
expect('a Turnstile would only be refused by the page', cap.widgetRuns(vendorOf('turnstile')), false);

const msg = (m) => JSON.stringify({ source: 'knightloader-captcha-widget', id: 'c1', ...m });
expect('a solved token is taken', cap.widgetMessage(msg({ kind: 'solved', detail: 'tok' }), 'c1'), { kind: 'solved', detail: 'tok' });
expect('ready has no detail', cap.widgetMessage(msg({ kind: 'ready', detail: null }), 'c1'), { kind: 'ready', detail: null });
expect('another challenge is not this one', cap.widgetMessage(msg({ kind: 'solved', detail: 'tok' }), 'c2'), null);
expect('another sender is ignored', cap.widgetMessage(JSON.stringify({ source: 'recaptcha', id: 'c1', kind: 'solved', detail: 'x' }), 'c1'), null);
expect('an unknown kind is ignored', cap.widgetMessage(msg({ kind: 'surprise' }), 'c1'), null);
expect('not JSON is ignored', cap.widgetMessage('{', 'c1'), null);
expect('null is ignored', cap.widgetMessage('null', 'c1'), null);

expect('a script that never came is the network', cap.widgetFailure(null, 'script'), { by: 'network' });
expect('a page that never came is the network', cap.widgetFailure(null, null), { by: 'network' });
expect('a vendor code is the vendor refusing', cap.widgetFailure(null, 'invalid-sitekey'), { by: 'vendor', code: 'invalid-sitekey' });
// routes_captcha_widget.go answers 400 for a challenge JD sent without a site
// key, which no amount of network would fix.
expect('an HTTP error is the instance refusing', cap.widgetFailure(400, null), { by: 'instance', status: 400 });

// The bridge, run twice as the WebView runs it, in a window whose parent is
// itself.
const origin = 'http://192.168.1.10:8749';
const listeners = [];
const forwarded = [];
const win = {
  location: { origin },
  addEventListener: (type, fn) => type === 'message' && listeners.push(fn),
  ReactNativeWebView: { postMessage: (s) => forwarded.push(JSON.parse(s)) },
};
new Function('window', cap.WIDGET_BRIDGE)(win);
new Function('window', cap.WIDGET_BRIDGE)(win);
expect('the bridge listens once however often it runs', listeners.length, 1);
const deliver = (e) => listeners.forEach((fn) => fn(e));
const pageMessage = { source: 'knightloader-captcha-widget', id: 'c1', kind: 'solved', detail: 'tok' };
deliver({ origin, data: pageMessage });
deliver({ origin: 'https://www.google.com', data: pageMessage });
deliver({ origin, data: { source: 'something else' } });
deliver({ origin, data: 'a string' });
expect('only the page’s own messages reach the app', forwarded, [pageMessage]);

if (problems.length) {
  console.error(`check-captcha: ${problems.length} problem(s).`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: captchas come nearest deadline first, a click answer is in JD’s shape and the picture’s pixels, the widget page gets every field and opens only for a vendor it runs, only its own messages reach the app, and the banner says what came and went');
