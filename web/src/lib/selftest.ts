import type { SelfTestRequestView, SelfTestResult, SelfTestStatus } from './api';

/**
 * The half of the self-test that only a browser can answer.
 *
 * WHY THIS EXISTS AT ALL. Four of the eleven checks on the diagnostics page are
 * about the reverse proxy in front of this instance, and not one of them can be
 * answered by the server. A probe run there dials its own listener on loopback,
 * bypasses the proxy entirely and proves nothing: it would report "WebSocket
 * fine" on an instance whose users are staring at a list that stopped moving an
 * hour ago. The server can only ever see what the proxy handed it. The whole
 * question is whether that matches what the browser sent, and this module is
 * the one place that holds both halves.
 *
 * NOTHING HERE RENDERS A SENTENCE. Every function returns the same
 * SelfTestResult shape the server's own checks return - a status and a stable
 * code - and the page looks that code's sentence up in whichever of the
 * forty-two locales is loaded. That is not symmetry for its own sake: it means
 * a row drawn from a browser check and a row drawn from a server check are the
 * same component with the same treatment, and there is no second vocabulary to
 * keep in step.
 */

/** How long the probe socket is given to open. */
const WS_PROBE_TIMEOUT_MS = 3000;

/**
 * The two marks the clock comparison turns on, in milliseconds.
 *
 * Five seconds is comfortably wider than any round trip this correction cannot
 * already account for, and narrower than anything that matters: a difference
 * under it changes nothing anywhere. Two minutes is where the consequences
 * start - TLS certificates begin to look not-yet-valid, and the signed requests
 * some debrid providers use start being rejected as replays - so past it the
 * row is a real fault rather than a curiosity.
 */
const SKEW_WARN_MS = 5_000;
const SKEW_FAIL_MS = 120_000;

/**
 * probeWebSocket opens a socket of its own to /api/ws and reports whether it
 * came up.
 *
 * ITS OWN SOCKET AND NEVER THE APP'S. connectWS (api.ts) reconnects every
 * 1500ms for ever and surfaces no error anywhere, which is exactly the failure
 * this probe exists to make visible - so reading its state would be reading the
 * thing under test through the thing under test. A separate socket, closed the
 * moment it opens, answers the question without touching the live one.
 *
 * IT NEVER REJECTS. A refused upgrade, a proxy that answers 200 with HTML, a
 * timeout: all of them are the same answer to the operator ("the live
 * connection did not open") and the difference between them is not something
 * the WebSocket API hands to a page anyway. The security error a mixed-content
 * page throws synchronously is caught for the same reason.
 */
export function probeWebSocket(timeoutMs: number = WS_PROBE_TIMEOUT_MS): Promise<boolean> {
  return new Promise((resolve) => {
    let done = false;
    const finish = (ok: boolean, sock?: WebSocket) => {
      if (done) return;
      done = true;
      // Closed even on success: the point was to find out whether the upgrade
      // is allowed through, and a probe that left a second connection open on
      // every visit to this page would be a slow leak on the hub.
      try {
        sock?.close();
      } catch {
        /* already closing */
      }
      resolve(ok);
    };
    let sock: WebSocket;
    try {
      const proto = location.protocol === 'https:' ? 'wss' : 'ws';
      sock = new WebSocket(`${proto}://${location.host}/api/ws`);
    } catch {
      resolve(false);
      return;
    }
    const timer = window.setTimeout(() => finish(false, sock), timeoutMs);
    sock.onopen = () => {
      window.clearTimeout(timer);
      finish(true, sock);
    };
    sock.onerror = () => {
      window.clearTimeout(timer);
      finish(false, sock);
    };
    sock.onclose = () => {
      window.clearTimeout(timer);
      finish(false);
    };
  });
}

/**
 * clockSkewMs is the server's clock minus the browser's, in milliseconds,
 * with half the round trip discounted.
 *
 * THE MIDPOINT IS THE WHOLE POINT. The server stamps `now` somewhere between
 * the request leaving and the answer arriving, so comparing it against either
 * end reads half of a slow link as drift - on a phone over mobile data that is
 * comfortably a second, which is a fifth of the warning threshold for nothing.
 * The midpoint of the two local timestamps is the best estimate of the instant
 * the server answered, and it is the same correction NTP's own offset formula
 * makes for the same reason.
 *
 * A positive number means the server is ahead of the browser.
 */
export function clockSkewMs(view: SelfTestRequestView, sentAt: number, gotAt: number): number {
  const server = new Date(view.now).getTime();
  if (Number.isNaN(server)) return 0;
  return server - (sentAt + gotAt) / 2;
}

/** The verdict on a clock difference. See SKEW_WARN_MS and SKEW_FAIL_MS. */
export function skewStatus(ms: number): SelfTestStatus {
  const d = Math.abs(ms);
  if (d < SKEW_WARN_MS) return 'pass';
  if (d < SKEW_FAIL_MS) return 'warn';
  return 'fail';
}

/**
 * fmtSkew prints a clock difference the way the rest of this app prints a
 * duration (see format.ts's fmtEta): abbreviated units, no prose, so it needs
 * no translation and reads the same in all forty-two languages.
 */
