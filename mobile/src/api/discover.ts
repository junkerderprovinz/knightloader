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
// It does not learn the instance's name. /api/health carries none, because it
// is reachable unauthenticated from wherever the instance is, which for a
// published instance is the internet, while a multicast announce never leaves
// the local network. The name arrives with the first authenticated call once a
// token is entered.

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

// How many probes are in flight at once, and not all 253.
//
// React Native routes fetch through OkHttp on Android, whose stock Dispatcher
// allows 64 concurrent requests and queues the rest. Firing the whole sweep
// with Promise.all starts every abort timer at once, so the queued addresses
// hit their own deadline while still waiting for a slot and are aborted before
// they are sent: a server at 192.168.1.100, inside most routers' DHCP pool, is
// never contacted at all.
//
// A pool below OkHttp's limit means every slot is a request on the wire, and
// each address gets its full timeout from the moment its turn comes. 48 rather
// than 64 leaves headroom for the app's own traffic. Measured against a
// simulated dispatcher with a server at .200, well past the first batch, that
// finds it in 7.3s when dead addresses are silently dropped and in 0.1s when
// they answer with a reset, which is what a home LAN does.
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
 * "0.0.0.0" is the case that matters: expo-network's Android implementation
 * reads the Wi-Fi address, which is 0 while Wi-Fi is off. That string passes a
 * length-and-prefix check, so a phone on mobile data would sweep 0.0.0.1
 * through 0.0.0.254 over a metered connection.
 *
 * Loopback and link-local (169.254/16, what an interface gives itself when DHCP
 * fails) are excluded for the same reason internal/discovery.LocalIPv4 excludes
 * them.
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
 * A /24 because that is what a home network is. A phone on a larger subnet
 * finds the instances that share its first three octets, and the address field
 * is there for anything outside them.
 *
 * Returns an empty list rather than throwing when the phone has no usable
 * address (mobile data, airplane mode, a captive portal, or an Android device
 * connected only by Ethernet): "found none" is the truth in each of those
 * cases, and an error would send somebody looking for a fault.
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

  // A fixed pool of workers pulling from one cursor rather than firing every
  // request at once; see CONCURRENCY.
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
