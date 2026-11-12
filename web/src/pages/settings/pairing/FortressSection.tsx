// FortressSection is KnightLoader's place in the fortress, on the Pairing page:
// the card where the phrase shows and changes and KnightLoader leaves, the name
// of this server, then GlimStone's fortress with the other apps and their
// permissions, or the twelve words to join one. Open on another app's card
// shows its pages in a new tab through the fortress, or the status of an app
// without pages of its own.
import { useEffect, useMemo, useRef, useState } from 'react';
import {
  fill,
  fortressText,
  openRefusalText,
  type ChangeAnswer,
  type FortressAnswer,
  type FortressApp,
  type FortressInstance,
  type FortressLogLine,
  type FortressStatusReport,
  type FortressText,
  type PhraseAnswer,
} from '@glimstone/core';
import { Fortress, FortressPairing } from '@glimstone/react/Fortress';
import { FortressStatusWindow } from '@glimstone/react/FortressStatusWindow';
import { PasswordField } from '@glimstone/react/PasswordField';
import { Window } from '@glimstone/react/Window';
import { Card, SectionTitle, TextInput } from '../../../components/ui';
import {
  ApiError,
  changeFortressPhrase,
  type ConnectInfo,
  fetchFortressMemberStatus,
  joinFortress,
  leaveFortress,
  openFortressMember,
  setFortressDevice,
  setFortressPermission,
  showFortressPhrase,
  type FortressState,
} from '../../../lib/api';
import { fmtDate } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import { useToast } from '../../../lib/toast';
import { SetRow } from './parts';
import { refusalText } from './PhraseCard';
import { SELF, fortressApps, fortressPermissions, memberStatus, permissionOf, phraseCode } from './fortressModel';

type T = ReturnType<typeof useT>['t'];

const TONE = { success: 'ok', warn: 'info', fail: 'fail' } as const;

/** The status window of an app without pages of its own, while it shows. */
interface Shown {
  app: FortressApp;
  instance: FortressInstance;
  report?: FortressStatusReport;
  log?: FortressLogLine[];
  error?: string;
}

const refusalCode = (e: unknown) => (e instanceof ApiError && e.code ? e.code : 'failed');
// The name saves itself after a pause in typing, as the settings do.
const SAVE_AFTER_MS = 800;

