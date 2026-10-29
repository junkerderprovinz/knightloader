// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { PopupPlace } from '../lib/clipboardWatch';
import { dismissPopup, onLinkPopup, openFromPopup, popupNow, type PopupBatch } from '../lib/desktop';
import { I18nProvider } from '../lib/i18n';
import { LEAVE_MS, LinkPopup } from './LinkPopup';

vi.mock('../lib/desktop', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/desktop')>()),
  onLinkPopup: vi.fn(() => () => {}),
  popupNow: vi.fn(async () => ({ seq: 0, from: 0, n: 0, seconds: 5, place: 'bottom-right' })),
  dismissPopup: vi.fn(async () => {}),
  openFromPopup: vi.fn(async () => {}),
}));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

async function render() {
  await act(async () =>
    root.render(
      <I18nProvider>
        <LinkPopup />
      </I18nProvider>,
    ),
  );
}

const batch = (over: Partial<PopupBatch>): PopupBatch => ({ seq: 1, from: 1, n: 1, seconds: 5, place: 'bottom-right', ...over });

/** Hands the page a batch the way desktop/linkpopup.go does. */
function tell(over: Partial<PopupBatch>) {
  const send = vi.mocked(onLinkPopup).mock.calls[0]![0];
  act(() => send(batch(over)));
}

const note = () => host.querySelector<HTMLElement>('.kl-popup');
const open = () => host.querySelector<HTMLButtonElement>('.kl-popup-open')!;

/** One step of the clock per call: the leaving fade's own timer starts only
 *  once the step that began it has rendered. */
function wait(ms: number) {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.clearAllMocks();
  vi.useRealTimers();
});

describe('the link popup', () => {
  it('stays empty until links go in', async () => {
    await render();
    expect(note()).toBeNull();
  });

  it('says how many links went in and from where', async () => {
    await render();
    tell({ host: 'files.example' });
    expect(note()?.textContent).toContain('Caught a link');
    expect(note()?.textContent).toContain('files.example');
    tell({ seq: 2, n: 3 });
    expect(note()?.textContent).toContain('Caught 3 links');
    expect(note()?.textContent).not.toContain('files.example');
  });

  it('celebrates the catch, and bumps the count for a further copy', async () => {
    await render();
    tell({});
    expect(host.querySelectorAll('.kl-popup-spark')).toHaveLength(5);
    expect(host.querySelector('.kl-popup-bump')).toBeNull();
    tell({ seq: 2, n: 2 });
    expect(host.querySelectorAll('.kl-popup-spark')).toHaveLength(0);
    expect(host.querySelector('.kl-popup-bump')?.textContent).toBe('Caught 2 links');
    expect(host.querySelector('.kl-popup-again')).not.toBeNull();
  });

  it('comes in from the place it was given', async () => {
    await render();
    for (const place of ['top-left', 'bottom-center'] as PopupPlace[]) {
      tell({ seq: 1, from: place.length, place });
      expect(host.querySelector('.kl-popup-stage')?.classList).toContain(`kl-popup-at-${place}`);
    }
  });

  it.each([3, 5, 8, 15])('goes by itself after %i seconds', async (seconds) => {
    await render();
    tell({ seconds });
    wait(seconds * 1000 - 1);
    expect(note()?.classList).not.toContain('kl-popup-leave');
    wait(1);
    expect(note()?.classList).toContain('kl-popup-leave');
    wait(LEAVE_MS);
    expect(note()).toBeNull();
    expect(dismissPopup).toHaveBeenCalledWith(1);
  });

  it('stays until clicked when it has no time', async () => {
    await render();
    tell({ seconds: 0 });
    wait(60_000);
    expect(note()?.classList).not.toContain('kl-popup-leave');
    expect(dismissPopup).not.toHaveBeenCalled();
  });

  it('starts its time again for every copy', async () => {
    await render();
    tell({});
    wait(4000);
    tell({ seq: 2, n: 2 });
    wait(4000);
    expect(note()?.classList).not.toContain('kl-popup-leave');
    wait(1000);
    wait(LEAVE_MS);
    expect(dismissPopup).toHaveBeenCalledWith(2);
  });

  it('stays while the pointer is on it', async () => {
    await render();
    tell({});
    act(() => note()!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })));
    wait(15_000);
    expect(note()?.classList).not.toContain('kl-popup-leave');
    act(() => note()!.dispatchEvent(new MouseEvent('mouseout', { bubbles: true })));
    wait(5000);
    wait(LEAVE_MS);
    expect(note()).toBeNull();
  });

  it('leaves without opening anything when its cross is clicked', async () => {
    await render();
    tell({ seconds: 0 });
    act(() => host.querySelector<HTMLButtonElement>('[aria-label="Dismiss"]')!.click());
    expect(note()?.classList).toContain('kl-popup-leave');
    wait(LEAVE_MS);
    expect(note()).toBeNull();
    expect(openFromPopup).not.toHaveBeenCalled();
    expect(dismissPopup).toHaveBeenCalledWith(1);
  });

  it('opens the collector when clicked', async () => {
    await render();
    tell({});
    act(() => open().click());
    expect(openFromPopup).toHaveBeenCalled();
    expect(note()).toBeNull();
    expect(dismissPopup).not.toHaveBeenCalled();
  });

  it('shows the popup already on screen when the page loads late', async () => {
    vi.mocked(popupNow).mockResolvedValueOnce(batch({ seq: 4, from: 3, n: 5 }));
    await render();
    expect(note()?.textContent).toContain('Caught 5 links');
  });
});
