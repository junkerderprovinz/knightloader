// @vitest-environment jsdom
// The fortress on the Pairing page: outside one, the twelve words join it;
// inside, the Fortress card on top shows the phrase behind the password with
// its QR code, changes it and lets KnightLoader leave. Open on another app's
// card shows its pages in a new tab, or its status where it has no pages.
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fortressText } from '@glimstone/core';

import type { ConnectInfo, FortressState } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { interpolate } from '../../../lib/interpolate';
import { en } from '../../../lib/locales/en';
import { FortressSection } from './FortressSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const PHRASE = 'abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about';
const NEW_PHRASE = 'zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo wrong';

const server = vi.hoisted(() => ({
  view: null as unknown as FortressState,
  join: null as null | (() => never),
  throttled: false,
  calls: [] as string[],
  openRefusal: '',
}));

vi.mock('../../../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../../lib/api')>();
  const wrong = () => new actual.ApiError('the password is required to show the phrase', 'passwordWrong', undefined, 403);
  return {
    ...actual,
    joinFortress: async (phrase: string) => {
      server.calls.push(`join ${phrase}`);
      server.join?.();
      server.view = { ...server.view, joined: true, phrase: true };
      return server.view;
    },
    showFortressPhrase: async (password: string) => {
      if (server.throttled) throw new actual.ApiError('too many attempts, wait a moment', undefined, undefined, 429);
      if (password !== 'hunter2') throw wrong();
      return { phrase: PHRASE };
    },
    changeFortressPhrase: async (password: string) => {
      server.calls.push(`change ${password}`);
      if (server.view.passwordSet && password !== 'hunter2') throw wrong();
      return { phrase: NEW_PHRASE, left: ['Cellar'] };
    },
    setFortressDevice: async (device: string) => {
      server.calls.push(`device ${device}`);
      if (device.length > 64) throw new actual.ApiError('the name must be one line of at most 64 characters', 'fortress.deviceName', undefined, 400);
      server.view = { ...server.view, device: device || server.view.defaultDevice };
      return server.view;
    },
    openFortressMember: async (id: string) => {
      server.calls.push(`open ${id}`);
      if (server.openRefusal) throw new actual.ApiError('refused', server.openRefusal, undefined, 403);
      return '/fortress/open/0123';
    },
    fetchFortressMemberStatus: async (id: string) => {
      server.calls.push(`status ${id}`);
      return {
        report: { state: 'ok', needsYou: [], work: [], next: [] },
        log: [{ at: '2026-11-12T19:00:00Z', level: 'info', text: 'The array was checked' }],
      };
    },
    removeFortressMember: async (id: string) => {
      server.calls.push(`remove ${id}`);
      server.view = { ...server.view, members: [], removed: [{ app: 'arrowloop', name: 'ArrowLoop', device: 'Tower' }] };
      return server.view;
    },
    leaveFortress: async () => {
      server.calls.push('leave');
      server.view = { ...server.view, joined: false, phrase: false };
      return server.view;
    },
  };
});

const toasts = vi.hoisted(() => [] as string[]);
vi.mock('../../../lib/toast', () => ({ useToast: () => ({ toast: (m: string) => toasts.push(m) }) }));

function state(over: Partial<FortressState> = {}): FortressState {
  return {
    joined: false,
    phrase: false,
    device: 'nas',
    defaultDevice: 'nas',
    passwordSet: true,
    members: [],
    permissions: [],
    pairedWith: [],
    behind: [],
    left: [],
    removed: [],
    removedHere: false,
    ...over,
  };
}
const joined = (over: Partial<FortressState> = {}) => state({ joined: true, phrase: true, ...over });
const GROUP = { relayMode: 'project', relayUrl: '', projectRelayUrl: 'wss://parleyport.halleluja.design/relay/connect' } as ConnectInfo;

