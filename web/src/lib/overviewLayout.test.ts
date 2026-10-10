import { describe, expect, it } from 'vitest';

import {
  DEFAULT_LAYOUT,
  OVERVIEW_CARDS,
  moved,
  pack,
  readLayout,
  shownCards,
  spanOf,
  withHidden,
  withOrder,
  withWidth,
} from './overviewLayout';

describe('the overview arrangement', () => {
  it('opens on four cards and keeps the rest for later', () => {
    expect(shownCards(DEFAULT_LAYOUT)).toEqual(['needs', 'speed', 'recent', 'disk']);
    expect([...shownCards(DEFAULT_LAYOUT), ...DEFAULT_LAYOUT.hidden].sort()).toEqual([...OVERVIEW_CARDS].sort());
  });

  it('starts from the default while nothing is stored', () => {
    expect(readLayout(null)).toBe(DEFAULT_LAYOUT);
    expect(readLayout('nonsense')).toBe(DEFAULT_LAYOUT);
  });

  it('appends a card the stored order does not know and drops one this build lacks', () => {
    const layout = readLayout({ order: ['disk', 'gone', 'needs'], hidden: ['gone', 'disk'], widths: { disk: 4, needs: 5, gone: 2 } });
    expect(layout.order).toEqual(['disk', 'needs', 'speed', 'recent', 'instances', 'torrents', 'volume']);
    expect(layout.hidden).toEqual(['disk']);
    expect(layout.widths).toEqual({ disk: 4 });
  });

  it('fills every row, so no row ends in a gap', () => {
    expect(pack([6, 6, 3, 3])).toEqual([6, 6, 3, 3]);
    expect(pack([3])).toEqual([6]);
    expect(pack([2, 2, 3])).toEqual([2, 4, 6]);
    expect(pack([4, 3, 2])).toEqual([6, 3, 3]);
    expect(pack([])).toEqual([]);
  });

  it('moves a card one place among the cards on show', () => {
    const start = withHidden(DEFAULT_LAYOUT, 'speed', true);
    expect(shownCards(start)).toEqual(['needs', 'recent', 'disk']);
    const later = moved(start, 'needs', 1);
    expect(shownCards(later)).toEqual(['recent', 'needs', 'disk']);
    expect(moved(later, 'recent', -1)).toBe(later);
    expect(moved(later, 'disk', 1)).toBe(later);
  });

  it('hides a card, shows it again and remembers its width', () => {
    const hidden = withHidden(DEFAULT_LAYOUT, 'recent', true);
    expect(shownCards(hidden)).not.toContain('recent');
    expect(shownCards(withHidden(hidden, 'recent', false))).toContain('recent');
    expect(spanOf(DEFAULT_LAYOUT, 'recent')).toBe(3);
    expect(spanOf(withWidth(DEFAULT_LAYOUT, 'recent', 4), 'recent')).toBe(4);
  });

  it('takes a dragged order without losing the hidden cards', () => {
    const layout = withOrder(DEFAULT_LAYOUT, ['disk', 'recent', 'speed', 'needs']);
    expect(shownCards(layout)).toEqual(['disk', 'recent', 'speed', 'needs']);
    expect(layout.order).toHaveLength(OVERVIEW_CARDS.length);
  });
});
