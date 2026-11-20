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

const DEBRID = "IconDownload";

const ENTRIES = [
  { id: "alldebrid", name: "AllDebrid", group: "debrid", mark: M.alldebrid },
  { id: "bestdebrid", name: "BestDebrid", group: "debrid", glyph: DEBRID },
  { id: "cocoleech", name: "CocoLeech", group: "debrid", mark: M.cocoleech },
  { id: "cooldebrid", name: "CoolDebrid", group: "debrid", mark: M.cooldebrid },
  { id: "debridlink", name: "Debrid-Link", group: "debrid", mark: M.debridlink },
  { id: "deepbrid", name: "Deepbrid", group: "debrid", mark: M.deepbrid },
  { id: "fakirdebrid", name: "FakirDebrid", group: "debrid", mark: M.fakirdebrid },
  { id: "megadebrid", name: "Mega-Debrid", group: "debrid", mark: M.megadebrid },
  { id: "mydebrid", name: "MyDebrid", group: "debrid", mark: M.mydebrid },
  { id: "offcloud", name: "Offcloud", group: "debrid", mark: M.offcloud },
  { id: "premiumize", name: "premiumize.me", group: "debrid", mark: M.premiumize },
  { id: "realdebrid", name: "Real-Debrid", group: "debrid", mark: M.realdebrid },
  { id: "rpnet", name: "RPNet", group: "debrid", mark: M.rpnet },
  { id: "torbox", name: "TorBox", group: "debrid", mark: M.torbox },
  { id: "zevera", name: "Zevera", group: "debrid", mark: M.zevera },

  { id: "1fichier.com", name: "1Fichier", group: "hoster", terms: ["desfichiers.com", "megadl.fr"], mark: M.fichier },
  { id: "ddownload.com", name: "ddownload", group: "hoster", terms: ["ddl.to"], mark: M.ddownload },
  { id: "fileboom.me", name: "FileBoom", group: "hoster", terms: ["fboom.me"], mark: M.fileboom },
  { id: "filer.net", name: "filer.net", group: "hoster", mark: M.filer },
  { id: "gofile.io", name: "Gofile", group: "hoster", mark: M.gofile },
  { id: "katfile.com", name: "KatFile", group: "hoster", mark: M.katfile },
  { id: "keep2share.cc", name: "Keep2Share", group: "hoster", terms: ["k2s.cc"], mark: M.keep2share },
  { id: "krakenfiles.com", name: "KrakenFiles", group: "hoster", mark: M.krakenfiles },
  { id: "mediafire.com", name: "MediaFire", group: "hoster", mark: M.mediafire },
  { id: "mega.nz", name: "MEGA", group: "hoster", terms: ["mega.co.nz"], mark: M.mega },
  { id: "nitroflare.com", name: "NitroFlare", group: "hoster", terms: ["nitro.download"], mark: M.nitroflare },
  { id: "pixeldrain.com", name: "Pixeldrain", group: "hoster", mark: M.pixeldrain },
  { id: "rapidgator.net", name: "Rapidgator", group: "hoster", terms: ["rg.to"], mark: M.rapidgator },
  { id: "send.now", name: "Send.now", group: "hoster", mark: M.sendnow },
  { id: "tezfiles.com", name: "TezFiles", group: "hoster", terms: ["publish2.me"], mark: M.tezfiles },
  { id: "uploadrar.com", name: "Uploadrar", group: "hoster", mark: M.uploadrar },
  { id: "workupload.com", name: "Workupload", group: "hoster", mark: M.workupload },
] as const satisfies readonly CatalogEntry<DownloadGroup>[];

export type DownloadProviderId = (typeof ENTRIES)[number]["id"];

/** Every debrid service and file hoster, with its group's place in the picker. */
export const DOWNLOAD_PROVIDERS: Catalog<DownloadGroup, DownloadProviderId> = {
  groups: ["debrid", "hoster"],
  entries: ENTRIES,
};
