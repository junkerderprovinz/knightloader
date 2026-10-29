import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import type { ComponentType } from 'react';
import { IconCollector, IconDownload, IconDownloadArrow, IconSearch } from './icons';

const markup = (Icon: ComponentType) => renderToStaticMarkup(<Icon />);

describe('the page glyphs', () => {
  it('draw the collector as a magnifying glass of its own, not the list filter', () => {
    expect(markup(IconCollector)).not.toBe(markup(IconSearch));
    expect(markup(IconCollector)).toContain('<circle');
  });

  it('draw Downloads as a bare arrow, not the disc a transfer state wears', () => {
    const arrow = markup(IconDownloadArrow);
    expect(arrow).not.toBe(markup(IconDownload));
    expect(arrow).not.toContain('evenodd');
  });

  it('give every page glyph the same 22px box', () => {
    for (const Icon of [IconCollector, IconDownloadArrow]) {
      expect(markup(Icon)).toMatch(/width="22" height="22"/);
    }
  });
});
