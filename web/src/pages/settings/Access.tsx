import { useEffect, useState, type ReactNode } from 'react';
import {
  Button,
  Card,
  Field,
  IconBadge,
  IconTile,
  InfoBubble,
  LabelBadge,
  Modal,
  PasswordInput,
  SectionTitle,
  TextArea,
  TextInput,
  ToggleRow,
} from '../../components/ui';
import { QRCode } from '../../components/QRCode';
import {
  type ApiToken,
  type AuthState,
  type NewApiToken,
  type ConnectInfo,
  type QRMatrix,
  type RelayConfig,
  type RelayMode,
  activateConnect,
  createToken,
  fetchAuth,
  fetchConnect,
  fetchRelayConfig,
  saveRelayConfig,
  PhraseRejected,
  joinConnect,
  leaveConnect,
  revealConnect,
  fetchRemoteAccess,
  fetchTokens,
  revokeToken,
  setPassword,
} from '../../lib/api';
import { copyToClipboard } from '../../lib/clipboard';
import { fmtDate } from '../../lib/format';
import { useT, type TranslationKey } from '../../lib/i18n';
import {
  IconCheck,
  IconClipboard,
  IconClose,
  IconKey,
  IconPlus,
  IconTrash,
} from '../../lib/icons';
import { useToast } from '../../lib/toast';
import { useDraft, useFeatures } from './context';
import { NeutralSwitch } from './controls';
import { ModuleToggle } from './ModuleToggle';
import { PasskeyCard } from './access/PasskeyCard';
import { TwoFactorCard } from './access/TwoFactorCard';
import { label, useTx } from './tx';

/**
 * PENDING holds the English strings of the Access page (password lock, intake
 * ports, API tokens and remote access) until the catalogue has them; the
 * lookup asks the catalogue first.
 */
const PENDING = {
  'settings.access.tokens.title': 'API tokens',
  'settings.access.tokens.intro':
    'Named credentials for a script, a browser extension or a phone. Each one can be revoked on its own, without changing the shared password every other client uses.',
  'settings.access.tokens.empty': 'No tokens issued yet.',
  'settings.access.tokens.new': 'New token',
  'settings.access.tokens.namePlaceholder': 'e.g. my phone',
  'settings.access.tokens.cancel': 'Cancel',
  'settings.access.tokens.create': 'Create',
  'settings.access.tokens.creating': 'Creating…',
  'settings.access.tokens.created': 'Created',
  'settings.access.tokens.lastUsed': 'Last used',
  'settings.access.tokens.neverUsed': 'never',
  'settings.access.tokens.revoke': 'Revoke',
  'settings.access.tokens.secretTitle': 'Copy this token now',
  'settings.access.tokens.secretWarning':
    'This is the only time this token is shown. It is stored as a one-way hash on this instance, so if it is lost there is no way to read it back, only to revoke it and create a new one.',
  'settings.access.tokens.copy': 'Copy',
  'settings.access.tokens.copied': 'Copied',
  'settings.access.tokens.done': 'Done',
  'settings.access.tokens.howToUse': 'Send it as a header: Authorization: Bearer <token>',
  'settings.access.tokens.createFailed': 'Could not create the token: {error}',

  'settings.access.identity.title': "This instance's identity",
  'settings.access.identity.nameLabel': 'Name',
  'settings.access.identity.namePlaceholder': 'e.g. Home server',
  'settings.access.identity.nameHint':
    'Offered first when pairing and in the QR code below, instead of whatever the OS or container runtime happens to call this machine. Optional - leave it empty to keep using that.',
  'settings.access.identity.domainsLabel': 'Known domains',
  'settings.access.identity.domainsHint':
    'Remembered automatically the first time a request actually arrives on one, so it stays listed here even when later requests come in over the LAN IP instead. Add one by hand for a domain that is already configured but has not been visited through yet - one full address per line, e.g. https://kl.example.com.',

  'settings.access.intakePortsHint':
    'Other ways this instance can be reached directly, outside the normal login - each with its own reachability shown here.',
} as const;

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return (key: PendingKey, vars?: Record<string, string | number>) => {
    let s = (t(key as unknown as TranslationKey) as string | undefined) ?? PENDING[key];
    if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
    return s;
  };
}

