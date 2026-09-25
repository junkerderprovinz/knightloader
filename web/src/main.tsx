import React from 'react';
import { createRoot } from 'react-dom/client';
import '@fontsource-variable/noto-sans';
import '@fontsource-variable/noto-sans-arabic';
import '@fontsource-variable/noto-sans-hebrew';
import '@fontsource-variable/noto-sans-thai';
import './index.css';
// flags.css is left to LanguagePicker, which loads it after paint: the
// heraldic flags alone weigh hundreds of kB and appear in one menu.
import { AppRouter } from './app/router';
import { applyStoredTheme } from './lib/theme';
import { applyStoredLanguage } from './lib/i18n';
import { applyCachedAppearance, readCachedDisco } from './lib/appearance';
import { applyDisco } from './lib/disco';
import { sendApiCallsUnderBase, settleUnderBase, withBase } from './lib/basePath';

// Before anything fetches or routes.
sendApiCallsUnderBase();
settleUnderBase();

// Apply persisted preferences before first paint (no flash). Disco starts from
// the rainbow state, so it comes after it.
applyStoredTheme();
applyStoredLanguage();
applyCachedAppearance();
applyDisco(readCachedDisco());

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <AppRouter />
  </React.StrictMode>,
);

// Registered after load so it does not compete with first paint. Service
// workers need a secure context, which a plain-HTTP LAN install is not.
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    // Its scope is the folder it is served from, so under a base path it
    // covers the app and nothing else on the host.
    navigator.serviceWorker.register(withBase('/sw.js')).catch(() => {
      // Only installability and the share target depend on it.
    });
  });
}
