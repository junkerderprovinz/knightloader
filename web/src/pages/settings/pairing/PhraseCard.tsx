// PhraseCard holds the twelve words. Outside a group it offers two tiles:
// generate a phrase, or enter one that already exists. A phrase generated here
// stays on the card, with the way to the button on the other instance. Inside
// a group the card shows the words again and leaves the group, and when nobody
// has come after a minute it offers the two ways out. Who is in the group
// stands on the card below (GroupCard.tsx).
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, Card, IconBadge, InfoBubble, Modal, PasswordInput, SectionTitle } from '../../../components/ui';
import {
  ApiError,
  PhraseRejected,
  activateConnect,
  joinConnect,
  leaveConnect,
  revealConnect,
  type ConnectInfo,
  type QRMatrix,
} from '../../../lib/api';
import { basePath } from '../../../lib/basePath';
import { copyToClipboard } from '../../../lib/clipboard';
import { useT } from '../../../lib/i18n';
import { IconCheckDrawn, IconClipboard, IconClose, IconEdit, IconEye, IconRetry, IconSignOut, IconWarning } from '../../../lib/icons';
import { useShake } from '../../../lib/useShake';
import { useToast } from '../../../lib/toast';
import { PhraseGlyph } from './pairingArt';
import { Subcard } from './parts';
import { usePhraseEntry } from './PhraseInput';
import { WordSlots } from './WordSlots';
import type { PairStage } from './pairStage';

type T = ReturnType<typeof useT>['t'];

// The note about a missing password is put away per browser. Several
// instances can share an origin behind one proxy, so the key carries the path.
const NO_PASSWORD_DISMISSED = `kl.pairing.noPasswordHint.dismissed:${basePath()}`;

// How far two polls may place the same join apart: joinedAgo is whole seconds,
// and each answer is a request old by the time it is read.
const JOIN_SLACK_MS = 5000;

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

/** WordGrid shows the twelve words with the QR code for the Android app, and
 *  under them what the code is for and who may see the words. A window's
 *  title already carries that explanation, so there it goes without. */
