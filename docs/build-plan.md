Everything checks out. I have what I need: the six packages are unwired (zero references), `GET /api/settings` is `writeJSON(w, a.Settings.Get())` — fully unredacted, the store migration list is append-only strings, and `rules.PriorityMin/Max` is `-2..2`.

---

# KnightLoader — build plan for the 344 open common features

## 0. Verification first, because two numbers were wrong

I re-parsed the census rather than trusting the summary. Weight×status matrix:

| | have | partial | missing | total |
|---|---|---|---|---|
| core | 40 | 71 | 57 | 168 |
| **common** | **26** | **85** | **257** | **368** |
| niche | 13 | 45 | 332 | 390 |

My parser reads 926 of 928 rows (two rows contain literal `|` in a cell) and both dropped rows are common-not-done — so **344 is correct**. The nine readers' arithmetic also closes exactly:

- 32 alreadyCovered + 35 dropped + 277 to build = **344** ✓

Three source facts that change the plan, checked in the tree, not assumed:

1. **The six packages are not wired at all.** `grep` for `rules.|dedupe.|collide.|schedule.|proxycfg.|reconnect.` across `internal/app`, `internal/api`, `internal/settings`, `cmd/` returns **nothing**. The wiring agent has landed zero lines so far. Every wave below is gated on that landing, and the 32 alreadyCovered rows are closed *by that wiring*, not by me.
2. **`GET /api/settings` is unredacted** — `internal/api/api.go:163` is literally `writeJSON(w, a.Settings.Get())`. Area 5 proposed putting `proxycfg.Entry` (with `Password`) and `reconnect.Config` (with the router password) *into* `settings.Settings`. That would ship every proxy and router password to every connected browser on page load. **Area 4 is right and area 5 is wrong**; this is a resolved conflict, not a preference.
3. **`internal/store` migrations are an append-only `[]string` of raw `ALTER TABLE`s** and `rules.PriorityMin/Max = -2..2`. Both drive Wave 1 decisions below.

---

## 1. Dedupe: 277 build rows → 24 families → 46 packages

The same mechanism appears under up to five menu names. Merged, with row counts that sum back to 277:

| # | Family | Rows | Merged from areas | The duplicate names |
|---|---|---|---|---|
| T | Task table & list control | 36 | 2,3,6,9 | "column model" ×2, "collapse/expand" ×3, "search categories" ×3, "clean up" ×3, "skipped state" ×3, "context menu" |
| C | Reconnect | 32 | 3,4,5,7,8 | "reconnect page" ×2, "auto-reconnect" ×4, "script import" ×2 |
| R | Rules (Packagizer + Link Filter) | 23 | 5,6,8 | "rule editor", "link filter list", "views", "capture groups", "built-in bottom rules" |
| Q | Queue control | 20 | 2,3,4,9 | "move up/down" ×2, "stop mark" ×2, "quick settings" ×2, "speed meter" ×2 |
| S | Status strip & notifications | 18 | 1,3,5,9 | "status strip" ×2, "notifications" ×2, "quiet/silent mode" ×2, "tab tooltip" ×2 |
| A | Accounts & premium | 15 | 3,5,7 | "account strip", "master switch" ×2, "cookie/basic auth" ×2 |
| I | Intake (paste, drop, containers, watch) | 15 | 1,3,6,9 | "container upload" ×3, "clipboard/drop" ×2 |
| K | Captcha | 14 | 3,5,7 | "captcha prompt" ×3, solvers, JS widget, skip |
| N | Network & connections | 13 | 4,5 | "connection manager table" ×2 |
| X | Extraction & archives | 13 | 1,5 | "extract destination/collision" ×2 |
| V | Collector flow & facets | 13 | 6,8,9 | "facet sidebar" ×3, confirm, auto-confirm |
| G | Settings shell & modules | 10 | 1,5,9 | "sub-pages/feature registry", "per-subsystem switches", "modules page" |
| B | Boot, resume, retention | 7 | 2,5 | "autostart on start" ×2, "remove finished" ×2 |
| F | Failure classification | 6 | 4,7 | "classify failure" / "a reason on every failure" |
| P | Properties panel | 6 | 2,6 | "properties panel" ×2 |
| E | Scripting & events | 6 | 1,8 | — |
| U | Onboarding, shortcuts, locales | 6 | 3,9 | "keyboard shortcuts" ×2 |
| M | Machine API, browser, PWA | 5 | 8,9 | "browser extension" ×2 |
| D | Diagnostics, backup, restart | 5 | 3,9 | "create a log" ×2 |
| W | End-of-queue action | 5 | 1,8 | "shutdown" / "on idle" |
| Rv | Resolver options & variants | 4 | 5,6 | — |
| Sc | Scheduler action set | 2 | 1 | — |
| Fs | Folder chooser | 2 | 3,9 | "open download folder" ×2 |
| Fnd | UI state store | 1 | 9 | — |
| | **Total** | **277** | | |

Deduping removed roughly 40 % of the apparent work: 9 readers proposed 152 packages, this is 46.

---

## 2. The ordering decision that makes twelve waves possible

Every one of the 46 packages wants to write `internal/app/app.go` (1844 lines), `internal/api/api.go`, `internal/settings/settings.go` and `web/src/pages/Settings.tsx`. At one writer per hot file per wave, that is **46 waves**, not 12.

So Wave 1 spends one agent on de-hotifying, and the plan goes wide afterwards:

- **`app.go` → `app.go` + `app_tasks.go` + `app_dispatch.go` + `app_links.go` + `app_extract.go` + `app_queue.go` + `app_accounts.go`.** Same package, no behaviour change, no import churn — but seven independent write lanes instead of one.
- **`api.go` → router + `routes_*.go` with a registration table**, so a package adds routes from its own file. The `/api/help` self-describing index (family M) is generated from that same table, so it cannot drift.
- **`settings.go` → `settings.go` (Store/Load/Sanitize/Validate) + per-domain sub-structs in their own files**, each with its own sanitize hook.
- **Secret-bearing config gets its own stores: `connections.json`, `reconnect.json`, and later solver keys via `internal/accounts`** — never `settings.Settings`, per finding (2). Wave 1 builds this pattern once so waves 2–7 inherit it.
- **One store migration widens `core.Task` for the whole twelve-wave plan**, in Wave 1: `FinishedAt, Enabled, Skipped, SkipReason, Hold, Forced, Comment, DownloadPassword, ExpectedHash, AutoExtract, Chunks, Connection, Host, Source, MirrorOf, Resumable, Filename, Variant, ManualPackage, Reason`. Migrations are append-only and ordered; letting fifteen packages each append one is a serialization point and a merge-conflict farm. Doing it once costs an hour and removes `store.go` from the critical path forever.

Hot-file lanes after Wave 1 — **one writer per lane per wave**:

