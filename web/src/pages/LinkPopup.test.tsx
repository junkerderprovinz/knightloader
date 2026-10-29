// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { dismissPopup, onLinkPopup, openFromPopup, popupNow, type PopupBatch } from '../lib/desktop';
import { I18nProvider } from '../lib/i18n';
import { LEAVE_MS, LINGER_MS, LinkPopup } from './LinkPopup';

vi.mock('../lib/desktop', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/desktop')>()),
  onLinkPopup: vi.fn(() => () => {}),
  popupNow: vi.fn(async () => ({ seq: 0, from: 0, n: 0 })),
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

/** Hands the page a batch the way desktop/linkpopup.go does. */
function tell(batch: PopupBatch) {
  const send = vi.mocked(onLinkPopup).mock.calls[0]![0];
  act(() => send(batch));
}

const note = () => host.querySelector('button');

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
    tell({ seq: 1, from: 1, n: 1, host: 'files.example' });
    expect(note()?.textContent).toContain('Caught a link');
    expect(note()?.textContent).toContain('files.example');
    tell({ seq: 2, from: 1, n: 3 });
    expect(note()?.textContent).toContain('Caught 3 links');
    expect(note()?.textContent).not.toContain('files.example');
  });

  it('goes by itself once its time is up', async () => {
    await render();
    tell({ seq: 1, from: 1, n: 2 });
    wait(LINGER_MS);
    expect(note()?.classList).toContain('kl-popup-leave');
    wait(LEAVE_MS);
    expect(note()).toBeNull();
    expect(dismissPopup).toHaveBeenCalledWith(1);
  });

  it('starts its time again for every copy', async () => {
    await render();
    tell({ seq: 1, from: 1, n: 1 });
    wait(LINGER_MS - 1000);
    tell({ seq: 2, from: 1, n: 2 });
    wait(LINGER_MS - 1000);
    expect(note()?.classList).not.toContain('kl-popup-leave');
    wait(1000);
    wait(LEAVE_MS);
    expect(dismissPopup).toHaveBeenCalledWith(2);
  });

  it('stays while the pointer is on it', async () => {
    await render();
    tell({ seq: 1, from: 1, n: 1 });
    act(() => note()!.dispatchEvent(new MouseEvent('mouseover', { bubbles: true })));
    wait(LINGER_MS * 3);
    expect(note()?.classList).not.toContain('kl-popup-leave');
    act(() => note()!.dispatchEvent(new MouseEvent('mouseout', { bubbles: true })));
    wait(LINGER_MS);
    wait(LEAVE_MS);
    expect(note()).toBeNull();
  });

  it('opens the collector when clicked', async () => {
    await render();
    tell({ seq: 1, from: 1, n: 1 });
    act(() => note()!.click());
    expect(openFromPopup).toHaveBeenCalled();
    expect(note()).toBeNull();
    expect(dismissPopup).not.toHaveBeenCalled();
  });

  it('shows the popup already on screen when the page loads late', async () => {
    vi.mocked(popupNow).mockResolvedValueOnce({ seq: 4, from: 3, n: 5 });
    await render();
    expect(note()?.textContent).toContain('Caught 5 links');
  });
});
