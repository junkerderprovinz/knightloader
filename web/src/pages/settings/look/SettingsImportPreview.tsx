import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { Button, InfoBubble, Modal } from '../../../components/ui';
import { NeutralSwitch } from '../controls';
import { useT } from '../../../lib/i18n';
import type { SettingsExportDoc } from '../../../lib/api';
import { TRANSFER_GROUPS, describe, type TransferGroup, type TransferRow } from '../../../lib/settingsTransfer';

/**
 * "What should be taken over?" - the whole feature, really.
 *
 * Nothing is written before this dialog has been seen and confirmed, and that is
 * the promise the card above it makes. So every reason a row might be a bad idea
 * has to be visible HERE, on the row, rather than in a paragraph above the list
 * that nobody reads:
 *
 *   - a list replaces the whole list on this box, row for row. "Merge" is per
 *     top-level key and not per row (settings.Store.SetPartial's own comment
 *     says so), which is usually what somebody moving boxes wants and is not
 *     what the word sounds like.
 *   - a folder that is not here yet gets CREATED on save, including inside a
 *     container with no matching volume mounted, and every download then lands
 *     in the container's own writable layer.
 *   - a row that arrives without its password saves, enables and then fails
 *     silently hours later.
 *   - a key this build does not have cannot be taken over at all, and would
 *     otherwise be dropped by encoding/json without a word.
 *
 * The identity rows are drawn DISABLED rather than hidden. A control that
 * vanishes teaches nobody, which is ToggleRow's own reasoning for having a
 * disabled state at all, and "why did my instance id not come across" is exactly
 * the question somebody asks a week later.
 */
export function SettingsImportPreview({
  doc,
  rows,
  busy,
  error,
  onApply,
  onClose,
}: {
  doc: SettingsExportDoc;
  rows: TransferRow[];
  busy: boolean;
  /** The refusal, already turned into a sentence in the reader's language by
   *  the caller - this dialog does no error mapping of its own. */
  error: string;
  onApply: (keys: string[]) => void;
  onClose: () => void;
}) {
  const { t } = useT();

  /** A row can only be taken over when this build has the key and the key is
   *  allowed to travel. Everything else is drawn and switched off. */
  const selectable = useMemo(() => rows.filter((r) => !r.identity && !r.unknown), [rows]);

  // Pre-selected: what actually differs, minus the folders. A folder that is not
  // here is the one row where ticking it by default would create a directory
  // nobody asked for - settings.Validate makes the folder it claims to be
  // checking, so the mistake is not recoverable by pressing Cancel afterwards.
  const [picked, setPicked] = useState<Set<string>>(
    () => new Set(selectable.filter((r) => !r.same && !r.path).map((r) => r.key)),
  );

  const newFolders = useNewFolders(rows);

  const toggle = (key: string, on: boolean) => {
    setPicked((prev) => {
      const next = new Set(prev);
      if (on) next.add(key);
      else next.delete(key);
      return next;
    });
  };

  const grouped = useMemo(() => {
    const out = new Map<TransferGroup, TransferRow[]>();
    for (const g of TRANSFER_GROUPS) {
      const inGroup = rows.filter((r) => r.group === g);
      if (inGroup.length > 0) out.set(g, inGroup);
    }
    return out;
  }, [rows]);

  const created = new Date(doc.createdAt);
  const when = Number.isNaN(created.getTime()) ? doc.createdAt : created.toLocaleString();

  return (
    <Modal
      title={t('settings.transfer.previewTitle')}
      onClose={() => (busy ? undefined : onClose())}
      footer={
        <>
          <span className="flex-1" />
          <Button kind="ghost" onClick={onClose} disabled={busy}>
            {t('settings.transfer.cancel')}
          </Button>
          <Button kind="primary" onClick={() => onApply([...picked])} disabled={busy || picked.size === 0}>
            {busy ? t('settings.transfer.applying') : t('settings.transfer.apply', { n: picked.size })}
          </Button>
        </>
      }
    >
      <div className="flex min-w-0 flex-col gap-3">
        <span className="text-[11px] text-carbon-textMuted">
          {t('settings.transfer.previewFrom', { version: doc.version, date: when })}
        </span>
        {doc.secrets === 'omitted' && (
          <span className="text-[11px] text-carbon-textSub">{t('settings.transfer.previewSecretless')}</span>
        )}

        <div className="flex flex-wrap items-center gap-2">
          <Button kind="ghost" onClick={() => setPicked(new Set(selectable.map((r) => r.key)))} disabled={busy}>
            {t('settings.transfer.selectAll')}
          </Button>
          <Button kind="ghost" onClick={() => setPicked(new Set())} disabled={busy}>
            {t('settings.transfer.selectNone')}
          </Button>
          <Button
            kind="ghost"
            onClick={() => setPicked(new Set(selectable.filter((r) => !r.same).map((r) => r.key)))}
            disabled={busy}
          >
            {t('settings.transfer.selectChanged')}
          </Button>
        </div>

        {picked.size === 0 && (
          <span className="text-[11px] text-carbon-textMuted">{t('settings.transfer.nothingSelected')}</span>
        )}
        {error && <span className="text-xs text-statusFail">{error}</span>}

        {/* Its own scroller. Eighty-seven rows do not fit a viewport, and a
            dialog whose footer has scrolled off the bottom of the screen is one
            with no way left to press Cancel. */}
        <div className="flex max-h-[52vh] min-w-0 flex-col gap-4 overflow-y-auto pr-1">
          {[...grouped].map(([group, groupRows]) => (
            <div key={group} className="flex min-w-0 flex-col gap-2">
              <span className="text-[11px] font-medium uppercase tracking-[1px] text-carbon-textMuted">
                {t(GROUP_LABEL[group])}
              </span>
              {groupRows.map((row, i) => (
                <Row
                  key={row.key}
                  row={row}
                  hue={i}
                  on={picked.has(row.key)}
                  busy={busy}
                  isNewFolder={newFolders.has(row.key)}
                  onChange={(next) => toggle(row.key, next)}
                />
              ))}
            </div>
          ))}
        </div>
      </div>
    </Modal>
  );
}

