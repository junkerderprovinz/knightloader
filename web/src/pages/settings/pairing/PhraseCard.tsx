// PhraseCard is where an instance joins a group. Outside one it offers two
// tiles: generate a phrase, or enter one that already exists, each opening a
// window. Inside one it shows who else is there, and when nobody has come
// after a minute it offers the two ways out, in the same two windows.
import { useEffect, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, Card, IconBadge, InfoBubble, LabelBadge, Modal, PasswordInput, SectionTitle } from '../../../components/ui';
import {
  ApiError,
  PhraseRejected,
  activateConnect,
  fetchInstances,
  joinConnect,
  leaveConnect,
  removeApp,
  removeInstance,
  removeMember,
  revealConnect,
  type ConnectInfo,
  type GroupApp,
  type GroupMember,
  type Instance,
  type QRMatrix,
} from '../../../lib/api';
import { basePath } from '../../../lib/basePath';
import { copyToClipboard } from '../../../lib/clipboard';
import { useT } from '../../../lib/i18n';
import {
  IconBrowser,
  IconCheckDrawn,
  IconChevronEnd,
  IconClipboard,
  IconClose,
  IconEdit,
  IconEye,
  IconInstances,
  IconPhone,
  IconPlus,
  IconRetry,
  IconSignOut,
  IconTrash,
  IconWarning,
} from '../../../lib/icons';
import { useShake } from '../../../lib/useShake';
import { useToast } from '../../../lib/toast';
import { usePhraseEntry } from './PhraseInput';
import { WordSlots } from './WordSlots';
import { usePeerStats } from '../../../components/InstanceCard';
import { STAGE_BADGE, clock, pairStage } from './pairStage';

type T = ReturnType<typeof useT>['t'];

// The note about a missing password is put away per browser. Several
// instances can share an origin behind one proxy, so the key carries the path.
const NO_PASSWORD_DISMISSED = `kl.pairing.noPasswordHint.dismissed:${basePath()}`;

function noPasswordDismissed(): boolean {
  try {
    return localStorage.getItem(NO_PASSWORD_DISMISSED) === '1';
  } catch {
    return false;
  }
}

function dismissNoPassword() {
  try {
    localStorage.setItem(NO_PASSWORD_DISMISSED, '1');
  } catch {
    // Private mode or storage switched off: it is put away for this visit.
  }
}

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

/** WordGrid shows the twelve words with the QR code for the Android app. A
 *  window's title already names them, so there it goes without the caption. */
function WordGrid({ phrase, qr, bare = false, t }: { phrase: string; qr: QRMatrix | null; bare?: boolean; t: T }) {
  return (
    <div className="flex flex-col gap-2">
      {!bare && (
        <p className="flex items-center gap-1.5 text-xs font-semibold text-carbon-textSub">
          {t('pairing.wordsLabel')} <InfoBubble tip={t('pairing.wordsTip')} />
        </p>
      )}
      <WordSlots words={phrase.split(/\s+/)} label={t('pairing.wordsLabel')} qr={qr} />
      {qr && <p className="text-end text-[11px] text-carbon-textMuted">{t('pairing.qrCaption')}</p>}
    </div>
  );
}

/** Row is one member of the group: a glyph, its name, what to say about it
 *  and, for one that can be taken out here, the badge that does it. Rows share
 *  one height, so one without a badge lines up with the rest. */
function Row({
  glyph,
  name,
  mark,
  badge,
  action,
}: {
  glyph: ReactNode;
  name: string;
  mark?: string;
  badge?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <li className="flex h-11 items-center gap-2.5 rounded-[var(--radius-control)] bg-carbon-surface2 px-3">
      <span className="shrink-0 text-carbon-textMuted [&>svg]:h-4.5 [&>svg]:w-4.5">{glyph}</span>
      <span className="min-w-0 truncate text-sm font-semibold text-carbon-text">{name}</span>
      {mark && <span className="glim-eyebrow shrink-0">{mark}</span>}
      {(badge || action) && (
        <span className="ms-auto flex shrink-0 items-center gap-2">
          {badge}
          {action}
        </span>
      )}
    </li>
  );
}