export function FortressSection({
  view,
  onView,
  onRefresh,
  group,
  hueStart,
}: {
  view: FortressState;
  onView: (v: FortressState) => void;
  /** Asks for the whole page again: joining and leaving move the pairing too. */
  onRefresh: () => void;
  /** The group, whose relay the QR code beside the phrase names for the Android app. */
  group: ConnectInfo | null;
  hueStart: number;
}) {
  const { t, lang } = useT();
  const { toast } = useToast();
  // GlimStone words the fortress for an Instances tab. On this page the
  // group's card has an Enter phrase too, so the fortress's says what it
  // joins, and the hints do not send anybody to Instances. GlimStone's change
  // texts tell members out of reach that they need the new phrase, but the
  // kit lets them follow on the old one for 30 days.
  const text = useMemo(() => {
    const own = fortressText(lang);
    return {
      ...own,
      enterPhrase: own.joinTitle,
      outHint: t('pairing.fortressOutHint'),
      phraseWhere: t('pairing.fortressPhraseWhere'),
      changePhraseAbout: t('pairing.fortressChangePhraseAbout'),
      changeBody: t('pairing.fortressChangeBody'),
      changeAway: t('pairing.fortressChangeAway'),
      changedLeft: t('pairing.fortressChangedLeft'),
    };
  }, [lang, t]);
  // The step that waits for the password before a new phrase is made.
  const [asking, setAsking] = useState<((answer: ChangeAnswer) => void) | null>(null);
  const [shown, setShown] = useState<Shown | null>(null);

  const apps = fortressApps(view.members, (unix) => fmtDate(new Date(unix * 1000).toISOString()));

  const onJoin = async (words: string[]): Promise<FortressAnswer> => {
    try {
      onView(await joinFortress(words.join(' ')));
      onRefresh();
      return { ok: true };
    } catch (e) {
      if (e instanceof ApiError && e.code === 'fortress.inFortress') return { error: t('pairing.fortressInFortress') };
      return { error: refusalText(t, e) };
    }
  };
  const onShowPhrase = async (password?: string): Promise<PhraseAnswer> => {
    try {
      const r = await showFortressPhrase(password ?? '');
      return { words: r.phrase.split(' ') };
    } catch (e) {
      return { error: passwordRefusal(t, text, e) };
    }
  };
  // The answer shows the new words, so they come only for the password, as
  // showing the phrase does. GlimStone's change window waits meanwhile.
  const onChangePhrase = (): Promise<ChangeAnswer> =>
    view.passwordSet
      ? new Promise((resolve) => setAsking(() => resolve))
      : changePhrase(t, text, '').then((answer) => {
          if (answer.words) onRefresh();
          return answer;
        });
  const onLeave = async (): Promise<FortressAnswer> => {
    try {
      onView(await leaveFortress());
      onRefresh();
      return { ok: true };
    } catch (e) {
      return { error: refusalText(t, e) };
    }
  };
  // The tab opens right after the answer, which comes quickly, while the
  // browser still counts it as the click's.
  const onOpen = (app: FortressApp, instance: FortressInstance) => {
    if (!view.members.find((m) => m.id === instance.id)?.open) {
      void showStatus(app, instance);
      return;
    }
    if (!view.passwordSet) {
      toast(t('pairing.fortressOpenNeedsPassword'), 'info');
      return;
    }
    openFortressMember(instance.id).then(
      (path) => void window.open(path, '_blank', 'noopener'),
      (e: unknown) => toast(openRefusalText(text, refusalCode(e)), 'fail'),
    );
  };
  const showStatus = async (app: FortressApp, instance: FortressInstance) => {
    setShown({ app, instance });
    const mine = (s: Shown | null) => s?.instance.id === instance.id;
    try {
      const status = memberStatus(await fetchFortressMemberStatus(instance.id, lang), fmtDate);
      setShown((s) => (mine(s) ? { app, instance, ...status } : s));
    } catch (e) {
      setShown((s) => (mine(s) ? { app, instance, error: openRefusalText(text, refusalCode(e)) } : s));
    }
  };
  const onPermission = async (id: string, on: boolean): Promise<FortressAnswer> => {
    try {
      onView(await setFortressPermission(permissionOf(id, on)));
      return { ok: true };
    } catch (e) {
      return { error: refusalText(t, e) };
    }
  };

  return (
    <section id="fortress" className="flex scroll-mt-6 flex-col gap-10">
      {view.joined && (
        <>
          <FortressPairing
            text={{ ...text, pairing: t('pairing.fortressTitle') }}
            lang={lang}
            app={SELF}
            onShowPhrase={view.phrase ? onShowPhrase : undefined}
            phrasePassword={view.passwordSet}
            phraseQr={group ? (words) => phraseCode(words, group) : undefined}
            onChangePhrase={view.phrase ? onChangePhrase : undefined}
            onLeave={onLeave}
            pairedWith={view.pairedWith}
            hueIndex={hueStart}
          />
          <DeviceCard view={view} onView={onView} hue={hueStart + 1} />
        </>
      )}
      <Fortress
        text={text}
        lang={lang}
        app={SELF}
        joined={view.joined}
        apps={apps}
        permissions={fortressPermissions(view.permissions, view.members, t)}
        onPermission={onPermission}
        notify={(message, severity) => toast(message, TONE[severity])}
        onJoin={onJoin}
        onRemove={() => Promise.resolve({ error: text.failed })}
        onOpen={onOpen}
        pairedWith={view.pairedWith}
        hueStart={hueStart + 2}
      />
      {shown && (
        <FortressStatusWindow
          app={shown.app}
          instance={shown.instance}
          report={shown.report}
          log={shown.log}
          loading={!shown.report && !shown.error}
          error={shown.error}
          text={text}
          lang={lang}
          onClose={() => setShown(null)}
        />
      )}
      {asking && (
        <ChangePassword
          text={text}
          onAnswer={(answer) => {
            setAsking(null);
            if (answer.words) onRefresh();
            asking(answer);
          }}
        />
      )}
    </section>
  );
}

