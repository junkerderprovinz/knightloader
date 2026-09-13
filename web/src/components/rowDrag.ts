import type { RowDragKey } from './listRows';

/**
 * The arithmetic behind dragging a row or a whole folder in the download list:
 * what the pointer is aiming at, what the list would look like if it were let
 * go now, and how far every row has to slide to show that.
 *
 * It lives outside TaskList.tsx because none of it needs React, a DOM or a
 * server, and because all three of the faults this file exists to stop are
 * arithmetic rather than rendering:
 *
 *   - a folder that only ever aimed at other folders' HEADERS. Dragged across
 *     the eight open links of another folder, the nearest header was usually
 *     its OWN, so the drag aimed at itself and the list stood still for most of
 *     the gesture,
 *   - a preview that gave up whenever the drop crossed a priority band, while
 *     the drop itself went through,
 *   - a drop that has to land exactly where the preview promised.
 *
 * Guarded by web/check-row-drag.mjs, which runs these functions on plain
 * numbers.
 */

/** One drawn row's box, as the drag froze it before anything moved. */
export interface RowSlot {
  unit: RowDragKey;
  top: number;
  bottom: number;
}

/** Whether two drag units are the same row or the same folder. */
export function sameUnit(a: RowDragKey, b: RowDragKey): boolean {
  if (a.kind !== b.kind) return false;
  return a.kind === 'task' ? a.id === (b as typeof a).id : a.name === (b as typeof a).name;
}

/** What aimAt needs to know about the list it is aiming into. */
export interface AimContext {
  /** Which folder a link belongs to - how a link row becomes a landing place
   *  for a whole folder. */
  packageOf: (taskId: string) => string;
  /** Whether this unit is something the queue can be told to move rows against
   *  at all: a finished or failed row is in no band, and a folder whose links
   *  disagree has no one band to move into. */
  canTarget: (unit: RowDragKey) => boolean;
}

/**
 * The unit the pointer is over, and whether the drag would land after it.
 *
 * Aiming is done against the frozen snapshot and the pointer's own Y, never
 * against the element the browser delivered the event to: every row is
 * displaced by a transform for as long as the pointer is down, so the element
 * under the pointer is the one the preview has MOVED there.
 *
 * A whole folder aims at whole FOLDERS - but at any row of one, not only at its
 * header. The header is 44px of a folder that is 300px tall on screen, so
 * "nearest header" left the preview standing still for everything in between,
 * which is exactly what "das live verschieben geht nicht" looks like from a
 * chair. Folding a folder's links into their folder's own extent makes every
 * pixel of the list a landing place, and the half of the FOLDER the pointer is
 * in - not the half of its header - decides before or after. A folder still
 * never lands between two links of another folder: groupByPackage re-merges a
 * package at its first appearance, so that is not a place a folder can come to
 * rest.
 */
export function aimAt(
  slots: readonly RowSlot[],
  y: number,
  dragged: RowDragKey,
  ctx: AimContext,
): { target: RowDragKey; after: boolean } | null {
  const boxes: { unit: RowDragKey; top: number; bottom: number }[] = [];
  if (dragged.kind === 'package') {
    // Every drawn row folded into the folder it belongs to, so the whole of a
    // folder is one landing place and there are no dead pixels between two
    // headers. A link row of folder B means folder B, which is also the only
    // thing a drop there could honestly do.
    const byName = new Map<string, { top: number; bottom: number }>();
    for (const slot of slots) {
      const name = slot.unit.kind === 'package' ? slot.unit.name : ctx.packageOf(slot.unit.id);
      if (name === undefined) continue;
      const seen = byName.get(name);
      if (seen) {
        seen.top = Math.min(seen.top, slot.top);
        seen.bottom = Math.max(seen.bottom, slot.bottom);
      } else byName.set(name, { top: slot.top, bottom: slot.bottom });
    }
    for (const [name, box] of byName) boxes.push({ unit: { kind: 'package', name }, ...box });
  } else {
    for (const slot of slots) boxes.push({ unit: slot.unit, top: slot.top, bottom: slot.bottom });
  }

  let best: { unit: RowDragKey; top: number; bottom: number } | null = null;
  let bestDist = Infinity;
  for (const box of boxes) {
    if (!ctx.canTarget(box.unit)) continue;
    const dist = y < box.top ? box.top - y : y > box.bottom ? y - box.bottom : 0;
    if (dist < bestDist) {
      bestDist = dist;
      best = box;
    }
  }
  if (!best) return null;
  return { target: best.unit, after: y > (best.top + best.bottom) / 2 };
}

/**
 * The list the drag is promising: `moved` lifted out where it is and put back
 * against `anchor`'s own edge - its first id when the block lands before it,
 * its last when after - so a folder (several ids at once) keeps its own
 * internal order and lands as one run, exactly where a single link would.
 *
 * The same splice the drop itself makes, which is the point: the preview cannot
 * promise an arrangement the drop would not produce, because both come out of
 * this function.
 *
 * null means there is nothing to show: the block was dropped on itself, on part
 * of itself, or back on its own footprint. THE LAST ONE IS NOT AN EDGE CASE -
 * dragging a row onto the upper half of the row below it, or the lower half of
 * the one above, is the shortest movement the gesture has, and it leaves the
 * list exactly as it was.
 */
export function previewOrder(
  flat: readonly string[],
  moved: readonly string[],
  anchor: readonly string[],
  after: boolean,
): string[] | null {
  if (moved.length === 0 || anchor.length === 0) return null;
  const movedSet = new Set(moved);
  if (anchor.some((id) => movedSet.has(id))) return null;
  const rest = flat.filter((id) => !movedSet.has(id));
  const anchorId = after ? anchor[anchor.length - 1] : anchor[0];
  const at = rest.indexOf(anchorId);
  if (at < 0) return null;
  const out = rest.slice();
  out.splice(after ? at + 1 : at, 0, ...moved);
  if (out.length === flat.length && out.every((id, i) => id === flat[i])) return null;
  return out;
}

/**
 * How far each drawn row has to slide to show `wanted`, in pixels: row key ->
 * offset, computed entirely off the frozen snapshot.
 *
 * The rows are stacked back up from the first slot's top, each taking its own
 * measured height and keeping the GAP that belongs to its new position rather
 * than to itself - the gap is a property of the seam between two rows (the rule
 * above a folder header), not of the row that happens to sit above it.
 *
 * Bails out whole rather than in part, and returns null to say so. A poll that
 * adds or removes a task mid-drag leaves the snapshot describing a list that no
 * longer exists, and half-correct offsets would leave rows lying on top of each
 * other.
 */
export function stackOffsets(
  slots: readonly RowSlot[],
  wanted: readonly string[],
  key: (unit: RowDragKey) => string,
): Map<string, number> | null {
  if (slots.length === 0 || wanted.length !== slots.length) return null;
  const geom = new Map(slots.map((s) => [key(s.unit), s] as const));
  const out = new Map<string, number>();
  let y = slots[0].top;
  for (let i = 0; i < wanted.length; i++) {
    const g = geom.get(wanted[i]);
    if (!g) return null;
    out.set(wanted[i], y - g.top);
    const next = slots[i + 1];
    y += g.bottom - g.top + (next ? next.top - slots[i].bottom : 0);
  }
  return out;
}
