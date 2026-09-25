// Kept free of imports so web/check-module-lines.mjs can load it in Node.

/**
 * The key prefix under which the interface names an id a module line carries,
 * by the name of the value it arrives in.
 */
const NAMED_BY: Record<string, string> = {
  method: 'settings.reconnect.method.',
  quality: 'settings.resolvers.quality.',
  source: 'settings.resolvers.toolsFrom.',
};

/**
 * moduleArgs puts the interface's own names on the ids in a module line, so
 * the line says "Requests" where the Network page's tab does rather than the
 * server's "http". `name` translates a key, or answers undefined for a key the
 * interface does not have, and an id without a name shows as it came.
 */
export function moduleArgs(
  args: Record<string, string> | undefined,
  name: (key: string) => string | undefined,
): Record<string, string> | undefined {
  if (!args) return args;
  const out = { ...args };
  for (const [arg, prefix] of Object.entries(NAMED_BY)) {
    const id = args[arg];
    if (id === undefined) continue;
    out[arg] = name(prefix + id) ?? id;
  }
  return out;
}