`L1` app_*.go (7 sub-lanes) · `L2` routes_*.go · `L3` settings_*.go · `L4` pages/settings/* · `L5` TaskList.tsx · `L6` lib/api.ts · `L7` core/task.go + store.go (frozen after W1) · `L8` locales/* (one writer per wave, per your i18n convention)

---

## 3. The twelve waves

### Wave 1 — Seams, the model, and the table you actually look at (36 rows, 5 agents)
*Payoff: the download list stops being a five-slot grid. Columns, collapse, search, right-click, bulk delete.*

| Agent | Package | Owns (exclusive write) | Rows |
|---|---|---|---|
| 1A | Seam split + one migration + bulk routes + UI state store | `app.go`→7 files, `api.go`→routes, `settings.go`→sub-structs, `core/task.go`, `store.go` | 6 |
| 1B | Column registry, header, resize/reorder/lock, collapse tree, sort + run-order tint, value columns, source URL column | `TaskList.tsx`, new `columns.ts`, `ColumnMenu.tsx` | 16 |
| 1C | List toolbar, search categories, 8 quickfilters, cleanup classes, context menu shell, check-online | `Downloads.tsx`, `Collector.tsx`, new `ListToolbar/SearchField/ContextMenu` | 14 |
| 1D | Client contracts: widened Task type, uistate client, bulk endpoints, `fmtDate` | `lib/api.ts`, `lib/uistate.ts`, `lib/format.ts` | — |
| 1E | i18n (~90 keys × 26) | `lib/locales/*` | — |

Contention: 1A owns all Go hot files; 1B/1C/1D are disjoint frontend files. 1B and 1C both render rows — 1B owns the row, 1C owns the page around it.

### Wave 2 — Settings shell + the rule engine gets a face (41 rows, 6 agents)
| Agent | Package | Owns | Rows |
|---|---|---|---|
| 2A | Settings sub-pages, feature registry, modules page, advanced key table | `pages/settings/*` scaffold, `settings_features.go`, `Sidebar.tsx` | 10 |
| 2B | Rule editor (both flavours), views, test box, capture groups, file-type categories, variables menu | `settings/Rules.tsx`, `internal/rules/*`, `routes_rules.go` | 8 |
| 2C | Link filter wiring, filtered holding area, package buckets, catch-all, source tracking | `app_links.go`, `internal/crawler` | 9 |
| 2D | Rule effects on the download: filename, auto-extract, auto-confirm/start, built-in bottom rules | `app_tasks.go`, `app_extract.go` | 6 |
| 2E | Connection manager table + proxy string import/detect | `settings/Connections.tsx`, `internal/proxycfg`, **`connections.json`**, `routes_connections.go` | 8 |
| 2F | i18n | `locales/*` | — |

### Wave 3 — Reconnect, in full (40 rows, 6 agents)
| Agent | Package | Owns | Rows |
|---|---|---|---|
| 3A | Reconnect page, API, run-it-now, IP-check list + range rejection, run policy | `settings/Reconnect.tsx`, **`reconnect.json`**, `routes_reconnect.go` | 13 |
| 3B | Methods: UPnP/SSDP, script+interpreter, LiveHeader `[[[HSRC]]]` import, router address discovery | `internal/reconnect/{upnp,script,router}.go` | 12 |
| 3C | Reconnect in the shell, queue handshake, the single auto-trigger | `app_dispatch.go`, `QueueBar.tsx` | 7 |
| 3D | Failure classification (`Reason` on every failure) | `app_errors.go`, `internal/engine` | 6 |
| 3E | `internal/httpx` one client policy + pause speed | `internal/httpx`, `internal/throttle` | 2 |
| 3F | i18n | `locales/*` | — |

Four separate readers each proposed owning the auto-reconnect trigger. **3C owns it; 3A/3B/3D consume it.**

### Wave 4 — Queue control and the overview (29 rows, 6 agents)
4A one-step moves + 7 priorities + stop mark + force (`app_queue.go`, `routes_queue.go`) — 10 · 4B per-task/global chunks precedence (`app_dispatch.go`, `settings_network.go`) — 3 · 4C overview strip, speed meter, quick-settings gear, `/api/controls` (`Counters/SpeedGraph/QuickSettings/QueueBar`) — 6 · 4D per-download proxy routing, connection column, ban list, direct gateway (`internal/netproxy`, `proxycfg/picker`) — 4 · 4E properties panel (`TaskList` panel, `app_tasks.go`) — 6 · 4F i18n

### Wave 5 — Extraction as a first-class thing (22 rows, 5 agents)
5A archive settings page: destination, subfolder, collision via `internal/collide`, delete/trash (`settings/Archives.tsx`, `extract.Options`) — 8 · 5B extraction jobs + progress, deep extraction, split-file joining, archives as list objects (`internal/extract` worker, `app_extract.go`) — 5 · 5C boot/resume/retention + download history + mirror siblings (`app.go` New/Close, `store` history table) — 7 · 5D multiple watch folders + full crawljob intent (`internal/watch`, `app_watch.go`) — 2 · 5E i18n

### Wave 6 — Accounts and premium (15 rows, 5 agents)
6A one account entity, many per service + both master switches (`internal/accounts`, `Accounts.tsx`) — 4 · 6B account info: traffic, expiry, refresh ticker, account strip (debrid/torbox clients) — 5 · 6C per-host limits from the multihoster + host priority order (`app_dispatch.go`, resolver registry) — 4 · 6D cookie / basic-auth credential store (`internal/hostcred`) — 2 · 6E i18n

### Wave 7 — Captcha (14 rows, 5 agents)
7A challenge store, waiting state, hub event, prompt modal, timeout action (`internal/captcha`) — 6 · 7B captcha settings + solver order + two solver clients — 6 · 7C JS-widget route + narrow per-route CSP — 1 · 7D skip-and-blacklist — 1 · 7E i18n

### Wave 8 — Intake and the collector flow (26 rows, 6 agents)
8A add-links form: destination, options, history, hoster link password — 5 · 8B clipboard, paste, whole-window drop, container upload (`.rsdf` local, `.dlc` refused with the real reason) — 7 · 8C confirm scope/start-mode/placement + delayed auto-confirm countdown — 5 · 8D collector facet sidebar + stats strip — 8 · 8E deep page analysis as an async crawl job — 1 · 8F i18n

### Wave 9 — Status, notifications, ambient (18 rows, 5 agents)
9A global status strip + activity event stream — 3 · 9B notification centre: typed events, per-event settings, quiet mode, sound — 11 · 9C tab title + favicon ring — 2 · 9D tooltip system + rich row tooltips — 2 · 9E i18n

### Wave 10 — Scheduler, end-of-queue, diagnostics (14 rows, 6 agents)
10A scheduler action set + repeat shapes + next-execution column — 2 · 10B end-of-queue / idle action with cancel countdown — 5 · 10C log ring, diagnostics bundle, help page — 3 · 10D backup/restore + supervised restart — 2 · 10E server folder chooser + path history — 2 · 10F i18n

### Wave 11 — Scripting, machine API, resolver options (16 rows, 7 agents)
11A goja script host + trigger registry + sandbox API — 3 · 11B script editor page + user action buttons — 2 · 11C API tokens, `PATCH /api/settings`, `/api/help`, event-stream subscriptions — 3 · 11D bookmarklet + MV3 extension + PWA share target — 3 · 11E resolver options page + yt-dlp variants — 4 · 11F drag-and-drop reorder — 1 · 11G i18n

### Wave 12 — Onboarding and locale breadth (6 rows, 4 agents)
12A command registry + palette + rebindable shortcuts — 2 · 12B first-touch help dialogs + first-run wizard — 3 · 12C 13 new locales (26 → 39), one writer, tsc + key-parity gate — 1 · 12D final parity sweep across all ~330 keys added in waves 1–11

---

## 4. Conflicts between readers I have already resolved

You should not have to arbitrate these mid-build:

1. **Secrets in settings** — area 5 wanted proxy and router passwords inside `settings.Settings`. Refused; `GET /api/settings` is unredacted (verified). Separate stores, redact-on-GET, merge-on-PUT.
2. **Skipped: flag or status?** — area 3 wanted `core.StatusSkipped`, areas 2 and 9 wanted a flag. **Flag + typed reason wins.** A new Status value breaks every exhaustive `Record<TaskStatus,…>`, the store round-trip, and forward-rollback. Area 3's own risk note argues against area 3's design. Same ruling for captcha-waiting and held.
3. **Held vs Paused** — area 6 flagged it, area 8 named it `Hold`. `Hold` is a flag; reusing `StatusPaused` would make `resumeAll` start links the user deliberately parked.
4. **Priority ±2 or ±3** — `rules.PriorityMin/Max = -2..2` is built and tested; area 2 wants JD's seven levels. **Widen `rules` to ±3** (one clamp + test) rather than forking the range. Existing stored ±2 rows keep the value and quietly become "higher/lower" instead of "highest/lowest" — acceptable, but it is a real semantic change and belongs in the changelog.
5. **`downloadPassword` ≠ archive password** — three areas nearly conflated them. Two distinct fields, two distinct labels in all 26 locales.
6. **Auto-reconnect trigger** — proposed by four areas. One owner (3C).
7. **`Enabled` defaults to `true`** in the Wave 1 migration. A zero-value default disables every stored task on first boot. This is the single most dangerous line in the plan.

---

## 5. The total, honestly

**Of the 344 open common features:**

| Outcome | Count | |
|---|---|---|
| Closed by wiring the six existing packages (Wave 0, in flight) | **32** | rules ×2 flavours, dedupe, collide, schedule, proxycfg, reconnect |
| Built across waves 1–12 | **277** | 46 packages |
| **Total closed** | **309** | 90 % |
| Dropped with reason | **35** | 10 % |
| **Left open** | **0** | |

**The 35 drops**, grouped — every one is a platform impossibility, not a difficulty dodge:

- ~~**No desktop (17)**~~ — **overturned on 2026-08-09: the desktop bundles ship.** This bucket was always factually wrong (12.2) and now it is moot: Windows, macOS and Linux are published builds, so tray icon, tray menu, tray manager, the two minimize/close pairs, start minimized and the two dialog-stacking rows are ordinary Wails work and move into **package 21**. What stays dropped from the seventeen is the Swing-specific half, and for a reason that has nothing to do with the platform: the **five Swing menu editors** and the **Tools menu** edit a menu bar this app does not have, and **AntiStandBy plus its mode** is a keep-awake daemon whose one honest use (do not sleep mid-download) belongs to the operating system's own power settings, not to a download manager holding a wake lock the user cannot see. Nine rows recovered, eight still dropped.
- **Third-party service we cannot use (9):** my.jdownloader.org relay (e-mail/password/device name, `-myjd`, direct-connect mode, headless-mandatory), JD's proprietary router-script database (Reconnect Wizard, Search Router Model), affiliate "Buy Premium", `.dlc` decryption key service.
- **Owned by the deployment, not the app (6):** find updates, update button, install-on-exit, silent install, `-norestart`, install4j installer.
- **Duplicated by an existing surface (3):** LinkDoubleClickAction, PAC proxy type (no JS engine in Go), Native Auth (Windows NTLM).

**What is still open after this plan** — and this is the honest part:

- **128 core rows not done** (71 partial + 57 missing). The plan closes roughly 20 of them incidentally (package tree, enabled flag, quick-settings popup, stop-mark, captcha dialog, status label, process indicators, connection manager page, column popup, row drag, Ctrl+V) but core was explicitly out of scope and **~108 core rows remain**. Several are load-bearing; I would look at that list before starting Wave 4.
- **377 niche rows**, untouched by design.
- The census's own totals were 79/202/647; after this plan the common column reads 335 have / 0 partial / 9 missing-by-choice… but the file itself has 928 rows and **the Verdict column needs updating as each wave lands**, or the census stops being the source of truth by Wave 3.

**Size, without flattery:** 46 packages, of which 6 are XL (`internal/script` host, captcha core, per-download proxy routing, link-filter holding area, locale breadth, settings shell) and 19 are L. Wave 1 alone is a full refactor of the four largest files in the repo plus a 20-column migration. At 5–6 agents wide this is **twelve waves and roughly a quarter of sustained work** — not a sprint, and not something to compress by skipping Wave 1's seam split, which is the only reason waves 2–12 can run six agents wide instead of one.

**Start condition:** Wave 1 cannot begin until the wiring agent lands, because 1A rewrites the exact three files that agent is editing. Confirm that merge first; everything else follows.

---

I verified the arithmetic, the cross-area conflicts, and every load-bearing source claim against the working tree. Here is the amendment.

---

# Amendment to `docs/build-plan.md` — the 128 open core rows

*Appends as sections 6–12. Sections 0–5 stand except where section 12 corrects them.*

## 6. Verification: the core matrix, counted around the pipe rows

The original parser read 926 of 928. A pipe-aware parser (weight/status located by value, not by column index) reads **928 of 928**. The three rows that break naive splitting are lines **297**, **357** and **609** — and all three are `common`, not core:

| Line | Row | Weight/status |
|---|---|---|
| 297 | Unskip all skipped Links (`collected\|queued\|running\|…` in Blocker) | common / missing |
| 357 | Import (plain text) (`(socks4\|socks5\|http)://…`) | common / missing |
| 609 | Alle / Ausgewählte x (`… \| … \| …`) | common / partial |

The corrected matrix:

| | have | partial | missing | total | not done |
|---|---|---|---|---|---|
| **core** | 40 | 71 | 57 | **168** | **128** |
| common | 26 | 86 | 258 | 370 | 344 |
| niche | 13 | 45 | 332 | 390 | 377 |
| | 79 | 202 | 647 | **928** | 849 |

**128 is right.** Section 0's core row was already correct; its *common* row (26/85/257 = 368) was the one understated by the two lost rows — true common total is **370**, not 368. The prose correction to 344 was right, the table was not. 168 + 370 + 390 = 928 ✓. Open core+common = 128 + 344 = **472** ✓.

Per-area core, independently recomputed — all nine readers' counts close exactly:

| Area | core | have | partial | missing | **not done** |
|---|---|---|---|---|---|
| 1 Extensions | 12 | 3 | 5 | 4 | 9 |
| 2 Downloads tab | 21 | 8 | 8 | 5 | 13 |
| 3 Toolbar/menus | 20 | 9 | 7 | 4 | 11 |
| 4 Reconnect/network | 17 | 2 | 3 | 12 | 15 |
| 5 Settings tree | 19 | 7 | 8 | 4 | 12 |
| 6 LinkGrabber | 23 | 7 | 12 | 4 | 16 |
| 7 Accounts/premium | 20 | 1 | 11 | 8 | 19 |
| 8 Automation/rules | 17 | 0 | 7 | 10 | 17 |
| 9 Interface/QoL | 19 | 3 | 10 | 6 | 16 |
| | **168** | **40** | **71** | **57** | **128** |

The readers' dispositions also close: 5 alreadyCovered + 87 foldsInto + 32 newPackage + 4 dropped = 128 ✓ (re-bucketed below to 5 / 85 / 35 / 3 after one drop is overturned and one fold is promoted).

---

## 7. Dedupe: 128 rows → 58 mechanisms

Core rows duplicate across areas harder than common ones, because a core feature is reachable from more menus. The families that span more than one area — the rest are single-mechanism rows:

| Family | Rows | Areas | The duplicate names |
|---|---|---|---|
| Chunks per download | 4 | 2,4,5,7 | "Max. Verbindungen pro Download" ×2, "Max. Chunks per Download", "Max. Chunks pro Datei" |
| Clipboard / paste / drop, in page | 7 | 5,6,9 | "clipboard settings", "paste box", "Add & Analyse from Clipboard" ×2, "Ctrl+V into a table", "Drag&Drop Action", "Clipboard Observer" |
| Accounts: entity, table, add, check, enable | 8 | 5,7 | "account table columns", "Account Manager toolbar", "Account-Manager", "Account-Tabelle", "Add" ×2, "Account-Prüfung", "Enabled" |
| Pause & speed-limit switch | 4 | 2,3,4 | "Pause" ×2, "Speedlimiter enabled", "Speed Limiter" |
| Quick settings & overview strip | 4 | 3,4,9 | "Quick Settings popup", "inline editors" ×2, "Download Overview" |
| Settings shell: sub-pages, modules, key table | 5 | 1,5 | "extension registry", "Extension Modules", "Settings sidebar", "Filter Settings", "Key/Description/Value/Type" |
| Connection manager table | 4 | 4 | "Add new Proxy", "Connection Manager", "Proxytype", "Use (checkbox)" |
| Reconnect page, method, switches, IP list | 4 | 4 | "Reconnect page", "Reconnect Method", "Auto Reconnect Enabled", "BalancedWebIPCheck" |
| List toolbar, search, delete, clear | 4 | 2,6,9 | "Delete", "Untere Leiste", "Linksammlerliste leeren", "Filter & Searchbar" |
| Task columns & header menu | 3 | 2,6,9 | "Name", "Spalten", "column control popup" |
| Package tree, collapse, row drag | 3 | 2,6,9 | "Paket (Baumansicht)", "Paket-/Link-Baum", "row drag" |
| Enabled flag on links | 3 | 2,6 | "Enable/Disable", "Enabled column", "Aktiviert/Deaktiviert" |
| Desktop presence (tray + window policy) | 3 | 1,3,9 | "Tray Icon / Light Tray", "Click on Close/Minimize", "On close action" |
| Account info: tier, traffic, premium bar | 3 | 3,7 | "Premium/Service bar", "AccountType", "Traffic left" |
| Confirm policy (dupe / offline / auto-confirm) | 3 | 5,6 | "linkgrabberautoconfirm…", "Was soll mit den Links…", "Was soll mit den Offline-Links…" |
| Rule effects on the download | 3 | 1,8 | "Extract archives after download", "rules affect new links only", "Download Directory" |
| Archive settings page + delete policy | 2 | 1,5 | "Delete Archive Files after extraction", "Archive Extractor page" |
| Stop all | 2 | 2,3 | "Stop Downloads", "Stop all running downloads" |
| Crawl jobs / intake bubble | 2 | 6,9 | "Crawling for Downloads", "Parse Clipboard / LinkCrawler bubble" |

Merging removed nine duplicate proposals and collapsed two separately-proposed packages into one each (crawl jobs; stop-all).

---

## 8. Per existing wave: what it must additionally do

Owning agent named from section 3's tables. **One writer per hot-file lane per wave is preserved throughout** — every place a core addition would have broken it is called out and moved.

### Wave 0 — the wiring, before anything else
- **Blocking fix, not a wave item:** call `Settings.Redacted()`. See section 12.1.
- **Dedupe gains a mode.** `AddLinks` drops a known URL silently, so a duplicate can never be shown marked in the collector, and Wave 8C's `onDupes` policy would be dead config. Add `dupeManager ∈ {refuse-at-add, stage-and-mark}`: refuse for watch / CnL / bridge, stage-and-mark for the LinkGrabber, carrying `dedupe.Match` (verdict, matching signal, sibling id) as a task field. **Raise this now, while the wiring is uncommitted.** (1 row)

### Wave 1 — 5 agents → **7**
**Split 1A.** It is now a seam split of a 2389-line `app.go` (not 1844), plus `api.go`, plus `settings.go`, plus a 24-column migration, plus a signature change on the busiest function in the app. `core/task.go` + `store.go` is already declared as its own lane (`L7`), so the split is along an existing seam:

- **1A — Go seams.** `app.go`→7 files, `api.go`→router + `routes_*.go` **with the registration table**, `settings.go`→sub-structs. Additionally: the table entry carries namespace, method, summary and auth requirement, and **a test fails when an exported `App` method no subsystem route reaches** — the `/api/help` generator is built from it *now*. Retrofitting this in Wave 11 means auditing ~200 hand-registered routes. Nothing may call `mux.HandleFunc` directly after this wave.
- **1A2 — model and migration (lane L7).** The one migration, extended past section 2's list with: `Origin`, `ChangedAt` (JD's "Geändert am"), `ArchivePart` (`extract.SetKey` already computes the volume number and throws it away), `Resumable`, and the `AvailUncheckable` availability constant. **And the four fields the wiring already put on `core.Task` with no column at all** — `Comment`, `Chunks`, `AutoExtract`, `MatchedRules` (see 12.6). `AutoExtract` already landed as `*bool`, which is the tri-state area 1 asked for; persist it as TEXT defaulting to `''` so unset stays distinguishable from off. Also here: `AddLinks(urls, pkg)` becomes an options struct — four call sites, including the `cnl.Adder` interface and the bridge, and doing it in Wave 2 means editing files two other agents own.
- **1B — additionally:** open the column menu on header **right-click**, and refuse to hide the last column. Key collapse state by **stable package identity, not name** — `SetPackage` rewrites the name, so a rename or a Packagizer re-package silently re-expands a 400-child package. Collapsed packages keep contributing to header totals. Per-list column profiles (the LinkGrabber set is not the Downloads set) in 1D's uistate. The Enabled checkbox as its **own column with a different affordance** from the selection checkbox. Online/total ratio on the package header, with "3/5 online" never rendered as "3 online, 2 offline". **1B also absorbs drag-and-drop** — see the re-rank in section 10.
- **1C — additionally:** `Del` = remove from list, `Shift+Del` = remove with files, bound here rather than deferred to 12A for eleven waves; the destructive variant confirms with file count and summed bytes. Both delete variants exist server-side already (`DELETE /api/tasks/{id}?files=1`) and the UI simply never passes the flag. Move the bar **below** the list and make it **unconditional** — it is currently hidden when the list is empty, which is the one moment a new user needs the add button. Clear must be one bulk `DELETE`, not one request per row. Mount the same toolbar on the Collector, which has no search field at all.
- **1D — additionally:** declare the **asynchronous `POST /api/links` contract** now, or every intake surface built in waves 2–8 is coded against the synchronous response and rewritten. Fix the **command-record type** `{id, labelKey, icon, group, surfaces, enabled(), visible(), defaultShortcut, run()}` and the `useCommands(surface, ctx)` hook. Every wave registers its commands as it builds them; if waves 1–11 keep writing inline `onClick`, the Wave 12 customiser cannot be built at all.
- **1E — i18n: 38 locales, not 26.** See 12.9.
- **1F — NEW: derived task state.** See section 9.

### Wave 2 — 6 agents → **9. This wave should be split.**
- **2A — split.** **2A** keeps sub-page routing (real routes `/settings/general` etc., last page remembered in uistate, a remembered-but-gone page falling back to General rather than rendering an empty frame), `Sidebar.tsx`, and the **modules page**. The module registry must be a **fixed compiled-in table**, not a settings-key list, with a verdict per JD extension slot: shipped-and-toggleable, desktop-build-only, or not-built-with-a-reason — Go has no portable dynamic plugin loading, so the page says the set is fixed at build time rather than leaving an empty "Not yet installed" section that reads as broken. The Enabled checkbox is a **real kill switch**: off stops the watcher goroutine polling, stops the scheduler firing, closes port 9666. A checkbox that leaves the goroutine running is the failure mode, and it stays invisible. The table is shared data read by the modules page, the settings sidebar and `/api/help`; three copies drift by Wave 4. This also closes area 5's XL common row "Archive Extractor, Anti-Standby, Chat, …" — one job, not two.
- **2I — NEW agent: the advanced key table.** `desc:"…"` struct tags on every settings field plus a reflect-based `Describe()` returning `{key path, description, kind, enum choices, min/max, default, current, isModified}`, `PATCH`-by-key through the same `sanitize()`/`Validate()` path so a raw edit cannot bypass a clamp, per-kind editors, a debounced search that ANDs with "show only modified", and a "no key matches" row. **Ship the Go test that fails when a settings field has no `desc` tag in the same wave** — waves 3–12 keep adding fields, and an untagged field renders blank forever. Must not reflect over `reconnect.Config` or `proxycfg.Entry`. Files: `pages/settings/Advanced.tsx`, `settings_describe.go`, `routes_settings.go` — disjoint from 2A.
- **2B — split.** **2B** keeps the frontend (`settings/Rules.tsx`): a **two-section** dialog, not JD's three (see 12.11), with `Compile()`'s `[]Problem` rendered inline against the offending condition, a variables picker in every template field with a live preview, and the four new condition types. **2H — NEW agent** takes the backend: `internal/rules` store + `routes_rules.go` (section 9, package 3). Disjoint files, same lane split cleanly.
- **2B additionally:** `<jd:orgfilename:N>` cannot be done in `expand.go` alone — `cond.match` calls `re.MatchString` and discards submatches, so `Matcher.Apply` must keep `FindStringSubmatch` per matching regex condition and pass a per-field submatch table into `expand()`. Same plumbing closes `<jd:hoster:N>` and `<jd:orgpackagename:N>`. **`<jd:source:N>` is a name collision** — see 12.10.
- **2C — additionally:** the filtered holding area must be excluded from the queue, from the counters **and from the `known` map `AddLinks` builds**, or re-pasting a filtered link is a silent no-op instead of a deliberate second chance. Catch-all package gains a configurable name and a `VariousPackageLimit`, running after the Packagizer and **skipping any package carrying `ManualPackage`**. 2C also takes the crawl-job package (section 9).
- **2D — additionally:** pin rule application to exactly one place (`stage()`/`AddLinks`) and **persist what the rules decided** onto the task. `dirFor()` recomputes the folder from settings on every call, so a rule-chosen folder not written to `Task.Dir` silently re-derives after a restart and the file moves. Precedence: hand-set `Task.Dir` > rule folder > settings, and record which of the three it was. Validate the *expanded* folder at stage time — `dirFor` falls through to the default when the expansion is not absolute, so a relative rule path lands everything in the default folder with no error, and `settings.Validate` is only ever applied to the settings folder. `Action.Filename` has nowhere to land: `engine.DownloadTo` takes no name. Widen it or rename on completion — and renaming on completion breaks resume across a restart, because the `.part` file is keyed on the engine's own name.
- **2E — additionally:** the add form must post exactly the fields `proxycfg.Validate` checks and surface its error verbatim — `Sanitize` **silently drops** any row `Validate` rejects, so a save handler that skips the explicit `Validate` makes the user's new proxy vanish with no message. `socks4`/`socks4a` must never be handed to `Transport.Proxy` or to gopeed's `RequestProxy`: both build an `http.ProxyURL` handler (verified — `RequestProxy.ToHandler()` returns exactly that), which is what `Entry.NeedsOwnDialer()` flags. The `Use` checkbox writes `Enabled`; 4D clears that row's bans on the false→true edge.
- **2G — NEW agent: the persistent shell bar.** Moved here from Wave 4 — see section 10.

### Wave 3 — 6 agents → **7**
- **3A — additionally:** `AutoEnabled` on `reconnect.Config`, stored in reconnect's own config, **never** in `settings.Settings`. Default on, but gate the reported state behind `Validate` so a fresh install with no method shows auto-reconnect as inactive. `CheckURL` widens from one string to an ordered `[]string`, shuffled per run, falling through on failure, keeping the 20s ceiling and 64 KB cap. **No silent built-in default** — the field deliberately has none, because a self-hosted downloader should not start reporting its address to a service the user never chose; offer a suggested list the first-run form applies with one click. Use `reconnect`'s own constants as the picker's option values so JD's spellings (`liveheader`/`curl`, `external`/`batch`) keep importing, and show `Validate`'s "unknown method *x*" message rather than swallowing it into "switched off".
- **3C — additionally:** the auto-reconnect toggle goes in the shell bar next to halt and speed, driven from the same persisted field 3A owns, and renders **disabled** rather than merely off when `Validate()` returns `ErrNotConfigured`. A task parked for a new IP **must not increment `Retries`** and must not be re-dispatched — today every error retries on a growing backoff regardless of cause, so an IP-blocked task burns its whole `MaxRetries` budget from the same address and then sits errored. Lean on the single-flight in `Reconnector.Do`: twenty blocked tasks raise one request and read one result.
- **3D — additionally:** the reason taxonomy needs `ip_blocked` and `hoster_limit` as distinct values, because that is the exact signal 3C fires on — a generic error must never trigger a reconnect, or every 404 reboots the router. An unmatched error stays `unknown` rather than being guessed. Send an **absolute `retryAt`**, never a remaining duration, plus a reason string beside it. **3D also captures `Resumable`** — `Accept-Ranges` (or a 206 probe) on the request the direct resolver already makes, carried as an optional `Result.Resumable` field so the blast radius across the five resolvers is one field, not a signature change. Unknown must be a **third value**: warning "you will lose 4.2 GB" about a transfer that resumes fine trains people to click through the dialog.
- **3E — split.** **3E** keeps `internal/httpx` plus the **reversible-limit state machine**: `pauseSpeed` (default 100 KiB/s) and a **server-side** `previousSpeedLimit` — held in the browser, a second client un-pausing or a reload during pause leaves the instance permanently capped at 10 KiB/s with no way to tell. One `App.SetPaused(bool)`; never call `Throttle.Set` directly, because `ApplySettings` also pushes the limit to the JD backend and both paths must agree. A `PUT /api/settings` during pause overwrites the *remembered* value, not the live one. `speedLimitEnabled` shares that same memory, so pause and the limit switch cannot remember two different previous values. A third queue mode (`running` / `paused-throttled` / `halted`), distinct from `SetHalted`.
- **3G — NEW agent: the bandwidth budget.** See section 9.

### Wave 4 — 6 agents → **7**
- **4A — additionally:** the hard stop (section 9, package 9) lands in 4A's lane, since 4A already owns `app_queue.go` and `routes_queue.go`. `App.Pause` mutates `a.active` and calls `dispatchLocked`, then calls the backend after releasing the lock — looping it over `a.active` while completion events mutate the same map is a race. Snapshot ids under the lock, set halted **first** so nothing refills freed slots, then pause outside it. 4A also takes the disabled-links **bulk route** and the counters decision (exclude disabled from remaining bytes and aggregate ETA, keep them in the file count).
- **4B — additionally:** one precedence formula, written down once, resolving four readers who each proposed a different order (12.8): **value** = first of (per-task, matching rule, global setting, 4); then **clamp** to `min(value, resolver cap, per-host cap from 6C, account-tier cap from 6B, 16)`. A resolver returning `Connections` is stating what the host tolerates, so it is a **ceiling, not an override** — that is what lets somebody set 1 chunk for a hoster that bans multi-connection and actually get 1. A per-task 0 means "use the global", not "no connections". Replace **both** hardcoded fallbacks (`app.go` `conns = 4`, and the flat 8 the debrid backend hands the engine). Correction to section 3: the census's "internal/engine exposes no connection count" is stale — `Engine.Download/DownloadTo` already take `conns` and pass it as `OptsExtra.Connections`; what is missing is a source for the number. **4B also owns the disabled-links dispatch skip** (it owns `app_dispatch.go`), keeping that decision in one place with the forced/priority logic rather than splitting it across two agents.
- **4C — additionally:** the fourth spinner (chunks, from 4B), a Pause entry, the speed-limit on/off switch, and opening from a click on the **speed meter** as well as the gear. Mount into 2G's shell bar, not into Dashboard, or it is unreachable from four of six routes. Drop the Menu Customizer link (common-pass drop). The overview strip carries total bytes, loaded bytes, ETA, a **Total/Visible/Selected** switch and "include disabled" — where *Visible* means after 1C's search and quickfilters, not `Object.values(tasks)`. **Open connections has nothing behind it**: `core.Update` carries no live count. Either add one from the gopeed progress callback or label the configured chunk count "chunks". Never invent a connection number. Save on blur or Enter, not per keystroke — `ApplySettings` ends in `dispatchLocked`.
- **4E — additionally:** edit **the whole selection**, not one task. A field left blank must not write an empty comment over forty tasks: mixed values render as a placeholder and are written only when touched. Rename needs `Name *string` on `TaskOptions` and a stated rule per status — a running task's file is open by the engine, so refuse with a reason or apply on next restart, never to the open handle; a finished task's rename must rename on disk too. Cut the new name to a single path segment (`internal/rules` already does exactly this for `Action.Filename`).
- **4G — NEW agent: host identity.** See section 9.

### Wave 5 — 5 agents → **9. This wave should be split.**
- **5A — additionally:** register Archives as its own entry in 2A's page registry — that *is* the content of this row; the options largely exist but only as an inline block. `DeleteArchive` becomes a three-value enum `keep|trash|delete`: **read the old bool on load, map it, write the new key** — a JSON field that changes type breaks the settings round-trip for every existing install. Add the info-file sweep (`.nfo/.sfv/.diz/.url`) **scoped to files belonging to the same package**, never a directory-wide extension sweep; a shared download folder makes that a data-loss bug. "Trash" in a container has no recycle bin: a `.knightloader-trash` folder under the download directory with an age-based sweep, and the help text must say so. A one-line capability banner naming the formats actually handled. **Existing bug to fix while in there:** success deletes `res.Volumes` blindly, so a mirror or a re-added duplicate sharing a volume file loses it.
- **5B — additionally:** two entry points it does not plan. `StartExtraction(taskIDs)` so a finished download can be unpacked on demand — extraction fires only as the tail of a finishing download today, so a wrong-password or out-of-space failure can never be retried without re-downloading the whole set. `AbortExtraction(jobID)` cancelling through a context **and removing the partial output** — a half-written extraction folder is indistinguishable from a successful one, and the next deep-extraction pass would walk it. The submenu is a group inside 1C's context-menu shell, not a second menu system. The two toggles are read **at extraction time**, not download time, so toggling after the download finished still takes effect. For a multi-volume set, `extractCandidateLocked` returns the *first* volume, not the part that finished last — read the override from that first volume, or the same archive extracts or not depending on which part happened to complete last.
- **5F / 5G / 5H — NEW agents:** archive format layer, collision policy, availability. See section 9.
- **5I — NEW agent: the server folder chooser**, moved from 10E. See section 10.

### Wave 6 — 5 agents → **8**
- **6A — additionally, and it is now the largest single agent in the plan (4 rows → 12).** `knownServices` and the frontend `SERVICES` array stop being the account list and become a service *catalogue*; the page lists 0..n stored accounts, several per service, and `GET /api/accounts` changes shape from one-state-per-known-service to one-row-per-account with a stable account id. A real table (Enabled / Service / Status / Label / Expiry / Traffic left / gear), not stacked cards — and **no Password column**: the API never returns a stored secret, so that cell shows set / not set plus an edit action. Verify **before** persisting, with "save anyway" so an offline service does not block adding a key. A credential from `KL_TORBOX`/`KL_ALLDEBRID`/`KL_REALDEBRID` renders read-only **with the reason**, never a save button that silently does nothing, and never blank — a blank read-only field looks like data loss. `Enabled` gates `rewireBackends` exactly as a missing credential does, and **must default true** when the three service-keyed secrets migrate into account rows: the same hazard as `Task.Enabled` (section 4, conflict 7). The "New" dialog needs a searchable host picker; "Buy Premium"/"Renew" survives as a plain outbound link to the service's own pricing page with **no affiliate id and no tracking parameter**, disabled when the account is not expiring — that satisfies the row and the no-commercial-assets rule at once. "Refresh" forces one fetch now with a per-row spinner, distinct from 6B's ticker.
- **6B — additionally:** widen `AccountState` with tier, traffic and expiry, and render the strip into 2G's shell bar, not the Accounts page. **The strip must read a cached snapshot**, because `TestAccount` is a live network call per service with a 15s timeout — otherwise every page load fires three third-party calls and a slow debrid host stalls the bar on every route change. Model **unlimited as its own case**, not a large number: a progress bar fed a zero maximum renders 0% used, which reads as "out of traffic". Store `{used, limit, unlimited, resetsAt}`. Default the tier to `unknown`, **never free** — showing a paying user "Free" is the complaint you will get.
- **6C — additionally:** refresh the supported-host list on a timer and on demand, persist it with a `fetchedAt`, and **keep the last good list when a refresh fails** — today a transient error returns nil, the resolver registers with an empty host set, and that service silently claims nothing until restart. Surface "host list last refreshed" and the JD container's own version.
- **6F / 6G / 6H — NEW agents:** service catalogue, account health, JD-sidecar hoster accounts. See section 9.

### Wave 7 — 5 agents → **6**
- **7F — NEW agent, and it must run first.** Nothing in the tree produces a challenge (`grep` for captcha across `internal/` returns three comments and no code), so 7A–7D would ship four untestable packages. See section 9.
- **7A — additionally:** render from 7F's typed descriptor rather than assuming one image-and-textbox shape; Continue / Cancel / Refresh plus the countdown. **Two JD affordances must not be built:** the Buy-Premium button (no affiliate arrangement), and cancel-countdown-on-mouse-move (a NAS instance's viewer is not at the machine) — replace the latter with "the countdown pauses while the answer field has focus".

### Wave 8 — 6 agents → **6** (8E freed, 8G added)
- **8A — additionally:** two fields absent from 8A's list — the **archive** password for the batch (which is *not* the hoster link password 8A already carries; section 4, conflict 5) and the batch comment, alongside priority and auto-extract. Decide precedence inside 8A: form values apply at stage time and the Packagizer runs after them, so a rule wins by default, with an "these values overwrite Packagizer rules" checkbox that inverts it. The recently-used destination list must be a **persisted history** (JD keeps 25), not the current live-task-derived suggestion — the point of it is the folder you used last week, which no live task remembers. Consume 5I's chooser rather than shipping a second free-text path field.
- **8B — additionally:** put the URL extractor in the **links route, server-side**, not in `Collector.tsx` — the watch folder, Click'n'Load, the bridge and container import all inherit it; doing it in the browser leaves three of five intake paths unscraped. Scan the whole blob, keep first-seen order, strip trailing prose punctuation and unmatched closing brackets, rejoin a URL a mail client wrapped across a line break, and fall back to line-splitting so a bare `example.org/file.zip` still works. Off switch (JD's `AddLinksPreParserEnabled`). Document-level paste listener that **ignores the event when the target is an `<input>`, `<textarea>` or contenteditable** — otherwise pasting a password into 1C's search field queues it as a download and prints it in the list. Read `event.clipboardData`, not `navigator.clipboard`, which is why this half works everywhere. The one-shot "paste from clipboard" button must **feature-detect and hide itself**: the normal deployment is `http://192.168.x.x`, not a secure context, so `navigator.clipboard` is undefined there. Drop calls `addLinks` directly (today it only appends text into the textarea, still requiring a click), the drop zone is the whole window and the Downloads list, and a drop onto the textarea itself stays a plain text drop. `clipboardprocessblacklist` has no web analogue — a page cannot learn which application the clipboard came from — and its purpose is already served by the link filter's source/URL conditions; point there rather than reinventing it, and say on the page which keys are inert in a plain browser rather than showing dead switches.
- **8C — additionally:** `onDupes` and `onOffline`, each `∈ {include, exclude, exclude-and-remove, ask, use-global}`, **defaulting to exclude — never exclude-and-remove**, because nothing should be deleted by a default. Only `Online == "offline"` may be excluded; `unknown` and `uncheckable` must be included, or one hoster refusing a probe quietly removes a whole package. Evaluate both in one pass and report combined ("3 offline and 2 duplicate links were not started") rather than two prompts back to back. "ask" falls back to the global default when the confirm was fired by auto-confirm, the watch folder or Click'n'Load, where nobody is watching. **The `autoStart` migration is the trap:** today it conflates confirm and start; splitting it into `autoConfirm` + `autoConfirmDelay` + `autoStart` must map existing `autoStart=true` installs to **both** true, or every user who had it on wakes up with links parked in the collector. "Confirm without start" becomes a real third state. `addAtTop` governs crawler results too, not only the manual form.
- **8E is freed** — deep page analysis becomes a forced-on flag on the Wave 2 crawl job.
- **8G — NEW agent: out-of-page intake** (CnL/FlashGot route completeness, bridge clipboard, container relay). See section 9.

### Wave 9 — 5 agents → **5**
- **9A — additionally:** define activity as a **typed job with counters**, not a free-text status line — one hub message `{kind: crawl|linkcheck|captcha|autoconfirm, active, total}`, with 7A and 8C publishing into that same channel rather than inventing their own. Today the hub carries only `task`, `queue` and `removed`, and link-check progress is inferable only per task.
- **9B — additionally:** the controls on the bubble itself, which is the census's missing half — a gear opening that event's settings row, an X, and "never show this type again" behind a confirm. **Two things in the current toast block it:** it auto-dismisses on a hardcoded 4000ms timeout with no hover-pause, so a bubble carrying controls disappears while it is being read; and the container sets `pointer-events-none`, so the controls are unclickable until pointer events are restored on the bubble.
- **9C — additionally:** carry what JD's tray tooltip carries (running, total, percent in the favicon ring; speed and percent in the title) and **restore the plain title and favicon when the queue goes idle**, or a finished run leaves a stale ring in the tab strip forever. This is the container build's answer to "Light Tray"; the desktop build's answer is package 21.

### Wave 10 — 6 agents → **7** (10E moves out, two new)
- **10D — additionally:** give the server a graceful stop first. `main.go` is `log.Fatal(http.ListenAndServe(...))`, so there is no `Shutdown` path for restart either — an `*http.Server` plus `Shutdown(ctx)` and a signal loop (the bridge path already has one to copy). Quit is restart's sibling: drain, `a.Close()`, exit 0. **Under Docker/Unraid the supervisor restarts the process, so quit and restart are indistinguishable** — the endpoint must report the build it is in and the UI must label the button *restart* there, or people report quit as broken.
- **10E moves to Wave 5 as 5I.** See section 10.
- **10G / 10H — NEW agents:** reach-the-file, desktop presence. See section 9.

### Wave 11 — 7 agents → **6** (11F moves to Wave 1)
- **11C — additionally: one Remote access sub-page.** This is what turns the loose pieces into the feature people ask for: issue and revoke **named API tokens** (a phone gets its own, revocable without changing the shared password), show the addresses this instance is genuinely reachable on with a QR code, offer the PWA install 11D builds, and **warn loudly when the server answers on a non-loopback address with no password set** — auth is off by default (empty hash = no password) and nothing tells you that you are exposed. State plainly that there is no hosted relay and that off-LAN reach is a port forward, a reverse proxy or a VPN, so the absence reads as a decision. **Record the ruling in the census Verdict** rather than leaving the row open: an account service plus NAT traversal for other people's boxes is a hosted product with ongoing cost and liability, not a feature of a self-hosted binary, and nobody should start it. Same for JD's method names: KnightLoader publishes its own vocabulary and does **not** mimic them — a MyJD-compatible shim buys nothing, because MyJD clients speak to the vendor relay, which is already dropped. Writing that in `/api/help` stops somebody half-building it.

### Wave 12 — 4 agents → **3**
- **12A absorbs the command customiser** (package 22) and must consume Wave 1's registry rather than inventing a second one. One customiser with a surface picker, not JD's five Swing editors. A small set of commands is **unhideable** so a user cannot lock themselves out of a destructive-but-necessary action, and the shortcut recorder **rejects browser-reserved chords** (Ctrl+W/T/N) at capture time with a named reason rather than accepting a binding that silently never fires. Layouts are versioned; a layout naming a removed command degrades to the default entry.
- **12C shrinks to a parity gate.** The working tree already has 38 locales. See 12.9.

---

## 9. The genuinely new packages — 22 packages, 35 rows

| # | Package | Rows | Wave | Agent | Effort |
|---|---|---|---|---|---|
| 1 | Derived task state (`stateKey` + `stateArgs`, server-side) | 1 | 1 | **1F (new)** | M |
| 2 | Rule condition data: `Origin`, online, dupe verdict, resolver caps | 1 | 1 + 2 | 1A2 + 2B | M |
| 3 | Rule set store, master switches, rule ordering | 3 | 2 | **2H (new)** | M |
| 4 | Crawl jobs: the add path stops blocking the request | 2 | 2 | 2C | L |
| 5 | Persistent shell bar + app menu | 1 | 2 | **2G (new)** | M |
| 6 | Bandwidth budget (`DownloadSpeedManager`) | 1 | 3 | **3G (new)** | L |
| 7 | Disabled links: the flag the scheduler honours | 3 | 1 + 4 | 1A2/1B + 4B/4A | M |
| 8 | Stop all + the truthful unresumable warning | 2 | 4 | 4A | M |
| 9 | Host identity: name + favicon, served from our own origin | 1 | 4 | **4G (new)** | M |
| 10 | Encrypted ZIP and the archive-format gap | 1 | 5 | **5F (new)** | L |
| 11 | Download-target collision policy | 1 | 5 | **5G (new)** | M |
| 12 | Availability: `Checker` interface + the fourth state | 2 | 5 | **5H (new)** | L |
| 13 | Service catalogue + credential schema | 2 | 6 | **6F (new)**, before 6A | M |
| 14 | Account health: error states, benching, persisted status | 3 | 6 | **6G (new)** | L |
| 15 | Hoster accounts via the JD sidecar (reconciliation) | 1 | 6 | **6H (new)** | L |
| 16 | Captcha challenge source + typed descriptors | 1 | 7 | **7F (new)**, first | L |
| 17 | CnL + FlashGot listener completeness | 1 | 8 | **8G (new)** | S |
| 18 | Ambient clipboard capture (bridge only) | 1 | 8 | 8G | M |
| 19 | Encrypted-container relay through the shipped JD | 1 | 8 | 8G | M |
| 20 | Reach the file: stream, reveal folder, open natively | 2 | 10 | **10G (new)** | L |
| 21 | Desktop presence: tray, tray menu, close/minimize ×2, start hidden, dialog stacking ×2 | 9 | 10 | **10H (new)**, **decided 2026-08-09** | L |
| 22 | Command registry + layout override | 1 | 1 + 12 | 1D + 12A | XL |
| | | **35** | | | |

Highlights of what each is for, where it is not obvious from the name:

**1. Derived task state** is a coordination contract more than it is code, and landing it late is worse than not doing it. `StatusPill` maps status through an exhaustive `Record<TaskStatus,…>` of seven values (verified), while JD's Status column shows a *sentence* derived from several independent facts. Section 4's conflict 2 already ruled that skipped/held/captcha-waiting are flags plus a typed reason so that `Record` keeps compiling — nobody was assigned the consequence. Compute `{stateKey, stateArgs}` server-side; **never a pre-translated sentence**, because the server does not know which of the 38 locales a given browser shows and two clients of one instance can differ. **Write the precedence order down and table-test it** (fatal error > skip reason > waiting-for-X > retry countdown > extracting > running > queued > paused > done) or two waves each insert a case at the top of the switch and "skipped: disk full" disappears behind a reconnect wait. `NextTry` is absolute and the UI has no repaint tick, so ship the timestamp and count down from **one shared 1s interval**, not a timer per row. By Wave 5, three packages will each have hard-coded a string into `StatusPill` and 38 locale files; unwinding that costs more than the package.

**3. Rule set store** — `rules.json` beside `settings.json`, **not** inside `settings.Settings`: `PUT /api/settings` decodes a whole struct and replaces it wholesale, so a browser that loaded the page before a rule was added posts its stale copy back and deletes that rule with no error anywhere. **The switch must be spelled `Disabled bool`, never `Enabled bool`** — the zero value has to be a live rule set, exactly as `rules.Rule` already does it; an `Enabled` field defaults to false and switches *both* engines off on first boot after the upgrade, and the symptom reads as a matching bug, not a settings bug. Slice order is load-bearing (`Apply` is cumulative, later rule wins per field), so reorder is one route taking the whole ordered id list, not move-up/move-down that two browsers can interleave into a scrambled list.

**4. Crawl jobs** — `POST /api/links` does the entire job inside the request: crawl (30s timeout each) then resolve per discovered link, serially, before a single byte is written. Twenty gallery URLs is a silent spinner for minutes, and a reverse proxy with a 60s read timeout returns 504 while the server is still happily staging. Click'n'Load is worse: the calling site reports "no downloader running" while the links do in fact arrive. Return 202 with a job id; broadcast counters as they change. **Three edge cases decide whether it works.** `AutoStart` must fire once on job completion over the job's created set, not per URL as results arrive, or a package starts downloading while half of it is still being crawled. The duplicate guard snapshots `known` once at the top of `AddLinks`, so with two jobs in flight both snapshots predate the other's inserts and the same list pasted twice queues twice — the guard must consult live state per URL under the insert lock. And `AddLinksWithPasswords` applies the archive password to `created` *after* `AddLinks` returns; once the call returns early there is nothing to apply it to, so the password must become part of the job's intent or Click'n'Load submissions silently lose their archive passwords.

**6. Bandwidth budget** — the cap is handed independently to three enforcers (engine throttle, JD's `SetSpeedLimit`, yt-dlp's `--limit-rate`), so a 1 MB/s cap with one download on each pulls **3 MB/s**: the limit is silently multiplied by the number of backends in use. Inside the engine there is no per-download share, so one fast host takes the whole bucket. Division must return unused share on the next tick, or the cap becomes a floor as well as a ceiling and leaves the line half idle — the classic wrong build. The identity problem is solved by a fact I verified: gopeed resolves `base.Request.Proxy` **in favour of the request** over the global config, so a per-task loopback listener makes every byte attributable, tunnels included (a marker header fails for HTTPS, i.e. for nearly all real traffic). Two mechanical traps: `rate.Limiter.SetLimit` does not cancel an outstanding `WaitN`, so lower shares *between* chunks, never mid-wait; and throttle's fixed 32 KiB chunk is far too coarse for a small share — at 4 KB/s a task stalls eight seconds per chunk and looks hung. This needs a **measured throughput test**, not a unit test: a bug here surfaces as "downloads are slow", not as an error.

**7. Disabled links** — the flag exists in the migration and means nothing to the engine. `dispatchLocked` skips `Enabled == false` outright: never enters the queue, never holds a slot, never counts toward `MaxPerHost`. **The path that will actually break it:** `resumeAll` resumes everything in `StatusPaused`, and `RestartTasks([])` / `startTasks([])` treat an empty id list as "all" — every one of those must filter on Enabled, or the first click of "Resume all" revives exactly the links the user deliberately parked. Same trap section 4 flagged for `Hold`.

**8. Stop all** — two different stops and only one exists. `POST /api/queue {"halted":true}` stops the dispatcher and deliberately leaves running transfers alone; that is JD's "finish running ones", a *different* button, which means the census's claim that no global transport route exists is stale. The warning is the whole point and is unbuildable until `Resumable` is populated by 3D.

**9. Host identity** — every row says which *resolver* carries it, none says which *host* the file is on, which is what users sort and filter by. The census calls this blocked on per-hoster plugin knowledge; it is a lookup table of ~120 entries, and an unknown host correctly renders as `some-host.tld`. **The favicon must be fetched server-side** — not an optimisation: letting the browser load `https://<hoster>/favicon.ico` per row tells every hoster in the list, from the user's own IP, that someone is looking at this page, which is the opposite of what a self-hosted downloader is for. Cap the body, negative-cache failures for a day, serve with a Content-Type from **our own allowlist plus nosniff**, never the remote server's declared type. `/api/host-icon/{host}` must resolve only hosts that appear on a task in this instance, or it is an unauthenticated request proxy into the user's LAN. For debrid links the host comes from the *originally pasted* URL, not `result.DirectURL`, or every premium download claims to come from the same place.

**10. Encrypted ZIP** — the one core extraction row the twelve waves never touch, because Wave 5 plans the job layer and this is the format layer. Verified: `if f.Flags&0x1 != 0` returns a flat error, so a password-protected zip fails as if it were corrupt and neither the global list nor the per-task password ever reaches it, even though both already reach rar and 7z. **The edge case that bites:** a WinZip AES entry declares compression method 99 with the real method in an `0x9901` extra field, so a naive reader writes AES ciphertext to disk as *stored* data and reports success — a corrupt file with a green tick, worse than an error. Format detection today keys entirely off the file name, so a `.rar` renamed to `.zip` is opened with the wrong reader; add a magic-byte probe ahead of the suffix switch. `arj`/`lzh`/`ace` have no pure-Go readers and are extinct: give them a named reason, not a generic failure. **Pure Go is a hard constraint** — the container binary is static, so CGO bindings would break the image. The dependency review (provenance, maintenance, zip-slip/zip-bomb behaviour) is a gate before the wave, not a formality during it. This must not slip past Wave 5: an encrypted zip is the single most common archive a JDownloader refugee arrives with.

**11. Collision policy** — verified: gopeed's HTTP fetcher manager returns `AutoRename() = true` **unconditionally**, and `CheckDuplicateAndRename` overwrites whatever name it was handed. There is no config to switch it off, so the policy must be applied before the handoff and the directory arranged so gopeed's rename is a no-op. `internal/collide` is built, tested, 427 lines, and referenced by exactly one place today. For `rename`, delete collide's `O_EXCL` placeholder immediately before returning the name, or gopeed sees an existing file and renames again → `name (2) (2).ext`. Collide must sanitize with gopeed's own `SafeFilename` rules first, or the reserved name and the written name differ and the reservation is meaningless. Multi-file resources make `Options.Name` a *folder* name, which collide does not reserve. Only the engine path is covered — JD and TorBox fetch in their own process — so the UI must not offer a collision decision on a task routed elsewhere.

**12. Availability** — three values where JD has four, and the missing one is the useful one: "the host was asked and would not say". A Real-Debrid, headless-JD or yt-dlp link sits at `""` forever because probing only happens when the resolver is `direct`, so in the UI it is indistinguishable from a link nobody has checked. The census blames plugin knowledge; that is only true if *the app* does the checking. An optional `Check(ctx, urls) ([]Availability, error)` beside `resolver.Resolver`, **batched** because every one of those APIs is batched and probing fifty links one at a time is how an account gets rate-limited. The direct HEAD probe finally gains the distinction it lacks: 404/410 offline, 429/503 or transport error **uncheckable**, 2xx/3xx online — right now every transport error is filed as "offline", so one flaky minute marks a live link dead and the user deletes it. **Verify per service that the check endpoint is free before wiring it: the only thing worse than not checking a link is silently spending the user's premium traffic to do it.** Links that have quietly read "unknown" for months will start reporting offline — correct, but it belongs in the changelog.

**13. Service catalogue** — two hardcoded lists hand-synced in two languages, with the frontend "where to get your key" string untranslated. One server-owned catalogue; the page renders the picker and credential form generically and knows nothing service-specific. **The stored-value shape is the edge:** `accounts.Store` holds one opaque string per service, which cannot hold user+password or two accounts for one host. The sealed value becomes a JSON object, with the old plain string read as `{"apiKey": "<old>"}` **forever** — never a one-shot rewrite of `accounts.json`, because a half-written migration leaves an install with no credentials and no way to tell that from "never configured". Reuse `reconnect`'s existing redact/merge convention rather than inventing a second one. Done: adding a fourth service is one Go entry plus one locale key, and grepping `web/src/` for a service id returns nothing.

**14. Account health** — an account gains its own state machine (`ok`, `invalid`, `expired`, `temp_disabled` with `benchedUntil`, `error`), separate from whichever task happened to use it; today a service call's failure reports onto the task only and nothing ever reaches the account. **Benching must not fail the tasks routed there:** `dispatchLocked` sets `StatusError` when no resolver matches, so unregistering AllDebrid mid-queue turns 40 queued links into 40 hard errors instead of holding them for the fallback chain. Exactly one probe per bench expiry, never a loop — it is a paid API. **A global outage looks identical to a bad key:** distinguish by HTTP status and the code each API supplies, and default to `temp_disabled`, never `invalid` — wrongly marking a good key invalid is the bug users will report.

**15. JD-sidecar hoster accounts** — the census blocker ("an arbitrary hoster account cannot be added, there is no per-hoster login plugin") is true natively and false in practice: the headless JD sidecar already in the tree carries roughly a thousand of them and is already registered as the lowest-priority catch-all. This is a **reconciler**, not more UI: desired state here, actual state in JD's own config. **The edge case that bites:** a recreated or updated JD container comes back with an empty account list and every premium login is silently gone; downloads then quietly fall back to free-user speeds and nobody sees an error, only slowness. So it runs on every reconnect, not only at boot, and "not present on the sidecar" is a distinct state from "invalid credentials". Adding a login must raise the jd resolver above direct **for that host specifically**, or the link is fetched anonymously and the premium account is never used. **Credential custody genuinely widens** — the password leaves an AES-GCM store for a third-party process's config file — and the dialog must say so before it is saved. That is a decision to confirm before building, not after.

**16. Captcha challenge source** — the descriptor `{id, source, host, taskID, kind, prompt, payload, expiresAt}` with `kind ∈ image | click | widget | unsupported`, where `unsupported` **names the vendor** so the UI says which one it cannot show. **The answer must reach the same challenge instance:** JD expires a challenge on its own timer, and solving an expired id silently does nothing while the user sees "sent". `internal/provision` deliberately never shows JD's own UI, so a challenge we fail to relay is invisible *and* stalls that download for its full window — a relay that drops challenges is worse than none.

**17–19 (8G).** CnL: five routes are missing, and each is a site silently concluding no downloader is running — `/flash/addcnl`, `/flashgot` (bare 200, no body), `/alive`, `/favicon.ico`, `/crossdomain.xml`. **The POST-only rule must survive**: submission routes are POST, probes are GET, full stop. A `GET /flash/addcnl` is a no-preflight, no-gesture route any ad iframe or email preview can hit to queue downloads and archive passwords — copying JD's published route list, which *does* answer GET there, turns every open page into a drive-by download vector. The authorized-sites allowlist must default to **empty-means-allow-all**; empty-means-deny breaks every CnL button on upgrade, invisibly. Ambient clipboard: scoped to the **bridge only** (see 12.3), off by default, with a ring of recently-seen hashes so KnightLoader's own copy-link buttons do not get re-added. Container relay: `/flash/addcrypted` v1 answers 501 today, but `internal/container` already routes `.dlc/.ccf/.rsdf` to the shipped JD "which has its own key and does this legitimately" — the same reasoning covers addcrypted v1, and the census entry predates that commit. Snapshot-diff the JD linkgrabber, serialise relays through one mutex, remove the harvested links from JD so it does not start them itself.

**20. Reach the file** — the census marks both browser-impossible; the desktop-only half genuinely is, but the mechanism has three web-native equivalents. **This is the first endpoint that serves arbitrary bytes off the host filesystem, and the path-containment check plus the content-type allowlist are the whole feature.** Re-derive the path server-side through `dirFor` joined with the stored filename, then check it is still inside the resolved download root **after symlink resolution** — a task whose `Dir` was hand-set to a parent, or whose name carries a separator, must 403 rather than serve `/etc/shadow`. Inline only for an allowlisted type derived from the stored extension, attachment for everything else, `nosniff` on both. A running task's partial file is fine to serve, but `Content-Length` must be the bytes actually there or every client hangs. **The route must stay under `/api/`** — verified, `openRoutes` returns true for everything not `/api/`-prefixed, so anything else is world-readable on a password-protected instance.

**21. Desktop presence** — see 12.2 for why this is not a drop, and for the gate. Wails v2.10.2 already supplies `StartHidden`, `HideWindowOnClose`, `WindowStartState` and `OnBeforeClose`; nothing needs a fork. Persist the choices in a **desktop-local config file, not `settings.Settings`** — settings are served whole to every connected browser and shared by every client of one server, whereas a window preference belongs to one installation on one machine. **The edge case that bites:** on Linux there is no guaranteed tray (GNOME ships none without an extension), and the systray libraries need `libayatana-appindicator3` at build time while `desktop.yml` installs only `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` (verified) — so the Linux job breaks the moment the dependency lands unless that package is added in the same change. Worse at runtime: honouring "hide to tray" where the icon never appears hides the application with no way to get it back. Probe at startup; when absent, disable the dependent options **with the reason shown** and fall back to exit/taskbar. Quitting from the tray goes through `a.Close()`, not a process kill. **None of this may be imported by the server module** — the tray libraries are CGO on Linux and macOS, and one stray import turns the container's static binary dynamic; it would pass CI before failing in the scratch image.

### Agent counts: two waves need splitting, one needs three extra agents

| Wave | Was | Now | Verdict |
|---|---|---|---|
| 1 | 5 | **7** | 1A splits into 1A (seams) + 1A2 (model/migration, lane L7 — already a declared separate lane); +1F. 1B absorbs drag rather than a second writer on `TaskList.tsx`. |
| 2 | 6 | **9** | **Split the wave.** 2A→2A+2I, 2B→2B+2H, +2G. Nine agents is past the point where one reviewer can hold the wave. Suggested cut: **2** = shell bar, settings sub-pages/modules, rule store + editor, rule effects; **2′** = advanced key table, link filter + crawl jobs, connection manager. |
| 3 | 6 | **7** | 3E splits into 3E (httpx + pause state machine) + 3G (bandwidth budget). 3E cannot own both: one is two rows, the other is an L package on the byte path of every download. |
| 4 | 6 | **7** | +4G. No lane conflict: disabled-links dispatch goes to 4B (owns `app_dispatch.go`, lightest agent), stop-all to 4A (owns `app_queue.go` + `routes_queue.go`). Putting either in its own agent would put a second writer on those lanes. |
| 5 | 5 | **9** | **Split the wave.** +5F, +5G, +5H, +5I. Suggested cut: **5** = archive settings, extraction jobs, boot/resume/retention, watch folders; **5′** = format layer, collision policy, availability, folder chooser. Lanes are disjoint (5G takes `internal/engine` + `app_dispatch.go`, both unclaimed in Wave 5; 5C owns `app.go` New/Close). |
| 6 | 5 | **8** | +6F, +6G, +6H. Area 5's reader already flagged this ("plan Wave 6 with an extra agent") and understated it by two. 6A alone goes 4 rows → 12. |
| 7 | 5 | **6** | +7F, and it must **run first**, not in parallel — 7A/7B/7C/7D all consume its descriptor type, so 7F publishes that type on day one. |
| 8 | 6 | **6** | 8E freed to Wave 2; +8G. 8B was going to carry 7 core folds on top of 7 common rows; splitting in-page from out-of-page intake keeps both reviewable. |
| 9 | 5 | **5** | Unchanged. |
| 10 | 6 | **7** | 10E out to Wave 5; +10G, +10H. |
| 11 | 7 | **6** | 11F out to Wave 1. |
| 12 | 4 | **3** | 12A absorbs the customiser; 12C collapses to a parity gate. |
| | **66** | **80** | +14 agent-slots. |

---

## 10. Re-ranking: what moves, and what a core row displaces

| Move | From | To | Why |
|---|---|---|---|
| Server folder chooser (10E → 5I) | 10 | **5** | "Download directory" is a **core** row, and 5A's extraction destination, 5D's watch folders, 8A's add-links destination and the per-task override all consume the same picker. Left in Wave 10 it gets built four times. It must be **template-aware**: the download directory is a `pathvars` template, and browsing must set only the fixed prefix and leave the `<jd:…>` tail intact, or picking a folder silently deletes the user's whole naming scheme. |
| Drag-and-drop reorder (11F → 1B) | 11 | **1** | Core row, and it writes drag behaviour into `TaskList.tsx`, which 1B rewrites. Ten waves apart re-creates exactly the hot-file contention Wave 1's seam split exists to remove. Absorbed by 1B as a second work item, **not** a second agent on `L5`. It gains a drop target the Downloads list does not have: a drop on a package header is move-to-package (`SetPackage`), a drop between rows is a `Position` write, and the drag carries the whole selection. Keep the top/bottom/±1 buttons as the keyboard-reachable equivalent so the feature is not mouse-only. |
| Shell bar (proposed W4 → 2G) | 4 | **2** | `Layout.tsx` renders `<Sidebar/>` and an `<Outlet/>` keyed on `location.pathname`, so the whole page subtree remounts on every navigation and there is nowhere a widget can live and keep running. That is why the only transport control in the app sits inside `Downloads.tsx` and is invisible on the other five routes. 4C, 6B, 9A and 9C all produce always-visible widgets and **none of them owns a place to put one**; without this they each graft a strip onto Dashboard and Downloads and the app ends up with four bars on two pages and none on the other four. Earlier is strictly cheaper: it shifts every page's spacing once, and Wave 2 is before waves 5–12 add pages. `Layout.tsx` is unowned by all 46 packages, and 2A owns `Sidebar.tsx`, so the two writers stay apart. **Do not lose `key={location.pathname}`** — it drives the `glim-page-enter` animation. **And the instance switcher is the trap:** the selected peer instance lives in `Downloads.tsx` page state today, so a global bar hard-wired to `/api` would stop the *local* queue while the page shows a remote one; the selection must move into a context the shell reads, and the bar must say which instance it is controlling. **It did not land in Wave 2.** Checked before Wave 4 opened: `Layout.tsx` still renders only `<Sidebar/>` and the keyed `<Outlet/>`, and `QueueBar` is still rendered from `Downloads.tsx` alone — so the move happened on paper and nowhere else, and 4C would have had nothing to mount into. Wave 4 takes it as **4G**, before 4C rather than beside it. One correction to the note above while it is being acted on: the key is `section` (the first path segment), not the full pathname, because thirteen settings sub-pages under one section must not remount the shell on every click in their own tab strip. |
| Crawl jobs (8E → 2C) | 8 | **2** | 8E was one common row (deep page analysis); the blocking add path is core, and waves 5, 8 and 11 each add an intake path that would otherwise inherit the bug and need converting afterwards. Deep analysis becomes a forced-on flag on the same job. |
| `/api/help` generation gate (11C → 1A) | 11 | **1** | Eleven waves of hand-registered routes are eleven waves of drift; 11C then becomes a retrofit across ~200 routes. Nearly free in 1A, which builds the table anyway. |
| Command-record type (12A → 1D) | 12 | **1** | Same argument, worse consequence: the customiser is unbuildable in Wave 12 if waves 1–11 ship inline handlers. The *customiser* stays in 12. |
| `AvailUncheckable` constant (5H → 1A2) | 5 | **1** | `core/task.go` is frozen after Wave 1 by section 2's lane rule. The package stays in Wave 5; only the constant rides along. |

**The one straight swap.** **Wave 9's 9B (notification centre, 11 common rows) gives up an agent's capacity to Wave 6.** 9B is the largest single package in the late plan and carries exactly **one** core row (the controls on the bubble). Wave 6 carries **12 core folds plus 6 new core rows** across 15 common rows and was budgeted 5 agents. A per-event notification settings matrix is polish; a self-hosted user with a debrid account opens the account manager every week and currently sees three static cards that cannot express a second account. Move the capacity, keep 9B's typed-event core and quiet mode, and defer its per-event settings grid to Wave 12's sweep.

**And one thing that must not slip:** package 10 (encrypted ZIP) sits in Wave 5 and cannot move later. Every wave that ships before it ships a download manager that fails on the most common archive its target users arrive with.

---

## 11. The honest total

**Of the 128 open core rows:**

| Outcome | Count | |
|---|---|---|
| Closed by the six existing packages and existing surfaces | **5** | rules conditions ×3, reconnect off-state, federation dashboard |
| Folded into existing waves 0–12 | **85** | across 21 existing agents |
| Built as new packages | **35** | 22 packages |
| **Total closed** | **125** | **97.7 %** |
| Dropped with reason | **3** | 2.3 % |
| **Left open** | **0** | |

**The 3 core drops** — all genuinely somebody else's hosted service or somebody else's job:

- **My.JDownloader pairing** (Settings > My.JDownloader). No protocol to join and no relay to join it to. Kept equivalent: local peer registration + the federation proxy, plus 11C's Remote access page so the absence has an answer instead of a silence.
- **Auto Update Check / Update Interval** and **Find Updates / Updates available!**. A container cannot update itself from the inside, so the app could only notify — and the deployment already both detects *and performs* it (Unraid Docker tab, CA Auto Update, ShipLog). A recorded project decision, not a scoping dodge. A version-based check also systematically disagrees with a digest-based one: a rebuilt image at the same semver reads "up to date" here and "update ready" there, and two indicators that disagree are worse than one. Kept: the version at `GET /api/health` and in the sidebar, plus a locally served changelog view in 10C.

**Area 3's drop of "On close action" is overturned** and folded into package 21 — see 12.2. That is the only reader disposition this amendment reverses.

**The whole plan, core + common, out of 472 open rows:**

| | common | core | total | |
|---|---|---|---|---|
| Closed by existing packages / surfaces | 32 | 5 | **37** | 7.8 % |
| Built | 277 (46 pkgs) | 120 (85 folds + 35 new, 22 pkgs) | **397** | 84.1 % |
| **Total closed** | **309** | **125** | **434** | **92.0 %** |
| Dropped | 35 | 3 | **38** | 8.0 % |
| Left open | 0 | 0 | **0** | |

**68 packages** (46 common + 22 core), **80 agent-slots** across twelve waves — fourteen more than section 3 budgeted, and two of those waves (2 and 5) should be split rather than run nine wide.

One qualifier on the 92 %, stated because section 5 asked for the honest part: **it becomes ~94 % if the desktop bundles ship.** The common drop bucket's "No desktop (17)" is factually wrong (12.2), and roughly nine of those seventeen are ordinary Wails work that package 21 absorbs. That re-triage is not done here — it is a common-pass job — but the number moves, and it moves on a decision about `release.yml`, not on any code.

**Decided 2026-08-09: they ship.** KnightLoader is published as a container *and* as native Windows, macOS and Linux applications. So the ~94 % is the real figure, package 21 grows from three rows to nine, and eight of the original seventeen stay dropped for reasons that were never about the platform (see the amended drop list in section 3). Two things the decision did *not* need, both verified rather than assumed: `desktop.yml` already carries an `attach` job that packages each platform and uploads it to the release the tag creates, so the "built and thrown away" gap 12.2 describes is already closed; and the pipeline had **never actually run** — it is tag- and dispatch-triggered, and there has never been a tag — so it was dispatched manually the same day to find out whether three platforms really build before a release depends on it.

---

## 12. What the core rows reveal about the plan already being wrong

Ranked by what it costs to find out late. Every line verified in the tree at `aa58035` plus the uncommitted working tree, not inferred.

**12.1 — The unredacted-settings hole is live now, and conflict #1's resolution was not the one that got built.** This is the most urgent item in the amendment. The in-flight wiring put `proxycfg.Entry` **into** `settings.Settings` (`settings.go:100`, `Connections []proxycfg.Entry`) and `reconnect.Config` beside it (`settings.go:105`) — exactly what section 4 conflict 1 refused. It then wrote the right guard: `func (s Settings) Redacted()` at `settings.go:123`, whose own doc comment says "the endpoint that serves the settings must use nothing but this". **Nothing calls it.** `GET /api/settings` is still `writeJSON(w, a.Settings.Get())` (`api.go:164`) and `PUT` echoes `applied` raw (`api.go:183`). Every proxy password and the router password ship to every connected browser on page load, today. Two one-line fixes, and they belong in Wave 0 before Wave 1 touches those files — not in 2E and 3A, which is where the plan currently puts the concern. Separately, the plan must pick a lane: conflict 1 mandated *separate stores* (`connections.json`, `reconnect.json`); the wiring chose one store plus redaction plus merge-on-`Set`. Redaction is a defensible alternative, but **Wave 2E and Wave 3A are currently written against files that do not exist**, and one of the two has to be rewritten before those agents start.

**12.2 — "No desktop (17)" is a factually wrong drop bucket, and the real question is a shipping decision.** `README.md:15` says KnightLoader ships as a container *and* as native desktop apps; `desktop/main.go` is a working Wails wrapper; `desktop/go.mod` requires `wails/v2 v2.10.2`; `.github/workflows/desktop.yml` builds `windows/amd64`, `darwin/universal` and `linux/amd64` on every `v*.*.*` tag. Tray icon and close/minimize are therefore ordinary Wails work with first-party API support, not platform impossibilities. **But `release.yml` contains zero mentions of desktop, wails or those artifacts** — the bundles are uploaded with `actions/upload-artifact` and never attached to a release, so nobody outside CI can download them. So: the drop *reason* is wrong, and package 21 is worth nothing until that changes. Decide the shipping question first; if the answer is no, these rows are dropped for an honest reason (we do not ship it) rather than a false one (we cannot build it).

**Answered 2026-08-09: yes, it ships** — Windows, macOS and Linux. Two corrections to the paragraph above, both from re-reading the file rather than the note about it: `desktop.yml` has since grown an `attach` job that zips each platform's bundle, waits for the release the tag creates, and uploads it, so the artifacts are no longer thrown away when they expire. `release.yml` still contains no mention of desktop, and that is correct — the attaching lives with the building, in the workflow that knows the platform slugs, rather than being duplicated into the release job. Package 21 is therefore live work, not gated work.

**12.3 — Three areas disagreed about the tray and about the clipboard; both are resolved here.** Area 3 dropped "On close action" on two grounds: Wails ships no systray API (true — it needs a third-party library, which is not an impossibility), and "desktop/main.go serves `api.Handler` only as the Wails asset handler, never on a TCP port, so with the window closed there is no running app left to return to". The premise is right (verified, `main.go:61`) but the conclusion is not: hide-to-tray keeps the in-process app alive and merely invisible; only `OnShutdown` calls `a.Close()`. **Overturned.** Area 9 folded "Light Tray" to 9C instead — that is the correct answer for the *container* build and it ships regardless, so both survive, aimed at different builds. On the clipboard, area 3 proposed an OS poller and area 9 argued flatly against one ("a core feature that exists in one build and not the other costs more in support than it returns"). Area 9 is right about the desktop build and missed one fact: the **bridge** (`knightloader -bridge`) already runs on the user's own desktop machine precisely to serve the NAS case, and already owns a long-running signal loop and a REST path to the remote. **Ruling: build the poller in the bridge only**, off by default, out of the server module and out of the desktop build; area 9's armed-paste toggle is the in-page half and ships in 8B.

**12.4 — The census's "one global proxy slot" blocker is wrong, and it over-sized Wave 4D.** Verified in gopeed v1.9.3: `downloader.go` `setupFetcher` resolves `ctl.GetProxy` with the comment "task request proxy config has higher priority, then use global proxy config", and `base.Request.Proxy` with `Mode: "custom"` wins over `d.cfg.Proxy`. `Engine.DownloadTo` sets only `req.Extra` today. So per-download routing does **not** need upstream chaining inside `netproxy`, which is what 4D is sized for; it needs one field on the request. Re-plan 4D before starting it. This is also the enabling fact for the bandwidth budget's per-task metering, and it means the same for the Connection Manager row.

**12.5 — `app.go` is 2389 lines, not 1844.** The wiring commit added ~700. Wave 1A's seam split is roughly a third bigger than section 2 budgeted, and — more practically — **every line number in all nine reader reports is stale by up to 380 lines** (`app.go:954`→`:1327`, and the `1562`/`1806`/`1041` citations likewise). Treat the reports' file references as correct and their line references as approximate.

**12.6 — The Wave-0 start condition is met, but four fields landed with no migration.** Section 0 finding 1 said the wiring agent "has landed zero lines"; that is now stale — all six packages are referenced from `internal/app` / `internal/settings`, and the working tree carries +755 uncommitted lines across `app.go`, `core/task.go` and `settings.go`. Good. But `core.Task` gained `Comment`, `Chunks`, `AutoExtract` and `MatchedRules` with **no `ALTER TABLE`**, and `internal/store` persists tasks as columns, not a JSON blob. Those four fields do not survive a restart today. 1A2's single migration must cover them alongside `Origin`, `ChangedAt`, `ArchivePart`, `Resumable` and `AvailUncheckable`. One piece of good news: `AutoExtract` already landed as `*bool`, so the tri-state area 1 asked for is half-built — it just needs a TEXT column defaulting to `''` so unset stays distinguishable from off.

**12.7 — The dedupe wiring's refuse-at-add contradicts a core row, and the code is still uncommitted.** `AddLinks` drops a known URL silently, which closes the common dedupe rows correctly but makes the core row ("Duplizierten Link entdeckt!") unbuildable and makes 8C's `onDupes` policy dead config — with refuse-at-add there is nothing left to ask about by confirm time. Needs a `dupeManager` mode. Raise it with the wiring agent now, while it costs nothing.

**12.8 — Four readers proposed four different chunk precedences, and one of them is right for a reason the others missed.** Area 5's: a resolver returning `Connections` is stating what the *host tolerates*, so it is a ceiling, not a value. That single reframing merges all four proposals into one formula (section 8, Wave 4B). **And the clamp conflict must be settled at 16, not 20** — `rules.MaxChunks = 16` is built and tested, and gopeed's own HTTP fetcher `DefaultConfig` is `Connections: 16` (both verified). This deliberately does **not** follow conflict 4's precedent of widening `rules` to match JD (±2 → ±3): priority is a pure UI enum with no engine meaning, whereas 16 is the engine's own bound. Widening to JD's 20 would let a rule express a number the engine will not honour.

**12.9 — There are 38 locales in the working tree, not 26.** `web/src/lib/locales/` holds 38 `.ts` files; `bg`, `ca`, `hi`, `id`, `is` and `lt` are untracked additions in progress. Every "×26" in section 3, in section 5, and in all nine reader reports is **×38**. Wave 12C ("13 new locales, 26 → 39") is all but done and collapses to a parity gate. The consequence is the opposite of a saving: the core packages alone add ~256 new keys, so ~9,700 translated strings land on top of the common plan's ~330 keys × 38. **i18n is now the single largest cost line in the plan**, and the one-writer-per-wave rule for `locales/*` is its throughput ceiling. That is worth revisiting before Wave 2, not after Wave 8.

**12.10 — `<jd:source:N>` is already taken, and means something else.** `expand.go` implements it as the *Nth path segment of the source URL*; JD means *capture group N of the source-URL regex*. A Packagizer template copied from a JD config will silently build a different folder — no error, just the wrong destination. Pick one meaning, move the other to a new tag name, and put it in the changelog.

**12.11 — JD's Packagizer dialog has a third section with nothing to hang on.** "… then do (post-extraction)" is Move to and Rename, and extraction runs exactly once in place with no post-extract step; the only after-action is deleting the volumes. Either 2B ships a **two**-section dialog, or Wave 5 lands a post-extract pipeline first. Discovering this after the dialog is laid out means laying it out again, so it is written down here: **two sections in Wave 2**, third section only as a Wave 5 follow-on if 5B builds the pipeline.

**12.12 — Two things are retrofit-impossible and must have their contracts fixed in Wave 1 even though they ship much later.** The `/api/help` route-table gate (ships 11C) and the command registry type (ships 12A). Both fail organisationally rather than technically: if waves 1–11 register routes by hand and write inline `onClick`, neither package can be built at all at the end. Each needs one line in the Wave 1 brief and one check in every wave's review.

**12.13 — Two security facts the core rows surfaced that no wave currently owns.** `openRoutes` returns `true` for every path not prefixed `/api/` (`api.go:449-455`), so package 20's file-streaming route must stay under `/api/` or it is world-readable on a password-protected instance. And auth is **off by default** (`auth.Guard.Enabled()` is false while the bcrypt hash is empty) with nothing warning a user who has bound the server to a non-loopback address — which is why 11C's Remote access page carries that warning, and why it is worth pulling forward if Wave 11 slips.