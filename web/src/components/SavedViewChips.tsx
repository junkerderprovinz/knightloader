// The saved views of one list, as a row of chips over it.
//
// A view is the search and the filters somebody had set, under a name they
// chose. The chips are how those come back: one click puts the list back the
// way it was, and clicking the lit one puts the whole list back.
//
// It goes in the action row the page already has, on the free side of the
// spacer that row already carries, rather than in a row of its own. That is
// not tidiness: components/TaskList.tsx's row window measures its viewport off
// the row strip's own rectangle and has to re-measure whenever the toolbar
// above it grows a line, and Collector.tsx carries two long comments about a
// row above the list costing a flex gap even when it renders nothing. The
// existing row already wraps, so a variable number of chips costs neither.
import { useCallback, useMemo, useRef, useState } from 'react';
import { useT } from '../lib/i18n';
import { IconBadge, InfoBubble } from './ui';
import { Tabs } from './Tabs';
import { ContextMenu, anchorBelow, useContextMenu, type MenuItem } from './ContextMenu';
import { ViewDeleteDialog, ViewNameDialog } from './SavedViewDialog';
import { MAX_VIEWS, useSavedViews } from '../lib/savedViews';
import {
  NO_NARROWING,
  sameNarrowing,
  type ListProfileKey,
  type Narrowing,
  type SavedView,
} from '../lib/listNarrowing';
import type { QuickFilterId } from './ListToolbar';
import { IconEdit, IconPin, IconTrash } from '../lib/icons';

/** Which window is open over the chips, if any. */
type Open = { kind: 'save' } | { kind: 'rename'; view: SavedView } | { kind: 'delete'; view: SavedView };

