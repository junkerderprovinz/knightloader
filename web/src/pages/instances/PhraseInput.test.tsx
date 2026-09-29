// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../../lib/i18n';
import { PhraseInput } from './PhraseInput';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const WORDS = 'orbit wagon lemon crisp absent tunnel galaxy harbor pencil ribbon velvet yellow'.split(' ');

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

function draw(onPair: (phrase: string) => Promise<string | null> = async () => null) {
  act(() =>
    root.render(
      <I18nProvider>
        <PhraseInput id="phrase" label="Words" tip="Tip" busy={false} onPair={onPair} />
      </I18nProvider>,
    ),
  );
}

// React tracks the value itself, so the native setter goes first and the input
// event after it, as a browser would fire them.
function type(text: string) {
  const area = host.querySelector('textarea')!;
  const set = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!;
  act(() => {
    set.call(area, text);
    area.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

const pair = () => [...host.querySelectorAll('button')].find((b) => b.textContent === 'Pair')!;
const slots = () => [...host.querySelectorAll('li[data-slot]')];
const alert = () => host.querySelector('[role="alert"]')!.textContent ?? '';

describe('PhraseInput', () => {
  it('fills twelve numbered slots from a numbered paste, one word per line', () => {
    draw();
    type(WORDS.map((w, i) => `${i + 1}. ${w}`).join('\n'));
    expect(slots()).toHaveLength(12);
    expect(slots().map((s) => s.textContent)).toEqual(WORDS.map((w, i) => `${i + 1}${w}`));
    expect(host.textContent).toContain('12 of 12 words');
    expect(pair().disabled).toBe(false);
  });

  it('reads words separated by commas', () => {
    draw();
    type(WORDS.join(', '));
    expect(pair().disabled).toBe(false);
  });

  it('keeps Pair off until twelve words are there', () => {
    draw();
    type(WORDS.slice(0, 11).join(' '));
    expect(pair().disabled).toBe(true);
    expect(host.textContent).toContain('11 of 12 words');
    expect(slots()[11].textContent).toBe('12·');
  });

  it('names an unknown word with its position and keeps Pair off', () => {
    draw();
    const words = [...WORDS];
    words[4] = 'absnet';
    type(words.join(' '));
    expect(alert()).toContain('Word 5');
    expect(alert()).toContain('absnet');
    expect(slots()[4].hasAttribute('data-unknown')).toBe(true);
    expect(pair().disabled).toBe(true);
  });

  it('says so when there are more than twelve words', () => {
    draw();
    type([...WORDS, 'orbit'].join(' '));
    expect(alert()).toContain('13 words');
    expect(pair().disabled).toBe(true);
  });

  it('sends the words one space apart and shows what the server refused', async () => {
    const onPair = vi.fn(async () => 'All twelve words exist, but they do not fit together.');
    draw(onPair);
    type(WORDS.join('\n'));
    await act(async () => pair().click());
    expect(onPair).toHaveBeenCalledWith(WORDS.join(' '));
    expect(alert()).toContain('do not fit together');
  });
});
