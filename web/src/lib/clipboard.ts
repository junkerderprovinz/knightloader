// copyToClipboard copies text, falling back to execCommand('copy'): the
// Clipboard API needs a secure context, and the usual install is plain HTTP on
// a LAN address, where navigator.clipboard is undefined.
export async function copyToClipboard(text: string): Promise<boolean> {
  if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Some browsers expose the API and still refuse the call.
    }
  }
  return copyWithExecCommand(text);
}

function copyWithExecCommand(text: string): boolean {
  if (typeof document === 'undefined') return false;
  const el = document.createElement('textarea');
  el.value = text;
  // Off-screen rather than hidden, since execCommand needs it focusable and selectable.
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
