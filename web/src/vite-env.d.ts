/// <reference types="vite/client" />

// Without this, TypeScript has no idea what `import('./flags.css')` resolves to
// and refuses the dynamic import that keeps half a megabyte of flags out of the
// first paint. A plain side-effect `import './x.css'` slips through unchecked;
// a dynamic one does not.

/**
 * The mobile app's version, substituted at build time from mobile/app.json —
 * see vite.config.ts's own comment for why it is read rather than typed.
 */
declare const __MOBILE_VERSION__: string;
