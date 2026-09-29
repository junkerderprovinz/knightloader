// PhraseCard is where an instance joins a group. Outside one it offers two
// tiles: generate a phrase, or enter one that already exists. Inside one it
// shows who else is there, and when nobody has come after a minute it says
// the likely reasons with the steps that fix them.
import { useEffect, useState, type ReactNode } from 'react';
import { Button, Card, InfoBubble, LabelBadge, PasswordInput, SectionTitle } from '../../../components/ui';
import { QRCode } from '../../../components/QRCode';
import {
  ApiError,
  PhraseRejected,
  activateConnect,
  joinConnect,
  leaveConnect,
  revealConnect,
  type ConnectInfo,
  type GroupApp,
  type GroupMember,
  type QRMatrix,
} from '../../../lib/api';
import { copyToClipboard } from '../../../lib/clipboard';
import { useT } from '../../../lib/i18n';
import {
  IconCheck,
  IconCheckDrawn,
  IconChevronEnd,
  IconClipboard,
  IconEye,
  IconEyeOff,
  IconKeyboard,
  IconPhone,
  IconPlus,
  IconRetry,
  IconSignOut,
  IconWarning,
} from '../../../lib/icons';
import { useShake } from '../../../lib/useShake';
import { useToast } from '../../../lib/toast';
import { PhraseInput } from './PhraseInput';
import { STAGE_BADGE, clock, pairStage } from './pairStage';
import { RouteGlyph } from './pairingArt';

type T = ReturnType<typeof useT>['t'];

/** The id of the login password's card on the same page. */
export const PASSWORD_ANCHOR = 'login-password';

/** refusalText says what is wrong with a phrase or a request the server
 *  turned down, in the reader's language. */
export function refusalText(t: T, e: unknown): string {
  if (e instanceof PhraseRejected) {
    if (e.reason === 'word_count') return t('pairing.errWordCount', { count: e.count });
    if (e.reason === 'unknown_word') return t('pairing.errUnknownWord', { position: e.position, word: e.word });
    return t('pairing.errChecksum');
  }
  if (e instanceof ApiError) {
    if (e.code === 'passwordWrong') return t('pairing.passwordWrong');
    if (e.code === 'phraseExists') return t('pairing.errExists');
  }
  return e instanceof Error && e.message ? e.message : t('pairing.actionError');
}

function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

/** useJoinedAgo counts on from the seconds the server last reported, so the
 *  search timer runs and the minute passes between two reloads. */
function useJoinedAgo(group: ConnectInfo): number {
  const [base, setBase] = useState({ ago: group.joinedAgo, at: Date.now() });
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const at = Date.now();
    setBase({ ago: group.joinedAgo, at });
    setNow(at);
  }, [group]);
  const waiting = group.active && group.members.length === 0 && !group.memberSeen;
  useEffect(() => {
    if (!waiting) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [waiting]);
  return base.ago + Math.max(0, Math.floor((now - base.at) / 1000));
}

function Caption({ children }: { children: ReactNode }) {
  return <p className="mb-2 flex items-center gap-1.5 text-xs font-semibold text-carbon-textSub">{children}</p>;
}

function WordGrid({ phrase, qr, t }: { phrase: string; qr: QRMatrix | null; t: T }) {
  return (
    <div>
      <Caption>
        {t('pairing.wordsLabel')} <InfoBubble tip={t('pairing.wordsTip')} />
      </Caption>
      <div className="flex flex-col items-start gap-4 sm:flex-row">
        <ol className="grid w-full max-w-[52rem] flex-1 grid-cols-2 gap-2 md:grid-cols-4" aria-label={t('pairing.wordsLabel')}>
          {phrase.split(/\s+/).map((w, i) => (
            <li
              key={i}
              className="flex min-w-0 items-baseline gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-1.5 text-sm"
            >
              <span className="glim-num w-4.5 shrink-0 text-end text-xs text-carbon-textMuted">{i + 1}</span>
              <span dir="ltr" className="truncate font-mono text-carbon-text">
                {w}
              </span>
            </li>
          ))}
        </ol>
        {/* The phone app scans the words instead of taking them typed. */}
        {qr && (
          <figure className="flex shrink-0 flex-col items-center gap-1.5">
            <QRCode matrix={qr} label={phrase} size={128} />
            <figcaption className="text-[11px] text-carbon-textMuted">{t('pairing.qrCaption')}</figcaption>
          </figure>
        )}
      </div>
    </div>
  );
}