/** The section with the page's state around it, so an answer reaches it. */
function Host({ start }: { start: FortressState }) {
  const [view, setView] = useState(start);
  return <FortressSection view={view} onView={setView} onRefresh={() => server.calls.push('refresh')} group={GROUP} hueStart={7} />;
}

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: false,
    media: query,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
  }));
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  server.join = null;
  server.throttled = false;
  server.openRefusal = '';
  server.calls.length = 0;
  toasts.length = 0;
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  document.body.innerHTML = '';
  vi.unstubAllGlobals();
  localStorage.clear();
});

async function draw(start: FortressState) {
  server.view = start;
  await act(async () =>
    root.render(
      <I18nProvider>
        <Host start={start} />
      </I18nProvider>,
    ),
  );
}

/** settle lets the promises and the frame a window opens in run out. */
const settle = () => act(async () => new Promise((done) => setTimeout(done, 20)));

const hook = (name: string) => document.querySelector<HTMLElement>(`[data-fortress="${name}"]`);
const press = async (name: string) => {
  await act(async () => hook(name)!.click());
  await settle();
};
const typeInto = async (field: HTMLInputElement, value: string) => {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  await act(async () => {
    setter.call(field, value);
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
};
const passwordField = () => document.querySelector<HTMLInputElement>('input[aria-label="Password of KnightLoader"]')!;
const alertText = () => document.querySelector('[role="alert"]')?.textContent;
const page = () => document.body.textContent ?? '';

describe('FortressSection', () => {
  it('joins with the twelve words pasted into the first field and reads the page again', async () => {
    await draw(state());
    expect(page()).toContain('Not in a fortress yet');
    await press('open-join');
    await typeInto(document.querySelector<HTMLInputElement>('[data-word="0"]')!, PHRASE);
    expect(document.querySelectorAll<HTMLInputElement>('[data-word]')[11].value).toBe('about');
    await press('join');
    expect(server.calls).toEqual([`join ${PHRASE}`, 'refresh']);
    expect(page()).toContain('KnightLoader is in the fortress now');
  });

  it('names a mistyped word in the words of the group card and keeps the window open', async () => {
    const { PhraseRejected } = await vi.importActual<typeof import('../../../lib/api')>('../../../lib/api');
    server.join = () => {
      throw new PhraseRejected({ reason: 'unknown_word', word: 'recieve', position: 7 });
    };
    await draw(state());
    await press('open-join');
    await typeInto(document.querySelector<HTMLInputElement>('[data-word="0"]')!, PHRASE);
    await press('join');
    expect(alertText()).toBe(interpolate(en['pairing.errUnknownWord'], { position: 7, word: 'recieve' }));
    expect(document.querySelector('[data-word="0"]')).toBeTruthy();
  });

  it('says KnightLoader is in a fortress already when the server says so', async () => {
    const { ApiError } = await vi.importActual<typeof import('../../../lib/api')>('../../../lib/api');
    server.join = () => {
      throw new ApiError('KnightLoader is in a fortress already', 'fortress.inFortress', undefined, 409);
    };
    await draw(state());
    await press('open-join');
    await typeInto(document.querySelector<HTMLInputElement>('[data-word="0"]')!, PHRASE);
    await press('join');
    expect(alertText()).toBe(en['pairing.fortressInFortress']);
  });

  it('says where the phrase comes from in words that fit this page', async () => {
    await draw(state());
    expect(page()).toContain(interpolate(en['pairing.fortressOutHint'], { app: 'KnightLoader' }));
    await press('open-join');
    expect(page()).toContain(en['pairing.fortressPhraseWhere']);
    expect(page()).not.toMatch(/under Instances|instances of KnightLoader show up here/);
  });

  it('says that the pairing with the other instances comes along into the fortress', async () => {
    await draw(state({ pairedWith: ['attic'] }));
    await press('open-join');
    expect(hook('moves')?.textContent).toBe(
      'KnightLoader is already paired with attic. Joining moves that pairing into the fortress, so there is nothing to pair again.',
    );
  });

  it('holds showing, changing and leaving in the Fortress card above the other apps', async () => {
    await draw(joined());
    const card = document.querySelector('[data-glim="FortressPairing"]')!;
    expect(card.textContent).toContain(en['pairing.fortressTitle']);
    expect(card.contains(hook('show'))).toBe(true);
    expect(card.contains(hook('open-change'))).toBe(true);
    expect(card.contains(hook('open-leave'))).toBe(true);
    const fortress = document.querySelector('[data-glim="Fortress"]')!;
    expect(card.compareDocumentPosition(fortress) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(fortress.querySelector('[data-fortress="show"]')).toBeNull();
  });

  it('shows the phrase with its QR code only after the password', async () => {
    await draw(joined());
    await press('show');
    await typeInto(passwordField(), 'wrong');
    await press('unlock');
    expect(alertText()).toBe('That password is not right.');
    await typeInto(passwordField(), 'hunter2');
    await press('unlock');
    expect(hook('words')!.querySelectorAll('li')).toHaveLength(12);
    expect(hook('qr')).toBeTruthy();
  });

  it('asks to wait after too many wrong passwords', async () => {
    server.throttled = true;
    await draw(joined());
    await press('show');
    await typeInto(passwordField(), 'hunter2');
    await press('unlock');
    expect(alertText()).toBe(en['auth.tooMany']);
  });

  it('asks for the password before the phrase changes, and a wrong one changes nothing', async () => {
    await draw(joined());
    await press('open-change');
    await press('change');
    expect(server.calls).toEqual([]);
    await typeInto(passwordField(), 'wrong');
    await press('change-unlock');
    expect(server.calls).toEqual(['change wrong']);
    expect(alertText()).toBe('That password is not right.');
    expect(hook('words')).toBeNull();
  });

  it('keeps the phrase when the password window closes', async () => {
    await draw(joined());
    await press('open-change');
    await press('change');
    await act(async () => {
      (document.activeElement ?? document.body).dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    await settle();
    expect(server.calls).toEqual([]);
    expect(alertText()).toBe(en['pairing.fortressChangeKept']);
  });

  it('changes the phrase without asking where KnightLoader has no password', async () => {
    await draw(joined({ passwordSet: false }));
    await press('open-change');
    await press('change');
    expect(server.calls).toEqual(['change ', 'refresh']);
    expect(hook('words')!.textContent).toContain('wrong');
  });

  it('changes the phrase for the password and leaves from the Fortress card', async () => {
    await draw(joined());
    expect(page()).toContain(en['pairing.fortressTitle']);
    await press('open-change');
    expect(page()).toContain(en['pairing.fortressChangeAway']);
    await press('change');
    await typeInto(passwordField(), 'hunter2');
    await press('change-unlock');
    expect(server.calls).toEqual(['change hunter2', 'refresh']);
    expect(hook('words')!.textContent).toContain('wrong');
    expect(page()).toContain(en['pairing.fortressChangedLeft'].replace('{devices}', 'Cellar'));
    const close = [...document.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.trim() === 'Close')!;
    await act(async () => close.click());
    await settle();

    await press('open-leave');
    await press('leave');
    expect(server.calls).toEqual(['change hunter2', 'refresh', 'leave', 'refresh']);
    expect(page()).toContain('Not in a fortress yet');
  });

  it('calls a member connected or not reachable', async () => {
    const member = { app: 'arrowloop', name: 'ArrowLoop', version: '3.0.0', deployment: 'container', kind: 'instance', relay: false, open: true };
    await draw(
      joined({
        members: [
          { ...member, id: 'm1', device: 'Tower', online: true },
          { ...member, id: 'm2', device: 'Cellar', online: false },
        ],
      }),
    );
    await press('others');
    expect(page()).toContain('Connected');
    expect(page()).toContain('Not reachable');
    expect(page()).not.toContain('Inactive');
  });

  const arrowLoop = { id: 'm1', app: 'arrowloop', name: 'ArrowLoop', version: '3.0.0', device: 'Tower', deployment: 'container', kind: 'instance', online: true, relay: false, open: true };

  it("opens another app's pages in a new tab through the fortress, below the base", async () => {
    const opened = vi.fn();
    vi.stubGlobal('open', opened);
    const base = document.createElement('base');
    base.href = '/kl/';
    document.head.append(base);
    await draw(joined({ members: [arrowLoop] }));
    await press('others');
    await press('open');
    base.remove();
    expect(server.calls).toEqual(['open m1']);
    expect(opened).toHaveBeenCalledWith(`${location.origin}/kl/fortress/open/0123`, '_blank', 'noopener');
  });

  it("says in GlimStone's words why an app did not open", async () => {
    vi.stubGlobal('open', vi.fn());
    server.openRefusal = 'ui_no_login';
    await draw(joined({ members: [arrowLoop] }));
    await press('others');
    await press('open');
    expect(toasts).toEqual([fortressText('en').openRefused.ui_no_login]);
  });

  it('asks for a password here before KnightLoader opens another app', async () => {
    await draw(joined({ passwordSet: false, members: [arrowLoop] }));
    await press('others');
    await press('open');
    expect(server.calls).toEqual([]);
    expect(toasts).toEqual([en['pairing.fortressOpenNeedsPassword']]);
  });

  it('shows the status of a plugin, which has no pages to open', async () => {
    await draw(joined({ members: [{ ...arrowLoop, app: 'tollroll', name: 'TollRoll', deployment: 'plugin', open: false }] }));
    await press('others');
    await press('open');
    expect(server.calls).toEqual(['status m1']);
    expect(page()).toContain('The array was checked');
  });

  it('saves the name of this server a moment after the typing stops', async () => {
    await draw(joined());
    const field = document.querySelector<HTMLInputElement>('#fortress-device')!;
    expect(field.value).toBe('nas');
    expect(field.placeholder).toBe('nas');
    await typeInto(field, 'Towe');
    await typeInto(field, 'Tower');
    expect(server.calls).toEqual([]);
    await act(async () => new Promise((done) => setTimeout(done, 1000)));
    expect(server.calls).toEqual(['device Tower']);
    expect(field.value).toBe('Tower');
    expect(toasts).toEqual([en['settings.saved']]);
  });

  it('says why a name that is too long is not kept', async () => {
    await draw(joined());
    await typeInto(document.querySelector<HTMLInputElement>('#fortress-device')!, 'x'.repeat(65));
    await act(async () => new Promise((done) => setTimeout(done, 1000)));
    expect(toasts).toEqual([en['pairing.fortressDeviceTooLong']]);
  });

  it('asks for the name of this server only inside a fortress', async () => {
    await draw(state());
    expect(document.querySelector('#fortress-device')).toBeNull();
  });

  it('offers neither showing nor changing a phrase KnightLoader does not keep', async () => {
    await draw(joined({ phrase: false }));
    expect(hook('show')).toBeNull();
    expect(hook('open-change')).toBeNull();
    expect(hook('open-leave')).toBeTruthy();
  });

  it('asks before another app goes, then offers the new phrase behind the password', async () => {
    await draw(joined({ members: [arrowLoop] }));
    await press('others');
    await press('open-remove');
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Remove ArrowLoop on Tower?');
    expect(server.calls).toEqual([]);
    await press('remove');
    expect(server.calls).toEqual(['remove m1', 'refresh']);
    expect(page()).toContain('Change the phrase now?');
    await press('change');
    await typeInto(passwordField(), 'hunter2');
    await press('change-unlock');
    expect(server.calls).toEqual(['remove m1', 'refresh', 'change hunter2', 'refresh']);
    expect(hook('words')!.textContent).toContain('wrong');
  });

  it('names who was removed until the phrase changes', async () => {
    await draw(joined({ members: [arrowLoop], removed: [{ app: 'arrowloop', name: 'ArrowLoop', device: 'Tower' }] }));
    expect(hook('removed')!.textContent).toContain('Removed: ArrowLoop on Tower. No new app can join until you change the phrase.');
  });

  it('says another app removed KnightLoader until it joins again', async () => {
    await draw(state({ removedHere: true }));
    expect(page()).toContain('Removed from the fortress');
    expect(page()).toContain('Another app in the fortress removed KnightLoader. Once the phrase has changed, the new one gets you back in.');
  });
});
