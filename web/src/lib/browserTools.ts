// The bookmarklet, and the /quickadd query contract shared by every way of
// handing KnightLoader a link from outside: the bookmarklet, the browser
// extension and the PWA share target (manifest.webmanifest's share_target
// uses the same url/text/title names). pages/QuickAdd.tsx reads all three.

/** The three fields a caller may hand /quickadd, always as query parameters. */
export interface QuickAddParams {
  url?: string;
  text?: string;
  title?: string;
}

/**
 * quickAddUrl is the /quickadd page of the instance at `address`, its origin
 * and base path, so https://example.com/kl leads to /kl/quickadd.
 */
export function quickAddUrl(address: string, params: QuickAddParams): string {
  const u = new URL(`${address.replace(/\/+$/, '')}/quickadd`);
  if (params.url) u.searchParams.set('url', params.url);
  if (params.text) u.searchParams.set('text', params.text);
  if (params.title) u.searchParams.set('title', params.title);
  return u.toString();
}

/**
 * buildBookmarklet returns the `javascript:` URI to drag to a bookmarks bar,
 * with this install's address (origin and base path) baked in. The snippet
 * opens a small window on that address instead of calling the API from the
 * visited page, because the sameOrigin middleware refuses requests with a
 * foreign Origin. Selected text goes along as `text`, so a block of links can
 * be sent in one click.
 */
export function buildBookmarklet(address: string): string {
  const body = `(function(){
    var s=window.getSelection?String(window.getSelection()):'';
    var u='${address}/quickadd?url='+encodeURIComponent(location.href)+'&title='+encodeURIComponent(document.title)+(s?'&text='+encodeURIComponent(s):'');
    window.open(u,'knightloader_add','width=420,height=560');
  })();`;
  // Collapsed whitespace keeps the href short.
  return 'javascript:' + encodeURIComponent(body.replace(/\s+/g, ' ').trim());
}
