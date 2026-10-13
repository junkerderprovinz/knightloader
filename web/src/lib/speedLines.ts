// The colours of the speed graphs' two lines, chosen on the Appearance page and
// kept in the settings beside the accent. They reach the graphs as the two
// custom properties on the document root that every graph draws with.
import { ACCENTS, DEFAULT_ACCENT } from './appearance';

/** The two lines as the settings keep them: #rrggbb, or empty for the default. */
export interface SpeedLines {
  download?: string;
  seeding?: string;
}

/** What the download line is drawn in. */
export const DOWNLOAD_INK = 'var(--line-download)';
/** What the seeding line is drawn in. */
export const SEEDING_INK = 'var(--line-seeding)';

const HEX = /^#[0-9a-fA-F]{6}$/;

const isHex = (c: string | undefined): c is string => !!c && HEX.test(c);

/** hueOf is a colour's place on the colour wheel, in degrees. A grey sits at 0. */
function hueOf(hex: string): number {
  const n = parseInt(hex.slice(1), 16);
  const r = (n >> 16) & 255;
  const g = (n >> 8) & 255;
  const b = n & 255;
  const max = Math.max(r, g, b);
  const d = max - Math.min(r, g, b);
  if (d === 0) return 0;
  const h = max === r ? (g - b) / d : max === g ? 2 + (b - r) / d : 4 + (r - g) / d;
  return (h * 60 + 360) % 360;
}

/**
 * seedingDefault is the seeding line's colour while none is chosen: the accent
 * preset that stands farthest round the wheel from the accent, so the two
 * lines differ whatever the accent is. The built-in gold gets the blue.
 */
export function seedingDefault(accent: string | undefined): string {
  const from = hueOf(isHex(accent) ? accent : DEFAULT_ACCENT);
  let best = ACCENTS[0].hex;
  let farthest = -1;
  for (const { hex } of ACCENTS) {
    const turn = Math.abs(hueOf(hex) - from);
    const apart = Math.min(turn, 360 - turn);
    if (apart > farthest) {
      farthest = apart;
      best = hex;
    }
  }
  return best;
}

/** lineColours is what the two lines are drawn in, as #rrggbb, with the defaults filled in. */
export function lineColours(lines: SpeedLines, accent: string | undefined): { download: string; seeding: string } {
  return {
    download: isHex(lines.download) ? lines.download : isHex(accent) ? accent : DEFAULT_ACCENT,
    seeding: isHex(lines.seeding) ? lines.seeding : seedingDefault(accent),
  };
}

/** linesFromSettings reads the two fields of the settings. */
export function linesFromSettings(s: { downloadLine?: string; seedingLine?: string }): SpeedLines {
  return { download: s.downloadLine, seeding: s.seedingLine };
}

// Mirrored into localStorage, as the rest of the look is, only so the first
// paint after a reload is already right.
const CACHE = 'kl-speed-lines';

/**
 * setInks puts the two colours on the document root, mixed by the theme's
 * --ink-mix as --accent-ink is, so a pale one still reads on the light theme.
 */
function setInks(download: string, seeding: string): void {
  const root = document.documentElement.style;
  root.setProperty('--line-download', `color-mix(in srgb, ${download} var(--ink-mix), black)`);
  root.setProperty('--line-seeding', `color-mix(in srgb, ${seeding} var(--ink-mix), black)`);
}

/** applySpeedLines draws both lines in the colours chosen, or in the defaults that follow `accent`. */
export function applySpeedLines(lines: SpeedLines | undefined, accent: string | undefined): void {
  const { download, seeding } = lineColours(lines ?? {}, accent);
  setInks(download, seeding);
  try {
    localStorage.setItem(CACHE, JSON.stringify({ download, seeding }));
  } catch {
    // Without storage the stylesheet's defaults show until the settings load.
  }
}

/** applyCachedSpeedLines is applied at boot, before React renders anything. */
export function applyCachedSpeedLines(): void {
  try {
    const { download, seeding } = JSON.parse(localStorage.getItem(CACHE) ?? '{}') as SpeedLines;
    if (isHex(download) && isHex(seeding)) setInks(download, seeding);
  } catch {
    // An unreadable entry leaves the stylesheet's defaults.
  }
}
