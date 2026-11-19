// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { VPN_MARKS } from '../lib/catalogs/vpnMarks';
import { ConnectionRow, type ConnectionTest } from './ConnectionRow';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let phone: boolean;

beforeEach(() => {
  phone = false;
  vi.stubGlobal('matchMedia', () => ({ matches: phone, addEventListener() {}, removeEventListener() {} }));
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const text = { test: 'Test', edit: 'Edit', remove: 'Remove' };
const tile = () => host.querySelector<HTMLElement>('.glim-connection-tile')!;
const button = (which: string) => host.querySelector<HTMLButtonElement>(`[data-connection="${which}"]`);

function render(props: { test?: ConnectionTest; onTest?: () => void; onEdit?: () => void; onRemove?: () => void } = {}) {
  act(() =>
    root.render(
      <ConnectionRow name="Mullvad VPN" mark={VPN_MARKS.mullvad} status="Signed in as maria" text={text} {...props} />,
    ),
  );
}

describe('a connection row', () => {
  it('lights a catalog mark on its brand colours and names the row after the service', () => {
    render();
    expect(tile().style.getPropertyValue('--tile')).toBe(VPN_MARKS.mullvad.tile);
    expect(tile().style.getPropertyValue('--tile-ink')).toBe(VPN_MARKS.mullvad.ink);
    expect(host.querySelector('[role="group"]')!.getAttribute('aria-label')).toBe('Mullvad VPN');
    expect(host.querySelector('.glim-connection-status')!.textContent).toBe('Signed in as maria');
  });

  it('puts the accent behind a mark that brings no colours', () => {
    act(() => root.render(<ConnectionRow name="Backup server" mark={<svg />} text={text} />));
    expect(tile().getAttribute('style')).toBeNull();
  });

  it('shows only the buttons it is given handlers for', () => {
    render({ onEdit: () => {} });
    expect(button('edit')).not.toBeNull();
    expect(button('test')).toBeNull();
    expect(button('remove')).toBeNull();
  });

  it('turns the test green or red with what it found', () => {
    render({ onTest: () => {}, test: { tone: 'ok', title: 'Reached' } });
    expect(button('test')!.className).toContain('bg-statusOkSolid');
    expect(host.querySelector('[role="status"]')!.textContent).toBe('Reached');
    render({ onTest: () => {}, test: { tone: 'danger', title: 'Refused' } });
    expect(button('test')!.className).toContain('bg-statusFailSolid');
  });

  it('holds the test while it runs', () => {
    render({ onTest: () => {}, test: { busy: true } });
    expect(button('test')!.disabled).toBe(true);
  });

  it('shows the glyphs alone on a phone, each named by its words', () => {
    phone = true;
    render({ onEdit: () => {}, onRemove: () => {} });
    expect(button('edit')!.textContent).toBe('');
    expect(button('edit')!.getAttribute('aria-label')).toBe('Edit');
    expect(button('remove')!.getAttribute('aria-label')).toBe('Remove');
  });
});
