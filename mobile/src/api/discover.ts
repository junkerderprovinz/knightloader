import * as Network from 'expo-network';

// Finding the server on this network without anybody typing an address.
//
// The server side of "out of the box" is internal/discovery: a UDP multicast
// announce every few seconds, which the web UI reads back through
// GET /api/discovery. A phone cannot join that group - React Native has no UDP
// socket, and adding one means a native module, a permission on Android 13+ for
// multicast on some devices, and a second protocol to keep working on both
// platforms. So this does the other half of the same job with the one thing the
// app already has: HTTP.
//
// GET /api/health is registered with reg.AddOpen (internal/api/routes_system.go)
// precisely so a probe can reach a locked instance, and it answers a two-field
// JSON. Asking every address on the phone's own subnet for it finds every
// KnightLoader on the network in a couple of seconds.
//
// WHAT THIS DELIBERATELY DOES NOT LEARN: the instance's name. /api/health does
// not carry one, and it should not - it is reachable unauthenticated from
// wherever the instance is reachable from, which for a published instance is
// the internet, whereas a multicast announce never leaves the local network.
// The name arrives the moment a token is entered and the normal authenticated
// call happens, which is soon enough for a list that is really a list of
// addresses.

/** Found is one address on this network that answered as a KnightLoader. */
export type Found = {
  /** The full base URL to connect to, ready to drop into the address field. */
  url: string;
  /** The version it reported, shown so two instances can be told apart. */
  version: string;
};

// The default KL_ADDR port (cmd/knightloader/main.go). Only this one: a second
// port doubles a sweep that is already 253 requests, and an instance moved off
// the default is one whose address its owner knows and can type.
const PORT = 8749;

// Long enough for a busy NAS to answer a handler that does nothing but write
// two JSON fields, short enough that a silently dropped packet does not hold a
// slot for long. See CONCURRENCY for what these two numbers cost together.
const TIMEOUT_MS = 1200;

// How many probes are actually in flight at once.
//
// NOT 253. React Native routes fetch through OkHttp on Android, whose stock
// Dispatcher allows 64 concurrent requests; everything beyond that queues. The
// first version of this file started all 253 abort timers at t=0 and fired them
// all off with Promise.all, so the queued ~189 were still waiting for a slot
// when their own 1500 ms deadline expired and were aborted WITHOUT EVER BEING
// SENT. A server on 192.168.1.100 - squarely inside most routers' DHCP pool -
// was never contacted, and the screen said "found none".
//
// A pool below OkHttp's own limit means every slot is a request that is really
// on the wire, and each address gets its full timeout starting when its turn
// comes rather than when the sweep did. 48 rather than 64 leaves headroom for
// the app's own traffic.
//
// Measured against a simulated dispatcher with the same limit, with the server
// deliberately placed at .200 so it sits well past the first batch:
//
//   all 253 at once   server found: NEVER   (the queued ones time out unsent)
//   pool of 48        server found: always  - 7.3s if every dead address is
//                     silently dropped, 0.1s when they answer with a reset,
//                     which is what a home LAN normally does.
const CONCURRENCY = 48;

/**
 * probe asks one address whether a KnightLoader lives there. Never throws:
 * on this sweep almost every address is nothing at all, and a refused
 * connection is the ordinary case, not an error worth propagating.
 */
async function probe(host: string): Promise<Found | null> {
  const url = `http://${host}:${PORT}`;
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), TIMEOUT_MS);
  try {
    const res = await fetch(`${url}/api/health`, { signal: ctrl.signal });
    if (!res.ok) return null;
    const body = await res.json();
    // Something else entirely may be listening on that port. The shape is what
    // says otherwise, so it is checked rather than assumed.
    if (body?.status !== 'ok' || typeof body?.version !== 'string') return null;
    return { url, version: body.version };
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
}

/**
 * usableIPv4 decides whether an address is one a /24 sweep makes sense from.
 *
 * "0.0.0.0" is the one that matters and the one that is easy to miss:
 * expo-network's Android implementation reads the WI-FI address specifically,
 * which is 0 when Wi-Fi is off. That string passes a naive length-and-prefix
 * check, so a phone on mobile data would sweep 0.0.0.1 through 0.0.0.254 -
 * 253 pointless requests over a metered connection, and the "no network, no
 * results" claim would hold only by accident.
 *
 * Loopback and link-local (169.254/16, what an interface gives itself when
 * DHCP fails) are excluded for the same reason internal/discovery.LocalIPv4
 * excludes them.
 */
function usableIPv4(ip: string): boolean {
  const parts = ip.split('.');
  if (parts.length !== 4) return false;
  if (parts.some((p) => p === '' || !/^\d{1,3}$/.test(p) || Number(p) > 255)) return false;
  if (ip.startsWith('0.') || ip.startsWith('127.') || ip.startsWith('169.254.')) return false;
  return true;
}

/**
 * scanLocalNetwork sweeps the phone's own /24 and returns whatever answered.
 *
 * A /24 because that is what a home network is - a phone on a larger subnet
 * finds the instances that share its first three octets and nothing else,
 * which is a smaller promise than a full sweep but an honest one, and the
 * address field is still right there for anything outside it.
 *
 * Returns an empty list rather than throwing when the phone has no usable
 * address (mobile data, airplane mode, a captive portal, or an Android device
 * whose only connection is Ethernet): "found none" is the truth in every one of
 * those cases, and an error would send somebody looking for a fault that is not
 * there.
 */
export async function scanLocalNetwork(): Promise<Found[]> {
  let ip: string | null = null;
  try {
    ip = await Network.getIpAddressAsync();
  } catch {
    return [];
  }
  if (!ip || !usableIPv4(ip)) return [];
  const prefix = ip.split('.').slice(0, 3).join('.');

  const hosts: string[] = [];
  for (let i = 1; i < 255; i++) {
    const host = `${prefix}.${i}`;
    if (host !== ip) hosts.push(host); // the phone is not the server
  }

  // A fixed pool of workers pulling from one cursor, rather than firing every
  // request at once - see CONCURRENCY for why that difference decides whether
  // most of the subnet is probed at all.
  const found: Found[] = [];
  let next = 0;
  const worker = async () => {
    for (;;) {
      const i = next++;
      if (i >= hosts.length) return;
      const hit = await probe(hosts[i]);
      if (hit) found.push(hit);
    }
  };
  await Promise.all(Array.from({ length: Math.min(CONCURRENCY, hosts.length) }, worker));

  found.sort((a, b) => a.url.localeCompare(b.url, undefined, { numeric: true }));
  return found;
}
