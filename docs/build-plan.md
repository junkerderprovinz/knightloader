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

- **No desktop (17):** tray icon, tray menu, tray manager, minimize/close actions ×2 pairs, start minimized, dialog stacking ×2, AntiStandBy + its mode, Swing menu editors ×5, Tools menu.
- **Third-party service we cannot use (9):** my.jdownloader.org relay (e-mail/password/device name, `-myjd`, direct-connect mode, headless-mandatory), JD's proprietary router-script database (Reconnect Wizard, Search Router Model), affiliate "Buy Premium", `.dlc` decryption key service.
- **Owned by the deployment, not the app (6):** find updates, update button, install-on-exit, silent install, `-norestart`, install4j installer.
- **Duplicated by an existing surface (3):** LinkDoubleClickAction, PAC proxy type (no JS engine in Go), Native Auth (Windows NTLM).

**What is still open after this plan** — and this is the honest part:

- **128 core rows not done** (71 partial + 57 missing). The plan closes roughly 20 of them incidentally (package tree, enabled flag, quick-settings popup, stop-mark, captcha dialog, status label, process indicators, connection manager page, column popup, row drag, Ctrl+V) but core was explicitly out of scope and **~108 core rows remain**. Several are load-bearing; I would look at that list before starting Wave 4.
- **377 niche rows**, untouched by design.
- The census's own totals were 79/202/647; after this plan the common column reads 335 have / 0 partial / 9 missing-by-choice… but the file itself has 928 rows and **the Verdict column needs updating as each wave lands**, or the census stops being the source of truth by Wave 3.

**Size, without flattery:** 46 packages, of which 6 are XL (`internal/script` host, captcha core, per-download proxy routing, link-filter holding area, locale breadth, settings shell) and 19 are L. Wave 1 alone is a full refactor of the four largest files in the repo plus a 20-column migration. At 5–6 agents wide this is **twelve waves and roughly a quarter of sustained work** — not a sprint, and not something to compress by skipping Wave 1's seam split, which is the only reason waves 2–12 can run six agents wide instead of one.

**Start condition:** Wave 1 cannot begin until the wiring agent lands, because 1A rewrites the exact three files that agent is editing. Confirm that merge first; everything else follows.