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
  logout,
  revokeToken,
  setPassword,
} from '../../lib/api';
import { copyToClipboard } from '../../lib/clipboard';
import { fmtDate } from '../../lib/format';
import { useT, type TranslationKey } from '../../lib/i18n';
import {
  IconCheck,
  IconClipboard,
  IconKey,
  IconPlus,
  IconSignOut,
  IconTrash,
} from '../../lib/icons';
import { useToast } from '../../lib/toast';
import { useDraft, useFeatures } from './context';
import { NeutralSwitch } from './controls';
import { label, useTx } from './tx';

/**
 * The Access page: the password lock (unchanged from before this wave), the
 * intake ports table (unchanged), and what build-plan.md section 8's Wave
 * 11 amendment on 11C adds - named API tokens and the Remote access story
 * (reachable addresses, a QR code, the PWA install BrowserTools.tsx also
 * offers, and the loud exposure warning).
 *
 * The strings the two new sections need are not in en.ts yet - locale files
 * are one writer's lane per wave (11G, phase 3 of this one), the same
 * arrangement Help.tsx, Diagnostics.tsx and BrowserTools.tsx already use, so
 * the lookup below asks the real catalogue first and falls back to English
 * here.
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

  // Nothing from settings.access.remote.* survives here now. The install and
  // store keys moved to settings.browsertools.* with their card; the
  // exposed-instance sentence is gone entirely (jdp, 2026-08-27: "Den text
  // hier entfernen") - the password card says "kein Passwort gesetzt" beside
  // the button that fixes it, in the warning hue when this instance is
  // actually reachable, and a paragraph restating that added length rather
  // than information. The `exposed` flag still drives that colour.

  'settings.access.identity.title': "This instance's identity",
  'settings.access.identity.nameLabel': 'Name',
  'settings.access.identity.namePlaceholder': 'e.g. Home server',
  'settings.access.identity.nameHint':
    'Offered first when pairing and in the QR code below, instead of whatever the OS or container runtime happens to call this machine. Optional - leave it empty to keep using that.',
  'settings.access.identity.domainsLabel': 'Known domains',
  'settings.access.identity.domainsHint':
    'Remembered automatically the first time a request actually arrives on one, so it stays listed here even when later requests come in over the LAN IP instead. Add one by hand for a domain that is already configured but has not been visited through yet - one full address per line, e.g. https://kl.example.com.',

  // The whole install/store family moved to BrowserTools.tsx with the card
  // it belonged to (jdp, 2026-08-27) and is spelled settings.browsertools.*
  // there - real, translated keys in every locale file, not PENDING ones.

  // One sentence for "can another KnightLoader reach this one", because which
  // road it takes is this card's business and not the reader's. The four cases
  // are written out rather than assembled from clauses: a sentence stitched
  // together at runtime reads like one in every language it was written in.

  // body/urlPlaceholder/urlHint/bothSidesHint/keyHint are real,
  // fully-translated locale keys now, rewritten (jdp, 2026-08-26: "Das ist
  // alles viel zu kompliziert! die infotexte sind verwirrend... Wo muss man
  // die relayadresse in der anderen instanz eingeben? ... Muss die
  // Relayadresse keine domain sein?") to state the two things a first-time
  // relay user actually needs and never found stated outright: the SAME
  // address+key goes into this same spot on every instance you connect,
  // and a domain is not required. Not listed here for the same shadowing
  // reason as the install keys above - see that comment. The card's shape
  // (one merged pairing+relay card, folded relay section) stays unchanged;
  // the problem measured out to be the copy, not the layout.

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
  const { features, toggle } = useFeatures();
  const { toast } = useToast();
  const [busyId, setBusyId] = useState<string | null>(null);

  // The listeners this instance answers on are a property of the build and the
  // environment, not of settings.json, so they are read out of the module
  // registry rather than described a second time here.
  const listeners = features.modules.filter((m) => m.page === 'access');

  // The same switch Modules.tsx's own row runs, reached from a second place
  // on purpose (jdp, 2026-08-24: "im Zugang tab ist bei CnL kein Toggle ...
  // für was brauchen wir das denn eigentlich" - the toggle belongs on the
  // page where the port itself lives, not only on the module registry's own
  // overview). Same failure handling as that row: the server refuses a
  // switch it cannot honour and says why, shown rather than swallowed.
  async function onToggle(id: string, next: boolean) {
    setBusyId(id);
    try {
      await toggle(id, next);
    } catch (e) {
      toast(tx('settings.modules.switchFailed', { reason: String(e).replace(/^Error:\s*/, '') }), 'fail');
    } finally {
      setBusyId(null);
    }
  }

  return (
    <div className="flex flex-col gap-10">
      {/* Identity first, password second (jdp, 2026-08-26) - and pulled out of
          the old RemoteAccessSection rather than left nested inside it: that
          section returned null until its own fetch resolved and skipped this
          card entirely on a desktop build. A name is a plain settings field
          with no such dependency - it belongs at the top regardless of
          deployment, not hidden behind a fetch that has nothing to do with
          it. */}
      <IdentityCard cx={cx} />
      <PasswordCard cx={cx} />

      {/* Only the exposed-warning banner now (jdp, 2026-08-26: "Die
          netzwerkzugriffcard entfernen wir. die ist völlig witzlos. auf der
          desktop version funktioniert das eh nicht") - NetworkAccessCard
          (this instance's own LAN address + QR) is gone, and with it the
          whole reason this used to be deployment-gated: nothing left in
          here depends on fetchRemoteAccess()'s deployment field at all. */}
      {/* The app card moved to the Browser & App tab (jdp, 2026-08-27) -
          getting KnightLoader onto a phone is the same question as getting
          it into a browser, and both now answer in one place. */}
      <RemoteAccessCard cx={cx} />
      {/* Two cards rather than one, and this is the one place on the page
          where jdp's own "es soll nur eine geben" does not apply - he asked
          for the split himself (2026-09-03) after asking what the relay can
          see. The reason it holds up: the card above is about the twelve
          words, which everybody uses; these two are about WHICH relay carries
          them, which is a separate question most people never open. */}
      <RelayCard />
      <OwnRelayCard />
      <TokensSection cx={cx} />

      {listeners.length > 0 && (
          <Card hue={6} className="flex flex-col gap-4">
            <SectionTitle hint={cx('settings.access.intakePortsHint')}>
              {tx('settings.sectionIntakePorts')}
            </SectionTitle>
            {listeners.map((m) => {
              const switchable = m.verdict === 'shipped' && m.switch !== 'none';
              return (
                <div key={m.id} className="flex items-baseline gap-3">
                  <span className="flex items-center text-sm text-carbon-text">
                    {label(tx, 'settings.module.', m.id)}
                    {m.reason && <InfoBubble tip={m.reason} />}
                  </span>
                  <span className="flex-1" />
                  <span className="text-[11px] text-carbon-textMuted" dir="ltr">
                    {m.detail || tx(m.enabled ? 'settings.modules.on' : 'settings.modules.off')}
                  </span>
                  {switchable && (
                    <NeutralSwitch
                      on={m.enabled}
                      disabled={busyId === m.id}
                      name={label(tx, 'settings.module.', m.id)}
                      onChange={(next) => void onToggle(m.id, next)}
                    />
                  )}
                </div>
              );
            })}
          </Card>
      )}
    </div>
  );
}