export function SavedViewChips({
  profile,
  allowed,
  narrowing,
  onApply,
}: {
  profile: ListProfileKey;
  /** The profile's own quick-filter ids. See useSavedViews for what it does with them. */
  allowed: readonly QuickFilterId[];
  /** What is narrowing the list right now, which is what a save captures. */
  narrowing: Narrowing;
  onApply: (next: Narrowing) => void;
}) {
  const { t } = useT();
  const { views, save, rename, remove, matching } = useSavedViews(profile, allowed);
  const menu = useContextMenu();
  const [open, setOpen] = useState<Open | null>(null);
  const [refusal, setRefusal] = useState('');
  const row = useRef<HTMLDivElement>(null);

  // Which chip is lit is worked out from the list's current state every time,
  // never stored. An "active view id" in the document would start lying the
  // moment somebody nudged one filter, and it would go on claiming a view is in
  // force while the list showed something else.
  const lit = useMemo(() => {
    const id = matching(narrowing);
    return new Set(id ? [id] : []);
  }, [matching, narrowing]);
  const narrowed = !sameNarrowing(narrowing, NO_NARROWING);

  const apply = useCallback(
    (next: Narrowing) => {
      onApply(next);
      // TRAP: the list can go from five thousand rows to twelve while somebody
      // is scrolled three thousand pixels down it. The row window recovers on
      // its own (it re-measures after every commit), but the SCROLL POSITION
      // does not, so what they get is a blank page under a list that has
      // already redrawn. Pulling this row back into view brings the top of the
      // list with it. `nearest` so it does nothing at all when the row is
      // already on screen, which is the collector's normal case.
      row.current?.scrollIntoView({ block: 'nearest' });
    },
    [onApply],
  );

  function openMenu(el: HTMLButtonElement | null): void {
    menu.openAt(anchorBelow(el));
  }

  // One entry per view, each opening a submenu of the two things that can be
  // done to it. MenuItem.submenu is built for exactly this, and it keeps a
  // dozen views from becoming two dozen menu rows.
  const items: MenuItem[] = views.map((v) => ({
    id: v.id,
    label: v.name,
    submenu: [
      {
        id: 'view',
        items: [
          {
            id: `${v.id}:rename`,
            label: t('views.rename'),
            icon: <IconEdit />,
            onSelect: () => {
              setRefusal('');
              setOpen({ kind: 'rename', view: v });
            },
          },
          {
            id: `${v.id}:delete`,
            label: t('views.delete'),
            icon: <IconTrash />,
            danger: true,
            onSelect: () => setOpen({ kind: 'delete', view: v }),
          },
        ],
      },
    ],
  }));

  /** The names already spoken for, so the save window can offer to overwrite. */
  const taken = (except?: string) =>
    new Set(views.filter((v) => v.id !== except).map((v) => v.name.toLowerCase()));

  function confirmSave(name: string): void {
    const r = save(name, narrowing);
    if (r.ok) {
      setOpen(null);
      setRefusal('');
      return;
    }
    // Refused, and the window stays open saying why: a view that silently
    // failed to save is one somebody goes looking for tomorrow.
    setRefusal(r.reason === 'full' ? t('views.full', { n: MAX_VIEWS }) : t('views.tooBig'));
  }

  // Nothing saved and nothing to save: render NOTHING, not an empty box. An
  // element with no contents is still a flex child of the row above, and it
  // would cost that row one of its own gaps for as long as this list has never
  // been narrowed. Collector.tsx carries two long comments about exactly that
  // phantom gap, and this is the same trap one level down.
  if (views.length === 0 && !narrowed) return null;

  return (
    <div ref={row} className="flex flex-wrap items-center gap-2">
      {views.length > 0 && (
        // select="many" over a set that holds at most one id, deliberately not
        // select="one". It gives un-apply for free (clicking the lit chip puts
        // the whole list back), and it means there is never a chip standing for
        // "no view at all", which would be a control whose label could only be
        // a word like "none". Tabs renders role="group" for many, which is the
        // honest role for a row of independent toggles.
        <Tabs
          select="many"
          size="sm"
          label={t('views.label')}
          active={lit}
          items={views.map((v) => ({ id: v.id, label: v.name, title: v.name }))}
          onSelect={(id) => {
            const view = views.find((v) => v.id === id);
            if (!view) return;
            apply(lit.has(id) ? NO_NARROWING : view.state);
          }}
        />
      )}

      {/* Only while there is something to save. The row already follows that
          rule for every other verb on it: a badge that can do nothing is a
          badge to read past on the way to the ones that can. */}
      {narrowed && (
        <IconBadge
          labelled
          hue={3}
          icon={<IconPin width={16} height={16} />}
          title={t('views.save')}
          aria-label={t('views.save')}
          disabled={views.length >= MAX_VIEWS}
          onClick={() => {
            setRefusal('');
            setOpen({ kind: 'save' });
          }}
        />
      )}
      {narrowed && views.length >= MAX_VIEWS && <InfoBubble tip={t('views.full', { n: MAX_VIEWS })} />}

      {views.length > 0 && (
        <IconBadge
          labelled
          hue={4}
          icon={<IconEdit width={16} height={16} />}
          title={t('views.manage')}
          aria-label={t('views.manage')}
          onClick={(e) => openMenu(e.currentTarget)}
        />
      )}

      {menu.anchor && (
        <ContextMenu
          anchor={menu.anchor}
          label={t('views.manage')}
          onClose={menu.close}
          groups={[{ id: 'views', items }]}
        />
      )}

      {open?.kind === 'save' && (
        <ViewNameDialog
          title={t('views.saveTitle')}
          initial=""
          taken={taken()}
          duplicates="overwrite"
          refusal={refusal}
          onConfirm={confirmSave}
          onClose={() => setOpen(null)}
        />
      )}
      {open?.kind === 'rename' && (
        <ViewNameDialog
          title={t('views.renameTitle')}
          initial={open.view.name}
          taken={taken(open.view.id)}
          duplicates="reject"
          onConfirm={(name) => {
            rename(open.view.id, name);
            setOpen(null);
          }}
          onClose={() => setOpen(null)}
        />
      )}
      {open?.kind === 'delete' && (
        <ViewDeleteDialog
          view={open.view}
          onConfirm={() => {
            remove(open.view.id);
            setOpen(null);
          }}
          onClose={() => setOpen(null)}
        />
      )}
    </div>
  );
}
