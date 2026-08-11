/**
 * The bookmarklet, and the query contract every "hand KnightLoader a link
 * from outside the app" entrance shares: the bookmarklet below, the MV3
 * extension (extension/src/background.js — see its own doc comment for why
 * it opens this same address rather than calling /api/links directly), and
 * the PWA share target (web/public/manifest.webmanifest's share_target,
 * whose params map onto the identical url/text/title names on purpose).
 * One page reads all three: pages/QuickAdd.tsx.
 */

/** The three fields a caller may hand /quickadd, always as query parameters. */
export interface QuickAddParams {
  url?: string;
  text?: string;
  title?: string;
}

export function quickAddUrl(origin: string, params: QuickAddParams): string {
  const u = new URL('/quickadd', origin);
  if (params.url) u.searchParams.set('url', params.url);
  if (params.text) u.searchParams.set('text', params.text);
  if (params.title) u.searchParams.set('title', params.title);
  return u.toString();
}

/**
 * buildBookmarklet returns the `javascript:` URI to drag to a bookmarks bar.
 *
 * `origin` is baked in at generation time — there is no fixed address to
 * hardcode, self-hosted means every install has its own — which is why this
 * is a function called with `window.location.origin` from the settings page
 * rather than a static constant. The snippet itself opens a small window at
 * this SAME origin rather than calling the API from the page it was clicked
 * on: internal/api/api.go's sameOrigin middleware refuses any request
 * carrying a foreign Origin header, on purpose (see that file's own
 * comment), so a bookmarklet that tried to fetch() from
 * https://some-hoster.example straight into the API would be refused by
 * design — same-origin is not an obstacle here, it is the reason this has
 * to open a window instead of firing a background request.
 *
 * Selected text, when there is any, rides along as `text` so a block
 * containing several links can be sent in one click without opening the
 * page's own address at all — window.getSelection() is empty when nothing
 * is selected, so the common "just send this page" case is unaffected.
 */
export function buildBookmarklet(origin: string): string {
  const body = `(function(){
    var s=window.getSelection?String(window.getSelection()):'';
    var u='${origin}/quickadd?url='+encodeURIComponent(location.href)+'&title='+encodeURIComponent(document.title)+(s?'&text='+encodeURIComponent(s):'');
    window.open(u,'knightloader_add','width=420,height=560');
  })();`;
  // Newlines survive inside a javascript: URI (browsers accept them), but
  // stripping them keeps the href attribute short enough that dragging it
  // does not turn the surrounding page layout into a scroll bar.
  return 'javascript:' + encodeURIComponent(body.replace(/\s+/g, ' ').trim());
}