function WordGrid({ phrase, qr, bare = false, t }: { phrase: string; qr: QRMatrix | null; bare?: boolean; t: T }) {
  return (
    <div className="flex flex-col gap-2">
      <WordSlots words={phrase.split(/\s+/)} label={t('pairing.wordsLabel')} qr={qr} />
      {(qr || !bare) && (
        <p className="flex items-center justify-end gap-1.5 text-subline text-carbon-textMuted">
          {qr && t('pairing.qrCaption')}
          {!bare && <InfoBubble tip={t('pairing.wordsTip')} />}
        </p>
      )}
    </div>
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
  stage,
  onCreatedHere,
  onGroup,
  onRefresh,
  hue,
}: {
  group: ConnectInfo;
  /** Where this instance stands in its group (pairStage.ts). */
  stage: PairStage;
  /** Says whether the phrase of this group was generated on this page, which
   *  tells a new group from one that is searching. */
  onCreatedHere: (created: boolean) => void;
  onGroup: (g: ConnectInfo) => void;
  /** Asks the server for the group again, for "Check again" and after leaving. */
  onRefresh: () => void;
  hue?: number;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [phrase, setPhrase] = useState<string | null>(null);
  const [qr, setQr] = useState<QRMatrix | null>(null);
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
    onCreatedHere(false);
    setConfirmLeave(false);
  }

  // Words shown here belong to the group they were shown for. Another tab or
  // an API client can leave it or start a new one, and the poll is the only
  // sign: the group goes inactive, or its join moment moves.
  const joinedAt = useRef<number | null>(null);
  useEffect(() => {
    const at = group.active ? Date.now() - group.joinedAgo * 1000 : null;
    const before = joinedAt.current;
    joinedAt.current = at;
    if (before === null || (at !== null && Math.abs(at - before) < JOIN_SLACK_MS)) return;
    forget();
    setShown((s) => (s === 'words' ? null : s));
  }, [group]);

  const create = () =>
    run(async () => {
      const r = await activateConnect();
      setPhrase(r.phrase);
      setQr(r.qr ?? null);
      onCreatedHere(true);
      onGroup(r.info);
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
      <p className="min-w-0 flex-[1_1_18rem] text-subline leading-relaxed text-carbon-text">{t('pairing.noPasswordHint')}</p>
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

  const enterTip = t('pairing.enterTip', {
    path: [t('settings.title'), t('settings.nav.pairing'), t('pairing.show')].join(', '),
  });

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

  const foot = (
    <div className="flex flex-wrap items-center justify-end gap-2.5">
      <Button kind="secondary" icon={<IconEye />} onClick={openWords} disabled={busy}>
        {t('pairing.show')}
      </Button>
      {leaveButton}
    </div>
  );

  // The relay card below lists what to check; here it is one line beside the
  // stage it explains.
  const relayDown = (stage === 'alone' || stage === 'gone') && group.relayMode !== 'off' && !group.connected && (
    <div className="flex flex-wrap items-center gap-2 rounded-[var(--radius-control)] bg-statusWarnBgSoft px-3 py-2.5 text-sm">
      <span className="text-statusWarn [&>svg]:h-4.5 [&>svg]:w-4.5">
        <IconWarning />
      </span>
      <span className="font-semibold text-carbon-text">{t('relay.notConnected')}</span>
      <InfoBubble tip={t('pairing.relayCheckTip')} />
      <span className="ms-auto">
        <Button kind="secondary" icon={<IconRetry />} onClick={onRefresh}>
          {t('pairing.checkAgain')}
        </Button>
      </span>
    </div>
  );

  const lead = (text: string) => <p className="text-subline text-carbon-textMuted">{text}</p>;

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
        {phrase ? (
          <>
            <WordGrid phrase={phrase} qr={qr} bare t={t} />
            <NextStep t={t} />
          </>
        ) : group.passwordSet ? (
          passwordPrompt
        ) : null}
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
        <div className="grid grid-cols-1 gap-3 min-[861px]:grid-cols-2">
          <Choice
            glyph={<PhraseGlyph kind="create" size={22} />}
            title={t('pairing.create')}
            sub={t('pairing.createSub')}
            disabled={busy}
            shake={shake}
            onClick={() => void create()}
          />
          <Choice
            glyph={<PhraseGlyph kind="enter" size={22} />}
            title={t('pairing.enter')}
            sub={t('pairing.enterSub')}
            onClick={() => setShown('enter')}
          />
        </div>
      </>
    );
  } else if (stage === 'new' && phrase) {
    body = (
      <>
        {noPasswordNote}
        <WordGrid phrase={phrase} qr={qr} t={t} />
        <NextStep t={t} />
        <div className="flex flex-wrap items-center justify-end gap-2.5">
          <span className="min-w-0 flex-[1_1_10rem] text-subline text-carbon-textMuted">{t('pairing.notFirst')}</span>
          <Button kind="secondary" icon={<IconEdit />} onClick={() => void leave(() => setShown('enter'))} disabled={busy}>
            {t('pairing.enter')}
          </Button>
          {copyButton}
          {leaveButton}
        </div>
      </>
    );
  } else if (stage === 'alone') {
    body = (
      <>
        {noPasswordNote}
        {lead(t('pairing.aloneLead'))}
        <div className="grid grid-cols-1 gap-3 min-[861px]:grid-cols-2">
          <Choice
            glyph={<PhraseGlyph kind="enter" size={22} />}
            title={t('pairing.twoTitle')}
            sub={t('pairing.twoBody')}
            onClick={() => setShown('join')}
          />
          <Choice
            glyph={<PhraseGlyph kind="create" size={22} />}
            title={t('pairing.notYetTitle')}
            sub={t('pairing.notYetBody')}
            onClick={openWords}
          />
        </div>
        {group.relayMode === 'off' && (
          <Subcard title={t('pairing.otherNetTitle')}>
            <p className="text-subline text-carbon-textSub">{t('pairing.otherNetBody')}</p>
          </Subcard>
        )}
        {relayDown}
        {foot}
      </>
    );
  } else {
    body = (
      <>
        {noPasswordNote}
        {lead(stage === 'paired' || stage === 'gone' ? t('pairing.pairedLead') : t('pairing.searchingLead'))}
        {relayDown}
        {foot}
      </>
    );
  }

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={`${t('pairing.phraseHint')} ${t('pairing.lead')} ${t('pairing.keyNote')}`}>
        {t('pairing.phraseTitle')}
      </SectionTitle>
      <div className="flex flex-col gap-3.5" data-stage={stage}>
        {body}
      </div>
      {windows}
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
      className="grid grid-cols-[44px_minmax(0,1fr)] items-center gap-3.5 rounded-[var(--radius-control)] bg-carbon-surface2 p-4
        text-start transition-colors enabled:hover:bg-carbon-surface3 disabled:cursor-not-allowed disabled:opacity-45
        focus-visible:shadow-[0_0_0_2px_var(--focus-ring)] focus-visible:outline-none"
    >
      <span className="grid h-11 w-11 place-items-center rounded-[var(--radius-control)] bg-accent text-accentContrast">{glyph}</span>
      <span>
        <span className="block text-sm font-semibold text-carbon-text">{title}</span>
        <span className="mt-0.5 block text-subline leading-[1.35] text-carbon-textSub">{sub}</span>
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
        <PhraseGlyph kind="arrow" size={16} className="rtl:-scale-x-100" />
      </span>
      <strong className="text-sm font-semibold text-carbon-text">{t('pairing.nextTitle')}</strong>
      <p className="flex flex-wrap items-center gap-1 text-sm text-carbon-textSub">
        {path.map((step, i) => (
          <span key={i} className="inline-flex items-center gap-1">
            {i > 0 && (
              <span className="text-carbon-textMuted">
                <PhraseGlyph kind="chevron" size={12} className="rtl:-scale-x-100" />
              </span>
            )}
            <span className={i === path.length - 1 ? 'font-semibold text-carbon-text' : undefined}>{step}</span>
          </span>
        ))}
        <span>{t('pairing.nextTail')}</span>
      </p>
    </div>
  );
}
