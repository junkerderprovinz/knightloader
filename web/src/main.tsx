import React from 'react';
import { createRoot } from 'react-dom/client';
import './index.css';
// flags.css is left to LanguagePicker, which loads it after paint: the
// heraldic flags alone weigh hundreds of kB and appear in one menu.
import { AppRouter } from './app/router';
import { applyStoredTheme } from './lib/theme';
import { applyStoredLanguage } from './lib/i18n';
import { applyCachedAppearance } from './lib/appearance';

// Apply persisted preferences before first paint (no flash).
applyStoredTheme();
applyStoredLanguage();
applyCachedAppearance();

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <AppRouter />
  </React.StrictMode>,
);

// Registered after load so it does not compete with first paint. Service
// workers need a secure context, which a plain-HTTP LAN install is not.
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {
      // Only installability and the share target depend on it.
    });
  });
}
