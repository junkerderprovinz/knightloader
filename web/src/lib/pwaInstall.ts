import { useEffect, useState } from 'react';

/**
 * The install prompt, captured once and shared by every component that might
 * offer an "Install" button — the settings page here (BrowserTools.tsx) and,
 * per build-plan.md section 8's Wave 11 note on 11C/11D, the Remote access
 * page 11C builds, which is where this install action is meant to actually
 * live once that page exists.
 *
 * `beforeinstallprompt` fires once, early, and only if nothing has called
 * `.preventDefault()` on it and then never used it does the browser fall
 * back to its own install affordance — so the event has to be caught at the
 * module level the moment the app loads, not inside whichever component
 * happens to mount later and ask for it. A second caller before this file
 * existed would just miss the event entirely.
 */
interface BeforeInstallPromptEvent extends Event {
  prompt(): Promise<void>;
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>;
}

let deferred: BeforeInstallPromptEvent | null = null;
let installed = false;
const listeners = new Set<() => void>();

function notify() {
  for (const l of listeners) l();
}

if (typeof window !== 'undefined') {
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault();
    deferred = e as BeforeInstallPromptEvent;
    notify();
  });
  // Fires on Chrome/Edge once the install actually completes, including via
  // the browser's own omnibox icon rather than this page's button — without
  // this a button offering to install an already-installed app is stale
  // until the next full reload.
  window.addEventListener('appinstalled', () => {
    installed = true;
    deferred = null;
    notify();
  });
}

/**
 * useInstallPrompt reports whether the browser is currently offering to
 * install this app, and a function to trigger that install.
 *
 * `available` is false on a browser that never fires the event (Firefox,
 * Safari) and once already installed — both are the correct state for a
 * caller to hide the button in, not an error to surface.
 */
export function useInstallPrompt(): { available: boolean; promptInstall: () => Promise<boolean> } {
  const [, setTick] = useState(0);
  useEffect(() => {
    const onChange = () => setTick((n) => n + 1);
    listeners.add(onChange);
    return () => {
      listeners.delete(onChange);
    };
  }, []);

  return {
    available: deferred !== null && !installed,
    promptInstall: async () => {
      if (!deferred) return false;
      const capture = deferred;
      // Single-use: the browser discards a prompted event whether the answer
      // was yes or no, and holding onto a stale reference would call .prompt()
      // a second time on an event that can no longer show anything.
      deferred = null;
      notify();
      await capture.prompt();
      const { outcome } = await capture.userChoice;
      return outcome === 'accepted';
    },
  };
}
