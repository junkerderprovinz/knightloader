// The overview's cards and how the person arranged them: an order, the cards
// set aside, and a width per card. The arrangement is one field of the
// interface state (lib/uistate.ts), so it follows the person between browsers.

/** Every card of the overview, in the order a new arrangement starts with. */
export const OVERVIEW_CARDS = ['needs', 'speed', 'recent', 'disk', 'instances', 'torrents', 'volume'] as const;
export type OverviewCard = (typeof OVERVIEW_CARDS)[number];

/** The grid has six columns, so a card is a third, a half, two thirds or the whole row wide. */
export const COLUMNS = 6;
export const SPANS = [2, 3, 4, 6] as const;
export type CardSpan = (typeof SPANS)[number];

const NATURAL_SPAN: Record<OverviewCard, CardSpan> = {
  needs: 6,
  speed: 6,
  recent: 3,
  disk: 3,
  instances: 2,
  torrents: 6,
  volume: 6,
};

export interface OverviewLayout {
  order: OverviewCard[];
  hidden: OverviewCard[];
  widths: Partial<Record<OverviewCard, CardSpan>>;
}

/** The overview opens on the head and four cards; the rest waits under Customize. */
export const DEFAULT_LAYOUT: OverviewLayout = {
  order: [...OVERVIEW_CARDS],
  hidden: ['instances', 'torrents', 'volume'],
  widths: {},
};

const isCard = (id: unknown): id is OverviewCard => OVERVIEW_CARDS.includes(id as OverviewCard);
const isSpan = (n: unknown): n is CardSpan => SPANS.includes(n as CardSpan);

/**
 * readLayout turns the stored field into an arrangement this build can draw.
 * A card the stored order does not know goes to the end, and one this build
 * does not have is dropped.
 */
export function readLayout(stored: unknown): OverviewLayout {
  if (!stored || typeof stored !== 'object') return DEFAULT_LAYOUT;
  const s = stored as Partial<Record<keyof OverviewLayout, unknown>>;
  const known = [...new Set(Array.isArray(s.order) ? s.order.filter(isCard) : [])];
  const widths: OverviewLayout['widths'] = {};
  for (const [id, span] of Object.entries(s.widths ?? {})) {
    if (isCard(id) && isSpan(span)) widths[id] = span;
  }
  return {
    order: [...known, ...OVERVIEW_CARDS.filter((id) => !known.includes(id))],
    hidden: Array.isArray(s.hidden) ? s.hidden.filter(isCard) : [],
    widths,
  };
}

export const spanOf = (layout: OverviewLayout, id: OverviewCard): CardSpan => layout.widths[id] ?? NATURAL_SPAN[id];

/** The cards on show, in the person's order. */
export const shownCards = (layout: OverviewLayout): OverviewCard[] =>
  layout.order.filter((id) => !layout.hidden.includes(id));

/**
 * pack fills every row: when the next card does not fit, the last card of the
 * row takes what is left, and so does the last card of all. The stored widths
 * stay as they are, so hiding a card never leaves a gap.
 */
export function pack(spans: readonly number[]): number[] {
  const out = [...spans];
  let used = 0;
  out.forEach((span, i) => {
    if (used + span > COLUMNS) {
      out[i - 1] += COLUMNS - used;
      used = 0;
    }
    used += span;
  });
  if (out.length > 0) out[out.length - 1] += COLUMNS - used;
  return out;
}

/** withOrder stores a new order of the cards on show; the hidden ones keep their places behind them. */
export function withOrder(layout: OverviewLayout, shown: readonly OverviewCard[]): OverviewLayout {
  return { ...layout, order: [...shown, ...layout.order.filter((id) => !shown.includes(id))] };
}

/** moved steps a card one place among the cards on show, or returns the layout as it is at an end. */
export function moved(layout: OverviewLayout, id: OverviewCard, step: -1 | 1): OverviewLayout {
  const shown = shownCards(layout);
  const from = shown.indexOf(id);
  const to = from + step;
  if (from < 0 || to < 0 || to >= shown.length) return layout;
  [shown[from], shown[to]] = [shown[to], shown[from]];
  return withOrder(layout, shown);
}

export function withHidden(layout: OverviewLayout, id: OverviewCard, hidden: boolean): OverviewLayout {
  const rest = layout.hidden.filter((h) => h !== id);
  return { ...layout, hidden: hidden ? [...rest, id] : rest };
}

export function withWidth(layout: OverviewLayout, id: OverviewCard, span: CardSpan): OverviewLayout {
  return { ...layout, widths: { ...layout.widths, [id]: span } };
}
