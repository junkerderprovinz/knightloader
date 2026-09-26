import { describe, expect, it } from 'vitest';

import type { QuickFilterId } from '../components/ListToolbar';
import { sanitiseNarrowing } from './listNarrowing';

const allowed: QuickFilterId[] = ['running', 'finished', 'disabled'];

describe('a stored narrowing', () => {
  it('keeps the filters the list knows, in the list order', () => {
    expect(sanitiseNarrowing({ filters: ['disabled', 'nope', 'running'] }, allowed).filters).toEqual([
      'running',
      'disabled',
    ]);
  });

  it('shows the disabled links for a view saved on the held filter', () => {
    expect(sanitiseNarrowing({ filters: ['held'] }, allowed).filters).toEqual(['disabled']);
    expect(sanitiseNarrowing({ filters: ['held', 'disabled', 'finished'] }, allowed).filters).toEqual([
      'finished',
      'disabled',
    ]);
  });
});
