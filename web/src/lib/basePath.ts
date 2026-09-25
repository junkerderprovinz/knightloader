// Where the app lives on its host. The server writes it into index.html as the
// <base> element: "/" at the root, "/kl/" behind a proxy that mounts the app at
// /kl. The build names its assets relative to it, and everything below puts it
// in front of what the app asks for or links to.

/** basePath is the prefix without its trailing slash, '' at the root. */
export function basePath(): string {
  const href = document.querySelector('base')?.getAttribute('href') ?? '/';
  return href.replace(/\/+$/, '');
}

/** withBase puts the base path in front of a path from the root, "/api/tasks" say. */
export function withBase(path: string): string {
  return basePath() + path;
}

/**
 * appAddress is this instance as another program has to name it, origin and
 * base path, for the bookmarklet or a metrics collector.
 */
export function appAddress(): string {
  return location.origin + basePath();
}

/** socketURL is the WebSocket address of a route such as "/api/ws". */
export function socketURL(path: string): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  return `${proto}://${location.host}${withBase(path)}`;
}

/**
 * sendApiCallsUnderBase makes fetch put the base path in front of every API
 * route. The app names its routes as the server registers them, "/api/...", in
 * a few hundred places, and one wrapper keeps a route added later from
 * forgetting the prefix. A URL that already carries it is left alone.
 */
export function sendApiCallsUnderBase(): void {
  const base = basePath();
  if (base === '') return;
  const fetchFromRoot = window.fetch.bind(window);
  window.fetch = (input, init) =>
    fetchFromRoot(typeof input === 'string' && input.startsWith('/api/') ? base + input : input, init);
}

/**
 * settleUnderBase moves a page opened on the bare address of an instance that
 * has a base path under that path, where the router looks for its routes. The
 * server answers on both, so nothing is loaded again.
 */
export function settleUnderBase(): void {
  const base = basePath();
  const here = location.pathname;
  if (base === '' || here === base || here.startsWith(`${base}/`)) return;
  history.replaceState(history.state, '', base + here + location.search + location.hash);
}
