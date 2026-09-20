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
import { AccountTable } from './AccountTable';
import { IconAccounts, IconClose, IconPlus, IconSearch, IconTrash } from '../lib/icons';
import { HosterIcon } from './HosterIcon';

// Faster than the 30s account health poll, since a new login moves from queued
// to active or rejected within seconds to minutes.
const POLL_MS = 8000;

type Dialog = { mode: 'new' } | { mode: 'edit'; login: HosterLogin };

export function HosterLoginSection() {
  const { t } = useT();
  const { toast } = useToast();
  const [logins, setLogins] = useState<HosterLogin[] | null>(null);
  const [hosts, setHosts] = useState<HosterHost[]>([]);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  // The login awaiting removal confirmation.
  const [confirming, setConfirming] = useState<HosterLogin | null>(null);

  const load = useCallback(async () => {
    try {
      setLogins(await fetchHosterLogins());
    } catch {
      // A missed poll keeps the previous rows.
    }
  }, []);

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

  async function onToggle(row: HosterLogin, enabled: boolean) {
    // Optimistic; the reload corrects the row once JD has reconciled.
    setLogins((cur) => cur?.map((x) => (x.host === row.host ? { ...x, enabled } : x)) ?? cur);
    try {
      await setHosterLoginEnabled(row.host, enabled);
    } catch {
      toast(t('common.loadFailed'), 'fail');
    }
    await load();
  }

  // Confirmed first, since the stored password cannot be read back.
  async function doRemove(host: string) {
    setConfirming(null);
    try {
      await removeHosterLogin(host);
      toast(t('accounts.hoster.removed'), 'info');
      await load();
    } catch {
      toast(t('common.loadFailed'), 'fail');
    }
  }

  const hasRows = !!logins && logins.length > 0;

  return (
    <div className="flex flex-col gap-3">
      {hasRows && (
        <AccountTable
          label={t('accounts.hoster.title')}
          rows={(logins ?? []).map((row) => ({
            key: row.host,
            iconHost: row.host,
            label: row.host,
            enabled: row.enabled,
            status: <HosterLoginStatusBadge login={row} />,
            tier: row.tier,
            expiry: row.expiry,
            // JD reports bytes left and max; without a max the row shows a dash.
            traffic: { used: Math.max(0, (row.trafficMax ?? 0) - (row.trafficLeft ?? 0)), limit: row.trafficMax ?? 0 },
            onToggle: (v) => void onToggle(row, v),
            onEdit: () => setDialog({ mode: 'edit', login: row }),
            onRemove: () => setConfirming(row),
          }))}
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
          existing={logins ?? []}
          editing={dialog.mode === 'edit' ? dialog.login : undefined}
          onClose={() => setDialog(null)}
          onSaved={load}
        />
      )}

      {confirming && (
        <Modal
          title={t('accounts.remove')}
          onClose={() => setConfirming(null)}
          footer={
            <>
              {/* Matches the debrid card's confirmation footer. */}
              <span className="flex-1" />
              <Button kind="ghost" icon={<IconClose width={16} height={16} />} onClick={() => setConfirming(null)}>
                {t('common.cancel')}
              </Button>
              <Button
                kind="ghost"
                icon={<IconTrash width={16} height={16} />}
                onClick={() => void doRemove(confirming.host)}
              >
                {t('accounts.remove')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">{t('accounts.removeConfirm', { name: confirming.host })}</p>
        </Modal>
      )}
    </div>
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

function HosterLoginDialog({
  hosts,
  existing,
  editing,
  onClose,
  onSaved,
}: {
  hosts: HosterHost[];
  existing: HosterLogin[];
  editing?: HosterLogin;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [query, setQuery] = useState('');
  const [picked, setPicked] = useState<HosterHost | null>(
    editing ? { id: editing.host, label: editing.host } : null,
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

  const title = picked ? t('accounts.hoster.loginTitle', { host: picked.label }) : t('accounts.hoster.pickHost');
  const canSave = username.trim() !== '' && password.trim() !== '';

  return (
    <Modal
      title={title}
      onClose={onClose}
      footer={
        picked ? (
          <>
            <span className="flex-1" />
            <Button kind="ghost" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button onClick={() => void doSave()} disabled={saving || !canSave}>
              {saving ? t('accounts.saving') : t('accounts.save')}
            </Button>
          </>
        ) : (
          <>
            <span className="flex-1" />
            <Button kind="secondary" onClick={onClose}>
              {t('common.cancel')}
            </Button>
          </>
        )
      }
    >
      {!picked ? (
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2">
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
                {/* Multihosters are only usable through JD, so they stay here,
                    labelled as such. */}
                {h.multihoster && (
                  <span className="glim-eyebrow ms-auto shrink-0">{t('accounts.hoster.multihoster')}</span>
                )}
              </button>
            ))}
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          {/* Only while adding; an edit stays on its host. */}
          {!editing && (
            <button
              type="button"
              onClick={() => setPicked(null)}
              className="self-start text-xs text-carbon-textMuted hover:text-carbon-text"
            >
              {t('accounts.changeService')}
            </button>
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
