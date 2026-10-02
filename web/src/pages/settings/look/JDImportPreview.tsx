import { useMemo, useState, type ReactNode } from 'react';
import { Button, Modal } from '../../../components/ui';
import { NeutralSwitch } from '../controls';
import { useT } from '../../../lib/i18n';
import { IconClose } from '../../../lib/icons';
import { en, type TranslationKey } from '../../../lib/locales/en';
import type { JDImportItem, JDImportPreview, JDImportReason } from '../../../lib/api';

type T = ReturnType<typeof useT>['t'];

const GROUPS: JDImportItem['group'][] = ['accounts', 'settings', 'packagizer', 'filter', 'downloads'];

const GROUP_LABEL = {
  accounts: 'settings.jdimport.group.accounts',
  settings: 'settings.jdimport.group.settings',
  packagizer: 'settings.jdimport.group.packagizer',
  filter: 'settings.jdimport.group.filter',
  downloads: 'settings.jdimport.group.downloads',
} as const;

/**
 * reasonText words a reason the server gave. A code this build has no
 * sentence for shows the server's English. The lists inside a reason, the
 * field a placeholder sits in and the actions a rule loses, are translated
 * one name at a time.
 */
export function reasonText(t: T, r: JDImportReason): string {
  const key = `settings.jdimport.reason.${r.code}`;
  if (!(key in en)) return r.text;
  const params: Record<string, string> = { ...r.params };
  if (params.field) params.field = lookup(t, `settings.jdimport.field.${params.field}`, params.field);
  if (params.actions) {
    params.actions = params.actions
      .split(',')
      .map((a) => lookup(t, `settings.jdimport.action.${a}`, a))
      .join(', ');
  }
  if (r.code === 'ruleBuiltinPackageFolder') params.switch = t('settings.subfolderByPackage');
  return t(key as TranslationKey, params);
}

/** itemName is how a row names its item; the password row has no name of its own. */
export function itemName(t: T, it: { kind: string; name: string; count?: number; total?: number }): string {
  if (it.kind === 'passwords') {
    return t('settings.jdimport.passwords', { n: it.total ?? 0, fresh: it.count ?? 0 });
  }
  return it.name;
}

function lookup(t: T, key: string, fallback: string): string {
  return key in en ? t(key as TranslationKey) : fallback;
}

/**
 * JDImportPreviewDialog lists what a JDownloader folder would bring, grouped,
 * one switch per item. What cannot come over is listed too, switched off, with
 * the reason under it, so nothing goes missing without a word.
 */
export function JDImportPreviewDialog({
  preview,
  busy,
  error,
  onApply,
  onClose,
}: {
  preview: JDImportPreview;
  busy: boolean;
  /** The refusal, already translated by the caller. */
  error: string;
  onApply: (ids: string[]) => void;
  onClose: () => void;
}) {
  const { t } = useT();
  const selectable = useMemo(() => preview.items.filter((it) => !it.blocked && !it.same), [preview]);
  const [picked, setPicked] = useState<Set<string>>(() => new Set(preview.items.filter((it) => it.ticked).map((it) => it.id)));

  const toggle = (id: string, on: boolean) =>
    setPicked((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });

  return (
    <Modal
      title={t('settings.jdimport.previewTitle')}
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
        {preview.files.length > 0 && (
          <span className="break-all text-[11px] text-carbon-textMuted">
            {t('settings.jdimport.previewFiles', { files: preview.files.join(', ') })}
          </span>
        )}
        {preview.problems.map((p, i) => (
          <span key={i} className="text-xs text-statusWarn">
            {reasonText(t, p)}
          </span>
        ))}

        <div className="flex flex-wrap items-center gap-2">
          <Button kind="ghost" onClick={() => setPicked(new Set(selectable.map((it) => it.id)))} disabled={busy}>
            {t('settings.transfer.selectAll')}
          </Button>
          <Button kind="ghost" onClick={() => setPicked(new Set())} disabled={busy}>
            {t('settings.transfer.selectNone')}
          </Button>
        </div>

        {picked.size === 0 && (
          <span className="text-[11px] text-carbon-textMuted">{t('settings.transfer.nothingSelected')}</span>
        )}
        {error && <span className="text-xs text-statusFail">{error}</span>}

        {/* Its own scroller keeps the footer's Cancel on screen. */}
        <div className="flex max-h-[52vh] min-w-0 flex-col gap-4 overflow-y-auto pe-1">
          {GROUPS.map((group) => {
            const rows = preview.items.filter((it) => it.group === group);
            if (rows.length === 0) return null;
            return (
              <div key={group} className="flex min-w-0 flex-col gap-2">
                <span className="text-[11px] font-medium uppercase tracking-[1px] text-carbon-textMuted">
                  {t(GROUP_LABEL[group])}
                </span>
                {group === 'downloads' && (
                  <span className="text-[11px] text-carbon-textSub">{t('settings.jdimport.downloadsHint')}</span>
                )}
                {rows.map((it, i) => (
                  <Row
                    key={it.id}
                    item={it}
                    hue={i}
                    on={picked.has(it.id)}
                    busy={busy}
                    onChange={(next) => toggle(it.id, next)}
                  />
                ))}
              </div>
            );
          })}
        </div>
      </div>
    </Modal>
  );
}