/** StateBadge says whether a row's member is reachable right now. */
function StateBadge({ on, t }: { on: boolean; t: T }) {
  return <LabelBadge label={on ? t('instances.connected') : t('instances.notConnected')} tone={on ? 'ok' : 'fail'} />;
}

// A member is listed only while it is reachable, so it is always connected.
function MemberRow({ m, action, t }: { m: GroupMember; action: ReactNode; t: T }) {
  return (
    <Row
      glyph={<IconInstances />}
      name={m.name || m.id}
      mark={m.direct ? t('pairing.direct') : t('pairing.viaRelay')}
      badge={<StateBadge on t={t} />}
      action={action}
    />
  );
}

/** PeerRow is an instance added by its address, asked live whether it answers. */
function PeerRow({ p, action, t }: { p: Instance; action: ReactNode; t: T }) {
  const stats = usePeerStats(`/api/instances/${encodeURIComponent(p.name)}`);
  return (
    <Row
      glyph={<IconInstances />}
      name={p.displayName ?? p.name}
      mark={t('pairing.byAddress')}
      badge={<StateBadge on={stats?.online ?? false} t={t} />}
      action={action}
    />
  );
}

function AppRow({ app, action, t }: { app: GroupApp; action: ReactNode; t: T }) {
  return (
    <Row
      glyph={app.deployment === 'extension' ? <IconBrowser /> : <IconPhone />}
      name={app.name || app.id}
      badge={<StateBadge on={app.connected} t={t} />}
      action={action}
    />
  );
}

/** What the removal window is about: a phone or extension of the group, an
 *  instance of the group, or an instance added by its address. */
type Removal = { kind: 'app' | 'member' | 'peer'; id: string; name: string };

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

/** RelayDownLine says in one line that the relay cannot be reached, and opens
 *  what to check on this instance. */
function RelayDownLine({ group, onRefresh, t }: { group: ConnectInfo; onRefresh: () => void; t: T }) {
  const [open, setOpen] = useState(false);
  const own = group.relayMode === 'own';
  const [before, after] = t('pairing.relayCheckProject').split('{host}');
  return (
    <section className="flex flex-col gap-2 rounded-[var(--radius-control)] bg-statusWarnBgSoft px-3 py-2.5 text-sm text-carbon-textSub">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-2">
        <span className="text-statusWarn [&>svg]:h-4.5 [&>svg]:w-4.5">
          <IconWarning />
        </span>
        <span className="font-semibold text-carbon-text">{t('relay.notConnected')}</span>
        <InfoBubble tip={t('pairing.relayCheckTip')} />
        <span className="ms-auto flex flex-wrap gap-2">
          <Button kind="ghost" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
            {t('pairing.relayCheckOpen')}
          </Button>
          <Button kind="secondary" icon={<IconRetry />} onClick={onRefresh}>
            {t('pairing.checkAgain')}
          </Button>
        </span>
      </div>
      {open && (
        <>
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
        </>
      )}
    </section>
  );
}

/** JoinWindow takes the words of another instance's group. Joining from a
 *  group of one replaces it in one step, which is right while nobody else is
 *  in it. */