export function Access() {
  const { tx } = useTx();
  const cx = useCx();
  /**
   * Bumped when the relay cards change the relay in force, so the connect card
   * re-reads /api/connect and its badge follows.
   */
  const [relayVersion, setRelayVersion] = useState(0);
  /**
   * Bumped when anything changes the lock, so the password, second-factor and
   * passkey cards agree: the password enables the other two and removing it
   * takes the second factor with it.
   */
  const [authVersion, setAuthVersion] = useState(0);

  return (
    <div className="flex flex-col gap-10">
      {/* Identity first: a plain settings field that depends on no fetch. */}
      <IdentityCard cx={cx} />
      <PasswordCard cx={cx} onAuthChanged={() => setAuthVersion((n) => n + 1)} />

      {/* The second factor and passkeys, two cards because somebody can want
          one without the other. */}
      <SecondWaysIn version={authVersion} onChanged={() => setAuthVersion((n) => n + 1)} />

      <RemoteAccessCard cx={cx} relayVersion={relayVersion} />
      {/* The card above is about the twelve words; these are about which relay
          carries them. */}
      <RelaySection onRelayChanged={() => setRelayVersion((n) => n + 1)} />
      <TokensSection cx={cx} />
    </div>
  );
}

// PasswordCard owns the password lock. It saves on its own button and not
// through PUT /api/settings.
function PasswordCard({
  cx,
  /** Called after the password changes, so the other cards re-read the lock. */
  onAuthChanged,
}: {
  cx: (k: PendingKey) => string;
  onAuthChanged: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [auth, setAuth] = useState<AuthState | null>(null);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [done, setDone] = useState(false);
  // Keyed onto the apply button so a repeated refusal shakes again.
  const [shake, setShake] = useState(0);
  // Whether this instance is reachable from elsewhere, which turns "no password"
  // into a problem (routes_remote.go's Exposed).
  const [exposed, setExposed] = useState(false);

  useEffect(() => {
    fetchAuth()
      .then(setAuth)
      .catch(() => setAuth(null));
    fetchRemoteAccess()
      .then((info) => setExposed(info.exposed))
      .catch(() => setExposed(false));
  }, []);

  async function onApply() {
    try {
      setAuth(await setPassword(current, next));
      setCurrent('');
      setNext('');
      setDone(true);
      setTimeout(() => setDone(false), 1800);
      onAuthChanged();
    } catch (e) {
      // The reason goes to the toast and the button shakes.
      toast(String(e).replace(/^Error:\s*/, ''), 'fail');
      setShake((n) => n + 1);
    }
  }

  const locked = auth?.enabled ?? false;

  return (
      <Card hue={0} className="flex flex-col gap-5">
        {/* The status stays visible; why a password matters is in the title's hint. */}
        <SectionTitle hint={t('settings.lockHint')}>
          {t('auth.password')}
        </SectionTitle>
        {locked && (
          <Field label={t('settings.passwordCurrent')}>
            <PasswordInput
              value={current}
              onChange={setCurrent}
              autoComplete="current-password"
              showLabel={t('common.showPassword')}
              hideLabel={t('common.hidePassword')}
            />
          </Field>
        )}
        <Field label={t('settings.passwordNew')} hint={t('settings.passwordHint')}>
          <PasswordInput
            value={next}
            onChange={setNext}
            autoComplete="new-password"
            showLabel={t('common.showPassword')}
            hideLabel={t('common.hidePassword')}
          />
        </Field>
        <div className="flex flex-wrap items-center gap-3">
          <Button
            key={shake}
            className={shake > 0 ? 'glim-shake' : ''}
            kind="secondary"
            hue={0}
            onClick={onApply}
            disabled={locked ? current === '' : next === ''}
          >
            {next === '' && locked ? t('settings.removePassword') : t('settings.setPassword')}
          </Button>
          {/* Beside the button that changes it, in the warning hue only when
              this instance is reachable from elsewhere. */}
          <span
            className={`text-sm ${locked ? 'text-statusOk' : exposed ? 'text-statusWarn' : 'text-carbon-textSub'}`}
          >
            {locked ? t('settings.lockOn') : t('settings.lockOff')}
          </span>
          {done && <span className="text-statusOk text-sm">{t('settings.passwordSaved')}</span>}
        </div>
      </Card>
  );
}

/**
 * SecondWaysIn reads /api/auth once for the second-factor and passkey cards, so
 * they cannot disagree, and renders nothing until it answers, since a guessed
 * "no password" would offer a setup the server refuses.
 */
function SecondWaysIn({ version, onChanged }: { version: number; onChanged: () => void }) {
  const [auth, setAuth] = useState<AuthState | null>(null);

  useEffect(() => {
    fetchAuth()
      .then(setAuth)
      .catch(() => setAuth(null));
  }, [version]);

  if (!auth) return null;
  return (
    <>
      <TwoFactorCard
        hue={4}
        passwordSet={auth.enabled}
        enabled={auth.twoFactor === true}
        recoveryLeft={auth.recoveryLeft}
        onChanged={onChanged}
      />
      <PasskeyCard hue={6} passwordSet={auth.enabled} />
    </>
  );
}

// IdentityCard holds an optional name and the domains this instance is known
// by, both ordinary settings fields. Domains are recorded automatically when a
// request arrives on one (routes_remote.go's rememberDomain); the box covers a
// domain that has not been visited yet.
function IdentityCard({ cx }: { cx: (k: PendingKey) => string }) {
  const { cfg, patch } = useDraft();

  return (
    <Card hue={2} className="flex flex-col gap-5">
      <SectionTitle>{cx('settings.access.identity.title')}</SectionTitle>
      <Field label={cx('settings.access.identity.nameLabel')} hint={cx('settings.access.identity.nameHint')}>
        <TextInput
          placeholder={cx('settings.access.identity.namePlaceholder')}
          value={cfg.instanceName}
          onChange={(e) => patch({ instanceName: e.target.value })}
        />
      </Field>
      <Field label={cx('settings.access.identity.domainsLabel')} hint={cx('settings.access.identity.domainsHint')}>
        <TextArea
          rows={3}
          spellCheck={false}
          dir="ltr"
          value={(cfg.knownDomains ?? []).join('\n')}
          onChange={(e) => patch({ knownDomains: e.target.value.split('\n').filter((d) => d.trim() !== '') })}
        />
      </Field>
    </Card>
  );
}

// RemoteAccessCard connects this instance with the other KnightLoaders you run
// through twelve words: both ends derive one key from them and meet on a relay
// neither has to be reachable from (internal/seedphrase, internal/relay). It
// opens with a numbered how-to.

/** How often the card re-reads whether the relay link is up. */
const CONN_POLL_MS = 4000;

function RemoteAccessCard({
  cx,
  relayVersion,
}: {
  cx: (k: PendingKey, vars?: Record<string, string | number>) => string;
  /** Changes when the relay cards switch relays. */
  relayVersion: number;
}) {
  const { t } = useT();

  // "Stored" and "connected" are separate facts: a phrase with an unreachable
  // relay is set up but not working.
  const [conn, setConn] = useState<ConnectInfo | null>(null);
  const [phrase, setPhrase] = useState('');
  // The server's matrix, so the code is not encoded twice.
  const [phraseQr, setPhraseQr] = useState<QRMatrix | null>(null);
  const [phraseBusy, setPhraseBusy] = useState(false);
  const [phraseErr, setPhraseErr] = useState('');
  const [phraseCopied, setPhraseCopied] = useState(false);
  const [joinInput, setJoinInput] = useState('');
  const [joinOpen, setJoinOpen] = useState(false);
  const [revealPw, setRevealPw] = useState('');
  const [revealOpen, setRevealOpen] = useState(false);

  const loadConn = () => fetchConnect().then(setConn).catch(() => {});
  // Polled, since a relay switched on connects a moment later; the route
  // answers from cached state.
  useEffect(() => {
    void loadConn();
    const timer = window.setInterval(loadConn, CONN_POLL_MS);
    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [relayVersion]);

  async function onActivate() {
    setPhraseErr('');
    setPhraseBusy(true);
    try {
      const r = await activateConnect();
      setPhrase(r.phrase);
      setPhraseQr(r.qr ?? null);
      setConn(r.info);
    } catch (e) {
      setPhraseErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPhraseBusy(false);
    }
  }

  async function onJoin() {
    setPhraseErr('');
    setPhraseBusy(true);
    try {
      setConn(await joinConnect(joinInput));
      setJoinInput('');
      setJoinOpen(false);
    } catch (e) {
      // A rejected phrase comes back as a reason code, since the server cannot
      // know the reader's language.
      if (e instanceof PhraseRejected) {
        setPhraseErr(
          e.reason === 'unknown_word'
            ? t('settings.access.phrase.errUnknownWord', { position: e.position, word: e.word })
            : e.reason === 'word_count'
              ? t('settings.access.phrase.errWordCount', { count: e.count, need: 12 })
              : t('settings.access.phrase.errChecksum'),
        );
      } else {
        setPhraseErr(e instanceof Error ? e.message : String(e));
      }
    } finally {
      setPhraseBusy(false);
    }
  }

  async function onReveal() {
    setPhraseErr('');
    setPhraseBusy(true);
    try {
      const r = await revealConnect(revealPw);
      setPhrase(r.phrase);
      setPhraseQr(r.qr ?? null);
      setRevealPw('');
      setRevealOpen(false);
    } catch (e) {
      setPhraseErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPhraseBusy(false);
    }
  }

  async function onLeave() {
    setPhraseErr('');
    setPhraseBusy(true);
    try {
      await leaveConnect();
      // Cleared at once, or the phrase would linger after leaving.
      setPhrase('');
      setPhraseQr(null);
      await loadConn();
    } catch (e) {
      setPhraseErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPhraseBusy(false);
    }
  }

  // Held until the connection state answers, rather than flickering in.
  if (!conn) return null;

  return (
    <Card hue={1} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.access.phrase.body')}>
        {t('settings.access.cardTitle')}
      </SectionTitle>

      {/* A numbered how-to before any button, still shown after setup for the
          next instance. */}
      <div className="flex flex-col gap-2">
        {/* The how-it-works bubble and the connection state share the lead's
            line and wrap under it when the words run long, which a length
            given in advance cannot promise in every language. */}
        <div className="flex flex-wrap items-start gap-x-4 gap-y-2">
          <p className="min-w-0 flex-1 basis-64 text-sm text-carbon-textSub">{t('settings.access.phrase.howLead')}</p>
          <div className="flex flex-wrap items-center gap-2">
            <LabelBadge
              label={t('settings.access.phrase.howButton')}
              tip={paragraphs(t('settings.access.phrase.howWhat'))}
              hue={2}
            />
            {/* Four sentences, since "disconnected" can mean no relay configured
                or a configured relay out of reach. */}
            <LabelBadge
              label={
                conn.connected
                  ? t('settings.access.phrase.statusConnected')
                  : t('settings.access.phrase.statusDisconnected')
              }
              tip={
                conn.connected
                  ? conn.relayMode === 'own'
                    ? t('settings.access.phrase.statusHintOwn')
                    : t('settings.access.phrase.statusHintProject')
                  : conn.relayMode === 'off'
                    ? t('settings.access.phrase.statusHintOff')
                    : t('settings.access.phrase.statusHintLost')
              }
              tone={conn.connected ? 'ok' : 'fail'}
            />
            {/* Which relay carries the words, as a reading; the switches live in the
                relay cards below. The address is in the tip. */}
            <LabelBadge
              label={
                conn.relayMode === 'off'
                  ? t('settings.access.relay.none')
                  : conn.relayMode === 'own'
                    ? t('settings.access.relay.own')
                    : t('settings.access.relay.project')
              }
              tip={
                conn.relayMode === 'off'
                  ? t('settings.access.relay.noneHint')
                  : t('settings.access.relay.whichHint', { address: conn.relayUrl })
              }
              tone={conn.relayMode === 'own' ? 'ok' : undefined}
            />
          </div>
        </div>
        <ol className="list-decimal space-y-1.5 ps-4 text-sm text-carbon-textSub">
          {/* Button names are interpolated from the buttons' own keys, so the
              steps cannot drift from the labels. */}
          <li>{t('settings.access.phrase.howStep1', { button: t('settings.access.phrase.activate') })}</li>
          <li>{t('settings.access.phrase.howStep2', { button: t('settings.access.phrase.joinButton') })}</li>
          <li>{t('settings.access.phrase.howStep3')}</li>
        </ol>
      </div>

      {/* Not set up: start a group or join one. */}
      {conn && !conn.active && (
        <div className="flex flex-col gap-3">
          {/* The warning beside the buttons: an unprotected instance puts the
              whole group at risk, since the phrase reaches every member. */}
          <div className="flex flex-wrap items-center gap-3">
            <Button hue={1} disabled={phraseBusy} onClick={() => void onActivate()}>
              {t('settings.access.phrase.activate')}
            </Button>
            <Button
              hue={3}
              icon={<IconClipboard width={16} height={16} />}
              onClick={() => {
                setJoinOpen(!joinOpen);
                setPhraseErr('');
              }}
            >
              {t('settings.access.phrase.joinButton')}
            </Button>
            {!conn.passwordSet && (
              <p className="min-w-[12rem] flex-1 text-[11px] leading-relaxed text-statusWarn">
                {t('settings.access.phrase.noPasswordWarning')}
              </p>
            )}
          </div>
          {joinOpen && (
            <div className="flex flex-col gap-2 sm:flex-row">
              <TextInput
                dir="ltr"
                spellCheck={false}
                className="min-w-0 flex-1"
                placeholder={t('settings.access.phrase.joinPlaceholder')}
                value={joinInput}
                onChange={(e) => setJoinInput(e.target.value)}
              />
              <Button hue={1} disabled={phraseBusy || joinInput.trim() === ''} onClick={() => void onJoin()}>
                {t('settings.access.phrase.joinConfirm')}
              </Button>
            </div>
          )}
        </div>
      )}

      {/* Set up: show the phrase or leave. Whether it is connected is the pill
          in the title. */}
      {conn?.active && (
        <div className="flex flex-col gap-3">
          {phrase ? (
            <div className="flex flex-col gap-2">
              <span className="text-xs font-semibold text-carbon-textSub">
                {t('settings.access.phrase.yourPhrase')}
              </span>
              {/* The QR code on the left and the words beside it; without a
                  code the row is just the words. */}
              <div className="flex flex-col items-start gap-3 sm:flex-row">
                {phraseQr && (
                  <div className="flex shrink-0 flex-col items-start gap-1.5">
                    <QRCode matrix={phraseQr} label={phrase} size={144} />
                    <span className="text-[11px] text-carbon-textMuted">{t('settings.access.phrase.qrHint')}</span>
                  </div>
                )}
                <div className="flex min-w-0 flex-1 items-start gap-1.5">
                  {/* Larger than the page's scale, since the words are read
                      aloud or typed on a phone. */}
                  <code
                    className="glim-num min-w-0 flex-1 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2.5
                      text-base leading-relaxed text-carbon-text"
                    dir="ltr"
                  >
                    {phrase}
                  </code>
                  <InfoBubble tip={t('settings.access.phrase.pasteHint')} />
                </div>
              </div>
              {/* Hide puts the key to the group away again; Copy sits beside it. */}
              <div className="flex flex-wrap gap-2">
                <Button hue={4} onClick={() => { setPhrase(''); setPhraseQr(null); }}>
                  {t('settings.access.phrase.hide')}
                </Button>
                <Button
                  kind="secondary"
                  icon={phraseCopied ? <IconCheck width={16} height={16} /> : <IconClipboard width={16} height={16} />}
                  onClick={async () => {
                    if (await copyToClipboard(phrase)) {
                      setPhraseCopied(true);
                      setTimeout(() => setPhraseCopied(false), 1800);
                    }
                  }}
                >
                  {phraseCopied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                </Button>
              </div>
            </div>
          ) : revealOpen ? (
            <div className="flex flex-col gap-2">
              {/* Why a password is asked for sits on the caption's (i). */}
              <span className="flex items-center gap-1.5 text-xs font-semibold text-carbon-textSub">
                {t('auth.password')}
                <InfoBubble tip={t('settings.access.phrase.revealWhy')} />
              </span>
              <div className="flex flex-col gap-2 sm:flex-row">
                <div className="min-w-0 flex-1">
                  <PasswordInput
                    value={revealPw}
                    onChange={setRevealPw}
                    autoComplete="current-password"
                    showLabel={t('common.showPassword')}
                    hideLabel={t('common.hidePassword')}
                  />
                </div>
                <Button hue={1} disabled={phraseBusy} onClick={() => void onReveal()}>
                  {t('settings.access.phrase.revealConfirm')}
                </Button>
              </div>
            </div>
          ) : (
            // Leave steps back, so it leads the row; showing the phrase comes
            // last. Revealing it on an unprotected instance warns but does not
            // block, since this page cannot tell whether anything can reach it.
            <div className="flex flex-wrap items-center gap-3">
              <Button hue={5} disabled={phraseBusy} onClick={() => void onLeave()}>
                {t('settings.access.phrase.leave')}
              </Button>
              <Button
                hue={1}
                onClick={() => {
                  setPhraseErr('');
                  // Without a password there is nothing to re-enter.
                  if (conn.passwordSet) setRevealOpen(true);
                  else void onReveal();
                }}
              >
                {t('settings.access.phrase.showAgain')}
              </Button>
              {!conn.passwordSet && (
                <p className="min-w-[12rem] flex-1 text-[11px] leading-relaxed text-statusWarn">
                  {t('settings.access.phrase.noPasswordWarning')}
                </p>
              )}
            </div>
          )}
        </div>
      )}

      {phraseErr && <p className="text-sm text-statusFail">{phraseErr}</p>}
    </Card>
  );
}

/**
 * RelaySection holds the project relay card and the own relay card and
 * everything both read and write. The two switches are mutually exclusive and
 * both may be off, which means no relay at all. Instances on the same network
 * find each other over UDP multicast (internal/discovery) without any relay.
 */
function RelaySection({ onRelayChanged }: { onRelayChanged: () => void }) {
  const { t } = useT();
  const [conn, setConn] = useState<ConnectInfo | null>(null);
  const [cfg, setCfg] = useState<RelayConfig | null>(null);
  const [busy, setBusy] = useState(false);
  const { toast } = useToast();

  // Reloads both: /api/connect says which relay is dialled, /api/relay/config
  // what is stored.
  const reload = () => {
    fetchConnect().then(setConn).catch(() => {});
    fetchRelayConfig().then(setCfg).catch(() => {});
  };
  // Polled, so a dial that succeeds after the switch reaches the screen.
  useEffect(() => {
    reload();
    const timer = window.setInterval(reload, CONN_POLL_MS);
    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  /**
   * saveMode saves the relay mode for both cards, so "one of these, or
   * neither" holds by construction. It reports success, so each caller can
   * shake itself on refusal; the reason goes to the toast.
   */
  async function saveMode(mode: RelayMode, relayUrl?: string, serve?: boolean): Promise<boolean> {
    setBusy(true);
    try {
      const c = await saveRelayConfig(relayUrl ?? cfg?.relayUrl ?? '', undefined, serve, mode);
      setCfg(c);
      reload();
      // The connect card's badge depends on this.
      onRelayChanged();
      toast(t2(mode));
      return true;
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'fail');
      return false;
    } finally {
      setBusy(false);
    }
  }

  // The confirmation names the new state rather than saying "saved".
  const t2 = (mode: RelayMode) =>
    mode === 'off'
      ? t('settings.access.relay.savedOff')
      : mode === 'own'
        ? t('settings.access.relay.savedOwn')
        : t('settings.access.relay.savedProject');

  if (!conn || !cfg) return null;

  return (
    <div className="grid gap-6 md:grid-cols-2">
      <ProjectRelayCard
        cfg={cfg}
        conn={conn}
        busy={busy}
        onPick={(on) => saveMode(on ? 'project' : 'off')}
      />
      <OwnRelayCard
        cfg={cfg}
        busy={busy}
        onPick={(on) => saveMode(on ? 'own' : 'off')}
        onSaveAddress={(url) => saveMode('own', url)}
        onServe={(v) => saveMode('own', cfg.relayUrl, v)}
      />
    </div>
  );
}

/**
 * ProjectRelayCard offers the project's relay and says plainly what its
 * operator can see. Frames are sealed, including the instance name
 * (relay.Identity); the outside of the envelope is not, and the bubble
 * describes it in the privacy document's words and names the address. The
 * address is fixed, so it is something to read rather than a field.
 */
function ProjectRelayCard({
  cfg,
  conn,
  busy,
  onPick,
}: {
  cfg: RelayConfig;
  conn: ConnectInfo;
  busy: boolean;
  /** Reports whether the save went through, so a refusal shakes the switch. */
  onPick: (on: boolean) => Promise<boolean>;
}) {
  const { t } = useT();
  const active = cfg.mode === 'project';
  const [shake, setShake] = useState(0);

  return (
    <Card hue={2} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.access.relay.body')}>{t('settings.access.relay.title')}</SectionTitle>

      <div key={shake} className={shake > 0 ? 'glim-shake' : undefined}>
        <ToggleRow
          hue={2}
          label={t('settings.access.relay.use')}
          checked={active}
          disabled={busy}
          onChange={(on) => void onPick(on).then((ok) => !ok && setShake((n) => n + 1))}
        />
      </div>

      {/* The extra space ties the sentence to the block below it. */}
      <p className="mt-2 text-sm text-carbon-textSub">{t('settings.access.relay.leadProject')}</p>

      {/* mt-auto keeps the footer row at the bottom when the own relay card
          beside this one is taller. */}
      <div className="mt-auto flex items-center justify-end gap-3">
        <LabelBadge
          label={t('settings.access.relay.seesButton')}
          tip={paragraphs(
            `${t('settings.access.relay.seesTip')}\n\n${t('settings.access.relay.seesAddress', { address: conn.projectRelayUrl })}`,
          )}
          hue={3}
        />
      </div>
    </Card>
  );
}

/**
 * OwnRelayCard offers a relay you run: served by this instance on the address
 * it already answers on, or at an address elsewhere, such as the container on
 * a small VPS. The address is stored per instance and not carried by the
 * phrase, so every instance needs it; the warning says so.
 */
function OwnRelayCard({
  cfg,
  busy,
  onPick,
  onSaveAddress,
  onServe,
}: {
  cfg: RelayConfig;
  busy: boolean;
  /** Each reports whether the save went through. */
  onPick: (on: boolean) => Promise<boolean>;
  onSaveAddress: (url: string) => Promise<boolean>;
  onServe: (v: boolean) => Promise<boolean>;
}) {
  const { t } = useT();
  // Seeded once and then owned by the field, so a poll cannot fight typing.
  const [addr, setAddr] = useState(cfg.relayUrl);
  const [copied, setCopied] = useState(false);
  const active = cfg.mode === 'own';
  // One counter per control, so a refusal shakes the one that was touched.
  const [useShake, setUseShake] = useState(0);
  const [serveShake, setServeShake] = useState(0);
  const [addrShake, setAddrShake] = useState(0);

  return (
    <Card hue={3} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.access.ownRelay.body')}>{t('settings.access.ownRelay.title')}</SectionTitle>

      <div key={useShake} className={useShake > 0 ? 'glim-shake' : undefined}>
        <ToggleRow
          hue={3}
          label={t('settings.access.ownRelay.use')}
          checked={active}
          disabled={busy}
          onChange={(on) => void onPick(on).then((ok) => !ok && setUseShake((n) => n + 1))}
        />
      </div>

      {/* The extra space ties the sentence to the block below it. */}
      <p className="mt-2 text-sm text-carbon-textSub">{t('settings.access.ownRelay.lead')}</p>

      {/* The configuration appears only once this relay is chosen. */}
      {active && (
        <>
          <div className="flex flex-col gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 p-3">
            <div key={serveShake} className={serveShake > 0 ? 'glim-shake' : undefined}>
              <ToggleRow
                hue={1}
                label={t('settings.access.ownRelay.serveLabel')}
                hint={t('settings.access.ownRelay.serveHint')}
                checked={cfg.serve}
                disabled={busy}
                onChange={(v) => void onServe(v).then((ok) => !ok && setServeShake((n) => n + 1))}
              />
            </div>
            {cfg.serve && (
              <p className="text-[11px] text-carbon-textMuted">
                {t('settings.access.ownRelay.serveClients', { count: cfg.serveClients })}
              </p>
            )}
          </div>

          <div className="flex flex-col gap-2">
            <span className="text-xs font-semibold text-carbon-textSub">
              {t('settings.access.ownRelay.addressLabel')}
            </span>
            <div className="flex flex-col gap-2 sm:flex-row">
              <TextInput
                dir="ltr"
                spellCheck={false}
                className="min-w-0 flex-1"
                placeholder={t('settings.access.ownRelay.addressPlaceholder')}
                value={addr}
                onChange={(e) => setAddr(e.target.value)}
              />
              <Button
                key={addrShake}
                className={addrShake > 0 ? 'glim-shake' : ''}
                hue={1}
                disabled={busy || addr.trim() === cfg.relayUrl}
                onClick={() => void onSaveAddress(addr.trim()).then((ok) => !ok && setAddrShake((n) => n + 1))}
              >
                {t('settings.access.ownRelay.save')}
              </Button>
            </div>
            <p className="text-[11px] leading-relaxed text-statusWarn">{t('settings.access.ownRelay.everyInstance')}</p>
          </div>

          {/* A command to copy rather than an install button, since the machine
              needing a relay cannot host one for itself. */}
          <div className="mt-auto flex flex-col gap-2">
            <span className="flex items-center gap-1.5 text-xs font-semibold text-carbon-textSub">
              {t('settings.access.ownRelay.containerLabel')}
              <InfoBubble tip={paragraphs(t('settings.access.ownRelay.containerHint'))} />
            </span>
            <div className="flex items-start gap-2">
              <code
                className="glim-num min-w-0 flex-1 overflow-x-auto rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-xs leading-relaxed text-carbon-text"
                dir="ltr"
              >
                {RELAY_RUN_COMMAND}
              </code>
              {/* `labelled`, so the action follows the Beschriftung setting. */}
              <IconBadge
                labelled
                hue={1}
                icon={copied ? <IconCheck width={16} height={16} /> : <IconClipboard width={16} height={16} />}
                title={t('settings.access.tokens.copy')}
                aria-label={t('settings.access.tokens.copy')}
                onClick={async () => {
                  if (await copyToClipboard(RELAY_RUN_COMMAND)) {
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1800);
                  }
                }}
              />
            </div>
          </div>
        </>
      )}
    </Card>
  );
}

/**
 * The command that starts a relay. The relay keeps nothing across a restart,
 * which is why neither this nor Dockerfile.relay declares a volume.
 */
const RELAY_RUN_COMMAND =
  'docker run -d --name knightloader-relay -p 8760:8760 --restart unless-stopped ghcr.io/junkerderprovinz/knightloader-relay:latest';

/**
 * paragraphs turns blank-line-separated text into paragraphs, since HTML
 * collapses the newlines a translator wrote.
 */
function paragraphs(text: string): ReactNode {
  const parts = text.split('\n\n').filter((p) => p.trim() !== '');
  if (parts.length < 2) return text;
  return (
    <span className="flex flex-col gap-2">
      {parts.map((p, i) => (
        <span key={i}>{p}</span>
      ))}
    </span>
  );
}

function TokensSection({ cx }: { cx: (k: PendingKey) => string }) {
  const { toast } = useToast();
  const [tokens, setTokens] = useState<ApiToken[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState('');
  const [creating, setCreating] = useState(false);
  // Keyed onto the create button so a repeated refusal shakes again.
  const [createShake, setCreateShake] = useState(0);
  const [created, setCreated] = useState<NewApiToken | null>(null);
  const [copied, setCopied] = useState(false);
  const [revoking, setRevoking] = useState<string | null>(null);

  const load = () => fetchTokens().then(setTokens).catch(() => {});
  useEffect(() => {
    load();
  }, []);

  async function onCreate() {
    setCreating(true);
    try {
      const tok = await createToken(name.trim());
      setCreated(tok);
      setName('');
      await load();
    } catch (e) {
      // The window stays open with the typed name; the reason goes to the toast.
      toast(cx('settings.access.tokens.createFailed').replace('{error}', String(e).replace(/^Error:\s*/, '')), 'fail');
      setCreateShake((n) => n + 1);
    } finally {
      setCreating(false);
    }
  }

  async function onRevoke(id: string) {
    setRevoking(id);
    try {
      await revokeToken(id);
      await load();
    } finally {
      setRevoking(null);
    }
  }

  function closeCreate() {
    setShowCreate(false);
    setCreated(null);
    setCopied(false);
  }

  return (
    <>
      <Card hue={5} className="flex flex-col gap-3">
        <SectionTitle hint={cx('settings.access.tokens.intro')}>
          {cx('settings.access.tokens.title')}
        </SectionTitle>
        <ModuleToggle id="downloadclient" hue={5} />
        {tokens.length === 0 ? (
          <p className="text-sm text-carbon-textMuted">{cx('settings.access.tokens.empty')}</p>
        ) : (
          <div className="flex flex-col divide-y divide-carbon-border/40">
            {tokens.map((tok) => (
              <div key={tok.id} className="flex items-center gap-3 py-2.5 first:pt-0 last:pb-0">
                {/* An inert tile marking the row, not a control. */}
                <IconTile icon={<IconKey width={16} height={16} />} hue={5} />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm text-carbon-text">{tok.name}</div>
                  <div className="text-[11px] text-carbon-textMuted">
                    {cx('settings.access.tokens.created')} {fmtDate(tok.createdAt)}
                    {' · '}
                    {cx('settings.access.tokens.lastUsed')}{' '}
                    {tok.lastUsed ? fmtDate(tok.lastUsed) : cx('settings.access.tokens.neverUsed')}
                  </div>
                </div>
                {/* `labelled`, so the action follows the Beschriftung setting. */}
                <IconBadge
                  labelled
                  hue={5}
                  icon={<IconTrash width={16} height={16} />}
                  disabled={revoking === tok.id}
                  title={cx('settings.access.tokens.revoke')}
                  aria-label={cx('settings.access.tokens.revoke')}
                  onClick={() => void onRevoke(tok.id)}
                  className="shrink-0"
                />
              </div>
            ))}
          </div>
        )}
        {/* Below the list and left-aligned, as "add another". */}
        <div>
          <Button
            kind="secondary"
            hue={5}
            icon={<IconPlus width={16} height={16} />}
            onClick={() => setShowCreate(true)}
          >
            {cx('settings.access.tokens.new')}
          </Button>
        </div>
      </Card>

      {showCreate && !created && (
        <Modal
          title={cx('settings.access.tokens.new')}
          onClose={() => (creating ? undefined : closeCreate())}
          footer={
            <>
              <span className="flex-1" />
              <Button
                kind="ghost"
                labelled
                icon={<IconClose />}
                title={cx('settings.access.tokens.cancel')}
                onClick={closeCreate}
                disabled={creating}
              />
              <Button
                key={createShake}
                className={createShake > 0 ? 'glim-shake' : ''}
                kind="primary"
                onClick={() => void onCreate()}
                disabled={creating || name.trim() === ''}
              >
                {creating ? cx('settings.access.tokens.creating') : cx('settings.access.tokens.create')}
              </Button>
            </>
          }
        >
          <Field label={cx('settings.access.tokens.title')}>
            <TextInput
              autoFocus
              placeholder={cx('settings.access.tokens.namePlaceholder')}
              value={name}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && name.trim() !== '' && !creating) void onCreate();
              }}
            />
          </Field>
        </Modal>
      )}

      {created && (
        <Modal
          title={cx('settings.access.tokens.secretTitle')}
          onClose={closeCreate}
          footer={
            <>
              <span className="flex-1" />
              {/* The one way out, and it acknowledges the secret rather than
                  dismissing it, so the glyph is a check. */}
              <Button
                kind="primary"
                labelled
                icon={<IconCheck />}
                title={cx('settings.access.tokens.done')}
                onClick={closeCreate}
              />
            </>
          }
        >
          <div className="flex flex-col gap-3">
            <p className="text-sm text-statusFail">{cx('settings.access.tokens.secretWarning')}</p>
            <div className="flex items-center gap-2">
              <div className="min-w-0 flex-1 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2">
                <code className="glim-num block overflow-x-auto whitespace-nowrap text-xs text-carbon-text" dir="ltr">
                  {created.secret}
                </code>
              </div>
              {/* `labelled`, so the action follows the Beschriftung setting. */}
              <IconBadge
                labelled
                hue={5}
                icon={copied ? <IconCheck width={16} height={16} /> : <IconClipboard width={16} height={16} />}
                title={copied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                aria-label={copied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                onClick={async () => {
                  if (await copyToClipboard(created.secret)) {
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1800);
                  }
                }}
              />
            </div>
            <p className="text-[11px] text-carbon-textMuted" dir="ltr">
              {cx('settings.access.tokens.howToUse')}
            </p>
          </div>
        </Modal>
      )}
    </>
  );
}
