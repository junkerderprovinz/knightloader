// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { PAYPAL } from '../lib/donate';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

interface FakeButtons {
  options: Record<string, (...args: unknown[]) => unknown>;
  close: ReturnType<typeof vi.fn>;
}

let root: Root;
let host: HTMLDivElement;
let scripts: HTMLScriptElement[];
let rendered: FakeButtons[];

// Stands in for the SDK: each namespace draws two buttons, as PayPal's does
// with every other funding source disabled.
function fakeNamespace() {
  return {
    Buttons(options: FakeButtons['options']) {
      const instance: FakeButtons = { options, close: vi.fn(() => Promise.resolve()) };
      return {
        render(box: HTMLElement) {
          rendered.push(instance);
          box.append(document.createElement('button'), document.createElement('button'));
          return Promise.resolve();
        },
        close: instance.close,
      };
    },
  };
}

beforeEach(async () => {
  vi.resetModules();
  scripts = [];
  rendered = [];
  // The settings behind the window come from the server, which is not here.
  vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
  const create = document.createElement.bind(document);
  vi.spyOn(document, 'createElement').mockImplementation((tag: string, opts?: ElementCreationOptions) => {
    const el = create(tag, opts);
    if (tag === 'script') scripts.push(el as HTMLScriptElement);
    return el;
  });
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  document.head.querySelectorAll('script').forEach((s) => s.remove());
  delete (window as unknown as Record<string, unknown>).paypalOnce;
  delete (window as unknown as Record<string, unknown>).paypalRecurring;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

/** Lets the last SDK script finish loading, then the buttons render. */
async function loadSdk() {
  const script = scripts[scripts.length - 1]!;
  (window as unknown as Record<string, unknown>)[script.dataset.namespace!] = fakeNamespace();
  await act(async () => {
    script.onload?.(new Event('load'));
  });
}

async function openWindow() {
  const { About } = await import('../pages/settings/Help');
  await act(async () => root.render(<About hue={0} />));
  const button = [...host.querySelectorAll('button')].find((b) => b.textContent === 'PayPal')!;
  await act(async () => button.click());
}

function tab(name: string) {
  return [...document.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find((b) => b.textContent === name)!;
}

function amountField() {
  return document.querySelector<HTMLInputElement>('input[inputmode="decimal"]')!;
}

async function typeAmount(text: string) {
  const field = amountField();
  const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  await act(async () => {
    setValue.call(field, text);
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

describe('PaypalDialog', () => {
  it('loads nothing from PayPal until the window opens', async () => {
    const { About } = await import('../pages/settings/Help');
    await act(async () => root.render(<About hue={0} />));
    expect(scripts).toHaveLength(0);

    const button = [...host.querySelectorAll('button')].find((b) => b.textContent === 'PayPal')!;
    await act(async () => button.click());
    expect(scripts).toHaveLength(1);
    const src = new URL(scripts[0]!.src);
    expect(src.origin + src.pathname).toBe('https://www.paypal.com/sdk/js');
    expect(src.searchParams.get('client-id')).toBe(PAYPAL.clientId);
    expect(src.searchParams.get('intent')).toBe('capture');
    expect(src.searchParams.get('currency')).toBe('EUR');
  });

  it('draws PayPal buttons in a light box', async () => {
    await openWindow();
    await loadSdk();
    expect(rendered).toHaveLength(1);
    const box = document.querySelector('.\\[color-scheme\\:light\\]')!;
    expect(box.querySelectorAll('button')).toHaveLength(2);
    expect(document.getElementById('paypal')).toBeNull();
  });

  it('creates a one-off order with a DONATION item', async () => {
    await openWindow();
    await loadSdk();
    const create = vi.fn(() => Promise.resolve('order'));
    await rendered[0]!.options.createOrder!(null, { order: { create, capture: vi.fn() } });
    const unit = (create.mock.calls[0] as unknown[])[0] as {
      purchase_units: { amount: { value: string }; items: { category: string; unit_amount: { value: string } }[] }[];
    };
    expect(unit.purchase_units[0]!.amount.value).toBe('25');
    expect(unit.purchase_units[0]!.items[0]!.category).toBe('DONATION');
    expect(unit.purchase_units[0]!.items[0]!.unit_amount.value).toBe('25');
  });

  it('lets a typed amount take the selection from the presets', async () => {
    await openWindow();
    await loadSdk();
    expect(tab('25 €').getAttribute('aria-selected')).toBe('true');

    await typeAmount('12,5');
    for (const a of ['10 €', '25 €', '50 €']) expect(tab(a).getAttribute('aria-selected')).toBe('false');
    expect(amountField().className).toContain('border-accent');
    const create = vi.fn(() => Promise.resolve('order'));
    await rendered[0]!.options.createOrder!(null, { order: { create, capture: vi.fn() } });
    const order = (create.mock.calls[0] as unknown[])[0] as { purchase_units: { amount: { value: string } }[] };
    expect(order.purchase_units[0]!.amount.value).toBe('12.50');

    await typeAmount('');
    expect(tab('25 €').getAttribute('aria-selected')).toBe('true');
    expect(amountField().className).not.toContain('border-accent');
  });

  it('refuses to open PayPal while the typed amount is not valid', async () => {
    await openWindow();
    await loadSdk();
    await typeAmount('12.');
    await typeAmount('0,5');
    const actions = { resolve: vi.fn(), reject: vi.fn() };
    rendered[0]!.options.onClick!(null, actions);
    expect(actions.reject).toHaveBeenCalled();
    expect(actions.resolve).not.toHaveBeenCalled();
  });

  it('subscribes monthly to the month plan with a whole quantity', async () => {
    await openWindow();
    await loadSdk();
    const once = rendered[0]!;

    await act(async () => tab('Monthly').click());
    expect(once.close).toHaveBeenCalled();
    const src = new URL(scripts[scripts.length - 1]!.src);
    expect(src.searchParams.get('intent')).toBe('subscription');
    expect(src.searchParams.get('vault')).toBe('true');
    await loadSdk();
    expect(rendered).toHaveLength(2);

    await typeAmount('12.6');
    const create = vi.fn(() => Promise.resolve('subscription'));
    await rendered[1]!.options.createSubscription!(null, { subscription: { create } });
    expect(create).toHaveBeenCalledWith({ plan_id: PAYPAL.plans.month, quantity: '13' });
  });

  it('says so when the SDK cannot be loaded', async () => {
    await openWindow();
    await act(async () => {
      scripts[scripts.length - 1]!.onerror?.(new Event('error'));
    });
    expect(document.body.textContent).toContain('PayPal cannot be reached right now.');
  });
});

describe('the PayPal button in the desktop build', () => {
  // The part of the Wails runtime the button uses, as the desktop build injects it.
  function desktop(platform: string) {
    const runtime = {
      BrowserOpenURL: vi.fn(),
      Environment: vi.fn(async () => ({ buildType: 'production', platform, arch: 'amd64' })),
    };
    (window as unknown as { runtime?: typeof runtime }).runtime = runtime;
    return runtime;
  }

  afterEach(() => {
    delete (window as unknown as { runtime?: unknown }).runtime;
  });

  it.each(['darwin', 'linux'])('opens the donation page in the system browser on %s', async (platform) => {
    const runtime = desktop(platform);
    await openWindow();
    expect(runtime.BrowserOpenURL).toHaveBeenCalledWith('https://www.paypal.com/donate/?hosted_button_id=76FVV52TKXTUS');
    expect(scripts).toHaveLength(0);
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  });

  it('keeps the PayPal window on Windows, where its popup works', async () => {
    const runtime = desktop('windows');
    await openWindow();
    expect(runtime.BrowserOpenURL).not.toHaveBeenCalled();
    expect(scripts).toHaveLength(1);
  });
});
