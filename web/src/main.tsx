// Before every other module, any of which may read a store as it loads.
import './lib/storageShim';
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
import { applyCachedSpeedLines } from './lib/speedLines';
import { listenForPageHue } from './lib/pageHue';
import { sendApiCallsUnderBase, settleUnderBase, withBase } from './lib/basePath';
import { followWindowGround } from './lib/desktop';
import { watchSystemAccent } from './lib/systemAccent';
import { setStoragePrefix } from '@glimstone/core';
import { setGlyphResolver } from '@glimstone/react/glyphs';
import { glyphFor } from './lib/glyphFor';

// GlimStone's own components keep their choices under kl- and pick their
// glyphs from KnightLoader's set.
setStoragePrefix('kl');
setGlyphResolver(glyphFor);

// Before anything fetches or routes.
sendApiCallsUnderBase();
settleUnderBase();

// Apply persisted preferences before first paint (no flash). Disco starts from
// the rainbow state, so it comes after it.
applyStoredTheme();
applyStoredLanguage();
applyCachedAppearance();
watchSystemAccent();
applyCachedSpeedLines();
applyDisco(readCachedDisco());
listenForPageHue();
followWindowGround();

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <AppRouter />
  </React.StrictMode>,
);

// Registered after load so it does not compete with first paint. Service
// workers need a secure context, which a plain-HTTP LAN install is not, and
// an origin of their own, which a page opened through the fortress lacks:
// there even reading navigator.serviceWorker throws. Only installability and
// the share target depend on it.
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    try {
      // Its scope is the folder it is served from, so under a base path it
      // covers the app and nothing else on the host.
      navigator.serviceWorker.register(withBase('/sw.js')).catch(() => {});
    } catch {
      // Left without one.
    }
  });
}
