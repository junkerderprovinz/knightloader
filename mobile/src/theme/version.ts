/**
 * The GlimStone release the ports in this folder follow, copied from the
 * design language's packages/core/src/version.ts. It ships beside the files it
 * describes so the About card cannot drift from them; bump it in the change
 * that lifts them.
 */
export const GLIMSTONE_VERSION = '4.5.8';

export const GLIMSTONE_REPO = 'https://github.com/junkerderprovinz/glimstone';

/** Where the version on the About card links: GlimStone's patch tags fold into the next minor and vanish, the newest release stays. */
export const GLIMSTONE_RELEASE_URL = `${GLIMSTONE_REPO}/releases/latest`;
