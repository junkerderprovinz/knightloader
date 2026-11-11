// @vitest-environment jsdom
// The sign-in card's labels: inside an empty field as its grey placeholder, and
// above a field that brings a placeholder or a hint of its own.
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import type { SignInText } from '../lib/signIn';
import { SignInCard } from './SignInCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const TEXT: SignInText = {
  password: 'Password',
  signIn: 'Sign in',
  passkey: 'Sign in with a passkey',
  forgot: 'Forgot password?',
  code: 'Code',
  codeHint: 'From your authenticator app',
  codeInvalid: 'That code is wrong',
  back: 'Back',
  resetCode: 'Code from the log',
  resetCodeTip: 'The server wrote it to its log.',
  newPassword: 'New password',
  confirmPassword: 'Repeat the password',
  setPassword: 'Set password',
  mismatch: 'The passwords differ',
  resetRejected: 'That code was not accepted',
  failed: 'Something went wrong',
  show: 'Show',
  hide: 'Hide',
};

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

function render(text = TEXT, onSignIn = async () => ({ needCode: true })) {
  act(() =>
    root.render(
      <SignInCard
        logo="/logo.svg"
        appName="KnightLoader"
        text={text}
        reset
        passwordProblem={() => null}
        onSignIn={onSignIn}
        onAskCode={async () => ({ ok: true })}
        onSetPassword={async () => ({ ok: true })}
      />,
    ),
  );
}

function caption(label: string): HTMLLabelElement {
  return [...host.querySelectorAll('label')].find((l) => l.firstChild?.textContent === label)!;
}

function field(label: string): HTMLInputElement {
  return document.getElementById(caption(label).htmlFor) as HTMLInputElement;
}

function type(input: HTMLInputElement, text: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text);
  act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
}

async function press(label: string) {
  const button = [...host.querySelectorAll('button')].find((b) => b.textContent === label)!;
  await act(async () => button.click());
}

describe('a sign-in field', () => {
  it('shows its label inside while it is empty, and keeps it as the field’s name', () => {
    render();

    const password = field('Password');
    expect(password.placeholder).toBe('Password');
    expect(password.matches(':placeholder-shown')).toBe(true);
    expect(caption('Password').className).toBe('sr-only');
  });

  it('lets the label go with the first key', () => {
    render();
    const password = field('Password');
    expect(password.matches(':placeholder-shown')).toBe(true);

    type(password, 's');

    expect(password.matches(':placeholder-shown')).toBe(false);
  });

  it('lets the label go when the browser fills the field in', () => {
    render();
    const password = field('Password');
    expect(password.matches(':placeholder-shown')).toBe(true);

    // A password manager writes the value without a key being pressed.
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(password, 'from-the-manager');

    expect(password.matches(':placeholder-shown')).toBe(false);
  });

  it('follows a new language', () => {
    render();

    render({ ...TEXT, password: 'Passwort' });

    expect(field('Passwort').placeholder).toBe('Passwort');
  });

  it('keeps the label above a field with a placeholder of its own', async () => {
    render();
    type(field('Password'), 'a-good-password');

    await press('Sign in');

    expect(caption('Code').className).toBe('glim-signin-label');
    expect(field('Code').placeholder).toBe('000000');
  });
});

describe('the reset form', () => {
  it('keeps the label above the code from the log, which has a hint and a placeholder', async () => {
    render();

    await press('Forgot password?');

    expect(caption('Code from the log').className).toBe('glim-signin-label');
    expect(field('Code from the log').placeholder).toBe('XXXX-XXXX-XXXX');
  });

  it('shows the new password’s labels inside their fields', async () => {
    render();

    await press('Forgot password?');

    expect(field('New password').placeholder).toBe('New password');
    expect(field('Repeat the password').placeholder).toBe('Repeat the password');
    expect(caption('New password').className).toBe('sr-only');
  });
});
