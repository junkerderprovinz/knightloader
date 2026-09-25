import { describe, expect, it } from 'vitest';

import { gridTemplate, resolveLayout, type ColumnId, type ResolvedLayout } from './columns';

/** The track of one column, by its place among the visible ones. */
function track(template: string, layout: ResolvedLayout, id: ColumnId): string {
  const tracks = template.match(/minmax\([^)]*\)|\S+/g) ?? [];
  return tracks[layout.visible.findIndex((c) => c.id === id)];
}

describe('gridTemplate', () => {
  it('lets the name fill what the other columns leave', () => {
    const layout = resolveLayout('downloads', null);
    const template = gridTemplate(layout);
    expect(track(template, layout, 'name')).toBe(`minmax(${layout.minWidthOf('name')}px, 1fr)`);
    expect(template.match(/1fr/g)).toHaveLength(1);
  });

  it('lets an untouched column give way down to its minimum', () => {
    const layout = resolveLayout('downloads', null);
    expect(track(gridTemplate(layout), layout, 'host')).toBe(
      `minmax(${layout.minWidthOf('host')}px, ${layout.widthOf('host')}px)`,
    );
  });

  it('keeps a dragged column at the width it was dragged to', () => {
    const layout = resolveLayout('downloads', { order: [], hidden: [], widths: { host: 300 }, v: 3 });
    expect(track(gridTemplate(layout), layout, 'host')).toBe('300px');
  });

  it('hands the rest to the last column once the name is dragged', () => {
    const layout = resolveLayout('downloads', { order: [], hidden: [], widths: { name: 400 }, v: 3 });
    const template = gridTemplate(layout);
    const last = layout.visible[layout.visible.length - 1];
    expect(track(template, layout, 'name')).toBe(`minmax(${layout.minWidthOf('name')}px, 400px)`);
    expect(track(template, layout, last.id)).toBe(`minmax(${layout.widthOf(last.id)}px, 1fr)`);
  });

  it('draws a column under the pointer as if its width were stored', () => {
    const layout = resolveLayout('downloads', null);
    const template = gridTemplate(layout, { id: 'name', width: 250 });
    expect(track(template, layout, 'name')).toBe(`minmax(${layout.minWidthOf('name')}px, 250px)`);
  });
});

describe('resolveLayout', () => {
  it('lets the download list narrow the variant column below the collector pickers', () => {
    const downloads = resolveLayout('downloads', null);
    const collector = resolveLayout('collector', null);
    expect(downloads.minWidthOf('variant')).toBeLessThan(collector.minWidthOf('variant'));
    expect(downloads.widthOf('variant')).toBeGreaterThanOrEqual(downloads.minWidthOf('variant'));
  });
});
