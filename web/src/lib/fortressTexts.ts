// The texts the server writes into the fortress status and log, where the
// browser cannot translate: another instance or a dashboard reads them as they
// come. They live in the locale files like every other string and are copied
// into internal/fortress/texts.json by scripts/gen-fortress-texts.mjs.

/** Keys the status reuses from the interface, besides everything under fortress. */
const REUSED = [
  'overview.needs.captcha',
  'overview.needs.accountFailing',
  'overview.needs.loginRejected',
  'overview.needs.accountExpires',
  'overview.needs.diskLow',
];

const taken = (key: string) =>
  key.startsWith('fortress.') || key.startsWith('health.part.') || key.startsWith('health.state.') || REUSED.includes(key);

/** fortressTexts picks the server's keys out of every locale, sorted so the file only changes with a text. */
export function fortressTexts(dicts: Record<string, Record<string, string>>): Record<string, Record<string, string>> {
  const out: Record<string, Record<string, string>> = {};
  for (const lang of Object.keys(dicts).sort()) {
    const picked: Record<string, string> = {};
    for (const key of Object.keys(dicts[lang]).filter(taken).sort()) picked[key] = dicts[lang][key];
    out[lang] = picked;
  }
  return out;
}
