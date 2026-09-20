/// <reference types="vite/client" />

// The vite/client types let TypeScript accept the dynamic import('./flags.css').

/** The mobile app's version, substituted at build time from mobile/app.json. */
declare const __MOBILE_VERSION__: string;
