// The line the sign-in page shows after a sign-out, carried over the reload.
import { signIn } from './kit';
import type { Translate } from './words';

const KEY = 'kl-signin-note';

/** signOut ends this browser's session and reloads into the sign-in page, which says goodbye. */
export async function signOut(): Promise<void> {
  await signIn.logout();
  try {
    sessionStorage.setItem(KEY, 'bye');
  } catch {
    // Without storage the page just has no goodbye.
  }
  location.reload();
}

/** takeSignInNote returns the goodbye once, or ''. */
export function takeSignInNote(t: Translate): string {
  try {
    const note = sessionStorage.getItem(KEY);
    sessionStorage.removeItem(KEY);
    return note === 'bye' ? t('signin.bye') : '';
  } catch {
    return '';
  }
}