// PasswordCard owns the password lock. It saves on its own button rather than
// with the rest of the settings: a password is not a preference you change by
// accident while adjusting the speed limit, and it does not go through
// PUT /api/settings at all.
function PasswordCard({ cx }: { cx: (k: PendingKey) => string }) {
  const { t } = useT();
  const [auth, setAuth] = useState<AuthState | null>(null);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');
  const [signingOut, setSigningOut] = useState(false);
  // Whether this instance can actually be reached from off this machine -
  // the one fact that turns "no password is set" from a preference into a
  // problem. See routes_remote.go's own doc comment on Exposed.
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
    setError('');
    try {
      setAuth(await setPassword(current, next));
      setCurrent('');
      setNext('');
      setDone(true);
      setTimeout(() => setDone(false), 1800);
    } catch (e) {
      setError(String(e).replace(/^Error:\s*/, ''));
    }
  }

  const locked = auth?.enabled ?? false;

  return (
      <Card hue={0} className="flex flex-col gap-5">
        {/* Status stays a visible, at-a-glance line rather than moving fully
            into the bubble (jdp, 2026-08-26: "in eine i infobubble und
            schöner beschreiben") - whether this instance is protected is
            worth seeing without hovering anything. Only the WHY (what a
            password actually guards against) moves into the title's own
            hint bubble, with nicer wording than the old lockOff sentence it
            replaces. */}
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
          <Button kind="secondary" hue={0} onClick={onApply} disabled={locked ? current === '' : next === ''}>
            {next === '' && locked ? t('settings.removePassword') : t('settings.setPassword')}
          </Button>
          {/* Beside the button that changes it, not as a line of its own above
              the fields (jdp, 2026-08-27: "Der Hinweis 'kein Passwort gesetzt'
              soll rechts vom Button passwort setzten stehen"). The state and
              the control that changes it read as one thing that way, and the
              card loses a full-width sentence that said what four words say.
              Warning hue only when this instance is actually reachable from
              elsewhere - unprotected on a machine nothing can reach is a
              preference, not a problem. */}
          <span
            className={`text-sm ${locked ? 'text-statusOk' : exposed ? 'text-statusWarn' : 'text-carbon-textSub'}`}
          >
            {locked ? t('settings.lockOn') : t('settings.lockOff')}
          </span>
          {/* Only once a password is actually protecting this instance -
              seeing this page at all already means the current session is
              authenticated, the same "locked" flag Sidebar.tsx's own sign-out
              entry reads (jdp, 2026-08-26: "Wenn man ein passwort gesetzt hat
              muss man sich doch auch auslogen können oder? dafür fehlt ein
              button" - right here on the card that sets the password, not
              only tucked into the sidebar). */}
          {locked && (
            <Button
              kind="ghost"
              icon={<IconSignOut width={15} height={15} />}
              disabled={signingOut}
              onClick={async () => {
                setSigningOut(true);
                try {
                  await logout();
                  location.reload();
                } catch (e) {
                  // A failed sign-out (offline, a server hiccup) must not
                  // leave the button looking clicked-but-dead with no
                  // explanation - the same gap Sidebar.tsx's own identical
                  // call has, not repeated silently here a second time.
                  setSigningOut(false);
                  setError(e instanceof Error ? e.message : String(e));
                }
              }}
            >
              {t('auth.signOut')}
            </Button>
          )}
          {done && <span className="text-statusOk text-sm">{t('settings.passwordSaved')}</span>}
          {error && <span className="text-statusFail text-sm">{error}</span>}
        </div>
      </Card>
  );
}

