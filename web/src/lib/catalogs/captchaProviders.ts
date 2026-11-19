// The captcha catalog: the paid services an app hands a captcha to before it
// asks a person, each with its mark. An app hands catalogPicker the ids it can
// send to and keeps what it needs to sign in (an API key, or a login for the
// one that takes the account itself) and the order it tries them in beside
// them by id.
//
// A solver is known by the word the app stores its key under. Its key unlocks
// no link, so a solver stands in a catalog of its own rather than beside the
// debrid services.

import type { Catalog, CatalogEntry } from "./catalog";
import { CAPTCHA_MARKS as M } from "./captchaMarks";

export type CaptchaGroup = "solver";

const SOLVER = "IconCaptcha";

const ENTRIES = [
  { id: "2captcha", name: "2Captcha", group: "solver", terms: ["ruCaptcha"], glyph: SOLVER },
  { id: "9kw", name: "9kw.eu", group: "solver", glyph: SOLVER },
  { id: "anticaptcha", name: "Anti-Captcha", group: "solver", glyph: SOLVER },
  { id: "capmonster", name: "CapMonster Cloud", group: "solver", terms: ["ZennoLab"], mark: M.capmonster },
  { id: "capsolver", name: "CapSolver", group: "solver", glyph: SOLVER },
  { id: "deathbycaptcha", name: "DeathByCaptcha", group: "solver", terms: ["Death By Captcha"], glyph: SOLVER },
  { id: "solvecha", name: "Solvecha", group: "solver", glyph: SOLVER },
] as const satisfies readonly CatalogEntry<CaptchaGroup>[];

export type CaptchaProviderId = (typeof ENTRIES)[number]["id"];

/** Every captcha solver, with its group's place in the picker. */
export const CAPTCHA_PROVIDERS: Catalog<CaptchaGroup, CaptchaProviderId> = {
  groups: ["solver"],
  entries: ENTRIES,
};