function MemberRow({ m, t }: { m: GroupMember; t: T }) {
  return (
    <li className="flex flex-wrap items-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2">
      <span className="min-w-0 break-words text-sm font-semibold text-carbon-text">{m.name || m.id}</span>
      <span className="ms-auto">
        <LabelBadge label={m.direct ? t('pairing.direct') : t('pairing.viaRelay')} tone={m.direct ? 'ok' : undefined} />
      </span>
    </li>
  );
}

function AppRow({ app }: { app: GroupApp }) {
  return (
    <li className="flex flex-wrap items-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2">
      <span className="shrink-0 text-carbon-textMuted [&>svg]:h-4.5 [&>svg]:w-4.5">
        <IconPhone />
      </span>
      <span className="min-w-0 break-words text-sm font-semibold text-carbon-text">{app.name || app.id}</span>
    </li>
  );
}

function WaitRow({ text, seconds }: { text: string; seconds?: number }) {
  return (
    <div className="flex items-center gap-3 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2.5 text-carbon-textSub">
      <span className="flex shrink-0 gap-1" aria-hidden="true">
        {[0, 1, 2].map((i) => (
          <span key={i} className="glim-live h-1.5 w-1.5 rounded-full bg-accent" style={{ animationDelay: `${i * 200}ms` }} />
        ))}
      </span>
      <span className="text-sm">{text}</span>
      {seconds !== undefined && <span className="glim-num ms-auto text-xs text-carbon-textMuted">{clock(seconds)}</span>}
    </div>
  );
}

/** RelayLine says in one line how this instance reaches the rest of its group. */
function RelayLine({ group, t }: { group: ConnectInfo; t: T }) {
  const mode = group.relayMode;
  let dot = 'bg-statusOkSolid';
  let text = t(mode === 'own' ? 'pairing.relayOwn' : 'pairing.relayProject');
  let tip: string | null = null;
  if (mode === 'off') {
    dot = 'bg-carbon-textMuted';
    text = t('pairing.relayOff');
    tip = t('pairing.relayOffTip');
  } else if (!group.connected) {
    dot = 'bg-statusWarnSolid';
    text = t(mode === 'own' ? 'pairing.relayOwnDown' : 'pairing.relayProjectDown');
    tip = t('pairing.relayDownTip');
  }
  return (
    <span className="inline-flex min-w-0 items-center gap-2 text-sm text-carbon-textSub" data-testid="relay-line">
      <span className="shrink-0 text-carbon-textMuted [&>svg]:h-4.5 [&>svg]:w-4.5">
        <RouteGlyph kind={mode} />
      </span>
      <span className={`h-2 w-2 shrink-0 rounded-full ${dot}`} aria-hidden="true" />
      <span>{text}</span>
      {tip && <InfoBubble tip={tip} />}
    </span>
  );
}

/** Hint is the warning panel for something the reader has to act on. */
function Hint({ title, tip, children }: { title: string; tip?: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3 rounded-[var(--radius-control)] bg-statusWarnBgSoft p-4 text-sm text-carbon-textSub">
      <h3 className="flex items-center gap-2 text-[15px] font-semibold text-carbon-text">
        <span className="text-statusWarn [&>svg]:h-4.5 [&>svg]:w-4.5">
          <IconWarning />
        </span>
        {title}
        {tip && <InfoBubble tip={tip} />}
      </h3>
      {children}
    </section>
  );
}

function Rule() {
  return <hr className="w-full border-0 border-t border-carbon-border" />;
}

function Lead({ title, children }: { title: string; children: ReactNode }) {
  return (
    <p className="min-w-0">
      <strong className="font-semibold text-carbon-text">{title}</strong> {children}
    </p>
  );
}

/** StepNumber is the round mark of one fix step: its number, a check once
 *  done, or muted while it has to wait for the step before. */
