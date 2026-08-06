import React from 'react';
import { createRoot } from 'react-dom/client';
import './index.css';
// flags.css is NOT imported here. It is 42 inlined flags, and the heraldic ones
// alone (Serbia 181 kB, Spain 81 kB) dwarf the rest of the stylesheet — in the
// main bundle it would block first paint on artwork that is 16 px tall and only
// ever appears in one menu. LanguagePicker pulls it in itself, after paint.
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
