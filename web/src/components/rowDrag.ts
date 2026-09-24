import type { RowDragKey } from './listRows';

// The arithmetic of dragging rows and packages in the download list: what the
// pointer aims at, the order a drop would produce, how far each row slides to
// preview it, and where the carried block is drawn. The preview and the drop
// share previewOrder, so the drop lands where the preview showed.
// web/check-row-drag.mjs tests these functions.

/** One drawn row's box, frozen when the drag started. */
export interface RowSlot {
  unit: RowDragKey;
  top: number;
  bottom: number;
}

// How far a press travels before it becomes a drag: Swing's
// DragSource.getDragThreshold() and Windows' SM_CXDRAG. Measured per axis, as
// Swing does.
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
 * selectedBlock is what a drag carries: the whole selection in drawn order, as
 * JDownloader moves it. A package whose links are all selected travels as one
 * package, keeping its internal order; links picked out of a package travel
 * loose.
 */
export function selectedBlock(rows: readonly BlockRow[], selected: ReadonlySet<string>): RowDragKey[] {
  const out: RowDragKey[] = [];
  // Links already carried by their travelling package.
  const taken = new Set<string>();
  for (const row of rows) {
    if (row.unit.kind === 'package') {
      // A package with no movable link has nothing for the queue to move.
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

export interface AimContext {
  packageOf: (taskId: string) => string;
  /** Whether the queue can move rows against this unit: finished and failed
   *  rows are in no band, and a package whose links disagree has no one band. */
  canTarget: (unit: RowDragKey) => boolean;
}

function isMoved(
  unit: RowDragKey,
  movedIds: ReadonlySet<string>,
  movedNames: ReadonlySet<string>,
  ctx: AimContext,
): boolean {
  if (unit.kind === 'package') return movedNames.has(unit.name);
  if (movedIds.has(unit.id)) return true;
  // A link of a travelling package is in flight too.
  const owner = ctx.packageOf(unit.id);
  return owner !== undefined && movedNames.has(owner);
}

/**
 * aimAt returns the unit the pointer is over and whether the drop lands after
 * it. It aims against the frozen snapshot, since rows are displaced by the
 * preview while the pointer is down.
 *
 * A block made only of packages aims at whole packages, each spanning its
 * header and links, so every pixel is a landing place; a package never lands
 * between another package's links. Rows of the block itself are never
 * targets, or the aim would mostly hit the block and the preview would stall.
 */
export function aimAt(
  slots: readonly RowSlot[],
  y: number,
  moved: readonly RowDragKey[],
  ctx: AimContext,
): { target: RowDragKey; after: boolean } | null {
  // A loose link in the block needs row-sized targets between package links.
  const asFolders = moved.length > 0 && moved.every((u) => u.kind === 'package');
  const movedNames = new Set(moved.flatMap((u) => (u.kind === 'package' ? [u.name] : [])));
  const movedIds = new Set(moved.flatMap((u) => (u.kind === 'task' ? [u.id] : [])));
  const boxes: { unit: RowDragKey; top: number; bottom: number }[] = [];
  if (asFolders) {
    // Each drawn row counts toward its package's box.
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
 * previewOrder lifts `moved` out and splices it back against the anchor's
 * first id (before) or last id (after), keeping the block as one run. The drop
 * uses the same function, so it lands where the preview showed.
 *
 * It returns null when nothing would change: a drop onto the block itself or
 * back onto its own footprint, which is the gesture's shortest movement.
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
 * stackOffsets maps each row key to the pixel offset that shows `wanted`,
 * restacking from the first slot's top. A gap belongs to the seam between two
 * positions, not to the row. It returns null when the snapshot no longer
 * matches the list, since partial offsets would overlap rows.
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

/**
 * carriedOffsets maps each carried row to the offset that draws the block under
 * the pointer: stacked as `landing` would land it, moved by the pointer's
 * travel since the press on `pressed`, and kept between `top` and `bottom` so a
 * drag never grows a scrollbar. Rows the snapshot never measured are left out.
 */
export function carriedOffsets(
  slots: readonly RowSlot[],
  landing: ReadonlyMap<string, number>,
  carried: ReadonlySet<string>,
  pressed: string,
  travel: number,
  bounds: { top: number; bottom: number },
  key: (unit: RowDragKey) => string,
): Map<string, number> {
  const base = travel - (landing.get(pressed) ?? 0);
  const out = new Map<string, number>();
  let top = Infinity;
  let bottom = -Infinity;
  for (const slot of slots) {
    const k = key(slot.unit);
    if (!carried.has(k)) continue;
    const dy = (landing.get(k) ?? 0) + base;
    out.set(k, dy);
    top = Math.min(top, slot.top + dy);
    bottom = Math.max(bottom, slot.bottom + dy);
  }
  let shift = 0;
  if (top < bounds.top) shift = bounds.top - top;
  // A block taller than the list keeps its top in view.
  else if (bottom > bounds.bottom) shift = Math.max(bounds.bottom - bottom, bounds.top - top);
  if (shift !== 0) for (const [k, dy] of out) out.set(k, dy + shift);
  return out;
}