function StepNumber({ n, done = false, waiting = false }: { n: number; done?: boolean; waiting?: boolean }) {
  const tone = done
    ? 'bg-statusOkSolid text-carbon-background'
    : waiting
      ? 'bg-carbon-surface3 text-carbon-textSub'
      : 'bg-accent text-accentContrast';
  return (
    <span className={`mt-1 grid h-6 w-6 place-items-center rounded-full text-xs font-bold ${tone}`}>
      {done ? <IconCheck width={14} height={14} /> : n}
    </span>
  );
}

function RelayDownHint({ group, onRefresh, t }: { group: ConnectInfo; onRefresh: () => void; t: T }) {
  const own = group.relayMode === 'own';
  const [before, after] = t('pairing.relayCheckProject').split('{host}');
  return (
    <Hint title={t('relay.notConnected')} tip={t('pairing.relayCheckTip')}>
      <p>{t('pairing.relayCheckLead')}</p>
      <ul className="flex list-disc flex-col gap-1 ps-5">
        <li>
          {own ? (
            t('pairing.relayCheckOwn')
          ) : (
            <>
              {before}
              <span dir="ltr" className="font-mono text-[0.92em] text-carbon-text">
                {hostOf(group.projectRelayUrl)}
              </span>
              {after}
            </>
          )}
        </li>
        <li>{t('pairing.relayCheckFilter')}</li>
      </ul>
      <div className="flex justify-end">
        <Button kind="secondary" icon={<IconRetry />} onClick={onRefresh}>
          {t('pairing.checkAgain')}
        </Button>
      </div>
    </Hint>
  );
}

