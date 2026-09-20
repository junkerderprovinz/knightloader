import raw from '../assets/logo.svg?raw';

/**
 * logoInline is the logo as markup, for the sidebar easter egg that moves the
 * sword and leaves the shield standing; CSS cannot reach inside an <img>.
 * Everywhere else the logo stays an <img>.
 *
 * A <style> inside inline SVG applies to the whole document, so the file's
 * generic `.cls-N` rules are scoped to the wrapper here. The SVG stays the one
 * source of the colours.
 */
const SCOPE = 'kl-logo';

export const LOGO_SCOPE_ID = SCOPE;

export const logoInline = raw
  // The XML declaration has no place inside an HTML document.
  .replace(/^<\?xml[^>]*\?>\s*/, '')
  .replace(
    /<style>([\s\S]*?)<\/style>/,
    (_all, css: string) => `<style>${css.replace(/\.cls-(\d+)/g, `#${SCOPE} .cls-$1`)}</style>`,
  );
