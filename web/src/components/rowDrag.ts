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

/**
 * How far a press has to travel before it stops being a click and becomes a
 * gesture.
 *
 * Five pixels, and the number is borrowed rather than invented: it is what
 * Swing hands its own tables through `DragSource.getDragThreshold()`, so it is
 * the distance the list this one is copying uses to tell a click from a drag,
 * and it matches Windows' own SM_CXDRAG. It has to be small enough that a
 * deliberate drag feels immediate and large enough that the shake in a hand
 * releasing a button does not turn every click into a one-row selection sweep.
 *
 * Measured per axis rather than as a diagonal distance, which is the same thing
 * Swing does: a hand dragging straight down the list crosses it a pixel or two
 * earlier than a circle of radius five would, and down the list is the
 * direction this gesture is actually used in.
 */
export const GESTURE_THRESHOLD_PX = 5;

export function pastThreshold(dx: number, dy: number): boolean {
  return Math.abs(dx) >= GESTURE_THRESHOLD_PX || Math.abs(dy) >= GESTURE_THRESHOLD_PX;
}

/** One drawn row and the movable task ids it stands for. */
export interface BlockRow {
  unit: RowDragKey;
  /** A folder's own movable links, or the one link a link row is. */
  ids: readonly string[];
}

/**
 * What a move gesture carries: the selected units, in the order the list draws
 * them.
 *
 * A MOVE MOVES THE WHOLE SELECTION and not the row the hand grabbed (jdp,
 * 2026-09-14: "dann ein zweiter klick den man hält und man kann per drag and
 * drop verschieben" - the whole marking, the way JDownloader's own table hands
 * its transfer handler the entire selection rather than the pressed row).
 *
 * A FOLDER WHOSE LINKS ARE ALL SELECTED TRAVELS AS A FOLDER, and its links do
 * not travel a second time on their own. That is the whole answer to "zieht ein
 * markierter Ordner seine Links mit": there is no separate question, because
 * clicking a folder header puts every one of that folder's links into the
 * selection (TaskListCard's own selectUnit), so a selected folder is exactly a
 * folder whose links are selected. Emitting the folder rather than its links
 * one by one is what keeps a folder landing as one contiguous run with its own
 * internal order intact, instead of as N loose links that the drop would then
 * have to guess how to re-group.
 *
 * The other way round is the case where somebody picked three links out of a
 * folder of eleven: those travel loose, and the folder stays where it is.
 *
 * Drawn order and nothing else, because previewOrder splices the block back in
 * as one contiguous run - the order it is collected in is the order it lands
 * in, and a person who selected top-to-bottom expects what they see.
 */
export function selectedBlock(rows: readonly BlockRow[], selected: ReadonlySet<string>): RowDragKey[] {
  const out: RowDragKey[] = [];
  // Ids already accounted for by a folder that is travelling whole. A folder's
  // own link rows follow its header in `rows`, so this is what stops them being
  // emitted a second time - and it works for a folded folder too, where those
  // rows are not drawn at all and there is simply nothing to skip.
  const taken = new Set<string>();
  for (const row of rows) {
    if (row.unit.kind === 'package') {
      // A folder with no movable link is not a unit at all: there is nothing in
      // it the queue could be told to move, so it cannot be part of a block.
      if (row.ids.length === 0 || !row.ids.every((id) => selected.has(id))) continue;
      out.push(row.unit);
      for (const id of row.ids) taken.add(id);
      continue;
    }
    if (taken.has(row.unit.id) || !selected.has(row.unit.id)) continue;
    out.push(row.unit);
  }
  return out;
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

/** Whether this drawn row is part of the block in flight. */
function isMoved(
  unit: RowDragKey,
  movedIds: ReadonlySet<string>,
  movedNames: ReadonlySet<string>,
  ctx: AimContext,
): boolean {
  if (unit.kind === 'package') return movedNames.has(unit.name);
  if (movedIds.has(unit.id)) return true;
  // A link of a folder that is itself travelling. The folder moves whole, so
  // its links are in flight even though nobody named them one by one.
  const owner = ctx.packageOf(unit.id);
  return owner !== undefined && movedNames.has(owner);
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
 *
 * `moved` IS THE WHOLE BLOCK, not the row the hand happened to grab, because
 * the JDownloader gesture moves the whole selection (see selectedBlock). Two
 * things follow from that, and both are the difference between a live preview
 * and a dead one:
 *
 *   - the fold-into-folders rule asks whether every unit in flight is a folder,
 *     not whether the grabbed one is. A block of folders wants folder-sized
 *     landing places; a block with one loose link in it needs row-sized ones,
 *     or that link could never come to rest between two links of a folder.
 *   - NO ROW OF THE BLOCK IS A LANDING PLACE FOR THE BLOCK. Dragging six
 *     selected rows, the pointer is over one of the six for most of the
 *     gesture; left in the list of targets, the nearest box is nearly always
 *     the block itself, previewOrder answers null for that (anchor inside
 *     moved) and the list stands still - the same dead drag the folder aim was
 *     written against, arriving from the other side. Skipping them means the
 *     aim is the nearest row that could actually receive the block.
 */
export function aimAt(
  slots: readonly RowSlot[],
  y: number,
  moved: readonly RowDragKey[],
  ctx: AimContext,
): { target: RowDragKey; after: boolean } | null {
  // Folder-sized landing places only when EVERY unit in flight is a folder. A
  // block that also holds a loose link needs the finer answer: a link has to be
  // able to land between two links of a folder, which a folder never can.
  const asFolders = moved.length > 0 && moved.every((u) => u.kind === 'package');
  const movedNames = new Set(moved.flatMap((u) => (u.kind === 'package' ? [u.name] : [])));
  const movedIds = new Set(moved.flatMap((u) => (u.kind === 'task' ? [u.id] : [])));
  const boxes: { unit: RowDragKey; top: number; bottom: number }[] = [];
  if (asFolders) {
    // Every drawn row folded into the folder it belongs to, so the whole of a
    // folder is one landing place and there are no dead pixels between two
    // headers. A link row of folder B means folder B, which is also the only
    // thing a drop there could honestly do.
    const byName = new Map<string, { top: number; bottom: number }>();
    for (const slot of slots) {
      const name = slot.unit.kind === 'package' ? slot.unit.name : ctx.packageOf(slot.unit.id);
      if (name === undefined || movedNames.has(name)) continue;
      const seen = byName.get(name);
      if (seen) {
        seen.top = Math.min(seen.top, slot.top);
        seen.bottom = Math.max(seen.bottom, slot.bottom);
      } else byName.set(name, { top: slot.top, bottom: slot.bottom });
    }
    for (const [name, box] of byName) boxes.push({ unit: { kind: 'package', name }, ...box });
  } else {
    for (const slot of slots) {
      if (isMoved(slot.unit, movedIds, movedNames, ctx)) continue;
      boxes.push({ unit: slot.unit, top: slot.top, bottom: slot.bottom });
    }
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