// IdentityCard: an optional friendly name and the domains this instance is
// known to be reachable through, both read/written through the normal
// settings draft (useDraft) and the shared Save bar, same as any other field
// on this page - not a live-preview control like Look.tsx's, so there is no
// reason to save it any differently.
//
// KnownDomains is shown here for the SAME reason a bare LAN IP still shows in
// NetworkAccessCard's own addresses list below: this is a normal settings
// field, editable independently of whatever request happened to load this
// page. The list itself is otherwise populated automatically
// (routes_remote.go's own rememberDomain, the moment a request actually
// arrives on a domain) - this textarea only matters for a domain that is
// already configured but has not been visited through yet, so pairingSelf and
// the QR code below have something to prefer before that first visit
// happens.
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

// ---- Connecting instances (the phrase) --------------------------------------

/**
 * RemoteAccessCard is the one place to connect this instance with the other
 * KnightLoaders you run. ONE card, and one way into it - which took three
 * redesigns to arrive at (jdp, 2026-08-25: "das muss einfach ein Punkt sein
 * nicht mehr" merged pairing and relay; 2026-08-27, on finding a separate
 * "Von überall erreichbar" card beside this one: "Wieso gibt es jetzt...
 * zwei card? Es soll nur eine geben?" merged that in; then pairing and
 * Tailscale were removed outright rather than folded away, because a fold
 * is still a thing to wonder about).
 *
 * What is left is twelve words. They carry a secret, both ends derive the
 * same key from it and meet on a relay neither has to be reachable from -
 * see internal/seedphrase and internal/relay. No account, no login, no
 * third-party site, no address to copy.
 *
 * The card opens with a numbered how-to rather than with its buttons. Twelve
 * words is an odd enough thing to be handed that "what am I looking at"
 * comes before "what do I press", the same reason BrowserTools.tsx explains
 * dragging before it shows the bookmarklet link.
 */
