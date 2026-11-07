// The system accent's arithmetic, kept free of imports so check-system-accent.mjs
// runs it as it is. The source itself is systemAccent.ts.

/**
 * The accent setting that takes its colour from the device (GlimStone's rule
 * 44), stored as this word and never as the colour, so a look snapshotted on
 * one phone does not carry its wallpaper to another.
 */
export const SYSTEM_ACCENT = 'system';

const hex = (v: string | undefined): v is string => !!v && /^#[0-9a-fA-F]{6}$/.test(v);

/**
 * resolveAccent is the colour the phone wears: the local choice over the
 * instance's, where a choice of the system's colour falls back to the
 * instance's on a phone without one, and undefined leaves the default.
 */
export function resolveAccent(
  chosen: string | undefined,
  instance: string | undefined,
  system: string | undefined,
): string | undefined {
  if (chosen === SYSTEM_ACCENT && hex(system)) return system;
  if (hex(chosen)) return chosen;
  return hex(instance) ? instance : undefined;
}

/**
 * argbToHex is the #RRGGBB of a colour Android hands over as an ARGB int.
 * Kotlin's Int is signed, so an opaque colour arrives negative.
 */
export function argbToHex(argb: number): string {
  return `#${((argb >>> 0) & 0xffffff).toString(16).padStart(6, '0').toUpperCase()}`;
}