/** DeviceCard holds the name the other members know this server by. */
function DeviceCard({ view, onView, hue }: { view: FortressState; onView: (v: FortressState) => void; hue: number }) {
  const { t } = useT();
  const { toast } = useToast();
  // What is being typed, null while the field shows the saved name.
  const [draft, setDraft] = useState<string | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  const save = async (name: string) => {
    try {
      onView(await setFortressDevice(name));
      setDraft((d) => (d === name ? null : d));
      toast(t('settings.saved'), 'ok');
    } catch (e) {
      toast(e instanceof ApiError && e.code === 'fortress.deviceName' ? t('pairing.fortressDeviceTooLong') : refusalText(t, e), 'fail');
    }
  };

  return (
    <Card hue={hue} className="flex flex-col">
      <SectionTitle>{t('pairing.fortressDeviceTitle')}</SectionTitle>
      <SetRow
        label={t('pairing.fortressDeviceName')}
        hint={t('pairing.fortressDeviceHint', { name: view.defaultDevice })}
        htmlFor="fortress-device"
      >
        <div className="w-[18rem] max-w-full">
          <TextInput
            id="fortress-device"
            value={draft ?? view.device}
            placeholder={view.defaultDevice}
            spellCheck={false}
            autoComplete="off"
            onChange={(e) => {
              const name = e.target.value;
              setDraft(name);
              clearTimeout(timer.current);
              timer.current = setTimeout(() => void save(name), SAVE_AFTER_MS);
            }}
          />
        </div>
      </SetRow>
    </Card>
  );
}

/** passwordRefusal words a refusal of the phrase routes, which ask for the
 *  login password and turn away a run of wrong ones for a while. */
function passwordRefusal(t: T, text: FortressText, e: unknown): string {
  if (e instanceof ApiError && e.code === 'passwordWrong') return text.passwordWrong;
  if (e instanceof ApiError && e.status === 429) return t('auth.tooMany');
  return refusalText(t, e);
}

/** changePhrase asks the server for a new phrase and words its answer for GlimStone's flow. */
async function changePhrase(t: T, text: FortressText, password: string): Promise<ChangeAnswer & { wrong?: boolean }> {
  try {
    const r = await changeFortressPhrase(password);
    return { words: r.phrase.split(' '), left: r.left ?? [] };
  } catch (e) {
    return { wrong: e instanceof ApiError && e.code === 'passwordWrong', error: passwordRefusal(t, text, e) };
  }
}

/** ChangePassword asks for the login password in front of a phrase change.
 *  A wrong one keeps it open, and closing it keeps the phrase. */
function ChangePassword({ text, onAnswer }: { text: FortressText; onAnswer: (answer: ChangeAnswer) => void }) {
  const { t } = useT();
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const send = async () => {
    if (busy || password === '') return;
    setBusy(true);
    const answer = await changePhrase(t, text, password);
    setBusy(false);
    if (answer.wrong) {
      setError(answer.error ?? text.passwordWrong);
      return;
    }
    onAnswer(answer);
  };

  return (
    <Window
      title={text.phraseTitle}
      text={{ cancel: text.cancel, close: text.close }}
      onClose={() => onAnswer({ error: t('pairing.fortressChangeKept') })}
      actions={[
        {
          label: text.changeConfirm,
          labelKey: 'fortressChangePhrase',
          primary: true,
          disabled: busy || password === '',
          attrs: { 'data-fortress': 'change-unlock' },
          onClick: () => {
            void send();
            return false;
          },
        },
      ]}
    >
      <form
        className="glim-fortress-text"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        <p>{fill(text.phrasePassword, { app: SELF.name })}</p>
        <PasswordField
          aria-label={fill(text.password, { app: SELF.name })}
          placeholder={fill(text.password, { app: SELF.name })}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          text={{ show: text.showPassword, hide: text.hidePassword }}
          autoFocus
        />
        {error && (
          <p className="glim-fortress-error" role="alert">
            {error}
          </p>
        )}
      </form>
    </Window>
  );
}
