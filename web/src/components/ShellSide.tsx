// A card beside the shell bar's speed graph. The bar sits above the keyed page
// div so its controls keep their state across pages, which leaves the page no
// place in its row, so the bar holds a slot there and the page fills it.
import { createContext, useContext, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

// Undefined outside the shell; null while the bar's slot is not mounted yet.
const SlotContext = createContext<HTMLElement | null | undefined>(undefined);

/** ShellSideProvider hands the pages inside it the slot beside the shell bar. */
export function ShellSideProvider({ slot, children }: { slot: HTMLElement | null; children: ReactNode }) {
  return <SlotContext.Provider value={slot}>{children}</SlotContext.Provider>;
}

/** ShellSide shows its children beside the shell bar, or in place on a page drawn outside the shell. */
export function ShellSide({ children }: { children: ReactNode }) {
  const slot = useContext(SlotContext);
  if (slot === undefined) return children;
  return slot && createPortal(children, slot);
}
