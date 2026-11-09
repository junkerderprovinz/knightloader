// How WellSelector holds its one row, free of React so a check can run it.
import { segmentFit, type SelectorFit } from '../theme/segmentFit';

/** A segment's padding on either side of its name. */
export const SEGMENT_PAD = 6;
/** The groove's padding and the space between its segments. */
export const WELL_PAD = 3;
export const WELL_GAP = 2;

/**
 * wellForm is how a well `groove` points wide holds labels of the given
 * one-line widths: segments of equal share, segments as wide as their names,
 * glyphs alone, or the dropdown. `glyph` is what a glyph and its gap add to a
 * segment, and null where not every segment has one.
 */
export function wellForm(groove: number, labels: readonly number[], glyph: number | null): SelectorFit {
  const room = groove - 2 * WELL_PAD;
  const count = labels.length;
  const oneLine = labels.map((w) => w + (glyph ?? 0) + 2 * SEGMENT_PAD);
  const even = (room - WELL_GAP * (count - 1)) / count;
  return segmentFit(room, even, oneLine, WELL_GAP, glyph !== null, true).fit;
}
