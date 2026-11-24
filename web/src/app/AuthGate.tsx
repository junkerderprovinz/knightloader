import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { SignInCard, SignInReauthWindow, type SignInCardService } from '@glimstone/react';
import {
  signInCardHandlers,
  signInServiceLabel,
  takeSignInCode,
  type SignInOffer,
  type SignInRemoved,
} from '@glimstone/core';
import { IDENTITY_PROVIDERS } from '@glimstone/core/identityProviders';
import { onUnauthorized } from '../lib/api';
import { Button } from '../components/ui';
import { useT } from '../lib/i18n';
import { IconForward } from '../lib/icons';
import { reauthClient, signIn } from '../lib/signin/kit';
import { takeSignInNote } from '../lib/signin/note';
import { addressPath } from '../lib/signin/path';
import { words } from '../lib/signin/words';
import logoUrl from '../assets/logo.svg';

/**
 * gateOpens decides from the kit's offer whether the app shows: nobody can
 * sign in, or this browser is signed in. An offer that did not come, because
 * the server is unreachable, is no locked instance: the app has error states
 * of its own for that.
 */
export function gateOpens(offer: SignInOffer | null): boolean {
  return offer === null || offer.open || offer.signedIn;
}

type Gate = { kind: 'loading' } | { kind: 'app' } | { kind: 'signIn'; offer: SignInOffer; lapsed: boolean };

// AuthGate shows the app, or the sign-in card when the instance asks for a
// sign-in. While the state is still unknown it renders nothing, so a locked
// instance never flashes the UI it is supposed to be protecting.
export function AuthGate({ children }: { children: ReactNode }) {
  const { t, lang } = useT();
  const [gate, setGate] = useState<Gate>({ kind: 'loading' });

  useEffect(() => {
    void signIn.options().then((r) => {
      const offer = r.ok ? r.value : null;
      setGate(gateOpens(offer) ? { kind: 'app' } : { kind: 'signIn', offer: offer!, lapsed: false });
    });
  }, []);

  // A session can end under an open page: somebody signed out everywhere,
  // changed the password or reset it. The next refused call brings the card
  // back, once the kit confirms the refusal was its own.
  useEffect(
    () =>
      onUnauthorized(() => {
        void signIn.options().then((r) => {
          if (r.ok && !gateOpens(r.value)) setGate({ kind: 'signIn', offer: r.value, lapsed: true });
        });
      }),
    [],
  );

  const w = useMemo(() => words(t, lang), [t, lang]);

  if (gate.kind === 'loading') return null;
  if (gate.kind === 'signIn') {
    return <SignIn offer={gate.offer} lapsed={gate.lapsed} onSignedIn={() => setGate({ kind: 'app' })} />;
  }
  return (
    <>
      {children}
      {/* Once, beside every page: it asks before a change that wants a fresh sign-in. */}
      <SignInReauthWindow api={reauthClient} text={w.reauth()} />
    </>
  );
}

/** The sign-in page on GlimStone's sign-in card and the kit's handlers. */
function SignIn({ offer, lapsed, onSignedIn }: { offer: SignInOffer; lapsed: boolean; onSignedIn: () => void }) {
  const { t, lang } = useT();
  const w = useMemo(() => words(t, lang), [t, lang]);
  const [code] = useState(takeSignInCode);
  const [note] = useState(() => takeSignInNote(t) || (lapsed ? t('signin.again') : ''));
  // What a reset took away, held until the reader has seen it.
  const [removed, setRemoved] = useState<SignInRemoved | null>(null);

  const all = { ...w.errors(), ...w.arrived(addressPath(t)) } as Record<string, string | undefined>;
  const refused = code ? (all[code] ?? t('signin.failed')) : undefined;

  const first = offer.services[0];
  let service: SignInCardService | undefined;
  if (first) {
    const { name, mark } = signInServiceLabel(first, IDENTITY_PROVIDERS, { services: { unnamed: t('signin.tile.services.unnamed') } });
    service = mark ? { name, mark } : { name };
  }

  const handlers = signInCardHandlers(signIn, {
    words: { errors: w.errors(), failed: t('signin.failed') },
    onSignedIn,
    onReset: setRemoved,
  });

  return (
    <SignInCard
      logo={logoUrl}
      appName="KnightLoader"
      text={w.card()}
      reset
      minPassword={offer.minPassword}
      note={note || undefined}
      error={refused}
      service={service}
      onService={first ? () => location.assign(signIn.serviceAddress(first.id, location.pathname)) : undefined}
      idPrefix="kl-signin"
      autoFocus
      step={removed ? <ResetDone text={w.resetNote(removed)} onContinue={onSignedIn} /> : undefined}
      onSignIn={handlers.onSignIn}
      onPasskeyOffer={offer.passkeys ? handlers.onPasskeyOffer : undefined}
      onAskCode={handlers.onAskCode}
      onSetPassword={handlers.onSetPassword}
    />
  );
}

/** ResetDone says what the reset took away before the app opens. */
function ResetDone({ text, onContinue }: { text: string; onContinue: () => void }) {
  const { t } = useT();
  return (
    <div role="status" className="flex flex-col gap-4 text-sm text-carbon-text">
      <p>{text}</p>
      <Button icon={<IconForward />} className="w-full" autoFocus onClick={onContinue}>
        {t('auth.reset.continue')}
      </Button>
    </div>
  );
}
