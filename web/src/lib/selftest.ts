import type { SelfTestRequestView, SelfTestResult, SelfTestStatus } from './api';

// The part of the self-test only a browser can answer. The reverse-proxy
// checks compare what the browser sent with what the server received; a probe
// run on the server would reach its own listener and bypass the proxy.
//
// Every function returns the same SelfTestResult (a status and a stable code)
// as the server's checks, so both kinds of row render and translate alike.

/** How long the probe socket is given to open. */
const WS_PROBE_TIMEOUT_MS = 3000;

// Under five seconds a clock difference changes nothing. From two minutes on,
// TLS certificates look not yet valid and some debrid providers reject signed
// requests as replays.
const SKEW_WARN_MS = 5_000;
const SKEW_FAIL_MS = 120_000;

/**
 * probeWebSocket opens its own socket to /api/ws and reports whether it came
 * up. Not the app's socket: connectWS retries forever without surfacing an
 * error, which is what this probe exists to expose. It never rejects, since
 * every failure means the same to the operator, including the synchronous
 * error of a mixed-content page.
 */
export function probeWebSocket(timeoutMs: number = WS_PROBE_TIMEOUT_MS): Promise<boolean> {
  return new Promise((resolve) => {
    let done = false;
    const finish = (ok: boolean, sock?: WebSocket) => {
      if (done) return;
      done = true;
      // Closed on success too, so each visit does not leave a connection open.
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
 * clockSkewMs is the server's clock minus the browser's, positive when the
 * server is ahead. It compares against the midpoint of the request, as NTP
 * does, so half of a slow round trip is not read as drift.
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

/** fmtSkew prints a clock difference in the untranslated units fmtEta uses. */
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
 * proxyVerdicts compares what the browser sent with what the instance
 * received and returns the host, proto and prefix rows; the card adds the
 * websocket row. `loc` is a parameter so the logic can be tested without a
 * browser.
 */
export function proxyVerdicts(view: SelfTestRequestView, loc: { host: string; protocol: string }): SelfTestResult[] {
  return [hostVerdict(view, loc), protoVerdict(view, loc), prefixVerdict(view)];
}

// A rewritten Host header breaks every write: sameOrigin answers 403 when the
// browser's Origin does not match r.Host, and so does the websocket upgrade,
// while page loads without an Origin still work. A proxy that rewrites Host
// usually still reports the original in X-Forwarded-Host, which names both
// sides of the substitution.
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

// X-Forwarded-Proto decides the scheme of the pairing addresses and the
// container handover. The session cookie's Secure flag follows r.TLS alone, so
// the row's sentence makes no claim about it.
function protoVerdict(view: SelfTestRequestView, loc: { protocol: string }): SelfTestResult {
  const https = loc.protocol === 'https:';
  if (!https) {
    // No https the browser can see, so there is nothing for a proxy to declare.
    return row('proto', 'pass', 'proxy.proto.direct');
  }
  if (view.forwardedProto === 'https') {
    return row('proto', 'pass', 'proxy.proto.ok');
  }
  if (view.forwardedProto === '' && view.tls) {
    // TLS ends in this process, so there is no proxy to send the header.
    return row('proto', 'pass', 'proxy.proto.direct');
  }
  return row('proto', 'fail', 'proxy.proto.missing');
}

// Serving under a path prefix is unsupported, so the verdict is "give the app
// its own host": the base path, the manifest, the icons, every fetch and the
// websocket are all rooted at '/'. A stripped prefix is only visible through
// X-Forwarded-Prefix, which Traefik's StripPrefix and most nginx setups send.
function prefixVerdict(view: SelfTestRequestView): SelfTestResult {
  if (view.forwardedPrefix !== '') {
    return row('prefix', 'fail', 'proxy.prefix.underPath', { path: view.forwardedPrefix });
  }
  // A weaker signal: the received path differs from the one requested, so
  // something in between rewrote it.
  if (view.path !== '' && view.path !== '/api/selftest/request') {
    return row('prefix', 'fail', 'proxy.prefix.underPath', { path: view.path });
  }
  return row('prefix', 'pass', 'proxy.prefix.ok', { host: view.host });
}

/**
 * wsVerdict turns probeWebSocket's answer into the fourth row. Behind a proxy
 * that drops the Upgrade header the app loads fine and never updates, which
 * looks like stuck downloads rather than a dead socket.
 */
export function wsVerdict(opened: boolean): SelfTestResult {
  return opened ? row('ws', 'pass', 'proxy.ws.ok') : row('ws', 'fail', 'proxy.ws.failed');
}
