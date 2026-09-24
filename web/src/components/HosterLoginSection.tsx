// Hoster logins for the headless JD sidecar (see internal/hosterauth). Saving
// writes the credential into JD's account config through its Remote API, and
// JD's hoster plugin performs the login; JD's own UI is never shown. The table
// is AccountTable, shared with the debrid card; this file adds the JD status
// badge and the host picker.
import { useCallback, useEffect, useState } from 'react';
import {
  type HosterHost,
  type HosterLogin,
  fetchHosterHosts,
  fetchHosterLogins,
  removeHosterLogin,
  saveHosterLogin,
  setHosterLoginEnabled,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { Button, EmptyState, Field, InfoBubble, Modal, TextInput } from './ui';
import { AccountTable, type AccountRow } from './AccountTable';
import { IconAccounts, IconChevronStart, IconClose, IconPlus, IconSearch, IconTrash } from '../lib/icons';
import { HosterIcon } from './HosterIcon';

// Faster than the 30s account health poll, since a new login moves from queued
// to active or rejected within seconds to minutes.
const POLL_MS = 8000;

type Dialog = { mode: 'new' } | { mode: 'edit'; login: HosterLogin };

/** HosterLogins is the logins and the host list, loaded once for both cards. */
export interface HosterLogins {
  logins: HosterLogin[] | null;
  hosts: HosterHost[];
  load: () => Promise<void>;
  toggle: (row: HosterLogin, enabled: boolean) => Promise<void>;
  remove: (host: string) => Promise<void>;
}

/**
 * useHosterLogins polls the logins for the accounts page. onEnabledHosts hears
 * the switched-on hosts after every load, since each is a row on the priority
 * card.
 */
export function useHosterLogins(onEnabledHosts?: (hosts: string) => void): HosterLogins {
  const { t } = useT();
  const { toast } = useToast();
  const [logins, setLogins] = useState<HosterLogin[] | null>(null);
  const [hosts, setHosts] = useState<HosterHost[]>([]);

  const load = useCallback(async () => {
    try {
      const rows = await fetchHosterLogins();
      setLogins(rows);
      onEnabledHosts?.(
        rows
          .filter((r) => r.enabled)
          .map((r) => r.host)
          .sort()
          .join(','),
      );
    } catch {
      // A missed poll keeps the previous rows.
    }
  }, [onEnabledHosts]);

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), POLL_MS);
    return () => window.clearInterval(timer);
  }, [load]);

  useEffect(() => {
    void fetchHosterHosts()
      .then(setHosts)
      .catch(() => {});
  }, []);

  async function toggle(row: HosterLogin, enabled: boolean) {
    // Optimistic; the reload corrects the row once JD has reconciled.
    setLogins((cur) => cur?.map((x) => (x.host === row.host ? { ...x, enabled } : x)) ?? cur);
    try {
      await setHosterLoginEnabled(row.host, enabled);
    } catch {
      toast(t('common.loadFailed'), 'fail');
    }
    await load();
  }

  async function remove(host: string) {
    try {
      await removeHosterLogin(host);
      toast(t('accounts.hoster.removed'), 'info');
      await load();
    } catch {
      toast(t('common.loadFailed'), 'fail');
    }
  }

  return { logins, hosts, load, toggle, remove };
}

/**
 * hosterLoginRow is one login as an AccountTable row, for this card and for
 * the multihosters on the debrid card.
 */
export function hosterLoginRow(
  row: HosterLogin,
  actions: { onToggle: (v: boolean) => void; onEdit: () => void; onRemove: () => void },
  via?: string,
): AccountRow {
  return {
    key: row.host,
    iconHost: row.host,
    label: row.host,
    via,
    enabled: row.enabled,
    status: <HosterLoginStatusBadge login={row} />,
    tier: row.tier,
    expiry: row.expiry,
    // JD reports bytes left and max; without a max the row shows a dash.
    traffic: { used: Math.max(0, (row.trafficMax ?? 0) - (row.trafficLeft ?? 0)), limit: row.trafficMax ?? 0 },
    ...actions,
  };
}

