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
// The table itself is AccountTable, shared with the debrid card (jdp,
// 2026-09-07: "bei beiden Cards (Debrid, hoster) sollen die spalten gleich
// sein"). What this file still owns is the two things that are genuinely
// different here: the status badge, whose states are about JD accepting a
// login rather than about a service answering, and the pick-a-host dialogue.
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
  setHosterLoginEnabled,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { Button, EmptyState, Field, InfoBubble, Modal, TextInput } from './ui';
import { AccountTable } from './AccountTable';
import { IconAccounts, IconPlus, IconSearch } from '../lib/icons';
import { HosterIcon } from './HosterIcon';

// Faster than ACCOUNTS.HEALTH_POLL_MS (30s): a login this reconciler just
// added moves through queued -> active/rejected in seconds to a couple of
// minutes while JD's own account checker runs, not the hours an expiry or a
// traffic figure takes to change - a poll as slow as that one would leave a
// freshly saved row looking stuck long after JD has already answered.
const POLL_MS = 8000;

/** What the dialogue is doing: adding a login for a host still to be picked,
 *  or editing the one that exists for a host already chosen. The password box
 *  starts on the redaction placeholder in the second case, which the server
 *  reads as "not retyped" and leaves the stored secret alone. */
type Dialog = { mode: 'new' } | { mode: 'edit'; login: HosterLogin };

export function HosterLoginSection() {
  const { t } = useT();
  const { toast } = useToast();
  const [logins, setLogins] = useState<HosterLogin[] | null>(null);
  const [hosts, setHosts] = useState<HosterHost[]>([]);
  const [dialog, setDialog] = useState<Dialog | null>(null);

  const load = useCallback(async () => {
    try {
      setLogins(await fetchHosterLogins());
    } catch {
      // A missed poll leaves the previous rows on screen rather than blanking
      // a working list - the same choice the debrid table above makes by only
      // flipping loadError on the very first load.
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
    // Optimistic, the same way the debrid table's own switch is: the toggle is
    // the row's only feedback, and a spinner over one reads as broken rather
    // than as busy. The reconcile behind it takes a moment - JD has to accept
    // or drop the account - and the poll above corrects the row when it lands.
    setLogins((cur) => cur?.map((x) => (x.host === row.host ? { ...x, enabled } : x)) ?? cur);
    try {
      await setHosterLoginEnabled(row.host, enabled);
    } catch {
      toast(t('common.loadFailed'), 'fail');
    }
    await load();
  }

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
            // Bytes or nothing: JD reports trafficLeft and trafficMax and no
            // percentage at all, so a row whose hoster states no quota shows a
            // dash rather than a bar with an invented full.
            traffic: { used: Math.max(0, (row.trafficMax ?? 0) - (row.trafficLeft ?? 0)), limit: row.trafficMax ?? 0 },
            onToggle: (v) => void onToggle(row, v),
            onEdit: () => setDialog({ mode: 'edit', login: row }),
            onRemove: () => void onRemove(row.host),
          }))}
        />
      )}

      {/* accounts.newAccount, the same key the debrid card's own button reads
          (jdp, 2026-09-07: "beie hinzufügen buttons sollen Konto hinzufügen
          heißen"). One key rather than two with identical text: two keys that
          have to agree across 42 catalogues are two keys that will one day
          disagree in one of them. */}
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
    </div>
  );
}

function HosterLoginStatusBadge({ login }: { login: HosterLogin }) {
  const { t } = useT();
  switch (login.status) {
    case 'off':
      // Its own reading, not a greyed-out "queued": a switched-off login is
      // not waiting for anything. JD does not have it at all.
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
      // 'queued' covers two real states this badge deliberately does not
      // split further on screen - "JD hasn't confirmed it yet" and "JD has
      // it but hasn't validated it yet" - both mean the same thing to a
      // user looking at a row: nothing to do, check back shortly. The
      // detail text (from hosterauth.LoginState.Detail) still says which one.
      return (
        <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-statusNeutral">
          <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusNeutralSolid" />
          {t('accounts.hoster.status.queued')}
          {login.detail && <InfoBubble tip={login.detail} />}
        </span>
      );
  }
}

/** The placeholder the server reads as "the caller did not retype this"
 *  (accounts.Redacted). Sent back unchanged, the stored password survives an
 *  edit that only changed the username. */
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
                {/* The icon here as well as in the list of configured logins
                    (jdp, 2026-09-05: "die logos der hoster werden nicht
                    angezeigt" - this picker is where he was looking). The
                    catalogue is hundreds of hosts long, so it costs what it
                    looks like it costs: HosterIcon's <img> is lazy, so only
                    the rows a person has actually scrolled to are ever
                    fetched, and the server keeps each one after the first. */}
                <HosterIcon host={h.id} />
                <span className="text-sm text-carbon-text">{h.label}</span>
              </button>
            ))}
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          {/* Only while adding: an edit is about THIS host's credentials, and
              a "choose a different service" link there would turn a correction
              into a second, differently-named login. */}
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
