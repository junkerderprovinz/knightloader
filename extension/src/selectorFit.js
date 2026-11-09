// How a well selector keeps to its one row, ported from glimstone's
// packages/core/src/segmentLayout.ts and selector.ts with the types stripped
// ("It keeps to one row" in the design language). Every well here has a glyph
// on each segment, so a row that does not fit shows the glyphs alone, each
// label staying the segment's name and its bubble.

/**
 * clientWidth rounds the room to a whole pixel, so a row that fits exactly can
 * come out half a pixel short.
 */
const SEGMENT_ROUNDING = 1;

/**
 * segmentFit decides how segments whose labels need `oneLine` pixels on one
 * line share `room`: at `even` pixels each, at their own widths, as glyphs
 * alone, as the dropdown, or cut short. `even` is null for a well whose
 * segments hug their labels.
 */
function segmentFit(room, even, oneLine, gap, glyphs, list) {
  const count = oneLine.length;
  const free = room + SEGMENT_ROUNDING - gap * (count - 1);
  const whole = (fit) => ({ fit, cut: oneLine.map(() => false) });
  if (even !== null && even * count <= free && oneLine.every((w) => w <= even + SEGMENT_ROUNDING)) return whole('even');
  if (oneLine.reduce((sum, w) => sum + w, 0) <= free) return whole('content');
  if (glyphs) return whole('squeezed');
  if (list) return whole('list');
  const share = (room - gap * (count - 1)) / count;
  return { fit: 'truncated', cut: oneLine.map((w) => w > share + SEGMENT_ROUNDING) };
}

/** The watch on each well, so a well drawn again is not watched twice. */
const segmentWatches = new WeakMap();

/**
 * fitSegments keeps the well `track` on its one row, at once and whenever the
 * box or a segment resizes, since a late web font changes what a label needs.
 * Call it again whenever the segments are drawn anew. The words are put back
 * for each reading, so the well sees when they fit again. A `--hug` track is
 * given its width here, since its labels do not size it.
 */
function fitSegments(track) {
  segmentWatches.get(track)?.disconnect();
  const hug = track.classList.contains('glim-seg--hug');
  const segs = [...track.querySelectorAll(':scope > button')];
  const measure = () => {
    if (segs.length === 0) return;
    const shown = ['glim-seg--content', 'glim-seg--squeezed'].filter((c) => track.classList.contains(c));
    track.classList.remove(...shown);
    const { width, minWidth } = track.style;
    Object.assign(track.style, { width: '100%', minWidth: '0' });
    const own = segs.map((s) => s.style.cssText);
    for (const s of segs) Object.assign(s.style, { flex: 'none', width: 'max-content', minWidth: '0', maxWidth: 'none' });
    const oneLine = segs.map((s) => parseFloat(getComputedStyle(s).width));
    segs.forEach((s, i) => (s.style.cssText = own[i]));
    const style = getComputedStyle(track);
    const inset = parseFloat(style.paddingLeft) + parseFloat(style.paddingRight);
    const room = track.clientWidth - inset;
    const gap = parseFloat(style.columnGap) || 0;
    Object.assign(track.style, { width, minWidth });
    track.classList.add(...shown);
    // A hidden box measures nothing; it is laid out once it shows.
    if (room <= 0) return;
    const even = hug ? null : Math.floor((room - gap * (segs.length - 1)) / segs.length);
    const { fit } = segmentFit(room, even, oneLine, gap, true, false);
    track.classList.toggle('glim-seg--content', fit === 'content');
    track.classList.toggle('glim-seg--squeezed', fit === 'squeezed');
    for (const s of segs) {
      if (fit === 'squeezed') s.dataset.tip = s.querySelector(':scope > span')?.textContent ?? '';
      else delete s.dataset.tip;
    }
    if (!hug) return;
    const row = fit === 'content' ? oneLine.reduce((sum, w) => sum + w, 0) + gap * (segs.length - 1) + inset + 1 : 0;
    track.style.width = row ? `${row}px` : '100%';
  };
  measure();
  const watch = new ResizeObserver(measure);
  watch.observe(track);
  if (track.parentElement) watch.observe(track.parentElement);
  for (const s of segs) watch.observe(s);
  segmentWatches.set(track, watch);
  void document.fonts?.ready.then(measure);
}
