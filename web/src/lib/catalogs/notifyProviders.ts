// The notification catalog: the services an app sends its messages to, each
// with its mark, and email and webhooks, which belong to nobody. An app hands
// catalogPicker the ids it can send to and keeps what it needs to deliver (an
// address, a token, a priority) beside them by id.
//
// A service somebody looks for by name stands under services. Email and the
// webhook are standards, and the webhook, which takes anything, is the tile an
// app marks last.
//
// A service an app reaches only through Shoutrrr or Apprise is a search word on
// that tile rather than a tile of its own: the id is what the app stores, and
// what it stores is the Shoutrrr or Apprise address.

import type { Catalog, CatalogEntry } from "./catalog";
import { SERVICE_MARKS as M } from "./serviceMarks";

export type NotifyGroup = "service" | "standard";

const SHOUTRRR = [
  "Bark",
  "Google Chat",
  "Home Assistant",
  "IFTTT",
  "Join",
  "Lark",
  "Mattermost",
  "MQTT",
  "Opsgenie",
  "PagerDuty",
  "Pushbullet",
  "Pushover",
  "Rocket.Chat",
  "Signal",
  "Teams",
  "Telegram",
  "Twilio",
  "WeCom",
  "XMPP",
  "Zulip",
];

const ENTRIES = [
  { id: "apprise", name: "Apprise", group: "service", mark: M.apprise },
  { id: "discord", name: "Discord", group: "service", mark: M.discord },
  { id: "gotify", name: "Gotify", group: "service", mark: M.gotify },
  { id: "healthchecks", name: "Healthchecks.io", group: "service", mark: M.healthchecks },
  { id: "matrix", name: "Matrix", group: "service", terms: ["Element"], mark: M.matrix },
  { id: "ntfy", name: "ntfy", group: "service", mark: M.ntfy },
  { id: "shoutrrr", name: "Shoutrrr", group: "service", terms: SHOUTRRR, mark: M.shoutrrr },
  { id: "slack", name: "Slack", group: "service", mark: M.slack },
  { id: "unraid", name: "Unraid", group: "service", mark: M.unraid },

  { id: "smtp", name: "Email", kind: "SMTP", group: "standard", glyph: "IconMail", word: true },
  { id: "webhook", name: "Webhook", group: "standard", terms: ["HTTP"], glyph: "IconLink", word: true },
] as const satisfies readonly CatalogEntry<NotifyGroup>[];

export type NotifyProviderId = (typeof ENTRIES)[number]["id"];

/** Every notification provider, with its group's place in the picker. */
export const NOTIFY_PROVIDERS: Catalog<NotifyGroup, NotifyProviderId> = {
  groups: ["service", "standard"],
  entries: ENTRIES,
};
