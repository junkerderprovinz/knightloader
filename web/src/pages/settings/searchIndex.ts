// What the settings search knows about: every page, every card on it, and the
// caption plus (i) text of every row on those cards - as translation KEYS, never
// as text.
//
// That is the whole of the "right in all 42 languages with no extra work"
// promise. Nothing here is a string somebody would have to translate again; the
// search resolves every key through the same `t` the page itself renders with,
// so the index is in whatever language the reader is already looking at.
//
// WHY THIS IS DECLARED AND NOT DERIVED
//
// The obvious idea is to grow this out of registry.tsx plus the key prefixes and
// have it maintain itself. It does not survive contact with the tree, in four
// separate ways:
//
//   - four helper names reach the catalogue, not one. Settings pages call `t(`,
//     `tx(` (tx.ts), `cx(` (a per-file fallback over a local PENDING table in
//     Access, Captcha, Connections, Diagnostics, Schedule, Scripts and Torrents)
//     and `rx(` (RuleEditor's useRx). Six of the twenty-two pages are invisible
//     to anything that only knows the first.
//   - a page's keys are not in that page's file. Most of settings.rules.* lives
//     in components/RuleEditor.tsx, and the Downloads page is eight files.
//   - one file draws two rail entries. Look.tsx renders both /settings/look and
//     /settings/appearance, split by `{appearance && (` / `{general && (`.
//   - a key prefix does not name its page. The Downloads page carries
//     settings.downloads.*, settings.stall.*, settings.crawl.*,
//     settings.hostRules.*, settings.feeds.* and a pile of bare
//     settings.<one-segment> keys - and those same prefixes also hold toasts and
//     refusals, which are not rows anybody can jump to.
//
// So it is written down, and web/check-settings-search.mjs (run by CI) is what
// keeps it from rotting: a key a page draws and this file does not name fails
// the build, and so does a key here that the page stopped drawing or that en.ts
// never had. `node web/check-settings-search.mjs --dump` prints what the pages
// actually draw, which is how this file was filled in the first place.
//
// TYPE-ONLY IMPORT, ON PURPOSE. This module must stay parseable as data - the
// check script reads it as text rather than compiling it - and it must add
// nothing to the bundle but the object below.
import type { TranslationKey } from '../../lib/i18n';

/** One control's caption, and the text behind its (i). Both are searched; the
 *  caption is what the result row shows and what the jump anchors on. */
export interface SettingsRow {
  /** The exact key passed to Field/FieldGroup/ToggleRow's `label`. It is what
   *  ui.tsx's Caption emits as `data-glim-label`, which is the whole of the
   *  jump mechanism: no per-row id, no data attribute at ~190 call sites. */
  key: TranslationKey;
  /** The exact key passed to its `hint`, when it has one. */
  hint?: TranslationKey;
}

/** One Card. `title` is the key inside its SectionTitle - at most one per Card,
 *  which is what makes the title a unique handle on the card in the DOM. */
export interface SettingsCard {
  title: TranslationKey;
  /** SectionTitle's own optional `hint`. */
  hint?: TranslationKey;
  rows: SettingsRow[];
  /**
   * A NAME this card carries that is not a row: a status badge, a caption drawn
   * beside a bare Toggle rather than through Caption, a select whose only name
   * is its aria-label, a sub-heading.
   *
   * Offered as its own result and shown by name, exactly like a row - but
   * picking it lands on the CARD, because there is no caption in the DOM to
   * scroll to. The alternative was to call these rows and let every one of them
   * fail the row lookup and fall through to "that row is not on screen right
   * now", which would be the search telling the reader something untrue about
   * their own page.
   *
   * Short text only. Prose belongs in `body` below: a paragraph shown as a
   * result's own first line is a result nobody can read at a glance.
   */
  also?: TranslationKey[];
  /**
   * PROSE this card carries: an explanation, an empty-state sentence, a help
   * topic's body, the second half of a hint built from two keys.
   *
   * Searched and never displayed. A hit shows the card, marked the same way a
   * hint match is - because that is what it is, and because a result row whose
   * first line is four hundred characters of paragraph is worse than no result.
   */
  body?: TranslationKey[];
}

