import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { Button, InfoBubble, Modal } from '../../../components/ui';
import { NeutralSwitch } from '../controls';
import { fmtDate } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import { IconClose } from '../../../lib/icons';
import type { SettingsExportDoc } from '../../../lib/api';
import { TRANSFER_GROUPS, describe, type TransferGroup, type TransferRow } from '../../../lib/settingsTransfer';

/**
 * SettingsImportPreview asks which keys to take over before anything is
 * written, and marks on each row why it might be a bad idea: a list replaces
 * the whole list, a missing folder gets created on save, a row may arrive
 * without its password, and an unknown key cannot be taken over. Identity rows
 * are shown disabled so it is clear they do not travel.
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
  /** The refusal, already translated by the caller. */
  error: string;
  onApply: (keys: string[]) => void;
  onClose: () => void;
}) {
  const { t } = useT();

  // Only keys this build knows and that may travel can be taken over.
  const selectable = useMemo(() => rows.filter((r) => !r.identity && !r.unknown), [rows]);

  // Pre-selects what differs except folders, since settings.Validate creates a
  // folder that does not exist.
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
  const when = Number.isNaN(created.getTime()) ? doc.createdAt : fmtDate(doc.createdAt);

  return (
    <Modal
      title={t('settings.transfer.previewTitle')}
      onClose={() => (busy ? undefined : onClose())}
      footer={
        <>
          <span className="flex-1" />
          <Button
            kind="ghost"
            labelled
            icon={<IconClose />}
            title={t('settings.transfer.cancel')}
            onClick={onClose}
            disabled={busy}
          />
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

        {/* Its own scroller keeps the footer's Cancel on screen. */}
        <div className="flex max-h-[52vh] min-w-0 flex-col gap-4 overflow-y-auto pe-1">
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

/** Group headings, drawn as plain spans since a SectionTitle needs its own card. */
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
  // Dimmed part by part, because opacity on the row would also dim the (i)
  // that explains it.
  const dim = blocked ? 'opacity-60' : '';
  return (
    <div className="flex min-w-0 items-start gap-2.5">
      <NeutralSwitch on={on && !blocked} onChange={onChange} name={row.key} disabled={blocked || busy} hue={hue} />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        {/* The raw key, as on the Advanced page: it is an identifier. */}
        <span className="flex flex-wrap items-center gap-1.5 text-xs text-carbon-text">
          <span className={`font-mono ${dim}`}>{row.key}</span>
          {row.identity && (
            <>
              <Badge dim={dim}>{t('settings.transfer.identityKey')}</Badge>
              <InfoBubble tip={t('settings.transfer.identityKeyHint')} />
            </>
          )}
          {row.unknown && <Badge dim={dim}>{t('settings.transfer.unknownKey')}</Badge>}
          {row.same && !row.identity && <Badge dim={dim}>{t('settings.transfer.same')}</Badge>}
          {row.wholeList && <Badge dim={dim}>{t('settings.transfer.wholeList')}</Badge>}
          {row.secretless && <Badge warn dim={dim}>{t('settings.transfer.noPassword')}</Badge>}
          {isNewFolder && <Badge warn dim={dim}>{t('settings.transfer.newFolder')}</Badge>}
        </span>
        {/* Both sides, even when one is empty. */}
        <span className={`min-w-0 break-all text-[11px] text-carbon-textMuted ${dim}`}>
          {t('settings.transfer.colStored')}: {describe(row.stored)}
        </span>
        <span className={`min-w-0 break-all text-[11px] text-carbon-textSub ${dim}`}>
          {t('settings.transfer.colFile')}: {describe(row.incoming)}
        </span>
        {row.arrives !== row.incoming && (
          <span className={`flex min-w-0 flex-wrap items-center break-all text-[11px] text-carbon-text ${dim}`}>
            {t('settings.transfer.colArrives')}: {describe(row.arrives)}
            <InfoBubble tip={t('settings.transfer.colArrivesHint')} />
          </span>
        )}
      </div>
    </div>
  );
}

/** Badge is a short marker beside the key, not a status badge. */
function Badge({ children, warn = false, dim = '' }: { children: ReactNode; warn?: boolean; dim?: string }) {
  return (
    <span
      className={`rounded-[var(--radius-pill)] px-1.5 py-px text-[11px] leading-[14px] ${
        warn ? 'bg-statusWarnBg text-statusWarn' : 'bg-carbon-surface3 text-carbon-textMuted'
      } ${dim}`}
    >
      {children}
    </span>
  );
}

/**
 * useNewFolders asks the folder chooser's route which folder rows name a
 * directory missing on this machine, since settings.Validate would create it
 * wherever the path lands. A probe that fails for another reason gives no
 * warning.
 */
function useNewFolders(rows: TransferRow[]): Set<string> {
  const [missing, setMissing] = useState<Set<string>>(new Set());
  // Serialised so the effect follows the content, not the array identity.
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
