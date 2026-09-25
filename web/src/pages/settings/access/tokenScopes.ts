import { TOKEN_SCOPES, type TokenScope } from '../../../lib/api';
import type { TranslationKey } from '../../../lib/i18n';

/** The rights a new token can start from; "custom" is the four switches. */
export type TokenPreset = 'full' | 'addRead' | 'read' | 'custom';

/** What each preset grants, in the server's canonical order. */
export const PRESET_SCOPES: Record<Exclude<TokenPreset, 'custom'>, readonly TokenScope[]> = {
  full: TOKEN_SCOPES,
  addRead: ['read', 'add'],
  read: ['read'],
};

export const PRESET_LABEL: Record<TokenPreset, TranslationKey> = {
  full: 'settings.access.tokens.preset.full',
  addRead: 'settings.access.tokens.preset.addRead',
  read: 'settings.access.tokens.preset.read',
  custom: 'settings.access.tokens.preset.custom',
};

export const SCOPE_LABEL: Record<TokenScope, TranslationKey> = {
  read: 'settings.access.tokens.scope.read',
  add: 'settings.access.tokens.scope.add',
  control: 'settings.access.tokens.scope.control',
  admin: 'settings.access.tokens.scope.admin',
};

export const SCOPE_HINT: Record<TokenScope, TranslationKey> = {
  read: 'settings.access.tokens.scope.readHint',
  add: 'settings.access.tokens.scope.addHint',
  control: 'settings.access.tokens.scope.controlHint',
  admin: 'settings.access.tokens.scope.adminHint',
};

/** presetOf names the preset a set of rights is, or "custom" when none is. */
export function presetOf(scopes: readonly TokenScope[]): TokenPreset {
  const have = new Set(scopes);
  for (const [preset, granted] of Object.entries(PRESET_SCOPES)) {
    if (granted.length === have.size && granted.every((s) => have.has(s))) return preset as TokenPreset;
  }
  return 'custom';
}

/** scopesLabel is how a token's rights read in the list: the preset's name, or
 *  the rights themselves when they are no preset. */
export function scopesLabel(t: (key: TranslationKey) => string, scopes: readonly TokenScope[]): string {
  const preset = presetOf(scopes);
  if (preset !== 'custom') return t(PRESET_LABEL[preset]);
  return scopes.map((s) => t(SCOPE_LABEL[s])).join(', ');
}

/** withScope switches one right on or off, keeping the canonical order. */
export function withScope(scopes: readonly TokenScope[], scope: TokenScope, on: boolean): TokenScope[] {
  return TOKEN_SCOPES.filter((s) => (s === scope ? on : scopes.includes(s)));
}
