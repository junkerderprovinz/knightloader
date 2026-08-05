import React from 'react';
import { createRoot } from 'react-dom/client';
import './index.css';
import { AppRouter } from './app/router';
import { applyStoredTheme } from './lib/theme';

// Apply the persisted theme before first paint (no flash).
applyStoredTheme();

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <AppRouter />
  </React.StrictMode>,
);
