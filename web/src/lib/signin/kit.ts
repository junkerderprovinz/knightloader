// The kit's sign-in as KnightLoader talks to it: one client for the sign-in
// page, the Security tile and the password question.
import { createSignInApi, type SignInReauth, type SignInReauthState } from '@glimstone/core';
import { basePath } from '../basePath';

export const signIn = createSignInApi({ root: basePath() + '/' });

/**
 * The kit's client asks for the password only before its own changes.
 * KnightLoader's API wants a fresh sign-in too, before a new way in such as
 * joining a fortress, so one question serves both and SignInReauthWindow draws
 * whichever is waiting.
 */
function shareQuestion(kit: SignInReauth) {
  let own: SignInReauthState = { asking: false, password: '', busy: false, error: null, refused: 0 };
  const listeners = new Set<() => void>();
  let waiting: ((confirmed: boolean) => void) | null = null;
  const update = (change: Partial<SignInReauthState>) => {
    own = { ...own, ...change };
    listeners.forEach((fn) => fn());
  };
  const settle = (confirmed: boolean) => {
    const resume = waiting;
    waiting = null;
    update({ asking: false, password: '', busy: false, error: null });
    resume?.(confirmed);
  };
  const kitAsks = () => kit.state().asking;

  const reauth: SignInReauth = {
    state: () => (kitAsks() ? kit.state() : own),
    subscribe(listener) {
      listeners.add(listener);
      const stop = kit.subscribe(listener);
      return () => {
        listeners.delete(listener);
        stop();
      };
    },
    set: (password) => (kitAsks() ? kit.set(password) : update({ password, error: null })),
    async submit() {
      if (kitAsks()) return kit.submit();
      if (!own.asking || own.busy || own.password === '') return;
      update({ busy: true, error: null });
      const r = await signIn.confirm(own.password);
      if (r.ok) settle(true);
      else update({ busy: false, error: r.error, refused: own.refused + 1 });
    },
    cancel: () => (kitAsks() ? kit.cancel() : settle(false)),
  };

  let pending: Promise<boolean> | null = null;
  const ask = (): Promise<boolean> => {
    // Without a window to answer in, the refusal goes back to the caller.
    if (listeners.size === 0) return Promise.resolve(false);
    pending ??= new Promise<boolean>((resolve) => {
      waiting = resolve;
      update({ asking: true, password: '', error: null });
    }).finally(() => (pending = null));
    return pending;
  };
  return { reauth, ask };
}

const question = shareQuestion(signIn.reauth);

/** The client SignInReauthWindow and the Security tile share. */
export const reauthClient = { ...signIn, reauth: question.reauth };

const READS = new Set(['GET', 'HEAD']);

function pathOf(input: RequestInfo | URL): string {
  const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
  try {
    return new URL(raw, location.href).pathname;
  } catch {
    return raw;
  }
}

/** Whether a refusal is the kit's call for a fresh sign-in that a password answers. */
async function wantsPassword(r: Response): Promise<boolean> {
  if (r.status !== 403) return false;
  try {
    const p = (await r.clone().json()) as { code?: string; params?: { askPassword?: boolean } };
    return p.code === 'signInAgain' && p.params?.askPassword === true;
  } catch {
    return false;
  }
}

/**
 * retryAfterPassword wraps fetcher so a change to KnightLoader's own API that
 * wants a fresh sign-in asks for the password and runs once more. The kit's
 * own calls ask through its client, and a read is never refused that way.
 */
export function retryAfterPassword(
  fetcher: typeof fetch,
  ask: () => Promise<boolean> = question.ask,
): (input: RequestInfo | URL, init?: RequestInit) => Promise<Response> {
  return async (input, init) => {
    const r = await fetcher(input, init);
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
    const path = pathOf(input);
    if (READS.has(method) || !path.includes('/api/') || path.includes('/api/signin/')) return r;
    if (init?.body instanceof ReadableStream || !(await wantsPassword(r))) return r;
    if (!(await ask())) return r;
    return fetcher(input, init);
  };
}

/** askBeforeStaleChanges routes every fetch of the page through retryAfterPassword. */
export function askBeforeStaleChanges(): void {
  window.fetch = retryAfterPassword(window.fetch.bind(window)) as typeof fetch;
}