/**
 * The group headings, keyed by group.
 *
 * A plain span and deliberately not a SectionTitle: at most one SectionTitle
 * belongs to a Card, its badge is positioned against a card's own top edge, and
 * nine of them stacked inside a scrolling dialog would each try to sit on a card
 * boundary that is not there.
 */
const GROUP_LABEL = {
  queue: 'settings.transfer.group.queue',
  folders: 'settings.transfer.group.folders',
  archives: 'settings.transfer.group.archives',
  rules: 'settings.transfer.group.rules',
  schedule: 'settings.transfer.group.schedule',
  network: 'settings.transfer.group.network',
  resolvers: 'settings.transfer.group.resolvers',
  look: 'settings.transfer.group.look',
  other: 'settings.transfer.group.other',
} as const;

function Row({
  row,
  hue,
  on,
  busy,
  isNewFolder,
  onChange,
}: {
  row: TransferRow;
  hue: number;
  on: boolean;
  busy: boolean;
  isNewFolder: boolean;
  onChange: (next: boolean) => void;
}) {
  const { t } = useT();
  const blocked = row.identity || row.unknown;
  return (
    <div className={`flex min-w-0 items-start gap-2.5 ${blocked ? 'opacity-60' : ''}`}>
      <NeutralSwitch on={on && !blocked} onChange={onChange} name={row.key} disabled={blocked || busy} hue={hue} />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        {/* The RAW key, and that is a deliberate trade rather than an oversight.
            Labelling all ~87 top-level keys would be 87 strings in 42 languages,
            which is a translation project and not a row of a dialog.
            Advanced.tsx already sets the precedent and says why: a key path is
            an identifier. The group heading above and the two values below are
            what make the row readable. */}
        <span className="flex flex-wrap items-center gap-1.5 text-xs text-carbon-text">
          <span className="font-mono">{row.key}</span>
          {row.identity && (
            <>
              <Badge>{t('settings.transfer.identityKey')}</Badge>
              <InfoBubble tip={t('settings.transfer.identityKeyHint')} />
            </>
          )}
          {row.unknown && <Badge>{t('settings.transfer.unknownKey')}</Badge>}
          {row.same && !row.identity && <Badge>{t('settings.transfer.same')}</Badge>}
          {row.wholeList && <Badge>{t('settings.transfer.wholeList')}</Badge>}
          {row.secretless && <Badge warn>{t('settings.transfer.noPassword')}</Badge>}
          {isNewFolder && <Badge warn>{t('settings.transfer.newFolder')}</Badge>}
        </span>
        {/* Both sides, always, even when one of them is empty: "here now:
            nothing" is an answer, and leaving the line out would make an empty
            value indistinguishable from a row that failed to render. */}
        <span className="min-w-0 break-all text-[11px] text-carbon-textMuted">
          {t('settings.transfer.colStored')}: {describe(row.stored)}
        </span>
        <span className="min-w-0 break-all text-[11px] text-carbon-textSub">
          {t('settings.transfer.colFile')}: {describe(row.incoming)}
        </span>
      </div>
    </div>
  );
}

