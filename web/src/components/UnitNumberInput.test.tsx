// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { SpeedLimitField } from '../pages/settings/downloads/SpeedLimit';
import { UnitNumberInput, type FieldUnit } from './ui';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const KIB = 1024;
const MIB = 1024 * KIB;

let root: Root;
let host: HTMLDivElement;
let sent: number[];

beforeEach(() => {
  sent = [];
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

/** The speed limit field over a value it owns, as the settings page keeps one. */
function Limit({ start }: { start: number }) {
  const [value, setValue] = useState(start);
  return (
    <SpeedLimitField
      value={value}
      onValue={(v) => {
        sent.push(v);
        setValue(v);
      }}
    />
  );
}

const field = () => host.querySelector('input')!;
const shown = () => field().getAttribute('aria-valuetext');
const latest = () => sent[sent.length - 1];

function type(text: string) {
  const input = field();
  // React tracks the value it last rendered, so the native setter is what
  // makes the input event count as a change.
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text);
  act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
}

function press(key: string) {
  act(() => field().dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true })));
}

describe('UnitNumberInput', () => {
  it('keeps the unit while a number is typed and moves to a larger one on Enter', () => {
    act(() => root.render(<Limit start={0} />));
    expect(shown()).toBe('0 KiB/s');

    type('1337');
    expect(shown()).toBe('1337 KiB/s');
    expect(latest()).toBe(1337 * KIB);

    press('Enter');
    expect(shown()).toBe('1.31 MiB/s');
    expect(latest()).toBe(1337 * KIB);
  });

  it('crosses the unit boundary when stepped either way', () => {
    act(() => root.render(<Limit start={768 * KIB} />));
    press('ArrowUp');
    expect(shown()).toBe('1 MiB/s');
    expect(latest()).toBe(MIB);

    press('ArrowDown');
    expect(shown()).toBe('768 KiB/s');
    expect(latest()).toBe(768 * KIB);
  });

  it('steps a typed value to the next whole step of its unit', () => {
    act(() => root.render(<Limit start={2 * MIB} />));
    type('1.3');
    expect(latest()).toBe(Math.round(1.3 * MIB));
    press('ArrowUp');
    expect(shown()).toBe('2 MiB/s');

    type('1.3');
    press('ArrowDown');
    expect(shown()).toBe('1 MiB/s');
  });

  it('sends nothing for text that is not a number and shows the value again on leaving', () => {
    act(() => root.render(<Limit start={2 * MIB} />));
    act(() => field().focus());
    type('fast');
    expect(sent).toEqual([]);
    expect(field().value).toBe('fast');

    act(() => field().blur());
    expect(shown()).toBe('2 MiB/s');
  });

  it('never steps below zero', () => {
    act(() => root.render(<Limit start={0} />));
    press('ArrowDown');
    expect(sent).toEqual([]);
    expect(shown()).toBe('0 KiB/s');
  });

  it('reads a unit typed after the number, in any case and with or without a space', () => {
    act(() => root.render(<Limit start={0} />));
    type('500k');
    expect(latest()).toBe(500 * KIB);
    type('2m');
    expect(latest()).toBe(2 * MIB);
    expect(shown()).toBe('2 MiB/s');
    type('1.5 MiB');
    expect(latest()).toBe(1.5 * MIB);
    type('3 GiB/s');
    expect(latest()).toBe(3 * 1024 * MIB);
    type('640 KB/s');
    expect(latest()).toBe(640 * KIB);
  });

  it('moves to the unit the typed value named once it lets go', () => {
    act(() => root.render(<Limit start={0} />));
    type('3M');
    press('Enter');
    expect(field().value).toBe('3');
    expect(shown()).toBe('3 MiB/s');
  });

  it('sends nothing for a unit the field does not have', () => {
    act(() => root.render(<Limit start={2 * MIB} />));
    type('5 h');
    type('5 ms');
    expect(sent).toEqual([]);
  });

  it('reads seconds and minutes typed into a time field', () => {
    const window: FieldUnit[] = [
      { label: 's', factor: 1, step: 10 },
      { label: 'min', factor: 60, step: 60 },
    ];
    function Window() {
      const [value, setValue] = useState(30);
      return (
        <UnitNumberInput
          value={value}
          units={window}
          onValue={(v) => {
            sent.push(v);
            setValue(v);
          }}
        />
      );
    }
    act(() => root.render(<Window />));
    type('90s');
    expect(latest()).toBe(90);
    type('2 min');
    expect(latest()).toBe(120);
    type('1.5m');
    expect(latest()).toBe(90);
    press('Enter');
    expect(shown()).toBe('1.5 min');
  });
});
