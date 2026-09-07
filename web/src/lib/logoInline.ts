import raw from '../assets/logo.svg?raw';

/**
 * logoInline is the mark as markup rather than as an image source.
 *
 * Everywhere else the logo is an `<img src={logoUrl}>`, which is the right
 * thing: it caches, it decodes off the main thread, and nothing needs to reach
 * inside it. The sidebar easter egg does need to reach inside it - it moves the
 * SWORD and leaves the shield standing - and CSS cannot cross an `<img>`
 * boundary, so that one place gets the document tree instead.
 *
 * Inlining brings one problem with it, and it is not obvious. The file carries
 * its own `<style>` block with eleven generic fill rules named `.cls-1` to
 * `.cls-11`, and a `<style>` inside inline SVG is NOT scoped to that SVG: it
 * applies to the whole document, the same as any other stylesheet. Left alone,
 * eleven rules with names that generic would be loose in the app, waiting for
 * the first component that happens to pick one.
 *
 * So the selectors are narrowed to the wrapper on the way in. Derived from the
 * file's own block rather than copied into index.css, because a second copy of
 * the palette drifts the first time somebody recolours the mark - and this way
 * the SVG stays the single source of the colours for the `<img>` users too.
 */
const SCOPE = 'kl-logo';

export const LOGO_SCOPE_ID = SCOPE;

export const logoInline = raw
  // The XML declaration is legal in a file and meaningless in a document; left
  // in, it is parsed as a stray processing instruction and dropped anyway.
  .replace(/^<\?xml[^>]*\?>\s*/, '')
  .replace(
    /<style>([\s\S]*?)<\/style>/,
    (_all, css: string) => `<style>${css.replace(/\.cls-(\d+)/g, `#${SCOPE} .cls-$1`)}</style>`,
  );