export function PhraseCard({
  group,
  onGroup,
  onRefresh,
  hue,
}: {
  group: ConnectInfo;
  onGroup: (g: ConnectInfo) => void;
  /** Asks the server for the group again, for "Check again" and after leaving. */
  onRefresh: () => void;
  hue?: number;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [phrase, setPhrase] = useState<string | null>(null);
  const [qr, setQr] = useState<QRMatrix | null>(null);
  const [createdHere, setCreatedHere] = useState(false);
  const [entering, setEntering] = useState(false);
  // Set once "Leave group" in the hint is pressed, so the hint stays up with
  // its second step instead of turning back into the two tiles.
  const [fixing, setFixing] = useState(false);
  const [password, setPassword] = useState('');
  const [askPassword, setAskPassword] = useState(false);
  const [confirmLeave, setConfirmLeave] = useState(false);
  const [busy, setBusy] = useState(false);
  const [shake, setShake] = useState(0);
  const [copies, setCopies] = useState(0);
  const joinedAgo = useJoinedAgo(group);

  const stage = pairStage(group, joinedAgo, createdHere);

  async function run(action: () => Promise<void>) {
    setBusy(true);
    try {
      await action();
    } catch (e) {
      toast(refusalText(t, e), 'fail');
      setShake((n) => n + 1);
    } finally {
      setBusy(false);
    }
  }

  function forget() {
    setPhrase(null);
    setQr(null);
    setCreatedHere(false);
    setAskPassword(false);
    setConfirmLeave(false);
  }

  const create = () =>
    run(async () => {
      const r = await activateConnect();
      setPhrase(r.phrase);
      setQr(r.qr ?? null);
      setCreatedHere(true);
      onGroup(r.info);
    });

  async function join(words: string): Promise<string | null> {
    setBusy(true);
    try {
      const g = await joinConnect(words);
      forget();
      setEntering(false);
      setFixing(false);
      onGroup(g);
      toast(t('pairing.joined'), 'ok');
      return null;
    } catch (e) {
      return refusalText(t, e);
    } finally {
      setBusy(false);
    }
  }

  const show = () =>
    run(async () => {
      const r = await revealConnect(password);
      setPhrase(r.phrase);
      setQr(r.qr ?? null);
      setAskPassword(false);
      setPassword('');
    });

  /** leave takes this instance out of its group, then runs then to set up
   *  what the card shows next. */
  const leave = (then: () => void) =>
    run(async () => {
      await leaveConnect();
      forget();
      then();
      onGroup({ ...group, active: false, members: [], joinedAgo: 0, memberSeen: false });
      onRefresh();
    });

  async function copy() {
    if (!phrase) return;
    if (await copyToClipboard(phrase)) setCopies((n) => n + 1);
    else setShake((n) => n + 1);
  }

  // The password card sits above this one on the same page.
  const noPasswordNote = !group.passwordSet && (
    <div className="flex flex-wrap items-center gap-3 rounded-[var(--radius-control)] bg-statusWarnBgSoft px-3 py-2.5">
      <p className="min-w-0 flex-[1_1_16rem] text-sm leading-relaxed text-carbon-text">{t('pairing.noPasswordHint')}</p>
      <Button
        kind="secondary"
        onClick={() => document.getElementById(PASSWORD_ANCHOR)?.scrollIntoView({ behavior: 'smooth', block: 'start' })}
      >
        {t('settings.setPassword')}
      </Button>
    </div>
  );

  const passwordPrompt = (
    <div className="flex flex-col gap-1.5">
      <span className="flex items-center gap-1.5 text-xs font-semibold text-carbon-textSub">
        {t('pairing.passwordLabel')}
        <InfoBubble tip={t('pairing.passwordTip')} />
      </span>
      <PasswordInput
        value={password}
        onChange={setPassword}
        autoComplete="current-password"
        showLabel={t('common.showPassword')}
        hideLabel={t('common.hidePassword')}
      />
      <div className="flex flex-wrap items-center justify-end gap-2">
        <Button kind="secondary" onClick={() => setAskPassword(false)}>
          {t('common.cancel')}
        </Button>
        <Button icon={<IconEye />} shake={shake} onClick={() => void show()} disabled={busy || password === ''}>
          {t('pairing.show')}
        </Button>
      </div>
    </div>
  );

  const revealed = phrase ? <WordGrid phrase={phrase} qr={qr} t={t} /> : askPassword ? passwordPrompt : null;

  const showToggle = phrase ? (
    <Button kind="secondary" icon={<IconEyeOff />} onClick={() => setPhrase(null)}>
      {t('pairing.hide')}
    </Button>
  ) : (
    <Button
      kind="secondary"
      icon={<IconEye />}
      shake={askPassword ? 0 : shake}
      // Without a password there is nothing to enter first.
      onClick={() => (group.passwordSet ? setAskPassword(true) : void show())}
      disabled={busy || askPassword}
    >
      {t('pairing.show')}
    </Button>
  );

  const leaveButton = confirmLeave ? (
    <Button kind="secondary" icon={<IconSignOut />} shake={shake} onClick={() => void leave(() => setEntering(false))} disabled={busy}>
      {t('pairing.confirmLeave')}
    </Button>
  ) : (
    <Button kind="secondary" icon={<IconSignOut />} onClick={() => setConfirmLeave(true)}>
      {t('pairing.leave')}
    </Button>
  );

  const stateRow = (label: string, tone: 'hue' | 'neutral' | 'warn' | 'ok', withName = true) => (
    <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5" data-testid="pair-state">
      <LabelBadge
        label={label}
        hue={tone === 'hue' ? hue : undefined}
        tone={tone === 'ok' || tone === 'warn' ? tone : undefined}
      />
      {withName && <span className="text-xs text-carbon-textMuted">{t('pairing.thisInstance', { name: group.name })}</span>}
    </div>
  );

  const enterTip = t('pairing.enterTip', {
    path: [t('settings.title'), t('settings.nav.access'), t('pairing.show')].join(', '),
  });

  const foot = (buttons: boolean) => (
    <div className="flex flex-wrap items-center gap-3 border-t border-carbon-border pt-4">
      <RelayLine group={group} t={t} />
      {buttons && (
        <div className="flex w-full flex-wrap items-center justify-end gap-2 sm:ms-auto sm:w-auto">
          {showToggle}
          {leaveButton}
        </div>
      )}
    </div>
  );

  function aloneHint(left: boolean) {
    return (
      <Hint title={t('pairing.aloneTitle')}>
        {!left && <p>{t('pairing.aloneLead')}</p>}
        {!left && <Rule />}
        <Lead title={t('pairing.twoTitle')}>{t('pairing.twoBody')}</Lead>
        <div className="grid grid-cols-[24px_minmax(0,1fr)] items-start gap-x-3 gap-y-2.5">
          <StepNumber n={1} done={left} />
          <div className="flex min-h-8 flex-wrap items-center gap-x-3 gap-y-2">
            <span className="font-medium text-carbon-text">{t('pairing.fixLeave')}</span>
            {left ? (
              <span className="ms-auto text-sm text-statusOk">{t('pairing.fixLeft')}</span>
            ) : (
              <span className="sm:ms-auto">
                <Button
                  kind="secondary"
                  icon={<IconSignOut />}
                  shake={shake}
                  onClick={() => void leave(() => setFixing(true))}
                  disabled={busy}
                >
                  {t('pairing.leave')}
                </Button>
              </span>
            )}
          </div>
          <StepNumber n={2} waiting={!left} />
          <div className="flex min-h-8 items-center">
            <span className="inline-flex items-center gap-1.5 font-medium text-carbon-text">
              {t('pairing.fixEnter')} <InfoBubble tip={enterTip} />
            </span>
          </div>
          <div className="col-start-2">
            <PhraseInput
              id="pairing-phrase-other"
              label={t('pairing.enterLabelOther')}
              tip={enterTip}
              bare
              disabled={!left}
              busy={busy}
              onPair={join}
            />
          </div>
        </div>
        {!left && (
          <>
            <Rule />
            <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
              <div className="min-w-0 flex-[1_1_16rem]">
                <Lead title={t('pairing.notYetTitle')}>{t('pairing.notYetBody')}</Lead>
              </div>
              {showToggle}
            </div>
            {revealed}
          </>
        )}
        {group.relayMode === 'off' && (
          <>
            <Rule />
            <Lead title={t('pairing.otherNetTitle')}>{t('pairing.otherNetBody')}</Lead>
          </>
        )}
      </Hint>
    );
  }

  const relayHint = (stage === 'alone' || stage === 'gone') && group.relayMode !== 'off' && !group.connected && (
    <RelayDownHint group={group} onRefresh={onRefresh} t={t} />
  );

  let body: ReactNode;
  if (stage === 'unpaired' && fixing) {
    body = (
      <>
        {stateRow(t('pairing.stateNotPaired'), 'neutral', false)}
        {noPasswordNote}
        {aloneHint(true)}
      </>
    );
  } else if (stage === 'unpaired') {
    body = (
      <>
        {noPasswordNote}
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Choice
            glyph={<IconPlus />}
            title={t('pairing.create')}
            sub={t('pairing.createSub')}
            pressed={false}
            dim={entering}
            disabled={busy}
            shake={shake}
            onClick={() => void create()}
          />
          <Choice
            glyph={<IconKeyboard />}
            title={t('pairing.enter')}
            sub={t('pairing.enterSub')}
            pressed={entering}
            dim={false}
            onClick={() => setEntering(true)}
          />
        </div>
        {entering && (
          <PhraseInput
            id="pairing-phrase"
            label={t('pairing.enterLabel')}
            tip={enterTip}
            busy={busy}
            onPair={join}
            onCancel={() => setEntering(false)}
          />
        )}
      </>
    );
  } else if (stage === 'alone') {
    body = (
      <>
        {stateRow(t(STAGE_BADGE.alone.key), STAGE_BADGE.alone.tone)}
        {noPasswordNote}
        {relayHint}
        {aloneHint(false)}
        {foot(false)}
      </>
    );
  } else {
    const badge = STAGE_BADGE[stage];
    body = (
      <>
        {stateRow(t(badge.key), badge.tone)}
        {noPasswordNote}
        {relayHint}
        {revealed}
        {stage === 'new' && (
          <>
            <div className="flex flex-wrap items-center gap-2">
              {phrase && (
                <Button
                  kind="secondary"
                  icon={copies > 0 ? <IconCheckDrawn /> : <IconClipboard />}
                  confirm={copies}
                  onClick={() => void copy()}
                >
                  {copies > 0 ? t('common.copied') : t('common.copy')}
                </Button>
              )}
              <span className="ms-auto inline-flex flex-wrap items-center gap-2 text-sm text-carbon-textMuted">
                {t('pairing.notFirst')}
                <Button kind="secondary" icon={<IconKeyboard />} onClick={() => void leave(() => setEntering(true))} disabled={busy}>
                  {t('pairing.enter')}
                </Button>
              </span>
            </div>
            <NextStep t={t} />
          </>
        )}
        <div>
          <Caption>{t('pairing.membersTitle')}</Caption>
          {stage === 'paired' ? (
            <ul className="flex flex-col gap-2">
              {group.members.map((m) => (
                <MemberRow key={m.id} m={m} t={t} />
              ))}
              {group.apps
                .filter((a) => a.connected)
                .map((a) => (
                  <AppRow key={a.id} app={a} />
                ))}
            </ul>
          ) : stage === 'new' ? (
            <WaitRow text={t('pairing.waitNext')} />
          ) : (
            <WaitRow text={t('pairing.searching')} seconds={stage === 'searching' ? joinedAgo : undefined} />
          )}
        </div>
        {foot(true)}
      </>
    );
  }

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('pairing.phraseHint')}>{t('pairing.phraseTitle')}</SectionTitle>
      <div className="flex flex-col gap-4.5" data-stage={fixing && stage === 'unpaired' ? 'fixing' : stage}>
        {body}
      </div>
    </Card>
  );
}