/**
 * Keyed by the page id the SERVER hands out (internal/api/routes_features.go's
 * featurePages), which is the same id registry.tsx maps to a component and
 * lib/commands/settings.ts already mints a palette command for. Cards in the
 * order the page DRAWS them, not the order the file happens to define them in,
 * so a result list reads like the page.
 *
 * This is not the list of pages. That comes from GET /api/features every time,
 * so a page the server did not send is never offered - see SettingsSearch.tsx.
 */
export const SETTINGS_INDEX: Record<string, SettingsCard[]> = {
  modules: [
    // The rows are the modules the server sent, named by the server. Nothing to
    // index: a module list that changes per deployment cannot be written down
    // here without lying about some of them.
    { title: 'settings.modules.sectionShipped', hint: 'settings.modules.fixedAtBuild', rows: [] },
    { title: 'settings.modules.sectionDesktop', rows: [] },
    { title: 'settings.modules.sectionNotBuilt', rows: [] },
  ],

  downloads: [
    {
      title: 'settings.downloads.locationTitle',
      rows: [
        { key: 'settings.downloadDir', hint: 'settings.downloadDirHint' },
        { key: 'settings.subfolderByPackage' },
        { key: 'settings.downloads.workDir', hint: 'settings.downloads.workDirHint' },
      ],
      // The download folder's hint is two keys glued together at the call site.
      body: ['settings.pathVars'],
    },
    {
      title: 'settings.downloads.collisionTitle',
      rows: [
        { key: 'settings.downloads.collision', hint: 'settings.downloads.collisionHint' },
        { key: 'settings.downloads.collisionAttempts', hint: 'settings.downloads.collisionAttemptsHint' },
      ],
    },
    {
      title: 'settings.downloads.limitsTitle',
      rows: [
        { key: 'settings.maxConcurrent' },
        { key: 'settings.maxPerHost' },
        { key: 'settings.chunks', hint: 'settings.chunksHint' },
        { key: 'settings.speedLimit', hint: 'settings.speedHint' },
        { key: 'settings.maxRetries', hint: 'settings.maxRetriesHint' },
        { key: 'settings.resumeOnStart', hint: 'settings.resumeOnStartHint' },
        { key: 'settings.keepFinishedDays', hint: 'settings.keepFinishedDaysHint' },
        { key: 'settings.historyMax', hint: 'settings.historyMaxHint' },
        { key: 'settings.verifyChecksums' },
        { key: 'settings.preParser', hint: 'settings.preParserHint' },
      ],
    },
    {
      title: 'settings.hostRules.title',
      hint: 'settings.hostRules.titleHint',
      rows: [
        { key: 'settings.hostRules.pattern', hint: 'settings.hostRules.patternHint' },
        { key: 'settings.hostRules.maxPerHost', hint: 'settings.hostRules.maxPerHostHint' },
        { key: 'settings.hostRules.chunks', hint: 'settings.hostRules.chunksHint' },
        { key: 'settings.hostRules.never', hint: 'settings.hostRules.neverHint' },
        { key: 'settings.hostRules.retryDelay', hint: 'settings.hostRules.retryDelayHint' },
        { key: 'settings.hostRules.retryMax', hint: 'settings.hostRules.retryMaxHint' },
        { key: 'settings.hostRules.retryTries', hint: 'settings.hostRules.retryTriesHint' },
      ],
    },
    {
      title: 'settings.headerProfiles.title',
      hint: 'settings.headerProfiles.hint',
      rows: [
        { key: 'settings.headerProfiles.name', hint: 'settings.headerProfiles.nameHint' },
        { key: 'settings.headerProfiles.origin', hint: 'settings.headerProfiles.originHint' },
        { key: 'settings.headerProfiles.headerName' },
        { key: 'settings.headerProfiles.headerValue' },
        { key: 'settings.headerProfiles.valueStored', hint: 'settings.headerProfiles.valueStoredHint' },
      ],
    },
    {
      title: 'settings.stall.title',
      rows: [
        { key: 'settings.stall.enabled', hint: 'settings.stall.enabledHint' },
        { key: 'settings.stall.timeout', hint: 'settings.stall.timeoutHint' },
        { key: 'settings.stall.restart', hint: 'settings.stall.restartHint' },
        { key: 'settings.stall.maxRestarts', hint: 'settings.stall.maxRestartsHint' },
      ],
    },
    {
      title: 'settings.downloads.diskTitle',
      hint: 'settings.downloads.diskHint',
      rows: [
        { key: 'settings.downloads.diskReserve', hint: 'settings.downloads.diskReserveHint' },
        { key: 'settings.downloads.diskLowSpace', hint: 'settings.downloads.diskLowSpaceHint' },
        { key: 'settings.downloads.diskCriticalSpace', hint: 'settings.downloads.diskCriticalSpaceHint' },
      ],
    },
    {
      title: 'settings.downloads.collectorTitle',
      rows: [
        { key: 'settings.downloads.autoConfirmDelay', hint: 'settings.downloads.autoConfirmDelayHint' },
        { key: 'settings.downloads.onDupes', hint: 'settings.downloads.onDupesHint' },
        { key: 'settings.downloads.addAtTop', hint: 'settings.downloads.addAtTopHint' },
      ],
    },
    {
      title: 'settings.crawl.title',
      rows: [
        { key: 'settings.crawl' },
        { key: 'settings.crawl.depth', hint: 'settings.crawl.depthHint' },
        { key: 'settings.crawl.maxPages', hint: 'settings.crawl.maxPagesHint' },
        { key: 'settings.crawl.sameHost', hint: 'settings.crawl.sameHostHint' },
        { key: 'settings.crawl.include', hint: 'settings.crawl.includeHint' },
        { key: 'settings.crawl.exclude', hint: 'settings.crawl.excludeHint' },
      ],
    },
    {
      title: 'settings.feeds.title',
      hint: 'settings.feeds.titleHint',
      rows: [
        { key: 'settings.feeds.url', hint: 'settings.feeds.urlHint' },
        { key: 'settings.feeds.interval', hint: 'settings.feeds.intervalHint' },
        { key: 'settings.feeds.filter', hint: 'settings.feeds.filterHint' },
        { key: 'settings.feeds.dir', hint: 'settings.feeds.dirHint' },
        { key: 'settings.feeds.priority', hint: 'settings.feeds.priorityHint' },
        { key: 'settings.feeds.status', hint: 'settings.feeds.lastPolledHint' },
        { key: 'settings.feeds.testResult', hint: 'settings.feeds.testHint' },
      ],
    },
    {
      title: 'settings.downloads.idleTitle',
      rows: [
        { key: 'settings.downloads.idleAction', hint: 'settings.downloads.idleActionHint' },
        { key: 'settings.downloads.idleCountdown', hint: 'settings.downloads.idleCountdownHint' },
      ],
    },
  ],

  archives: [
    {
      title: 'settings.archives.extractionTitle',
      rows: [
        { key: 'settings.extract' },
        { key: 'settings.archives.destination', hint: 'settings.archives.destinationHint' },
        { key: 'settings.archives.subfolder', hint: 'settings.archives.subfolderHint' },
        { key: 'settings.archives.moveTo', hint: 'settings.archives.moveToHint' },
        { key: 'settings.archives.collision', hint: 'settings.archives.collisionHint' },
      ],
      body: ['settings.pathVars'],
    },
    {
      title: 'settings.archives.afterwards',
      rows: [
        { key: 'settings.archives.disposal', hint: 'settings.archives.disposalHint' },
        { key: 'settings.archives.retention', hint: 'settings.archives.retentionHint' },
        { key: 'settings.archives.infoFiles', hint: 'settings.archives.infoFilesHint' },
      ],
    },
    {
      title: 'settings.archivePasswords',
      rows: [{ key: 'settings.archivePasswords', hint: 'settings.archivePasswordsHint' }],
    },
  ],

  // The General tab. Look.tsx's `{general && …}` half, in the order it draws.
  look: [
    {
      title: 'settings.sectionLinkIntake',
      hint: 'settings.linkIntakeHint',
      rows: [
        { key: 'settings.module.cnl' },
        { key: 'intake.clipboardWatch', hint: 'intake.clipboardWatchHint' },
        { key: 'settings.autoStart' },
        { key: 'settings.watchDir', hint: 'settings.watchDirHint' },
      ],
      // The extra sentence the watch-folder hint grows while the module is parked.
      body: ['settings.downloads.watchOff'],
    },
    // Both of these are a single control with its own name on it (the quiet-mode
    // switch, the language picker), so the card title is the only caption there
    // is to find.
    { title: 'notifications.quiet', rows: [] },
    // The per-event notification matrix. Title and explanation only, and the
    // seven event rows deliberately NOT listed: their keys live in
    // NOTIFY_EVENTS (lib/notify.ts) and the card renders them from that table,
    // so this page's own source never mentions one. The checker is right to
    // refuse them - an index entry for a key the page does not draw is a result
    // that jumps to a row nothing can scroll to.
    //
    // What it costs: searching for "captcha" does not surface the captcha rows
    // of this card. What it buys: searching for "notification" opens the card,
    // and the seven rows are then on screen. Making the rows findable means
    // moving their keys onto the page, which would be a table written twice.
    { title: 'notifications.title', hint: 'notifications.titleHint', rows: [] },
    { title: 'lang.label', rows: [] },
    { title: 'settings.dialogs.title', hint: 'settings.dialogs.hint', rows: [] },
    {
      title: 'settings.look.updatesTitle',
      hint: 'settings.look.updatesHint',
      rows: [{ key: 'settings.look.updatesAutoInstall', hint: 'settings.look.updatesAutoInstallHint' }],
      // The daily-check switch is drawn beside its own hand-built caption rather
      // than through Caption, so it has no anchor to land on.
      also: ['settings.look.updatesAuto'],
      // The container build swaps in its own sentence for the auto-install hint.
      body: ['settings.look.updatesAutoInstallContainerHint'],
    },
    { title: 'settings.system.lifecycleTitle', hint: 'settings.system.unavailable', rows: [] },
    { title: 'settings.system.backupRestoreTitle', hint: 'settings.system.backupRestoreHint', rows: [] },
    // Rendered by Help.tsx, drawn at the foot of this page (jdp, 2026-09-07:
    // "die Über-card soll in den allgemein-tab ganz nach unten") - which is why
    // Help.tsx maps to two pages in the check script's own table.
    { title: 'settings.about.title', rows: [] },
  ],

  // The same component's `{appearance && …}` half.
  appearance: [
    { title: 'settings.shape', hint: 'settings.shapeHint', rows: [] },
    { title: 'settings.navLabels.title', hint: 'settings.navLabels.hint', rows: [] },
    { title: 'settings.motion.title', hint: 'settings.motion.hint', rows: [] },
    {
      title: 'settings.colours',
      rows: [],
      // Every row in this card is a hand-built caption span beside a bare
      // Toggle rather than a ToggleRow, so none of them carries an anchor. They
      // are still the words somebody searches for, so they are findable and the
      // hit lands on the card.
      also: [
        'settings.accent',
        'settings.rainbow',
        'settings.rainbowOn',
        'settings.rainbowReactive',
        'settings.rainbowRotate',
        'settings.rainbowPaletteLabel',
        'settings.accentReset',
      ],
      body: [
        'settings.accentHint',
        'settings.rainbowHint',
        'settings.rainbowReactiveHint',
        'settings.rainbowRotateHint',
        'settings.rainbowPaletteHint',
      ],
    },
    { title: 'settings.theme', rows: [] },
  ],

  accounts: [
    {
      title: 'settings.accounts.setupTitle',
      rows: [{ key: 'settings.accounts.showInSidebar', hint: 'settings.accounts.showInSidebarHint' }],
    },
  ],

  instances: [
    {
      title: 'settings.instances.setupTitle',
      rows: [{ key: 'settings.instances.showInSidebar', hint: 'settings.instances.showInSidebarHint' }],
    },
  ],

  access: [
    {
      title: 'settings.access.identity.title',
      rows: [
        { key: 'settings.access.identity.nameLabel', hint: 'settings.access.identity.nameHint' },
        { key: 'settings.access.identity.domainsLabel', hint: 'settings.access.identity.domainsHint' },
      ],
    },
    {
      title: 'auth.password',
      hint: 'settings.lockHint',
      rows: [
        { key: 'settings.passwordCurrent' },
        { key: 'settings.passwordNew', hint: 'settings.passwordHint' },
      ],
    },
    {
      title: 'settings.access.cardTitle',
      hint: 'settings.access.phrase.body',
      rows: [],
      // Three badges in the card's own header row, reporting rather than
      // setting: how this works, whether it is connected, and which relay.
      also: [
        'settings.access.phrase.howButton',
        'settings.access.phrase.statusConnected',
        'settings.access.phrase.statusDisconnected',
        'settings.access.relay.none',
        'settings.access.relay.own',
        'settings.access.relay.project',
      ],
    },
    {
      title: 'settings.access.relay.title',
      hint: 'settings.access.relay.body',
      rows: [{ key: 'settings.access.relay.use' }],
      also: ['settings.access.relay.seesButton'],
    },
    {
      title: 'settings.access.ownRelay.title',
      hint: 'settings.access.ownRelay.body',
      rows: [
        { key: 'settings.access.ownRelay.use' },
        { key: 'settings.access.ownRelay.serveLabel', hint: 'settings.access.ownRelay.serveHint' },
      ],
    },
    { title: 'settings.access.tokens.title', hint: 'settings.access.tokens.intro', rows: [] },
  ],

  advanced: [
    {
      title: 'settings.advanced.mirrorsTitle',
      rows: [
        { key: 'settings.advanced.mirrorPolicy', hint: 'settings.advanced.mirrorPolicyHint' },
        { key: 'settings.advanced.keepMirrors', hint: 'settings.advanced.keepMirrorsHint' },
        { key: 'settings.advanced.mirrorFailover', hint: 'settings.advanced.mirrorFailoverHint' },
      ],
    },
    {
      title: 'settings.advanced.offlineTitle',
      rows: [{ key: 'settings.advanced.onOffline', hint: 'settings.advanced.onOfflineHint' }],
    },
    {
      title: 'settings.advanced.reclaimTitle',
      rows: [{ key: 'settings.advanced.reclaimTrust', hint: 'settings.advanced.reclaimTrustHint' }],
    },
    {
      title: 'settings.advanced.retryTitle',
      rows: [{ key: 'settings.advanced.retryTries', hint: 'settings.advanced.retryTriesHint' }],
    },
    // The generated key table. Its own search box, over the raw dotted paths and
    // their JSON values, is a different question from this one and stays out of
    // the index - see the check script's EXCLUDED list.
    { title: 'settings.advanced.allSettings', rows: [] },
  ],

  rules: [
    {
      title: 'settings.rules.setupTitle',
      rows: [],
      also: [
        'settings.rules.flavourLabel',
        'settings.rules.flavour.packagizer',
        'settings.rules.flavour.filter',
        'settings.rules.setOn',
        'settings.rules.setOff',
        'settings.rules.stopAfterMatch',
      ],
      body: ['settings.rules.setSwitchHint', 'settings.rules.stopHint'],
    },
    {
      title: 'settings.rules.listTitle',
      rows: [],
      // The rule editor's own condition and action fields. Every one of them is
      // a bare select or box carrying an aria-label rather than a Caption, and
      // all of them only exist while a rule is open for editing - so they are
      // searchable text on this card, never a row to be scrolled to.
      also: [
        'settings.rules.fieldPicker',
        'settings.rules.opPicker',
        'settings.rules.value',
        'settings.rules.min',
        'settings.rules.max',
        'settings.rules.category',
        'settings.rules.import',
        'settings.rules.export',
      ],
    },
    { title: 'settings.rules.testTitle', rows: [], body: ['settings.rules.testHint'] },
  ],

  categories: [
    {
      title: 'settings.categories.listTitle',
      hint: 'settings.categories.listHint',
      rows: [
        { key: 'settings.categories.name', hint: 'settings.categories.nameHint' },
        { key: 'settings.categories.id', hint: 'settings.categories.idHint' },
        { key: 'settings.categories.dir', hint: 'settings.categories.dirHint' },
        { key: 'props.priority', hint: 'settings.categories.priorityHint' },
        { key: 'props.autoExtract', hint: 'settings.categories.extractHint' },
        { key: 'settings.categories.speedLimit', hint: 'settings.categories.speedLimitHint' },
        { key: 'settings.categories.collision', hint: 'settings.categories.collisionHint' },
      ],
      body: ['settings.pathVars'],
    },
  ],

  connections: [
    {
      title: 'settings.connections.listTitle',
      rows: [
        { key: 'settings.connections.type', hint: 'settings.connections.typeHint' },
        { key: 'settings.connections.host' },
        { key: 'settings.connections.port' },
        { key: 'settings.connections.username', hint: 'settings.connections.usernameHint' },
        { key: 'settings.connections.password', hint: 'settings.connections.passwordHint' },
        { key: 'settings.connections.filter', hint: 'settings.connections.filterHint' },
        { key: 'settings.connections.cap', hint: 'settings.connections.capHint' },
        { key: 'settings.connections.testTarget', hint: 'settings.connections.testTargetHint' },
        { key: 'settings.connections.importLabel', hint: 'settings.connections.importHint' },
      ],
    },
  ],

  reconnect: [
    {
      title: 'settings.reconnect.setupTitle',
      rows: [
        { key: 'settings.reconnect.method', hint: 'settings.reconnect.methodHint' },
        { key: 'settings.reconnect.command', hint: 'settings.reconnect.commandHint' },
        { key: 'settings.reconnect.args', hint: 'settings.reconnect.argsHint' },
        { key: 'settings.reconnect.upnpLocation', hint: 'settings.reconnect.upnpLocationHint' },
        { key: 'settings.reconnect.interpreter', hint: 'settings.reconnect.interpreterHint' },
        { key: 'settings.reconnect.interpreterArgs', hint: 'settings.reconnect.interpreterArgsHint' },
        { key: 'settings.reconnect.script', hint: 'settings.reconnect.scriptHint' },
        { key: 'settings.reconnect.router', hint: 'settings.reconnect.routerHint' },
        { key: 'settings.reconnect.username', hint: 'settings.reconnect.usernameHint' },
        { key: 'settings.reconnect.password', hint: 'settings.reconnect.passwordHint' },
        { key: 'settings.reconnect.requestMethod' },
        { key: 'settings.reconnect.requestUrl', hint: 'settings.reconnect.requestUrlHint' },
        { key: 'settings.reconnect.requestHeaders', hint: 'settings.reconnect.requestHeadersHint' },
        { key: 'settings.reconnect.requestBody', hint: 'settings.reconnect.requestBodyHint' },
        { key: 'settings.reconnect.importLabel', hint: 'settings.reconnect.importHint' },
      ],
      // The HTTP method's own sub-heading. It draws a second title badge inside
      // this one card, so it is not a card of its own here: the jump resolves a
      // card through `.closest('.glim-card')` and would land on this one anyway.
      also: ['settings.reconnect.requests'],
      body: ['settings.reconnect.requestsHint'],
    },
    {
      title: 'settings.reconnect.checkTitle',
      rows: [
        { key: 'settings.reconnect.checkUrl', hint: 'settings.reconnect.checkUrlHint' },
        { key: 'settings.reconnect.checkPresets', hint: 'settings.reconnect.checkPresetsHint' },
        { key: 'settings.reconnect.interval', hint: 'settings.reconnect.intervalHint' },
        { key: 'settings.reconnect.timeout', hint: 'settings.reconnect.timeoutHint' },
      ],
    },
    {
      title: 'settings.reconnect.runTitle',
      rows: [],
      also: [
        'settings.reconnect.stateConfigured',
        'settings.reconnect.stateNotConfigured',
        'settings.reconnect.stateBusy',
        'settings.reconnect.stateIdle',
      ],
    },
  ],

  resolvers: [
    {
      title: 'settings.resolvers.quality',
      hint: 'settings.resolvers.intro',
      rows: [
        { key: 'settings.resolvers.quality', hint: 'settings.resolvers.qualityHint' },
        { key: 'settings.resolvers.customFormat', hint: 'settings.resolvers.customFormatHint' },
        { key: 'settings.resolvers.playlist' },
      ],
    },
    {
      title: 'settings.resolvers.subtitlesTitle',
      rows: [
        { key: 'settings.resolvers.subtitleLangs', hint: 'settings.resolvers.subtitleLangsHint' },
        { key: 'settings.resolvers.subtitleAuto' },
        { key: 'settings.resolvers.subtitleStrict', hint: 'settings.resolvers.subtitleStrictHint' },
      ],
    },
    {
      title: 'settings.resolvers.outputTitle',
      rows: [{ key: 'settings.resolvers.outputTitle', hint: 'settings.resolvers.outputHint' }],
    },
    {
      title: 'settings.resolvers.audioTitle',
      rows: [
        { key: 'settings.resolvers.audioFormat', hint: 'settings.resolvers.audioFormatHint' },
        { key: 'settings.resolvers.audioBitrate', hint: 'settings.resolvers.audioBitrateHint' },
        { key: 'settings.resolvers.audioLang', hint: 'settings.resolvers.audioLangHint' },
        { key: 'settings.resolvers.music', hint: 'settings.resolvers.musicHint' },
      ],
    },
    {
      title: 'settings.resolvers.embedTitle',
      rows: [
        { key: 'settings.resolvers.embedMetadata', hint: 'settings.resolvers.embedMetadataHint' },
        { key: 'settings.resolvers.embedThumbnail', hint: 'settings.resolvers.embedThumbnailHint' },
        { key: 'settings.resolvers.embedChapters', hint: 'settings.resolvers.embedChaptersHint' },
        { key: 'settings.resolvers.embedSubs', hint: 'settings.resolvers.embedSubsHint' },
        { key: 'settings.resolvers.embedSplitChapters', hint: 'settings.resolvers.embedSplitChaptersHint' },
        { key: 'settings.resolvers.embedNfo', hint: 'settings.resolvers.embedNfoHint' },
      ],
    },
    {
      title: 'settings.resolvers.measureTitle',
      rows: [
        { key: 'settings.resolvers.measure', hint: 'settings.resolvers.measureHint' },
        { key: 'settings.resolvers.measureShortPercent', hint: 'settings.resolvers.measureShortPercentHint' },
        { key: 'settings.resolvers.measureFailOnShort', hint: 'settings.resolvers.measureFailOnShortHint' },
      ],
    },
    {
      title: 'settings.resolvers.liveTitle',
      rows: [
        { key: 'settings.resolvers.live', hint: 'settings.resolvers.liveHint' },
        { key: 'settings.resolvers.liveFromStart', hint: 'settings.resolvers.liveFromStartHint' },
        { key: 'settings.resolvers.liveMaxMinutes', hint: 'settings.resolvers.liveMaxMinutesHint' },
        { key: 'settings.resolvers.liveMaxMB', hint: 'settings.resolvers.liveMaxMBHint' },
      ],
    },
    {
      title: 'settings.resolvers.cookiesTitle',
      rows: [
        { key: 'settings.resolvers.cookies', hint: 'settings.resolvers.cookiesHint' },
        { key: 'settings.resolvers.cookieJars', hint: 'settings.resolvers.cookieJarsHint' },
        { key: 'settings.resolvers.cookieHost' },
        { key: 'settings.resolvers.cookieText' },
      ],
    },
    {
      title: 'settings.resolvers.presetsTitle',
      hint: 'settings.resolvers.presetsHint',
      rows: [{ key: 'settings.resolvers.presetHost', hint: 'settings.resolvers.presetHostHint' }],
      // The per-host overrides are a table of bare selects named only by their
      // aria-label, one per column.
      also: ['settings.resolvers.moduleUnavailable'],
      body: ['settings.resolvers.moduleUnavailableHint'],
    },
  ],

  torrents: [
    {
      title: 'settings.torrents.seedingTitle',
      rows: [
        { key: 'settings.torrents.seedRatio', hint: 'settings.torrents.seedRatioHint' },
        { key: 'settings.torrents.seedDuration', hint: 'settings.torrents.seedDurationHint' },
      ],
    },
    {
      title: 'settings.torrents.transferTitle',
      rows: [{ key: 'settings.torrents.uploadLimit', hint: 'settings.torrents.uploadLimitHint' }],
    },
    {
      title: 'settings.torrents.portTitle',
      rows: [{ key: 'settings.torrents.port', hint: 'settings.torrents.portHint' }],
    },
    {
      title: 'settings.torrents.networkTitle',
      rows: [
        { key: 'settings.torrents.dht', hint: 'settings.torrents.dhtHint' },
        { key: 'settings.torrents.pex', hint: 'settings.torrents.pexHint' },
      ],
    },
  ],

  captcha: [
    // One card: a drag-sortable list of the solvers the server knows about, each
    // row named by the server rather than by the catalogue.
    { title: 'settings.captcha.orderTitle', hint: 'settings.captcha.orderHint', rows: [], body: ['settings.captcha.orderEmpty'] },
  ],

  schedule: [
    {
      title: 'settings.schedule.listTitle',
      hint: 'settings.schedule.orderHint',
      rows: [
        { key: 'settings.schedule.name' },
        { key: 'settings.schedule.action' },
        { key: 'settings.schedule.start' },
        { key: 'settings.schedule.end', hint: 'settings.schedule.endHint' },
        { key: 'settings.schedule.limit' },
        { key: 'settings.schedule.days', hint: 'settings.schedule.daysHint' },
      ],
      body: ['settings.schedule.empty', 'settings.schedule.emptyHint'],
    },
    // The status banner above this card is NOT here: its title key
    // (settings.schedule.statusTitle) is missing from en.ts, so it resolves to
    // undefined - see the check script's EXCLUDED list for what to do about it.
  ],

  diagnostics: [
    // The system card is likewise absent, for the same reason
    // (settings.diagnostics.systemTitle is missing from en.ts).
    { title: 'settings.diagnostics.logTitle', hint: 'settings.diagnostics.logHint', rows: [], body: ['settings.diagnostics.logEmpty'] },
  ],

  help: [
    { title: 'settings.help.intake.title', rows: [], body: ['settings.help.intake.body'] },
    { title: 'settings.help.collector.title', rows: [], body: ['settings.help.collector.body'] },
    { title: 'settings.help.rules.title', rows: [], body: ['settings.help.rules.body'] },
    { title: 'settings.help.queue.title', rows: [], body: ['settings.help.queue.body'] },
    { title: 'settings.help.limits.title', rows: [], body: ['settings.help.limits.body'] },
    { title: 'settings.help.captcha.title', rows: [], body: ['settings.help.captcha.body'] },
    { title: 'settings.help.after.title', rows: [], body: ['settings.help.after.body'] },
    { title: 'settings.help.schedule.title', rows: [], body: ['settings.help.schedule.body'] },
    { title: 'settings.help.instances.title', rows: [], body: ['settings.help.instances.body'] },
    { title: 'settings.help.access.title', rows: [], body: ['settings.help.access.body'] },
    { title: 'settings.help.advanced.title', rows: [], body: ['settings.help.advanced.body'] },
  ],

  browsertools: [
    { title: 'settings.browsertools.bookmarkletTitle', rows: [], body: ['settings.browsertools.bookmarkletStep1'] },
    { title: 'settings.browsertools.extensionTitle', rows: [], also: ['settings.browsertools.installLabel'] },
    { title: 'settings.browsertools.appTitle', hint: 'settings.browsertools.appBody', rows: [] },
  ],

  scripts: [
    {
      title: 'settings.scripts.listTitle',
      rows: [
        { key: 'settings.scripts.name' },
        { key: 'settings.scripts.trigger', hint: 'settings.scripts.triggerHint' },
        { key: 'settings.scripts.timeout', hint: 'settings.scripts.timeoutHint' },
        { key: 'settings.scripts.code' },
      ],
      body: ['settings.scripts.empty', 'settings.scripts.emptyHint'],
    },
  ],

  shortcuts: [
    // The card titles only. Its rows ARE the command names, and the command
    // palette (mod+k) already searches exactly those - a second box beside it
    // offering the same list is one search too many. The per-group cards below
    // it are titled from commands.group.*, which the palette also already
    // groups by.
    { title: 'settings.nav.shortcuts', hint: 'settings.shortcuts.subtitle', rows: [] },
  ],
};
