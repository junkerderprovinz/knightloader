// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { VPN_MARKS } from '../lib/catalogs/vpnMarks';
import { ConnectionRow, type ConnectionTest } from './ConnectionRow';
import { LabelBadge } from './ui';

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

  it('marks a badge so the row can size it like its buttons', () => {
    act(() => root.render(<ConnectionRow name="Backup server" mark={<svg />} text={text} badges={<LabelBadge label="Disabled" />} />));
    expect(host.querySelector('.glim-connection-badges .glim-badge')?.textContent).toBe('Disabled');
  });

  it('shows only the buttons it is given handlers for', () => {
    render({ onEdit: () => {} });
    expect(button('edit')).not.toBeNull();
    expect(button('test')).toBeNull();
    expect(button('remove')).toBeNull();
  });

  it("puts the row's own buttons before Test, Edit and Remove", () => {
    act(() =>
      root.render(
        <ConnectionRow
          name="Patreon"
          mark={<svg />}
          text={text}
          actions={[{ id: 'again', label: 'Sign in here', icon: <svg />, onClick: () => {} }]}
          onTest={() => {}}
          onEdit={() => {}}
          onRemove={() => {}}
        />,
      ),
    );
    const order = [...host.querySelectorAll('.glim-connection-actions button')].map((b) => b.getAttribute('data-connection'));
    expect(order).toEqual(['again', 'test', 'edit', 'remove']);
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

describe("a connection row's tile", () => {
  /** The one call of node:fs used here; the web build carries no Node types. */
  interface NodeFs {
    readFileSync(path: string, encoding: 'utf8'): string;
  }
  // Vitest hands a test every stylesheet empty, so the rules are read off the disk.
  const fs = (globalThis as unknown as { process: { getBuiltinModule(id: 'node:fs'): NodeFs } }).process.getBuiltinModule('node:fs');
  const css = fs.readFileSync('src/index.css', 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
  const rules = [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(([, selector, body]) => ({ selector: selector.trim(), body }));
  const rule = (selector: string) => rules.find((r) => r.selector === selector)?.body;

  it('reaches the edge of the row with no ring inside it', () => {
    expect(rule('.glim-connection-tile')).toBeDefined();
    expect(rule('.glim-connection-tile')).not.toMatch(/box-shadow|border/);
  });

  it('puts the badges right after the name, which grows no further than its words', () => {
    expect(rule('.glim-connection-head')).toMatch(/justify-content:\s*flex-start/);
    expect(rule('.glim-connection-name')).toMatch(/flex:\s*0 1 auto/);
  });

  it('gives a badge the height and type of the buttons beside it', () => {
    const badge = rule('.glim-connection-badges .glim-badge');
    expect(badge).toMatch(/height:\s*var\(--btn-h\)/);
    expect(badge).toMatch(/font-size:\s*var\(--text-body\)/);
  });

  it('steps its buttons one surface above the row, and their hover one further', () => {
    expect(rule('.glim-connection-actions .glim-btn-subtle')).toMatch(/background-color:\s*var\(--carbon-surface3\)/);
    expect(rule('.glim-connection-actions .glim-btn-subtle:not(:disabled):is(:hover, :focus-visible)')).toMatch(
      /background-color:\s*var\(--hover-up\)/,
    );
  });

  it('hides the resting drawing of a logo in one ink, as of every other, beside the lit one', () => {
    expect(rule('.glim-connection-tile :is(.glim-mark-rest, .glim-logo-ink.glim-mark-rest)')).toMatch(/display:\s*none/);
  });
});