/** Choice is one of the two ways into a group, as a tile large enough to say
 *  what happens after it. */
function Choice({
  glyph,
  title,
  sub,
  pressed,
  dim,
  disabled = false,
  shake = 0,
  onClick,
}: {
  glyph: ReactNode;
  title: string;
  sub: string;
  pressed: boolean;
  dim: boolean;
  disabled?: boolean;
  shake?: number;
  onClick: () => void;
}) {
  const ref = useShake<HTMLButtonElement>(shake);
  return (
    <button
      ref={ref}
      type="button"
      aria-pressed={pressed}
      disabled={disabled}
      onClick={onClick}
      className={`group grid grid-cols-[44px_minmax(0,1fr)] items-center gap-3.5 rounded-[var(--radius-control)] bg-carbon-surface2 p-4
        text-start transition-colors enabled:hover:bg-carbon-surface3 disabled:cursor-not-allowed disabled:opacity-45
        focus-visible:shadow-[0_0_0_2px_var(--focus-ring)] focus-visible:outline-none ${
          pressed ? 'ring-2 ring-inset ring-accent' : ''
        } ${dim ? 'opacity-60 hover:opacity-100' : ''}`}
    >
      <span
        className={`grid h-11 w-11 place-items-center rounded-[var(--radius-control)] ${
          pressed ? 'bg-accent text-accentContrast' : 'bg-carbon-surface3 text-carbon-text group-enabled:group-hover:bg-carbon-hoverRaised'
        }`}
      >
        {glyph}
      </span>
      <span>
        <span className="block text-[15px] font-semibold leading-snug text-carbon-text">{title}</span>
        <span className="mt-0.5 block text-sm leading-snug text-carbon-textSub">{sub}</span>
      </span>
    </button>
  );
}

/** NextStep points at the button to press on the other instance, along the
 *  path to it. */
function NextStep({ t }: { t: T }) {
  const path = [t('settings.title'), t('settings.nav.access'), t('pairing.enter')];
  return (
    <div className="grid grid-cols-[28px_minmax(0,1fr)] items-start gap-x-3 gap-y-1 rounded-[var(--radius-control)] bg-accentSoft px-4 py-3.5">
      <span className="row-span-2 grid h-7 w-7 place-items-center rounded-full bg-accent text-accentContrast">
        <IconChevronEnd width={16} height={16} className="shrink-0 rtl:-scale-x-100" />
      </span>
      <strong className="text-sm font-semibold text-carbon-text">{t('pairing.nextTitle')}</strong>
      <p className="text-sm text-carbon-textSub">
        <span className="font-semibold text-carbon-text">
          {path.map((step, i) => (
            <span key={i}>
              {i > 0 && (
                <span className="mx-1 inline-block align-[-1px] text-carbon-textMuted">
                  <IconChevronEnd width={12} height={12} className="shrink-0 rtl:-scale-x-100" />
                </span>
              )}
              {step}
            </span>
          ))}
        </span>{' '}
        {t('pairing.nextTail')}
      </p>
    </div>
  );
}