export function fmtSkew(ms: number): string {
  const secs = Math.round(Math.abs(ms) / 1000);
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.round(secs / 60)}m`;
  const h = Math.floor(secs / 3600);
  return `${h}h ${Math.round((secs % 3600) / 60)}m`;
}

/** The four proxy check ids, in the order the card draws them. */
export const PROXY_CHECKS = ['host', 'proto', 'prefix', 'ws'] as const;

/** One place that builds a browser-side row, so every one carries a timestamp. */
function row(id: string, status: SelfTestStatus, code: string, params?: Record<string, string>): SelfTestResult {
  return { id, status, code, params, at: new Date().toISOString() };
}

/**
 * proxyVerdicts compares what the browser knows it sent against what the
 * instance says it received, and returns the three rows that comparison can
 * decide. The fourth, the WebSocket, is probeWebSocket's answer and is added by
 * the card.
 *
 * `loc` is passed in rather than read from `window` so that every branch below
 * can be exercised without a browser at all.
 */
export function proxyVerdicts(view: SelfTestRequestView, loc: { host: string; protocol: string }): SelfTestResult[] {
  return [hostVerdict(view, loc), protoVerdict(view, loc), prefixVerdict(view)];
}

/**
 * The Host header, and it is first because its failure is the loudest and the
 * least obvious. internal/api's sameOrigin compares the browser's Origin
 * against r.Host and answers 403 to a mismatch, and the WebSocket library
 * applies the same rule to the upgrade. A GET with no Origin - the initial page
 * load - sails through. So a proxy configured with `proxy_set_header Host
 * $proxy_host`, or Traefik with passHostHeader off, gives you a UI that renders
 * perfectly and then refuses every single write, with the live view never
 * opening.
 *
 * X-Forwarded-Host is what makes the diagnosis exact rather than a shrug: a
 * proxy that rewrites Host almost always still reports what the browser
 * originally asked for in that header, so the two together name both halves of
 * the substitution.
 */
function hostVerdict(view: SelfTestRequestView, loc: { host: string }): SelfTestResult {
  const browser = loc.host;
  const forwarded = view.forwardedHost;
  if (forwarded !== '' && forwarded.toLowerCase() !== view.host.toLowerCase()) {
    return row('host', 'fail', 'proxy.host.rewritten', { browser: forwarded, server: view.host });
  }
  if (browser !== '' && view.host.toLowerCase() !== browser.toLowerCase()) {
    return row('host', 'fail', 'proxy.host.rewritten', { browser, server: view.host });
  }
  return row('host', 'pass', 'proxy.host.ok', { host: view.host });
}

/**
 * X-Forwarded-Proto, and the row is deliberately careful about what it claims.
 *
 * Two places in this app upgrade the scheme from that header (the pairing
 * addresses and the container handover) and a third does not: setSession sets
 * the cookie's Secure flag from r.TLS alone, whatever the header says. So a
 * green row here is telling the truth about the QR code and the container
 * handover and saying nothing at all about the cookie, which is why its
 * sentence names those and not "TLS is correctly detected".
 */
function protoVerdict(view: SelfTestRequestView, loc: { protocol: string }): SelfTestResult {
  const https = loc.protocol === 'https:';
  if (!https) {
    // No https anywhere in the chain the browser can see, so there is nothing
    // a proxy would have to declare. Not a finding, and not a warning either.
    return row('proto', 'pass', 'proxy.proto.direct');
  }
  if (view.forwardedProto === 'https') {
    return row('proto', 'pass', 'proxy.proto.ok');
  }
  if (view.forwardedProto === '' && view.tls) {
    // The browser is on https and the TLS ends inside this process: there is no
    // proxy in between, so the missing header is correct rather than missing.
    return row('proto', 'pass', 'proxy.proto.direct');
  }
  return row('proto', 'fail', 'proxy.proto.missing');
}

/**
 * A path prefix, which is not a configuration problem but an unsupported
 * deployment - so the row's verdict is "give this app a host of its own", not
 * "fix your prefix".
 *
 * The reasons are structural and all of them are in the tree: vite.config.ts
 * pins base '/', index.html links the manifest, the favicon and the icons
 * absolutely, the manifest's start_url and scope are both '/', every fetch in
 * api.ts is rooted, and so is the WebSocket. Serving this under example.com/kl
 * fails in both directions - strip the prefix and the browser still asks for
 * /api/tasks and escapes it; do not strip it and the SPA handler answers
 * index.html with a 200 for every asset path, which the browser refuses to run
 * as a module.
 *
 * X-Forwarded-Prefix is the only signal there is. A prefix that is NOT stripped
 * means the request never reaches this app's routes at all, and one that IS
 * stripped is invisible by construction - except that the proxy doing the
 * stripping (Traefik's StripPrefix, and most hand-written nginx blocks) says so
 * in that header.
 */
function prefixVerdict(view: SelfTestRequestView): SelfTestResult {
  if (view.forwardedPrefix !== '') {
    return row('prefix', 'fail', 'proxy.prefix.underPath', { path: view.forwardedPrefix });
  }
  // The second, weaker signal: the path this instance says it received is not
  // the path the browser asked for. It can only differ when something between
  // the two rewrote it.
  if (view.path !== '' && view.path !== '/api/selftest/request') {
    return row('prefix', 'fail', 'proxy.prefix.underPath', { path: view.path });
  }
  return row('prefix', 'pass', 'proxy.prefix.ok', { host: view.host });
}

/**
 * wsVerdict turns probeWebSocket's boolean into the fourth row.
 *
 * It is the most valuable row on the card and the one nothing else in this app
 * shows: behind an nginx that forgot `proxy_set_header Upgrade`, the app loads,
 * looks perfect, and simply never updates. connectWS reconnects every 1500ms
 * for ever with no error surfaced anywhere, so the operator's first thought is
 * "the downloads are stuck" rather than "the socket is down".
 */
export function wsVerdict(opened: boolean): SelfTestResult {
  return opened ? row('ws', 'pass', 'proxy.ws.ok') : row('ws', 'fail', 'proxy.ws.failed');
}