function JoinWindow({
  id,
  title,
  hint,
  label,
  tip,
  busy,
  onPair,
  onClose,
  t,
}: {
  id: string;
  title: string;
  hint: string;
  label: string;
  tip: string;
  busy: boolean;
  onPair: (phrase: string) => Promise<string | null>;
  onClose: () => void;
  t: T;
}) {
  const { field, paste, pair } = usePhraseEntry({ id, label, tip, bare: true, busy, onPair });
  return (
    <Modal
      title={title}
      hint={hint}
      onClose={onClose}
      wide
      footer={
        <>
          <Button kind="secondary" labelled icon={<IconClose />} title={t('common.close')} onClick={onClose} />
          {paste}
          {pair}
        </>
      }
    >
      {field}
    </Modal>
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
  // The window over the card: the words to read out, the field for the words
  // of a first instance, or the field for another group's words.
  const [shown, setShown] = useState<'words' | 'enter' | 'join' | null>(null);
  const [noteGone, setNoteGone] = useState(noPasswordDismissed);
  const navigate = useNavigate();
  const [password, setPassword] = useState('');
  const [confirmLeave, setConfirmLeave] = useState(false);
  const [busy, setBusy] = useState(false);
  const [shake, setShake] = useState(0);
  const [copies, setCopies] = useState(0);
  const joinedAgo = useJoinedAgo(group);
  // Instances added by address rather than by the phrase, listed with the
  // group so every connection can be taken out in one place.
  const [peers, setPeers] = useState<Instance[]>([]);
  const [removing, setRemoving] = useState<Removal | null>(null);

  useEffect(() => {
    fetchInstances()
      .then((list) => setPeers(list.filter((p) => !p.relayId)))
      .catch(() => {});
  }, [group]);

  const removeButton = (r: Removal) => (
    <Button
      kind="secondary"
      icon={<IconTrash width={14} height={14} />}
      className="px-2.5 text-xs"
      aria-label={t('instances.removeTitle', { name: r.name })}
      onClick={() => setRemoving(r)}
    >
      {t('instances.remove')}
    </Button>
  );

  async function remove(r: Removal) {
    await run(async () => {
      if (r.kind === 'app') {
        await removeApp(r.id);
      } else if (r.kind === 'member') {
        await removeMember(r.id);
      } else {
        const res = await removeInstance(r.id);
        if (!res.ok) throw new Error(await res.text());
      }
      setRemoving(null);
      onRefresh();
    });
  }

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
    setConfirmLeave(false);
  }

  const create = () =>
    run(async () => {
      const r = await activateConnect();
      setPhrase(r.phrase);
      setQr(r.qr ?? null);
      setCreatedHere(true);
      onGroup(r.info);
      setShown('words');
    });

  async function join(words: string): Promise<string | null> {
    setBusy(true);
    try {
      const g = await joinConnect(words);
      forget();
      setShown(null);
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

  const noPasswordNote = !group.passwordSet && !noteGone && (
    <div className="flex flex-wrap items-center gap-3 rounded-[var(--radius-control)] bg-statusWarnBgSoft px-3 py-2.5">
      <p className="min-w-0 flex-[1_1_16rem] text-sm leading-relaxed text-carbon-text">{t('pairing.noPasswordHint')}</p>
      <span className="flex items-center gap-2">
        <Button kind="secondary" onClick={() => navigate('/settings/access')}>
          {t('settings.setPassword')}
        </Button>
        <IconBadge
          icon={<IconClose width={16} height={16} />}
          title={t('common.dismiss')}
          onClick={() => {
            dismissNoPassword();
            setNoteGone(true);
          }}
        />
      </span>
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
      <div className="flex justify-end">
        <Button icon={<IconEye />} shake={shake} onClick={() => void show()} disabled={busy || password === ''}>
          {t('pairing.show')}
        </Button>
      </div>
    </div>
  );

  const openWords = () => {
    setShown('words');
    // Without a password there is nothing to enter before the words show.
    if (!phrase && !group.passwordSet) void show();
  };
  const closeShown = () => {
    setShown(null);
    setPassword('');
  };

  const leaveButton = confirmLeave ? (
    <Button kind="secondary" icon={<IconSignOut />} shake={shake} onClick={() => void leave(() => {})} disabled={busy}>
      {t('pairing.confirmLeave')}
    </Button>
  ) : (
    <Button kind="secondary" icon={<IconSignOut />} onClick={() => setConfirmLeave(true)}>
      {t('pairing.leave')}
    </Button>
  );

  /** stateRow puts the pairing state at the end of the card's first line,
   *  after what there is to say about it. */
  const stateRow = (label: string, tone: 'hue' | 'neutral' | 'warn' | 'ok', lead?: string) => (
    <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5">
      {lead && <p className="min-w-0 flex-[1_1_16rem] text-sm text-carbon-textSub">{lead}</p>}
      <span className="ms-auto" data-testid="pair-state">
        <LabelBadge
          label={label}
          hue={tone === 'hue' ? hue : undefined}
          tone={tone === 'ok' || tone === 'warn' ? tone : undefined}
        />
      </span>
    </div>
  );

  const enterTip = t('pairing.enterTip', {
    path: [t('settings.title'), t('settings.nav.pairing'), t('pairing.show')].join(', '),
  });

  const foot = (
    <div className="flex flex-wrap items-center justify-end gap-2">
      <Button kind="secondary" icon={<IconEye />} onClick={openWords} disabled={busy}>
        {t('pairing.show')}
      </Button>
      {leaveButton}
    </div>
  );

  const relayHint = (stage === 'alone' || stage === 'gone') && group.relayMode !== 'off' && !group.connected && (
    <RelayDownLine group={group} onRefresh={onRefresh} t={t} />
  );

  const copyButton = (
    <Button
      kind="secondary"
      icon={copies > 0 ? <IconCheckDrawn /> : <IconClipboard />}
      confirm={copies}
      onClick={() => void copy()}
    >
      {copies > 0 ? t('common.copied') : t('common.copy')}
    </Button>
  );

  const windows =
    shown === 'words' ? (
      <Modal
        title={t('pairing.wordsLabel')}
        hint={t('pairing.wordsTip')}
        onClose={closeShown}
        wide
        footer={
          <>
            <Button kind="secondary" labelled icon={<IconClose />} title={t('common.close')} onClick={closeShown} />
            {phrase && copyButton}
          </>
        }
      >
        {phrase ? <WordGrid phrase={phrase} qr={qr} bare t={t} /> : group.passwordSet ? passwordPrompt : null}
      </Modal>
    ) : shown === 'enter' ? (
      <JoinWindow
        id="pairing-phrase"
        title={t('pairing.enter')}
        hint={t('pairing.enterSub')}
        label={t('pairing.enterLabel')}
        tip={enterTip}
        busy={busy}
        onPair={join}
        onClose={closeShown}
        t={t}
      />
    ) : shown === 'join' ? (
      <JoinWindow
        id="pairing-phrase-other"
        title={t('pairing.enterIts')}
        hint={t('pairing.twoBody')}
        label={t('pairing.enterLabelOther')}
        tip={enterTip}
        busy={busy}
        onPair={join}
        onClose={closeShown}
        t={t}
      />
    ) : null;

  let body: ReactNode;
  if (stage === 'unpaired') {
    body = (
      <>
        {noPasswordNote}
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Choice
            glyph={<IconPlus />}
            title={t('pairing.create')}
            sub={t('pairing.createSub')}
            disabled={busy}
            shake={shake}
            onClick={() => void create()}
          />
          <Choice glyph={<IconEdit />} title={t('pairing.enter')} sub={t('pairing.enterSub')} onClick={() => setShown('enter')} />
        </div>
      </>
    );
  } else if (stage === 'alone') {
    body = (
      <>
        {stateRow(t(STAGE_BADGE.alone.key), STAGE_BADGE.alone.tone, t('pairing.aloneLead'))}
        {noPasswordNote}
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Choice glyph={<IconEye />} title={t('pairing.notYetTitle')} sub={t('pairing.notYetBody')} onClick={openWords} />
          <Choice glyph={<IconEdit />} title={t('pairing.twoTitle')} sub={t('pairing.twoBody')} onClick={() => setShown('join')} />
        </div>
        {group.relayMode === 'off' && (
          <p className="text-sm text-carbon-textSub">
            <strong className="font-semibold text-carbon-text">{t('pairing.otherNetTitle')}</strong> {t('pairing.otherNetBody')}
          </p>
        )}
        {relayHint}
      </>
    );
  } else {
    const badge = STAGE_BADGE[stage];
    body = (
      <>
        {/* Paired says nothing the rows below do not: each carries its own state. */}
        {stage !== 'paired' && stateRow(t(badge.key), badge.tone)}
        {noPasswordNote}
        {relayHint}
        {stage === 'new' && (
          <>
            <div className="flex flex-wrap items-center justify-end gap-2 text-sm text-carbon-textMuted">
              {t('pairing.notFirst')}
              <Button kind="secondary" icon={<IconEdit />} onClick={() => void leave(() => setShown('enter'))} disabled={busy}>
                {t('pairing.enter')}
              </Button>
            </div>
            <NextStep t={t} />
          </>
        )}
        <div className="flex flex-col gap-2">
          <ul className="flex flex-col gap-2" data-testid="members">
            <Row glyph={<IconInstances />} name={group.name} mark={t('instances.thisInstance')} badge={<StateBadge on t={t} />} />
            {group.members.map((m) => (
              <MemberRow
                key={m.id}
                m={m}
                t={t}
                action={removeButton({ kind: 'member', id: m.id, name: m.name || m.id })}
              />
            ))}
            {peers.map((p) => (
              <PeerRow
                key={p.name}
                p={p}
                t={t}
                action={removeButton({ kind: 'peer', id: p.name, name: p.displayName ?? p.name })}
              />
            ))}
            {group.apps.map((a) => (
              <AppRow
                key={a.id}
                app={a}
                t={t}
                action={removeButton({ kind: 'app', id: a.id, name: a.name || a.id })}
              />
            ))}
          </ul>
          {stage === 'new' ? (
            <WaitRow text={t('pairing.waitNext')} />
          ) : stage !== 'paired' ? (
            <WaitRow text={t('pairing.searching')} seconds={stage === 'searching' ? joinedAgo : undefined} />
          ) : null}
        </div>
        {foot}
      </>
    );
  }

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('pairing.phraseHint')}>{t('pairing.phraseTitle')}</SectionTitle>
      <div className="flex flex-col gap-4.5" data-stage={stage}>
        {body}
      </div>
      {windows}
      {removing && (
        <Modal
          title={t('instances.removeTitle', { name: removing.name })}
          onClose={() => setRemoving(null)}
          footer={
            <>
              <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={() => setRemoving(null)} />
              <Button
                kind="primary"
                labelled
                icon={<IconTrash />}
                title={t('instances.remove')}
                disabled={busy}
                shake={shake}
                onClick={() => void remove(removing)}
              />
            </>
          }
        >
          <p className="text-sm text-carbon-textSub">
            {removing.kind === 'app'
              ? t('instances.removeAppConfirm', { name: removing.name })
              : removing.kind === 'member'
                ? t('instances.removeMemberConfirm', { name: removing.name })
                : t('instances.removePeerConfirm', { name: removing.name })}
          </p>
        </Modal>
      )}
    </Card>
  );
}

/** Choice is one of two ways forward, as a tile large enough to say what
 *  happens after it. */
function Choice({
  glyph,
  title,
  sub,
  disabled = false,
  shake = 0,
  onClick,
}: {
  glyph: ReactNode;
  title: string;
  sub: string;
  disabled?: boolean;
  shake?: number;
  onClick: () => void;
}) {
  const ref = useShake<HTMLButtonElement>(shake);
  return (
    <button
      ref={ref}
      type="button"
      data-choice
      disabled={disabled}
      onClick={onClick}
      className="group grid grid-cols-[44px_minmax(0,1fr)] items-center gap-3.5 rounded-[var(--radius-control)] bg-carbon-surface2 p-4
        text-start transition-colors enabled:hover:bg-carbon-surface3 disabled:cursor-not-allowed disabled:opacity-45
        focus-visible:shadow-[0_0_0_2px_var(--focus-ring)] focus-visible:outline-none"
    >
      <span className="grid h-11 w-11 place-items-center rounded-[var(--radius-control)] bg-accent text-accentContrast [&>svg]:h-5.5 [&>svg]:w-5.5">
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
  const path = [t('settings.title'), t('settings.nav.pairing'), t('pairing.enter')];
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