/** HosterLoginSection is the hoster card: every login except the multihosters. */
export function HosterLoginSection({ data }: { data: HosterLogins }) {
  const { t } = useT();
  const [dialog, setDialog] = useState<Dialog | null>(null);
  // The login awaiting removal confirmation.
  const [confirming, setConfirming] = useState<HosterLogin | null>(null);

  const rows = (data.logins ?? []).filter((l) => !l.multihoster);
  const hosts = data.hosts.filter((h) => !h.multihoster);
  const hasRows = rows.length > 0;

  return (
    <div className="flex flex-col gap-3">
      {hasRows && (
        <AccountTable
          label={t('accounts.hoster.title')}
          rows={rows.map((row) =>
            hosterLoginRow(row, {
              onToggle: (v) => void data.toggle(row, v),
              onEdit: () => setDialog({ mode: 'edit', login: row }),
              onRemove: () => setConfirming(row),
            }),
          )}
        />
      )}

      {hasRows ? (
        <Button
          kind="secondary"
          hue={1}
          icon={<IconPlus width={16} height={16} />}
          className="self-start"
          onClick={() => setDialog({ mode: 'new' })}
        >
          {t('accounts.newAccount')}
        </Button>
      ) : (
        <EmptyState
          nested
          icon={<IconAccounts width={26} height={26} />}
          title={t('accounts.hoster.empty')}
          action={
            <Button kind="secondary" hue={1} icon={<IconPlus width={16} height={16} />} onClick={() => setDialog({ mode: 'new' })}>
              {t('accounts.newAccount')}
            </Button>
          }
        />
      )}

      {dialog && (
        <HosterLoginDialog
          hosts={hosts}
          existing={data.logins ?? []}
          editing={dialog.mode === 'edit' ? dialog.login : undefined}
          hue={1}
          onClose={() => setDialog(null)}
          onSaved={data.load}
        />
      )}

      {confirming && (
        <ConfirmRemoveLogin
          login={confirming}
          hue={1}
          onCancel={() => setConfirming(null)}
          onConfirm={() => {
            setConfirming(null);
            void data.remove(confirming.host);
          }}
        />
      )}
    </div>
  );
}

/** ConfirmRemoveLogin asks first, since the stored password cannot be read back. */
export function ConfirmRemoveLogin({
  login,
  hue,
  onCancel,
  onConfirm,
}: {
  login: HosterLogin;
  /** The palette position of the card the login is listed on. */
  hue: number;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useT();
  return (
    <Modal
      title={t('accounts.remove')}
      hue={hue}
      onClose={onCancel}
      footer={
        <>
          {/* Matches the debrid card's confirmation footer. */}
          <span className="flex-1" />
          <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onCancel} />
          <Button kind="ghost" icon={<IconTrash width={16} height={16} />} onClick={onConfirm}>
            {t('accounts.remove')}
          </Button>
        </>
      }
    >
      <p className="text-sm text-carbon-text">{t('accounts.removeConfirm', { name: login.host })}</p>
    </Modal>
  );
}

function HosterLoginStatusBadge({ login }: { login: HosterLogin }) {
  const { t } = useT();
  switch (login.status) {
    case 'off':
      // Not "queued": JD does not have a switched-off login at all.
      return (
        <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-carbon-textMuted">
          <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-carbon-textMuted" />
          {t('accounts.hoster.status.off')}
        </span>
      );
    case 'active':
      return (
        <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-statusOk">
          <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusOkSolid" />
          {t('accounts.hoster.status.active')}
        </span>
      );
    case 'rejected':
      return (
        <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-statusFail">
          <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusFailSolid" />
          {t('accounts.hoster.status.rejected')}
          {login.detail && <InfoBubble tip={login.detail} />}
        </span>
      );
    default:
      // 'queued' covers both "not yet confirmed" and "not yet validated" by JD;
      // the detail text says which.
      return (
        <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-statusNeutral">
          <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusNeutralSolid" />
          {t('accounts.hoster.status.queued')}
          {login.detail && <InfoBubble tip={login.detail} />}
        </span>
      );
  }
}

// accounts.Redacted: sent back unchanged, it keeps the stored password.
const REDACTED = '********';

