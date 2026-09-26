// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { ListArea } from './controls';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let lines: string[];

beforeEach(() => {
  lines = [];
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

/** The box over a list it owns, as the settings draft holds one. */
function Holder({ start }: { start: string[] }) {
  const [value, setValue] = useState(start);
  return (
    <ListArea
      lines={value}
      onLines={(next) => {
        lines = next;
        setValue(next);
      }}
    />
  );
}

const box = () => host.querySelector('textarea')!;

function setText(text: string) {
  const area = box();
  // React tracks the value it last rendered, so the native setter is what
  // makes the input event count as a change.
  Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(area, text);
  act(() => area.dispatchEvent(new Event('input', { bubbles: true })));
}

/** Types each key at the end of what the box shows at that moment. */
function type(keys: string) {
  for (const key of keys) setText(box().value + key);
}

describe('ListArea', () => {
  it('keeps the new line an Enter starts', () => {
    act(() => root.render(<Holder start={[]} />));
    type('pw1\n');
    expect(box().value).toBe('pw1\n');
  });

  it('takes a second entry typed after the first', () => {
    act(() => root.render(<Holder start={['pw1']} />));
    type('\npw2');
    expect(box().value).toBe('pw1\npw2');
    expect(lines).toEqual(['pw1', 'pw2']);
  });

  it('turns an emptied box into an empty list', () => {
    act(() => root.render(<Holder start={['pw1']} />));
    setText('');
    expect(lines).toEqual([]);
  });
});
