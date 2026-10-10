// A page's own actions, floating at the foot of its column (GlimStone, "Page
// actions"): the action the page exists for stands in the same corner on every
// page, flush with the end edge of the cards, last in its row and filled. The
// frame that scrolls the page holds a slot there and the page fills it, so the
// actions stay put while the page scrolls under them and stand above the
// phone's bottom bar, which is below that frame.
import { createContext, useContext, type ReactNode, type Ref } from 'react';
import { createPortal } from 'react-dom';
import { usePhoneLayout } from '../lib/phoneLayout';
import { ContextMenu, anchorAbove, useContextMenu, type MenuGroup } from './ContextMenu';
import { Button } from './ui';

// Undefined outside a frame; null while a frame's slot is not mounted yet.
const SlotContext = createContext<HTMLElement | null | undefined>(undefined);

/** PageActionsProvider hands the pages inside it the slot their actions go into. */
export function PageActionsProvider({ slot, children }: { slot: HTMLElement | null; children: ReactNode }) {
  return <SlotContext.Provider value={slot}>{children}</SlotContext.Provider>;
}

/**
 * PageActionsSlot is where a frame's floating actions stand: the last child of
 * the column that scrolls, so it ends where the cards end, scrollbar included.
 * It sticks to the foot of the column, and index.css keeps the room under the
 * page's last row while it is filled.
 *
 * The frame gives it its height: none where the page reaches the foot of the
 * column by itself, or the height that is left where it does not.
 */
export function PageActionsSlot({ ref, className }: { ref: Ref<HTMLDivElement>; className: string }) {
  return (
    <div
      ref={ref}
      // The slot lies over the page's last rows while it scrolls, so only the
      // actions themselves take the pointer.
      className={`glim-page-actions pointer-events-none sticky bottom-0 z-30 flex shrink-0 items-end justify-end ${className}`}
    />
  );
}

/**
 * PageActions is a page's row of floating actions. The primary one comes last
 * in the row, so it ends the row as the advancing button does everywhere else.
 * A page drawn outside a frame with a slot shows the row in place.
 */
export function PageActions({ children }: { children: ReactNode }) {
  const slot = useContext(SlotContext);
  const row = (
    <div data-new="page-actions" className="flex items-center gap-2.5">
      {children}
    </div>
  );
  if (slot === undefined) return <div className="flex justify-end">{row}</div>;
  return slot && createPortal(row, slot);
}

/**
 * PageAction is one floating action at the key height. It follows the label
 * setting for buttons, and at phone width it shows its glyph alone. With
 * `menu` a press lists those choices over the button instead of acting, for a
 * page that adds more than one kind of thing.
 */
export function PageAction({
  icon,
  label,
  primary = false,
  hint,
  disabled,
  menu,
  onClick,
  'data-new': mark,
}: {
  icon: ReactNode;
  label: string;
  /** The page's one filled action. */
  primary?: boolean;
  hint?: string;
  disabled?: boolean;
  menu?: MenuGroup[];
  onClick?: () => void;
  /** The change this action is marked as after an update (lib/whatsNew.ts). */
  'data-new'?: string;
}) {
  const phone = usePhoneLayout();
  const choices = useContextMenu();
  return (
    <>
      <Button
        kind={primary ? 'primary' : 'secondary'}
        keyHeight
        labelled={!phone}
        icon={icon}
        title={label}
        hint={hint}
        disabled={disabled}
        aria-haspopup={menu ? 'menu' : undefined}
        aria-expanded={menu ? choices.anchor !== null : undefined}
        data-new={mark}
        className="glim-float pointer-events-auto"
        onClick={menu ? (e) => choices.openAt(anchorAbove(e.currentTarget)) : onClick}
      />
      {menu && choices.anchor && (
        <ContextMenu anchor={choices.anchor} label={label} groups={menu} onClose={choices.close} />
      )}
    </>
  );
}