/**
 * HosterLoginDialog stores one login. `initial` opens it on a host already
 * picked elsewhere, as the debrid card does for a multihoster.
 */
export function HosterLoginDialog({
  hosts,
  existing,
  editing,
  initial,
  hue,
  onClose,
  onSaved,
}: {
  hosts: HosterHost[];
  existing: HosterLogin[];
  editing?: HosterLogin;
  initial?: HosterHost;
  /** The palette position of the card the window was opened from. */
  hue: number;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [query, setQuery] = useState('');
  const [picked, setPicked] = useState<HosterHost | null>(
    editing ? { id: editing.host, label: editing.host } : (initial ?? null),
  );
  const [username, setUsername] = useState(editing?.username ?? '');
  const [password, setPassword] = useState(editing ? REDACTED : '');
  const [saving, setSaving] = useState(false);

  const configured = new Set(existing.map((e) => e.host));
  const filtered = hosts.filter((h) => !configured.has(h.id) && h.label.toLowerCase().includes(query.trim().toLowerCase()));

  async function doSave() {
    if (!picked) return;
    setSaving(true);
    try {
      await saveHosterLogin(picked.id, username, password);
      toast(t('accounts.saved'), 'ok');
      await onSaved();
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'fail');
    } finally {
      setSaving(false);
    }
  }

  // The same titles as the debrid card's window, with this card's own noun.
  const title = !picked
    ? t('accounts.hoster.pickAccount')
    : editing
      ? t('accounts.editCredentialTitle', { service: picked.label })
      : t('accounts.addAccountTitle', { service: picked.label });
  const canSave = username.trim() !== '' && password.trim() !== '';

  return (
    <Modal
      title={title}
      hue={hue}
      onClose={onClose}
      footer={
        picked ? (
          <>
            <span className="flex-1" />
            <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
            <Button onClick={() => void doSave()} disabled={saving || !canSave}>
              {saving ? t('accounts.saving') : t('accounts.save')}
            </Button>
          </>
        ) : (
          <>
            <span className="flex-1" />
            <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          </>
        )
      }
    >
      {!picked ? (
        <div className="flex flex-col gap-3">
          <div className="flex h-[var(--btn-h)] items-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 px-3">
            <IconSearch width={15} height={15} className="shrink-0 text-carbon-textMuted" />
            <input
              autoFocus
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('accounts.hoster.searchHosts')}
              aria-label={t('accounts.hoster.searchHosts')}
              className="min-w-0 flex-1 bg-transparent text-sm text-carbon-text placeholder:text-carbon-textMuted outline-none"
            />
          </div>
          <div className="flex max-h-72 flex-col gap-1 overflow-y-auto">
            {filtered.length === 0 && (
              <p className="px-2 py-3 text-center text-sm text-carbon-textMuted">{t('accounts.noServicesFound')}</p>
            )}
            {filtered.map((h) => (
              <button
                key={h.id}
                type="button"
                onClick={() => setPicked(h)}
                className="flex items-center gap-3 rounded-[var(--radius-control)] px-3 py-2 text-start hover:bg-carbon-hover"
              >
                {/* Lazy, so only rows scrolled into view fetch their icon. */}
                <HosterIcon host={h.id} />
                <span className="text-sm text-carbon-text">{h.label}</span>
              </button>
            ))}
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          {/* Only while adding from this list; an edit stays on its host. */}
          {!editing && !initial && (
            <Button
              kind="secondary"
              labelled
              icon={<IconChevronStart className="rtl:-scale-x-100" />}
              title={t('accounts.changeAccount')}
              onClick={() => setPicked(null)}
              className="self-start"
            />
          )}

          <Field label={t('accounts.usernameField')}>
            <TextInput autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
          </Field>
          <Field label={t('accounts.passwordField')}>
            <TextInput type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>

          {/* In the body rather than an (i): the password is about to be handed
              to the JD sidecar, and that must be seen before saving. */}
          <p className="rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-xs text-carbon-textSub">
            {t('accounts.hoster.custodyNotice')}
          </p>
        </div>
      )}
    </Modal>
  );
}
