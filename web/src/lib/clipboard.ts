// A "copy to clipboard" write helper with a fallback, because the modern
// Clipboard API (navigator.clipboard) only exists in a secure context
// (HTTPS or localhost) — and this app's single most common deployment shape
// is exactly the opposite: a self-hosted instance reached over plain
// http://<lan-ip>:8749, where navigator.clipboard is undefined entirely
// (jdp, 2026-08-24: "Es gibt immer noch kein Kopieren badge für den
// generierten key" — the badge WAS there, gated behind 'clipboard' in
// navigator, which is exactly why it never rendered on a plain-HTTP LAN
// instance). The legacy execCommand('copy') path below has no such
// restriction and still works everywhere a real browser runs this app,
// deprecated as the API is - it is the only thing that actually copies text
// on an insecure origin.
export async function copyToClipboard(text: string): Promise<boolean> {
  if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Falls through to the legacy path - some browsers expose the API
      // but still refuse the call outside a user gesture or a secure
      // context in ways `'clipboard' in navigator` alone cannot predict.
    }
  }
  return copyWithExecCommand(text);
}

function copyWithExecCommand(text: string): boolean {
  if (typeof document === 'undefined') return false;
  const el = document.createElement('textarea');
  el.value = text;
  // Off-screen rather than hidden: a hidden/display:none element cannot be
  // focused or selected, both of which execCommand('copy') requires.
  el.style.position = 'fixed';
  el.style.top = '0';
  el.style.left = '-9999px';
  el.setAttribute('readonly', '');
  document.body.appendChild(el);
  const previousFocus = document.activeElement as HTMLElement | null;
  el.select();
  el.setSelectionRange(0, text.length);
  let ok = false;
  try {
    ok = document.execCommand('copy');
  } catch {
    ok = false;
  }
  document.body.removeChild(el);
  previousFocus?.focus?.();
  return ok;
}
