// The native hoster login: KL's own host list, username/password form and
// per-row sync status against the headless-JD sidecar - see
// internal/hosterauth's doc comment for the full design. This is
// KnightLoader's own Carbon UI end to end; nothing here ever shows JD's own
// web interface, an iframe of it, or redirects to it. Saving a login writes
// the credential into JD's own account config through JD's Remote API, and
// JD's existing, already-working hoster plugin performs the actual login -
// the same "JD's UI never shown, everything through JD's API" rule
// internal/resolver/jd/client.go already follows for every other JD call.
//
// Mounted from web/src/pages/Accounts.tsx's HosterLoginsSlot - see that
// file's comment on the slot for why this is one import and one render call
// there, not a rewrite of the page around it.
import { useCallback, useEffect, useState } from 'react';
import {
  type HosterHost,
  type HosterLogin,
  fetchHosterHosts,
  fetchHosterLogins,
  removeHosterLogin,
  saveHosterLogin,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { Button, EmptyState, Field, InfoBubble, Modal, TextInput } from './ui';
import { IconAccounts, IconPlus, IconSearch, IconTrash } from '../lib/icons';

// Faster than ACCOUNTS.HEALTH_POLL_MS (30s): a login this reconciler just
// added moves through queued -> active/rejected in seconds to a couple of
// minutes while JD's own account checker runs, not the hours an expiry or a
// traffic figure takes to change - a poll as slow as that one would leave a
// freshly saved row looking stuck long after JD has already answered.
const POLL_MS = 8000;

export function HosterLoginSection() {
  const { t } = useT();
  const { toast } = useToast();
  const [logins, setLogins] = useState<HosterLogin[] | null>(null);
  const [hosts, setHosts] = useState<HosterHost[]>([]);
  const [dialogOpen, setDialogOpen] = useState(false);

  const load = useCallback(async () => {
    try {
      setLogins(await fetchHosterLogins());
    } catch {
      // A missed poll leaves the previous rows on screen rather than blanking
      // a working list - the same choice the debrid/apiKey table above makes
      // by only flipping loadError on the very first load.
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

  async function onRemove(host: string) {
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
        <div className="glim-card overflow-x-auto p-0">
          <table className="w-full min-w-[32rem] border-collapse text-sm">
            <thead>
              <tr className="text-start text-xs text-carbon-textMuted">
                <th className="px-4 py-3 text-start font-medium">{t('accounts.hoster.col.host')}</th>
                <th className="px-2 py-3 text-start font-medium">{t('accounts.col.status')}</th>
                <th className="px-2 py-3 text-start font-medium">{t('accounts.hoster.col.username')}</th>
                <th className="w-10 px-2 py-3">
                  <span className="sr-only">{t('accounts.rowActions')}</span>
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-carbon-border/40">
              {logins?.map((row) => (
                <tr key={row.host} className="group transition-colors hover:bg-carbon-hover">
                  <td className="px-4 py-3 font-medium text-carbon-text">{row.host}</td>
                  <td className="px-2 py-3">
                    <HosterLoginStatusBadge login={row} />
                  </td>
                  <td className="px-2 py-3 text-carbon-textSub">{row.username || '—'}</td>
                  <td className="px-2 py-3 text-end">
                    <Button
                      kind="ghost"
                      className="opacity-0 transition-opacity focus-visible:opacity-100 group-hover:opacity-100"
                      icon={<IconTrash width={16} height={16} />}
                      aria-label={t('accounts.remove')}
                      onClick={() => void onRemove(row.host)}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {hasRows ? (
        <Button kind="secondary" icon={<IconPlus width={16} height={16} />} className="self-start" onClick={() => setDialogOpen(true)}>
          {t('accounts.hoster.add')}
        </Button>
      ) : (
        <EmptyState
          icon={<IconAccounts width={26} height={26} />}
          title={t('accounts.hoster.empty')}
          hint={t('accounts.hoster.emptyHint')}
          action={
            <Button kind="secondary" icon={<IconPlus width={16} height={16} />} onClick={() => setDialogOpen(true)}>
              {t('accounts.hoster.add')}
            </Button>
          }
        />
      )}

      {dialogOpen && (
        <HosterLoginDialog hosts={hosts} existing={logins ?? []} onClose={() => setDialogOpen(false)} onSaved={load} />
      )}
    </div>
  );
}

function HosterLoginStatusBadge({ login }: { login: HosterLogin }) {
  const { t } = useT();
  switch (login.status) {
    case 'active':
      return (
        <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-statusOk">
          <span className="h-1.5 w-1.5 rounded-full bg-statusOkSolid" />
          {t('accounts.hoster.status.active')}
        </span>
      );
    case 'rejected':
      return (
        <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-statusFail">
          <span className="h-1.5 w-1.5 rounded-full bg-statusFailSolid" />
          {t('accounts.hoster.status.rejected')}
          {login.detail && <InfoBubble tip={login.detail} />}
        </span>
      );
    default:
      // 'queued' covers two real states this badge deliberately does not
      // split further on screen - "JD hasn't confirmed it yet" and "JD has
      // it but hasn't validated it yet" - both mean the same thing to a
      // user looking at a row: nothing to do, check back shortly. The
      // detail text (from hosterauth.LoginState.Detail) still says which one.
      return (
        <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-statusNeutral">
          <span className="h-1.5 w-1.5 rounded-full bg-statusNeutralSolid" />
          {t('accounts.hoster.status.queued')}
          {login.detail && <InfoBubble tip={login.detail} />}
        </span>
      );
  }
}

function HosterLoginDialog({
  hosts,
  existing,
  onClose,
  onSaved,
}: {
  hosts: HosterHost[];
  existing: HosterLogin[];
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [query, setQuery] = useState('');
  const [picked, setPicked] = useState<HosterHost | null>(null);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
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
                <span className="text-sm text-carbon-text">{h.label}</span>
              </button>
            ))}
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          <button
            type="button"
            onClick={() => setPicked(null)}
            className="self-start text-xs text-carbon-textMuted hover:text-carbon-text"
          >
            {t('accounts.changeService')}
          </button>

          <Field label={t('accounts.usernameField')}>
            <TextInput autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
          </Field>
          <Field label={t('accounts.passwordField')}>
            <TextInput type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>

          {/* Stated plainly, in the body of the dialogue, not filed behind the
              (i) bubble InfoBubble is for: this is a genuine widening of
              custody - the password is about to be sent to and stored by the
              JD sidecar - and it has to be seen before the click that does
              it, not one hover away from being missed. */}
          <p className="rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-xs text-carbon-textSub">
            {t('accounts.hoster.custodyNotice')}
          </p>
        </div>
      )}
    </Modal>
  );
}
