/**
 * The GlimStone release this web UI follows, copied from the design language's
 * packages/core/src/version.ts. Bump it in the change that lifts the UI to a new
 * release, once that release is fully applied.
 */
export const GLIMSTONE_VERSION = '4.7.0';

export const GLIMSTONE_REPO = 'https://github.com/junkerderprovinz/glimstone';

/** Where the version on the Version card links: GlimStone's patch tags fold into the next minor and vanish, the newest release stays. */
export const GLIMSTONE_RELEASE_URL = `${GLIMSTONE_REPO}/releases/latest`;