function Row({
  item,
  hue,
  on,
  busy,
  onChange,
}: {
  item: JDImportItem;
  hue: number;
  on: boolean;
  busy: boolean;
  onChange: (next: boolean) => void;
}) {
  const { t } = useT();
  const blocked = !!item.blocked || !!item.same;
  // Dimmed part by part, so the reason under a blocked row stays readable.
  const dim = blocked ? 'opacity-60' : '';
  const kind =
    item.kind === 'hoster'
      ? t('settings.jdimport.kind.hoster')
      : item.kind === 'debrid'
        ? t('settings.jdimport.kind.debrid')
        : item.kind === 'exception'
          ? t('settings.jdimport.kind.exception')
          : '';
  return (
    <div className="flex min-w-0 items-start gap-2.5">
      <NeutralSwitch on={on && !blocked} onChange={onChange} name={item.id} disabled={blocked || busy} hue={hue} />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex flex-wrap items-center gap-1.5 text-xs text-carbon-text">
          <span className={`break-all ${item.kind === 'folder' ? 'font-mono' : ''} ${dim}`}>{itemName(t, item)}</span>
          {kind && <Badge dim={dim}>{kind}</Badge>}
          {item.same && <Badge dim={dim}>{t('settings.jdimport.badge.same')}</Badge>}
          {item.blocked && <Badge warn>{t('settings.jdimport.badge.blocked')}</Badge>}
          {item.off && !blocked && <Badge>{t('settings.jdimport.badge.off')}</Badge>}
          {item.replaces && !blocked && <Badge warn>{t('settings.jdimport.badge.replaces')}</Badge>}
        </span>
        {item.kind === 'folder' ? (
          <Sub>
            {item.detail
              ? t('settings.jdimport.folderStored', { dir: item.detail })
              : t('settings.jdimport.folderDefault')}
          </Sub>
        ) : (
          item.detail && <Sub>{item.detail}</Sub>
        )}
        {item.kind === 'package' && !item.blocked && <Sub>{t('settings.jdimport.links', { n: item.count ?? 0 })}</Sub>}
        {item.slot && <Sub>{t('settings.jdimport.slot', { slot: item.slot })}</Sub>}
        {item.blocked && <Sub>{reasonText(t, item.blocked)}</Sub>}
        {item.notes?.map((n, i) => (
          <Sub key={i}>{reasonText(t, n)}</Sub>
        ))}
      </div>
    </div>
  );
}

function Sub({ children }: { children: ReactNode }) {
  return <span className="min-w-0 break-words text-[11px] text-carbon-textMuted">{children}</span>;
}

/** Badge is a short marker beside the name, not a status badge. */
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
