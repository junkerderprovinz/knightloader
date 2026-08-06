import React from 'react';
import { createRoot } from 'react-dom/client';
import './index.css';
import './flags.css';
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
