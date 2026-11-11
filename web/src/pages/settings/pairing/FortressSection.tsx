// FortressSection is KnightLoader's place in the fortress, on the Pairing page:
// GlimStone's fortress with the twelve words to join, the phrase behind the
// password and the other apps with their permissions, then the card where the
// phrase changes and KnightLoader leaves. The name the other members see is
// the instance name on the identity card above.
import { useMemo, useState } from 'react';
import { fill, fortressText, type ChangeAnswer, type FortressAnswer, type FortressText, type PhraseAnswer } from '@glimstone/core';
import { Fortress, FortressPairing } from '@glimstone/react/Fortress';
import { PasswordField } from '@glimstone/react/PasswordField';
import { Window } from '@glimstone/react/Window';
import {
  ApiError,
  changeFortressPhrase,
  joinFortress,
  leaveFortress,
  setFortressPermission,
  showFortressPhrase,
  type FortressState,
} from '../../../lib/api';
import { fmtDate } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import { useToast } from '../../../lib/toast';
import { refusalText } from './PhraseCard';
import { SELF, fortressApps, fortressPermissions, permissionOf } from './fortressModel';

type T = ReturnType<typeof useT>['t'];

const TONE = { success: 'ok', warn: 'info', fail: 'fail' } as const;

export function FortressSection({
  view,
  onView,
  onRefresh,
  hueStart,
}: {
  view: FortressState;
  onView: (v: FortressState) => void;
  /** Asks for the whole page again: joining and leaving move the pairing too. */
  onRefresh: () => void;
  hueStart: number;
}) {
  const { t, lang } = useT();
  const { toast } = useToast();
  // GlimStone words the fortress for an Instances tab. On this page the
  // group's card has an Enter phrase too, so the fortress's says what it
  // joins, and the hints do not send anybody to Instances. A member is either
  // connected or not reachable, never merely inactive.
  const text = useMemo(() => {
    const own = fortressText(lang);
    return {
      ...own,
      enterPhrase: own.joinTitle,
      away: t('pairing.fortressAway'),
      outHint: t('pairing.fortressOutHint'),
      phraseWhere: t('pairing.fortressPhraseWhere'),
    };
  }, [lang, t]);
  // The step that waits for the password before a new phrase is made.
  const [asking, setAsking] = useState<((answer: ChangeAnswer) => void) | null>(null);

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
        onShowPhrase={view.phrase ? onShowPhrase : undefined}
        phrasePassword={view.passwordSet}
        pairedWith={view.pairedWith}
        hueStart={hueStart}
      />
      {view.joined && (
        <FortressPairing
          text={{ ...text, pairing: t('pairing.fortressTitle') }}
          lang={lang}
          app={SELF}
          onLeave={onLeave}
          onChangePhrase={view.phrase ? onChangePhrase : undefined}
          pairedWith={view.pairedWith}
          hueIndex={hueStart + apps.length + 2}
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
