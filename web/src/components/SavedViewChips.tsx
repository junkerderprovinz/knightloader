// A list's saved views as chips: a view is a named search and filter set, one
// click applies it and clicking the lit chip clears it. The chips sit in the
// page's existing action row, since a row of their own would move the list's
// row window.
import { useCallback, useMemo, useRef, useState } from 'react';
import { useT } from '../lib/i18n';
import { IconBadge } from './ui';
import { FoldChip, Tabs } from './Tabs';
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
import { IconPencil, IconPin, IconTrash } from '../lib/icons';

type Open = { kind: 'save' } | { kind: 'rename'; view: SavedView } | { kind: 'delete'; view: SavedView };

export function SavedViewChips({
  profile,
  allowed,
  narrowing,
  onApply,
  glyphs = false,
  folded = false,
}: {
  profile: ListProfileKey;
  /** The profile's quick-filter ids, passed to useSavedViews. */
  allowed: readonly QuickFilterId[];
  /** The list's current narrowing, which a save captures. */
  narrowing: Narrowing;
  onApply: (next: Narrowing) => void;
  /** Save and manage as glyphs, for a row short of room for their words. */
  glyphs?: boolean;
  /** Everything in one FoldChip, for a row shorter still. */
  folded?: boolean;
}) {
  const { t } = useT();
  const { views, save, rename, remove, matching } = useSavedViews(profile, allowed);
  const menu = useContextMenu();
  const [open, setOpen] = useState<Open | null>(null);
  const [refusal, setRefusal] = useState('');
  const row = useRef<HTMLDivElement>(null);

  // Derived rather than stored, so nudging one filter unlights the chip.
  const lit = useMemo(() => {
    const id = matching(narrowing);
    return new Set(id ? [id] : []);
  }, [matching, narrowing]);
  const narrowed = !sameNarrowing(narrowing, NO_NARROWING);

  const apply = useCallback(
    (next: Narrowing) => {
      onApply(next);
      // A list that shrinks while scrolled far down would leave a blank page,
      // so the chips' row is brought back into view with the list's top.
      row.current?.scrollIntoView({ block: 'nearest' });
    },
    [onApply],
  );

  function openMenu(el: HTMLButtonElement | null): void {
    menu.openAt(anchorBelow(el));
  }

  // One entry per view, each with a rename and delete submenu.
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
            icon: <IconPencil />,
            onSelect: () => {
              setRefusal('');
              setOpen({ kind: 'rename', view: v });
            },
          },
          {
            id: `${v.id}:delete`,
            label: t('views.delete'),
            icon: <IconTrash />,
            onSelect: () => setOpen({ kind: 'delete', view: v }),
          },
        ],
      },
    ],
  }));

  const taken = (except?: string) =>
    new Set(views.filter((v) => v.id !== except).map((v) => v.name.toLowerCase()));

  function confirmSave(name: string): void {
    const r = save(name, narrowing);
    if (r.ok) {
      setOpen(null);
      setRefusal('');
      return;
    }
    // The window stays open and says why.
    setRefusal(r.reason === 'full' ? t('views.full', { n: MAX_VIEWS }) : t('views.tooBig'));
  }

  // select="many" with at most one lit id: clicking the lit chip clears it, and
  // no "none" chip is needed.
  function select(id: string): void {
    const view = views.find((v) => v.id === id);
    if (!view) return;
    apply(lit.has(id) ? NO_NARROWING : view.state);
  }

  function startSave(): void {
    setRefusal('');
    setOpen({ kind: 'save' });
  }

  // An empty element would still cost the parent row a flex gap.
  if (views.length === 0 && !narrowed) return null;

  const full = views.length >= MAX_VIEWS;
  return (
    <div ref={row} className="flex items-center gap-2">
      {folded ? (
        <FoldChip
          label={t('views.label')}
          icon={<IconPin />}
          glyph
          lit={lit.size > 0}
          groups={[
            {
              id: 'views',
              items: views.map((v) => ({
                id: v.id,
                label: v.name,
                checked: lit.has(v.id),
                onSelect: () => select(v.id),
              })),
            },
            {
              id: 'edit',
              items: [
                ...(narrowed
                  ? [{ id: 'save', label: t('views.save'), icon: <IconPin />, disabled: full, onSelect: startSave }]
                  : []),
                ...(views.length > 0
                  ? [{ id: 'manage', label: t('views.manage'), icon: <IconPencil />, submenu: [{ id: 'views', items }] }]
                  : []),
              ],
            },
          ]}
        />
      ) : (
        <>
          {views.length > 0 && (
            // A long name is cut short on its chip and read in full in its bubble.
            <Tabs
              inline
              select="many"
              size="sm"
              className="[&>button]:max-w-56"
              label={t('views.label')}
              active={lit}
              items={views.map((v) => ({ id: v.id, label: v.name, title: v.name }))}
              onSelect={select}
            />
          )}

          {narrowed && (
            <IconBadge
              labelled={!glyphs}
              hue={3}
              icon={<IconPin width={16} height={16} />}
              title={t('views.save')}
              aria-label={t('views.save')}
              disabled={full}
              hint={full ? t('views.full', { n: MAX_VIEWS }) : undefined}
              onClick={startSave}
            />
          )}

          {views.length > 0 && (
            <IconBadge
              labelled={!glyphs}
              hue={4}
              icon={<IconPencil width={16} height={16} />}
              title={t('views.manage')}
              aria-label={t('views.manage')}
              onClick={(e) => openMenu(e.currentTarget)}
            />
          )}
        </>
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
