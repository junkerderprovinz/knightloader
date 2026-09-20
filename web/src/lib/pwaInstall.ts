import { useEffect, useState } from 'react';

// The install prompt, captured once at module load and shared by every
// component that offers an "Install" button. beforeinstallprompt fires once,
// early, so a component mounting later would miss it.
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
  // Also fires when the app was installed from the browser's own address-bar
  // icon, so the button disappears without a reload.
  window.addEventListener('appinstalled', () => {
    installed = true;
    deferred = null;
    notify();
  });
}

/**
 * useInstallPrompt reports whether the browser offers to install the app, and
 * a function that triggers it. `available` is false in Firefox and Safari,
 * which never fire the event, and once installed.
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
      // The event can only be prompted once, whatever the answer.
      deferred = null;
      notify();
      await capture.prompt();
      const { outcome } = await capture.userChoice;
      return outcome === 'accepted';
    },
  };
}