function RemoteAccessCard({ cx }: { cx: (k: PendingKey, vars?: Record<string, string | number>) => string }) {
  const { t } = useT();

  // --- Connection phrase ---
  //
  // "Stored" and "connected" stay two separate fields on ConnectInfo, and
  // are reported separately below: a phrase with an unreachable relay is
  // configured but not working, and one word for both is exactly what made
  // the card this replaced unable to say which had gone wrong.
  const [conn, setConn] = useState<ConnectInfo | null>(null);
  const [phrase, setPhrase] = useState('');
  // Kept beside the phrase rather than derived from it: the matrix is the
  // server's, and re-encoding it here would be a second implementation of
  // the same code to keep in step with the pairing one.
  const [phraseQr, setPhraseQr] = useState<QRMatrix | null>(null);
  const [phraseBusy, setPhraseBusy] = useState(false);
  const [phraseErr, setPhraseErr] = useState('');
  const [phraseCopied, setPhraseCopied] = useState(false);
  const [joinInput, setJoinInput] = useState('');
  const [joinOpen, setJoinOpen] = useState(false);
  const [revealPw, setRevealPw] = useState('');
  const [revealOpen, setRevealOpen] = useState(false);

  const loadConn = () => fetchConnect().then(setConn).catch(() => {});
  useEffect(() => {
    void loadConn();
  }, []);

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
      // A rejected phrase comes back as a reason plus its specifics, never
      // as a sentence: the server cannot know which language to write in.
      // Everything else here is already a message.
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
      // Cleared here rather than left for the reload: a phrase still on
      // screen after "leave" reads as though nothing happened.
      setPhrase('');
      setPhraseQr(null);
      await loadConn();
    } catch (e) {
      setPhraseErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPhraseBusy(false);
    }
  }

  // Held until the connection state has answered: an empty card for a
  // feature that has not reported yet flickers into place a moment later,
  // which reads as a bug. A loading guard, not a capability check.
  if (!conn) return null;

  return (
    <Card hue={1} className="flex flex-col gap-4">
      {/* Two labelled bubbles in the title's own right slot, stacked (jdp,
          2026-08-27: "Der button Soll heißen 'Wie funktioniert das?' und
          rechts oben in der card sein. darunter soll noch ein button sein als
          statusanzeige"). The long "what actually happens" paragraph used to
          sit at the bottom of the how-to, where it was four lines of prose
          under three numbered steps - true, and read by nobody who was not
          already curious. Behind a bubble it is available to exactly the
          person who wants it.

          The second one is the connection state, which was a dot and a
          sentence in the body. As a bubble-carrying pill it says the state in
          one word and keeps the explanation one hover away. */}
      {/* Absolutely positioned, level with the notch, so the title row
          contributes NO height at all (jdp, twice: "zwischen cardtitelbadge
          und anleitung ist sehr viel leerraum"). Measured, because the first
          two attempts only shaved a few pixels off: card padding 20 + title
          row 16 + card gap 16 = 52px to the first line of text, of which the
          notch only covers the top 11. Side-by-side and negative margins got
          that to 41. Taking the row out of the flow entirely - the same trick
          SectionTitle's own title uses - leaves the text starting at the
          card's own padding, about 9px under the notch.

          The lead paragraph carries pr-72 so it cannot run under the badges
          on a narrow card; the numbered list below sits low enough not to
          need it.

          top-4, not -top-2 (jdp, 2026-08-30: "die beiden buttons Wie
          funktioniert das? und verbunden sitzen auf dem card rand und sind
          halbtransparent. die sollen in die card"). At -top-2 they straddled
          the card's own top edge, half on the card and half on the page
          behind it, which is what read as half-transparent: a surface2 badge
          has nothing to be a step above when half of it is standing on the
          page ground. Inside the padding box they sit on the card, level with
          the first line of the lead paragraph, in the room pr-72 was already
          reserving for them - so this costs the card no height, which is the
          reason the row was taken out of the flow in the first place. */}
      <div className="absolute right-5 top-4 z-10 flex items-center gap-2">
        <LabelBadge
          label={t('settings.access.phrase.howButton')}
          tip={paragraphs(t('settings.access.phrase.howWhat'))}
          hue={2}
        />
        <LabelBadge
          label={
            conn.connected
              ? t('settings.access.phrase.statusConnected')
              : t('settings.access.phrase.statusDisconnected')
          }
          tip={t('settings.access.phrase.statusHint')}
          tone={conn.connected ? 'ok' : 'fail'}
        />
      </div>

      <SectionTitle hint={t('settings.access.phrase.body')}>
        {t('settings.access.cardTitle')}
      </SectionTitle>

      {/* What this actually is, before any button. Twelve words is an odd
          enough thing to be handed that "what am I looking at" comes before
          "what do I press" - the same reason the bookmarklet card explains
          dragging before it shows the link. A numbered list rather than a
          paragraph (jdp: "Bitte aufzählungen immer untereinander"), and it
          stays visible after setup: somebody adding a fourth instance in six
          months needs step 2, and hiding it once step 1 is done is exactly
          when it stops being findable. */}
      <div className="flex flex-col gap-2">
        <p className="pr-72 text-sm text-carbon-textSub">{t('settings.access.phrase.howLead')}</p>
        <ol className="list-decimal space-y-1.5 pl-4 text-sm text-carbon-textSub">
          {/* The button names are interpolated from the button's OWN key
              rather than written into the sentence: a step that quotes a
              label is a step that can disagree with the label, and across 42
              languages that drift is invisible until somebody hunts for a
              button that is called something else on their screen. */}
          <li>{t('settings.access.phrase.howStep1', { button: t('settings.access.phrase.activate') })}</li>
          <li>{t('settings.access.phrase.howStep2', { button: t('settings.access.phrase.joinButton') })}</li>
          <li>{t('settings.access.phrase.howStep3')}</li>
        </ol>
      </div>

      {/* Nothing set up yet: two ways in, and they are the two ends of the
          same act - start a group, or join one somebody already started. */}
      {conn && !conn.active && (
        <div className="flex flex-col gap-3">
          {/* The warning sits BESIDE the buttons now (jdp, 2026-08-27: "Der
              Passwort-hinweistext rechts neben den ... badge"), not as a
              filled alert block above them. jdp's earlier call still holds -
              warn loudly, do not block - and the sentence still carries the
              part that is not obvious: this phrase reaches every instance in
              the group, so an unprotected instance puts the others at risk
              too, not only itself. What changed is that a full-width red
              slab for one sentence outweighed the two controls it was about. */}
          <div className="flex flex-wrap items-center gap-3">
            <Button hue={1} disabled={phraseBusy} onClick={() => void onActivate()}>
              {t('settings.access.phrase.activate')}
            </Button>
            <Button
              hue={3}
              icon={<IconClipboard width={14} height={14} />}
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

      {/* Set up: offer the phrase and allow leaving. Whether it is actually
          WORKING moved to the status pill in the title (jdp, 2026-08-27) -
          "stored" and "connected" stay two separate facts, because a phrase
          with an unreachable relay is configured but not working, and one
          word for both is what made the old relay card unable to say which
          had gone wrong. This block is now the "stored" half; the pill is the
          "connected" half, and it is visible whether or not a phrase exists. */}
      {conn?.active && (
        <div className="flex flex-col gap-3">
          {phrase ? (
            <div className="flex flex-col gap-2">
              <span className="text-xs font-semibold text-carbon-textSub">
                {t('settings.access.phrase.yourPhrase')}
              </span>
              <div className="flex items-start gap-2">
                <code
                  className="glim-num min-w-0 flex-1 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-xs leading-relaxed text-carbon-text"
                  dir="ltr"
                >
                  {phrase}
                </code>
                <IconBadge
                  hue={1}
                  icon={phraseCopied ? <IconCheck width={14} height={14} /> : <IconClipboard width={14} height={14} />}
                  title={phraseCopied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                  aria-label={phraseCopied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                  onClick={async () => {
                    if (await copyToClipboard(phrase)) {
                      setPhraseCopied(true);
                      setTimeout(() => setPhraseCopied(false), 1800);
                    }
                  }}
                />
              </div>
              <p className="text-[11px] text-carbon-textMuted">{t('settings.access.phrase.pasteHint')}</p>
              {/* The QR is for the case the words are worst at: typing twelve
                  of them into a phone. Same component the pairing code uses,
                  and it is absent rather than broken when the server could
                  not encode one. */}
              {phraseQr && (
                <div className="flex flex-col items-center gap-1.5 pt-1">
                  <QRCode matrix={phraseQr} label={phrase} size={144} />
                  <span className="text-[11px] text-carbon-textMuted">{t('settings.access.phrase.qrHint')}</span>
                </div>
              )}
              {/* A way back (jdp, 2026-08-27: "Wenn man die Phrase einblendet,
                  kann man sie nicht wieder ausblenden"). Revealing was a
                  one-way door: the only way to get the words off the screen
                  was to reload the page. That matters more than a tidiness
                  fix, because what is on screen is the key to the whole
                  group - somebody who showed it to read it out should be able
                  to put it away before the next person walks past, and having
                  to reload to do that is the kind of friction that ends with
                  people just leaving it up. */}
              <div className="flex">
                <Button hue={4} onClick={() => { setPhrase(''); setPhraseQr(null); }}>
                  {t('settings.access.phrase.hide')}
                </Button>
              </div>
            </div>
          ) : revealOpen ? (
            <div className="flex flex-col gap-2">
              <p className="text-[11px] text-carbon-textMuted">{t('settings.access.phrase.revealWhy')}</p>
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
            // Same row shape as the not-yet-configured case above: two
            // filled, hued controls and the warning beside them. "Gruppe
            // verlassen" was a ghost button, invisible until hovered (jdp,
            // 2026-08-27: "beide sollen auch im nicht ausgewähtlen zustand
            // als badge erkennbar sein und in der farbengine sein") - a
            // destructive action that only appears on hover is the one that
            // most needs to be visible before the pointer arrives.
            //
            // Warn, do not block, on the reveal itself: showing the phrase on
            // an unprotected instance hands over the whole GROUP, not just
            // this machine. Not worth refusing over - an instance nothing can
            // reach has no problem here, and this page cannot prove which
            // case it is looking at.
            <div className="flex flex-wrap items-center gap-3">
              <Button
                hue={1}
                onClick={() => {
                  setPhraseErr('');
                  // With no password there is nothing to re-enter, so this
                  // goes straight to the answer instead of showing an empty
                  // field somebody has to press past.
                  if (conn.passwordSet) setRevealOpen(true);
                  else void onReveal();
                }}
              >
                {t('settings.access.phrase.showAgain')}
              </Button>
              <Button hue={5} disabled={phraseBusy} onClick={() => void onLeave()}>
                {t('settings.access.phrase.leave')}
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

// ---- Which relay carries the words ------------------------------------------

/**
 * RelayCard says which relay this instance is on and, without softening it,
 * what that relay's operator can see.
 *
 * It exists because of a question jdp asked on 2026-09-03, having read the
 * About text he had just chosen: "nichts verlässt deine eigenen Mauern... KL
 * kommuniziert aber über ein Relay. Ist das kein Bruch des Versprechens?" It
 * was. The frames are sealed - the path, the body, the task list, the mobile
 * app's bearer token, and since that day the instance NAME as well, which
 * used to fall back to os.Hostname() and introduced a person to the relay
 * operator by name on every connection (see relay.Identity).
 *
 * What no amount of sealing removes is the outside of an envelope: the group
 * key the relay routes on, the public IP each member dials in from, and the
 * timing and size of what passes. That is what this card states, in the
 * bubble, in the same words the privacy document uses - because a promise a
 * person cannot check is worth less than a plain description they can.
 */
function RelayCard() {
  const { t } = useT();
  const [conn, setConn] = useState<ConnectInfo | null>(null);

  useEffect(() => {
    fetchConnect().then(setConn).catch(() => {});
  }, []);

  if (!conn) return null;

  return (
    <Card hue={2} className="flex flex-col gap-4">
      <div className="absolute right-5 top-4 z-10 flex items-center gap-2">
        <LabelBadge
          label={t('settings.access.relay.seesButton')}
          tip={paragraphs(t('settings.access.relay.seesTip'))}
          hue={3}
        />
        <LabelBadge
          label={conn.selfHosted ? t('settings.access.relay.own') : t('settings.access.relay.project')}
          tip={t('settings.access.relay.whichHint')}
          tone={conn.selfHosted ? 'ok' : undefined}
        />
      </div>

      <SectionTitle hint={t('settings.access.relay.body')}>{t('settings.access.relay.title')}</SectionTitle>

      <p className="pr-72 text-sm text-carbon-textSub">
        {conn.selfHosted ? t('settings.access.relay.leadOwn') : t('settings.access.relay.leadProject')}
      </p>

      {/* The address, always shown and always the REAL one - conn.relayUrl is
          what the client dials, not what a field on this page hoped it would.
          A card that claims a relay while the process talks to another is the
          precise failure this page exists to make impossible. */}
      <div className="flex flex-col gap-1">
        <span className="text-xs font-semibold text-carbon-textSub">{t('settings.access.relay.address')}</span>
        <code
          className="glim-num min-w-0 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-xs leading-relaxed text-carbon-text"
          dir="ltr"
        >
          {conn.relayUrl}
        </code>
      </div>
    </Card>
  );
}

/**
 * OwnRelayCard is the escape hatch, and it is here because the hatch existed
 * in the API and in the settings file and in NO user interface at all - the
 * privacy document and the design spec both told people to run their own
 * relay, and there was no field anywhere to say so. A documented way out that
 * cannot be taken is not a way out.
 *
 * Three ways, in order of how much a person has to build:
 *
 *  1. This instance BECOMES the relay, on the address it already answers on,
 *     behind the reverse proxy and the certificate it already has. One
 *     switch, no second container, nothing to list anywhere. jdp asked for a
 *     press-a-button container install "wie beim widget in BV, dann müssen
 *     wir das relay nicht extra auf CA listen" - this reaches that goal
 *     without the container, and so without the docker socket or the SSH
 *     credentials such a button would need.
 *  2. An address you already run - a relay on a VPS, or another KnightLoader
 *     with the switch above turned on.
 *  3. The container, for the case the other two cannot serve: nobody's box is
 *     reachable, so the relay has to live somewhere that is.
 *
 * The order is deliberate and so is the warning at the bottom: the address is
 * per-instance and is NOT carried by the phrase, so pointing one instance at
 * a relay and forgetting the others leaves a group that quietly cannot see
 * itself.
 */
function OwnRelayCard() {
  const { t } = useT();
  const [cfg, setCfg] = useState<RelayConfig | null>(null);
  const [addr, setAddr] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [copied, setCopied] = useState('');
  const { toast } = useToast();

  const load = () =>
    fetchRelayConfig()
      .then((c) => {
        setCfg(c);
        setAddr(c.relayUrl);
      })
      .catch(() => {});
  useEffect(() => {
    void load();
  }, []);

  async function save(relayUrl: string, serve?: boolean) {
    setErr('');
    setBusy(true);
    try {
      const c = await saveRelayConfig(relayUrl, undefined, serve);
      setCfg(c);
      setAddr(c.relayUrl);
      toast(t('settings.access.ownRelay.saved'));
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function copy(what: string, text: string) {
    if (await copyToClipboard(text)) {
      setCopied(what);
      setTimeout(() => setCopied(''), 1800);
    }
  }

  if (!cfg) return null;

  return (
    <Card hue={3} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.access.ownRelay.body')}>{t('settings.access.ownRelay.title')}</SectionTitle>

      <p className="text-sm text-carbon-textSub">{t('settings.access.ownRelay.lead')}</p>

      {/* Way one. A switch rather than a button, because it is a state this
          instance is in and not an action it performs once. */}
      <div className="flex flex-col gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 p-3">
        <ToggleRow
          hue={1}
          label={t('settings.access.ownRelay.serveLabel')}
          hint={t('settings.access.ownRelay.serveHint')}
          checked={cfg.serve}
          disabled={busy}
          onChange={(v) => void save(cfg.relayUrl, v)}
        />
        {cfg.serve && (
          <p className="text-[11px] text-carbon-textMuted">
            {t('settings.access.ownRelay.serveClients', { count: cfg.serveClients })}
          </p>
        )}
      </div>

      {/* Way two. */}
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
          <Button hue={1} disabled={busy || addr.trim() === cfg.relayUrl} onClick={() => void save(addr.trim())}>
            {t('settings.access.ownRelay.save')}
          </Button>
          {cfg.relayUrl !== '' && (
            <Button hue={5} disabled={busy} onClick={() => void save('')}>
              {t('settings.access.ownRelay.reset')}
            </Button>
          )}
        </div>
        <p className="text-[11px] leading-relaxed text-statusWarn">{t('settings.access.ownRelay.everyInstance')}</p>
      </div>

      {/* Way three. Copyable rather than clickable: the machine that needs a
          relay is by definition not the machine that can host one for it, so
          a button here would run the container on the wrong box. */}
      <div className="flex flex-col gap-2">
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
          <IconBadge
            hue={1}
            icon={
              copied === 'run' ? <IconCheck width={14} height={14} /> : <IconClipboard width={14} height={14} />
            }
            title={t('settings.access.tokens.copy')}
            aria-label={t('settings.access.tokens.copy')}
            onClick={() => void copy('run', RELAY_RUN_COMMAND)}
          />
        </div>
      </div>

      {err && <p className="text-sm text-statusFail">{err}</p>}
    </Card>
  );
}

/**
 * The one command that starts a relay, kept beside the card that offers it.
 *
 * No volume and no environment beyond the port: the relay holds nothing
 * across a restart except the list of who is currently connected, which is
 * why Dockerfile.relay declares no VOLUME either. Anyone reading this should
 * be able to see at a glance that there is nothing here to back up.
 */
const RELAY_RUN_COMMAND =
  'docker run -d --name knightloader-relay -p 8760:8760 --restart unless-stopped ghcr.io/junkerderprovinz/knightloader-relay:latest';

/**
 * Turns blank-line-separated text into real paragraphs for a bubble.
 *
 * HTML collapses the newlines a translator wrote, so a three-paragraph
 * explanation would otherwise arrive as one wall of text - and the phrase
 * card's bubble holds the longest piece of prose in the app.
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

// ---- API tokens -------------------------------------------------------------

function TokensSection({ cx }: { cx: (k: PendingKey) => string }) {
  const [tokens, setTokens] = useState<ApiToken[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState('');
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState('');
  const [created, setCreated] = useState<NewApiToken | null>(null);
  const [copied, setCopied] = useState(false);
  const [revoking, setRevoking] = useState<string | null>(null);

  const load = () => fetchTokens().then(setTokens).catch(() => {});
  useEffect(() => {
    load();
  }, []);

  async function onCreate() {
    setCreateError('');
    setCreating(true);
    try {
      const tok = await createToken(name.trim());
      setCreated(tok);
      setName('');
      await load();
    } catch (e) {
      setCreateError(cx('settings.access.tokens.createFailed').replace('{error}', String(e).replace(/^Error:\s*/, '')));
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
    setCreateError('');
    setCopied(false);
  }

  return (
    <>
      <Card hue={5} className="flex flex-col gap-3">
        <SectionTitle hint={cx('settings.access.tokens.intro')}>
          {cx('settings.access.tokens.title')}
        </SectionTitle>
        {tokens.length === 0 ? (
          <p className="text-sm text-carbon-textMuted">{cx('settings.access.tokens.empty')}</p>
        ) : (
          <div className="flex flex-col divide-y divide-carbon-border/40">
            {tokens.map((tok) => (
              <div key={tok.id} className="flex items-center gap-3 py-2.5 first:pt-0 last:pb-0">
                {/* The row's own square badge rather than a bare glyph (jdp,
                    2026-08-27), at the one size every square badge in this
                    app shares. Inert - it marks the row, it is not a control -
                    which is what IconTile is for. */}
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
                <IconBadge
                  hue={5}
                  icon={<IconTrash width={15} height={15} />}
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
        {/* Back to a normal button on the left (jdp, 2026-08-27: "Der token
            erstellen button wieder zurück auf normal. also ganz links
            platzeiren"). It had been shrunk into the title row's right slot;
            below the list and left-aligned it reads as "add another one to
            what you just looked at" rather than as a header utility. */}
        <div>
          <Button
            kind="secondary"
            hue={5}
            icon={<IconPlus width={14} height={14} />}
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
              <Button kind="ghost" onClick={closeCreate} disabled={creating}>
                {cx('settings.access.tokens.cancel')}
              </Button>
              <Button kind="primary" onClick={() => void onCreate()} disabled={creating || name.trim() === ''}>
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
          {createError && <p className="mt-2 text-sm text-statusFail">{createError}</p>}
        </Modal>
      )}

      {created && (
        <Modal
          title={cx('settings.access.tokens.secretTitle')}
          onClose={closeCreate}
          footer={
            <>
              <span className="flex-1" />
              <Button kind="primary" onClick={closeCreate}>
                {cx('settings.access.tokens.done')}
              </Button>
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
              <IconBadge
                hue={5}
                icon={copied ? <IconCheck width={14} height={14} /> : <IconClipboard width={14} height={14} />}
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
