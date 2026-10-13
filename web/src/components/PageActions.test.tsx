// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { setLabelMode } from '../lib/labelModes';
import { PageAction, PageActions, PageActionsProvider, PageActionsSlot } from './PageActions';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

function stubWidth(phone: boolean) {
  vi.stubGlobal('matchMedia', () => ({ matches: phone, addEventListener() {}, removeEventListener() {} }));
}

const glyph = <svg data-glyph />;

beforeEach(() => {
  stubWidth(false);
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  setLabelMode('buttons', 'both');
  vi.unstubAllGlobals();
});

/** A frame as app/Layout.tsx builds one: the page, then the slot its actions go into. */
function Frame() {
  const [slot, setSlot] = useState<HTMLDivElement | null>(null);
  return (
    <I18nProvider>
      <PageActionsProvider slot={slot}>
        <div data-page>
          <PageActions>
            <PageAction icon={glyph} label="Customize" />
            <PageAction primary icon={glyph} label="Add links" />
          </PageActions>
        </div>
      </PageActionsProvider>
      <PageActionsSlot ref={setSlot} className="h-0" />
    </I18nProvider>
  );
}

it('draws the actions of a page into the slot of the frame around it, the primary one last', async () => {
  await act(async () => root.render(<Frame />));

  const held = host.querySelector('.glim-page-actions')!;
  expect([...held.querySelectorAll('button')].map((b) => b.textContent)).toEqual(['Customize', 'Add links']);
  expect(host.querySelector('[data-page]')!.querySelector('button')).toBeNull();
  expect(held.querySelectorAll('button')[1].className).toContain('bg-accent');
  expect(held.querySelectorAll('button')[0].className).not.toContain('bg-accent');
});

it('draws the actions in place where no frame holds a slot', async () => {
  await act(async () =>
    root.render(
      <I18nProvider>
        <PageActions>
          <PageAction primary icon={glyph} label="Add links" />
        </PageActions>
      </I18nProvider>,
    ),
  );
  expect(host.querySelector('button')!.textContent).toBe('Add links');
});

it('shows the glyph alone at phone width and still names the button', async () => {
  stubWidth(true);
  await act(async () =>
    root.render(
      <I18nProvider>
        <PageActions>
          <PageAction primary icon={glyph} label="Add links" />
        </PageActions>
      </I18nProvider>,
    ),
  );
  const button = host.querySelector('button')!;
  expect(button.textContent).toBe('');
  expect(button.querySelector('[data-glyph]')).not.toBeNull();
  expect(button.getAttribute('aria-label')).toBe('Add links');
});

it('follows the label setting for buttons', async () => {
  await act(async () =>
    root.render(
      <I18nProvider>
        <PageActions>
          <PageAction primary icon={glyph} label="Add links" />
        </PageActions>
      </I18nProvider>,
    ),
  );
  const button = () => host.querySelector('button')!;
  expect(button().textContent).toBe('Add links');
  expect(button().querySelector('[data-glyph]')).not.toBeNull();

  act(() => setLabelMode('buttons', 'glyph'));
  expect(button().textContent).toBe('');
  expect(button().getAttribute('aria-label')).toBe('Add links');

  act(() => setLabelMode('buttons', 'text'));
  expect(button().textContent).toBe('Add links');
  expect(button().querySelector('[data-glyph]')).toBeNull();
});

it('lists the choices of an action that adds several kinds instead of acting on the press', async () => {
  const picked: string[] = [];
  await act(async () =>
    root.render(
      <I18nProvider>
        <PageActions>
          <PageAction
            primary
            icon={glyph}
            label="Add an account"
            menu={[
              {
                id: 'kinds',
                items: [
                  { id: 'a', label: 'Debrid account', onSelect: () => picked.push('a') },
                  { id: 'b', label: 'Usenet server', onSelect: () => picked.push('b') },
                ],
              },
            ]}
          />
        </PageActions>
      </I18nProvider>,
    ),
  );
  const button = host.querySelector('button')!;
  expect(button.getAttribute('aria-expanded')).toBe('false');
  await act(async () => button.click());
  expect(button.getAttribute('aria-expanded')).toBe('true');

  const menu = document.querySelector('[role="menu"]')!;
  expect(menu.getAttribute('aria-label')).toBe('Add an account');
  const items = [...menu.querySelectorAll<HTMLButtonElement>('[role="menuitem"]')];
  expect(items.map((i) => i.textContent)).toEqual(['Debrid account', 'Usenet server']);

  await act(async () => items[1].click());
  expect(picked).toEqual(['b']);
  expect(document.querySelector('[role="menu"]')).toBeNull();
});
