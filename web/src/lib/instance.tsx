import { createContext, useCallback, useContext, useMemo, type ReactNode } from 'react';
import { useSearchParams } from 'react-router-dom';
import { apiBase } from './api';

/**
 * Which KnightLoader the app is pointed at ('' for this one, otherwise a peer's
 * name), hoisted out of the download page so the shell can read it too.
 *
 * It was `useState` inside Downloads, which held up for exactly as long as the
 * only transport control lived on that page. A control in the shell has no page
 * state to read, and the only value it can assume is '/api': press stop and THIS
 * machine's queue halts while the list underneath shows a peer's downloads. A
 * control that acts on a different machine than the list beside it is worse than
 * no control, so the scope has to be readable from above the page.
 *
 * The store is the URL rather than a `useState` in the provider, and that is the
 * whole trick:
 *   - Overview and Instances already open a peer with /downloads?instance=NAME,
 *     so a deep link and a pick from the dropdown land in the same place instead
 *     of one seeding the other and then drifting;
 *   - leaving the page takes the parameter with it, so the scope releases itself
 *     the moment the list that justified it is gone. State held in a provider
 *     would go on claiming "controlling nas" over a local overview, which is the
 *     same lie pointing the other way;
 *   - it survives a reload and can be handed to somebody else.
 */
export interface InstanceScope {
  /** '' is this instance; anything else is the peer's registered name. */
  instance: string;
  /** The API prefix for `instance`, ready for any call in lib/api.ts. */
  base: string;
  select: (instance: string) => void;
}

const PARAM = 'instance';

// No default value. Every other context in this app can fall back to something
// harmless; this one cannot, because the harmless-looking fallback ('' = act
// here) is the exact bug it exists to prevent. A widget mounted outside the
// provider should stop the app in the developer's face, not quietly point the
// stop button at the wrong machine.
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
          // Rebuilt from the previous parameters rather than from scratch: the
          // setter replaces the whole query string, and a page that keeps
          // anything else in it would lose it on a dropdown pick.
          const p = new URLSearchParams(prev);
          if (next) p.set(PARAM, next);
          else p.delete(PARAM);
          return p;
        },
        // Replaced, not pushed. Switching instance is a change of view, not a
        // step: pushing would make Back walk through every peer somebody
        // glanced at before it finally left the page.
        { replace: true },
      );
    },
    [setParams],
  );

  const value = useMemo(() => ({ instance, base: apiBase(instance), select }), [instance, select]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