/** A short marker beside the key. Its own small span rather than one of the
 *  status badges in ui.tsx: those carry a task's state with them, and a row here
 *  is not in a state, it is being described. */
function Badge({ children, warn = false }: { children: ReactNode; warn?: boolean }) {
  return (
    <span
      className={`rounded-[var(--radius-pill)] px-1.5 py-px text-[10px] leading-[14px] ${
        warn ? 'bg-statusWarnBg text-statusWarn' : 'bg-carbon-surface3 text-carbon-textMuted'
      }`}
    >
      {children}
    </span>
  );
}

/**
 * Which of the folder-valued rows names a directory that is not on this machine.
 *
 * Asked of the server, once per differing folder, rather than guessed: the
 * browser has no idea what is mounted inside the container, and the whole reason
 * this warning exists is that settings.Validate does NOT fail on a missing
 * folder. It creates it, in whatever layer the path lands in, and on a container
 * with no matching bind mount that layer disappears on the next `docker rm`.
 *
 * GET /api/folders is the folder chooser's own route and already answers exactly
 * this question (`exists`), so nothing new had to be built and this cannot
 * disagree with what the chooser shows. A probe that fails for any OTHER reason
 * - no permission, a path this instance refuses to list - answers "no warning"
 * rather than a warning nobody can act on: being told a folder is new when the
 * real problem is a permission sends somebody to fix the wrong thing.
 */
function useNewFolders(rows: TransferRow[]): Set<string> {
  const [missing, setMissing] = useState<Set<string>>(new Set());
  // Serialised, so the effect depends on the CONTENT of the list rather than on
  // the array identity that every re-render rebuilds. JSON rather than a
  // separator character, because a folder name may contain any character
  // somebody would have picked as one.
  const probe = JSON.stringify(rows.filter((r) => r.path).map((r) => [r.key, String(r.incoming)]));

  useEffect(() => {
    const pairs = JSON.parse(probe) as [string, string][];
    if (pairs.length === 0) return;
    let live = true;
    Promise.all(
      pairs.map(async ([key, path]) => {
        try {
          const r = await fetch(`/api/folders?path=${encodeURIComponent(path)}`);
          if (!r.ok) return null;
          const listing = (await r.json()) as { exists?: boolean };
          return listing.exists === false ? key : null;
        } catch {
          return null;
        }
      }),
    ).then((found) => {
      if (live) setMissing(new Set(found.filter((k): k is string => k !== null)));
    });
    return () => {
      live = false;
    };
  }, [probe]);

  return missing;
}
