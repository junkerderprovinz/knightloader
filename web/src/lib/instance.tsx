import { createContext, useCallback, useContext, useMemo, type ReactNode } from 'react';
import { useSearchParams } from 'react-router-dom';
import { apiBase } from './api';

/**
 * Which KnightLoader the app is pointed at ('' for this one, otherwise a
 * peer's name), readable from the shell so its controls act on the machine
 * the list shows.
 *
 * It lives in the URL (?instance=NAME): deep links from Overview and Instances
 * and the dropdown share one value, leaving the page drops the scope with the
 * list it belonged to, and it survives a reload.
 */
export interface InstanceScope {
  /** '' is this instance; anything else is the peer's registered name. */
  instance: string;
  /** The API prefix for `instance`, ready for any call in lib/api.ts. */
  base: string;
  select: (instance: string) => void;
}

const PARAM = 'instance';

// No default: falling back to '' would quietly point a control at this machine
// while a peer's list is shown, so using it outside the provider throws.
const Ctx = createContext<InstanceScope | null>(null);

export function useInstanceScope(): InstanceScope {
  const scope = useContext(Ctx);
  if (!scope) throw new Error('useInstanceScope() used outside <InstanceProvider>');
  return scope;
}

export function InstanceProvider({ children }: { children: ReactNode }) {
  const [params, setParams] = useSearchParams();
  const instance = params.get(PARAM) ?? '';

  const select = useCallback(
    (next: string) => {
      setParams(
        (prev) => {
          // Keep the other parameters; the setter replaces the whole query.
          const p = new URLSearchParams(prev);
          if (next) p.set(PARAM, next);
          else p.delete(PARAM);
          return p;
        },
        // Replaced, not pushed, so Back does not walk through every peer viewed.
        { replace: true },
      );
    },
    [setParams],
  );

  const value = useMemo(() => ({ instance, base: apiBase(instance), select }), [instance, select]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
