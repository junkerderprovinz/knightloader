// The download catalog: the debrid services that unlock hoster links with one
// account, and the file hosters an app downloads from on a login of their own,
// each with its mark. An app hands catalogPicker the ids it can reach and keeps
// what it needs to sign in (an API key, a username and password, where to find
// them) beside them by id.
//
// A debrid service is known by a word of its own, the id the app stores its key
// under. A hoster is known by the domain its links are written with and its
// logins are filed under, and the other domains it answers on are words its
// tile is found by.
//
// The hosters are the ones an app talks to with a client of its own. The many
// more that a downloader reaches only through a plugin come and go too fast for
// a shared list, and an app adds their tiles itself.

import type { Catalog, CatalogEntry } from "./catalog";
import { DOWNLOAD_MARKS as M } from "./downloadMarks";

export type DownloadGroup = "debrid" | "hoster";

const DEBRID = "IconUnlock";
const HOSTER = "IconCloud";

const ENTRIES = [
  { id: "alldebrid", name: "AllDebrid", group: "debrid", glyph: DEBRID },
  { id: "bestdebrid", name: "BestDebrid", group: "debrid", glyph: DEBRID },
  { id: "cocoleech", name: "CocoLeech", group: "debrid", glyph: DEBRID },
  { id: "cooldebrid", name: "CoolDebrid", group: "debrid", glyph: DEBRID },
  { id: "debriditalia", name: "DebridItalia", group: "debrid", glyph: DEBRID },
  { id: "debridlink", name: "Debrid-Link", group: "debrid", mark: M.debridlink },
  { id: "deepbrid", name: "Deepbrid", group: "debrid", glyph: DEBRID },
  { id: "fakirdebrid", name: "FakirDebrid", group: "debrid", glyph: DEBRID },
  { id: "linksnappy", name: "LinkSnappy", group: "debrid", glyph: DEBRID },
  { id: "megadebrid", name: "Mega-Debrid", group: "debrid", glyph: DEBRID },
  { id: "multiup", name: "MultiUp", group: "debrid", glyph: DEBRID },
  { id: "mydebrid", name: "MyDebrid", group: "debrid", glyph: DEBRID },
  { id: "neodebrid", name: "NeoDebrid", group: "debrid", glyph: DEBRID },
  { id: "offcloud", name: "Offcloud", group: "debrid", glyph: DEBRID },
  { id: "premiumize", name: "premiumize.me", group: "debrid", mark: M.premiumize },
  { id: "proleech", name: "ProLeech", group: "debrid", glyph: DEBRID },
  { id: "realdebrid", name: "Real-Debrid", group: "debrid", mark: M.realdebrid },
  { id: "rpnet", name: "RPNet", group: "debrid", glyph: DEBRID },
  { id: "torbox", name: "TorBox", group: "debrid", glyph: DEBRID },
  { id: "zevera", name: "Zevera", group: "debrid", glyph: DEBRID },

  { id: "1fichier.com", name: "1Fichier", group: "hoster", terms: ["desfichiers.com", "megadl.fr"], mark: M.fichier },
  { id: "clicknupload.click", name: "Clicknupload", group: "hoster", terms: ["Clickndownload"], glyph: HOSTER },
  { id: "ddownload.com", name: "ddownload", group: "hoster", terms: ["ddl.to"], glyph: HOSTER },
  { id: "fileboom.me", name: "FileBoom", group: "hoster", terms: ["fboom.me"], glyph: HOSTER },
  { id: "filer.net", name: "filer.net", group: "hoster", glyph: HOSTER },
  { id: "filespace.com", name: "FileSpace", group: "hoster", terms: ["spaceforfiles.com"], glyph: HOSTER },
  { id: "gofile.io", name: "Gofile", group: "hoster", glyph: HOSTER },
  { id: "katfile.com", name: "KatFile", group: "hoster", glyph: HOSTER },
  { id: "keep2share.cc", name: "Keep2Share", group: "hoster", terms: ["k2s.cc"], glyph: HOSTER },
  { id: "krakenfiles.com", name: "KrakenFiles", group: "hoster", glyph: HOSTER },
  { id: "mediafire.com", name: "MediaFire", group: "hoster", mark: M.mediafire },
  { id: "mega.nz", name: "MEGA", group: "hoster", terms: ["mega.co.nz"], mark: M.mega },
  { id: "nitroflare.com", name: "NitroFlare", group: "hoster", terms: ["nitro.download"], glyph: HOSTER },
  { id: "pixeldrain.com", name: "Pixeldrain", group: "hoster", mark: M.pixeldrain },
  { id: "rapidgator.net", name: "Rapidgator", group: "hoster", terms: ["rg.to"], glyph: HOSTER },
  { id: "send.now", name: "Send.now", group: "hoster", glyph: HOSTER },
  { id: "tezfiles.com", name: "TezFiles", group: "hoster", terms: ["publish2.me"], glyph: HOSTER },
  { id: "uploadrar.com", name: "Uploadrar", group: "hoster", glyph: HOSTER },
  { id: "workupload.com", name: "Workupload", group: "hoster", glyph: HOSTER },
] as const satisfies readonly CatalogEntry<DownloadGroup>[];

export type DownloadProviderId = (typeof ENTRIES)[number]["id"];

/** Every debrid service and file hoster, with its group's place in the picker. */
export const DOWNLOAD_PROVIDERS: Catalog<DownloadGroup, DownloadProviderId> = {
  groups: ["debrid", "hoster"],
  entries: ENTRIES,
};
