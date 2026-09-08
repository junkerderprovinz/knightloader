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
//     and `rx(` (RuleEditor's useRx). Six of the twenty-four pages are
//     invisible to anything that only knows the first.
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
    // Directly after the disk floors, the same order the page draws them in:
    // both are guards on whether a download STARTS, one measured against the
    // volume and one against the period's allowance.
    {
      title: 'settings.volume.title',
      hint: 'settings.volume.titleHint',
      rows: [
        { key: 'settings.volume.cap', hint: 'settings.volume.capHint' },
        { key: 'settings.volume.resetDay', hint: 'settings.volume.resetDayHint' },
        { key: 'settings.volume.action', hint: 'settings.volume.actionHint' },
        { key: 'settings.volume.throttle', hint: 'settings.volume.throttleHint' },
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
        { key: 'settings.downloads.idleCommandProgram', hint: 'settings.downloads.idleCommandProgramHint' },
        { key: 'settings.downloads.idleCommandArgs', hint: 'settings.downloads.idleCommandArgsHint' },
        { key: 'settings.downloads.idleCommandTimeout', hint: 'settings.downloads.idleCommandTimeoutHint' },
        { key: 'settings.downloads.idleCommandVerify', hint: 'settings.downloads.idleCommandVerifyHint' },
        { key: 'idleAction.lastRun', hint: 'idleAction.lastRunHint' },
      ],
      // The five menu entries and the two buttons. Names rather than rows:
      // picking one lands on the card, because a tab inside a strip and a
      // button have no caption in the DOM to scroll to.
      also: [
        'settings.downloads.idleActionPause',
        'settings.downloads.idleActionQuit',
        'settings.downloads.idleActionCommand',
        'settings.downloads.idleActionSuspend',
        'settings.downloads.idleCommandCheck',
        'settings.downloads.idleCommandRun',
      ],
      // Prose the card carries without a caption of its own: the deployment
      // sentence in the card title's own (i), the per-action explanation that
      // shares the menu's (i), the warning about what a command line is
      // stored as, and the placeholder that says a command IS stored even
      // though the box is empty. Searched, never shown as a result's own
      // first line.
      body: [
        'settings.downloads.idleDeploymentContainer',
        'settings.downloads.idleDeploymentDesktop',
        'settings.downloads.idleQuitHint',
        'settings.downloads.idleSuspendHint',
        'settings.downloads.idleArmHint',
        'settings.downloads.idleCommandSecretHint',
        'settings.downloads.idleCommandStored',
      ],
    },
    // The folder-ownership check. No rows: the card draws a button, a status
    // line and one block per folder, none of them a Field with a caption in the
    // DOM to jump to - so the role names go in `also` (offered by name, landing
    // on the card) and every sentence goes in `body`, which is searched and
    // never displayed. Somebody typing "besitzer", "chown" or "rechte" has to
    // find this card, and those words are only in the prose.
    {
      title: 'settings.owner.checkTitle',
      hint: 'settings.owner.checkHint',
      rows: [],
      also: [
        'settings.owner.run',
        'settings.owner.role.downloads',
        'settings.owner.role.work',
        'settings.owner.role.category',
        'settings.owner.role.watch',
      ],
      body: [
        'settings.owner.never',
        'settings.owner.checkedAt',
        'settings.owner.checkFailed',
        'settings.owner.v.ok',
        'settings.owner.v.ownerMismatch',
        'settings.owner.v.groupUnreadable',
        'settings.owner.v.dirUnreadable',
        'settings.owner.v.notWritable',
        'settings.owner.v.missing',
        'settings.owner.v.unknown',
        'settings.owner.fix.user',
        'settings.owner.fix.chown',
        'settings.owner.fix.mask',
        'settings.owner.fix.recreate',
        'settings.owner.fix.past',
      ],
    },
    // The address called once a package has finished and its files are in
    // place. Its own card file is downloads/MediaHooks.tsx, which the
    // downloads/ directory entry in check-settings-search.mjs already scans.
    {
      title: 'settings.mediahook.title',
      hint: 'settings.mediahook.hint',
      rows: [
        { key: 'settings.mediahook.name', hint: 'settings.mediahook.nameHint' },
        { key: 'settings.mediahook.wait', hint: 'settings.mediahook.waitHint' },
        { key: 'settings.mediahook.url', hint: 'settings.mediahook.urlHint' },
        { key: 'settings.mediahook.method', hint: 'settings.mediahook.methodHint' },
        { key: 'settings.mediahook.headerName', hint: 'settings.mediahook.headerNameHint' },
        { key: 'settings.mediahook.headerValue', hint: 'settings.mediahook.headerValueHint' },
        { key: 'settings.mediahook.valueStored', hint: 'settings.mediahook.valueStoredHint' },
        { key: 'settings.mediahook.lastCall', hint: 'settings.mediahook.lastCallHint' },
      ],
      also: [
        'settings.mediahook.add',
        'settings.mediahook.test',
        'settings.mediahook.usedBy',
        'settings.mediahook.usedByNone',
      ],
      body: [
        'settings.mediahook.empty',
        'settings.mediahook.emptyHint',
        'settings.mediahook.goesTo',
        'settings.mediahook.goesToForeign',
        'settings.mediahook.waitImmediate',
        'settings.mediahook.lastCallNever',
        'settings.mediahook.deleteInUse',
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
    {
      title: 'settings.transfer.title',
      hint: 'settings.transfer.hint',
      rows: [{ key: 'settings.transfer.withSecrets', hint: 'settings.transfer.withSecretsHint' }],
      // Names this card carries that are not rows: two Buttons and the caption
      // beside the third InfoBubble. None of them emits a data-glim-label, so
      // none of them has an anchor to scroll to, and the hit lands on the card.
      also: ['settings.transfer.export', 'settings.transfer.import', 'settings.transfer.notTravellingLabel'],
      body: ['settings.transfer.exportHint', 'settings.transfer.importHint', 'settings.transfer.notTravelling'],
      // The import preview's own forty-odd strings are deliberately not here,
      // and they need no EXCLUDED entry either: SettingsImportPreview.tsx hands
      // them to the catalogue through InfoBubble tips, plain children and a
      // Modal title, none of which the check script scans as a row or a card,
      // so it never asks about them. That is the right outcome twice over - a
      // search result that jumped to a row inside a dialog which does not exist
      // until a file has been chosen would lead nowhere at all.
    },
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
        { key: 'settings.categories.notify', hint: 'settings.categories.notifyHint' },
      ],
      also: ['settings.categories.notifyNone'],
      body: ['settings.pathVars', 'settings.categories.notifyEmpty', 'settings.categories.notifyMissing'],
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
      title: 'settings.resolvers.toolsTitle',
      hint: 'settings.resolvers.toolsHint',
      rows: [
        { key: 'settings.resolvers.toolsAutoCheck', hint: 'settings.resolvers.toolsAutoCheckHint' },
        { key: 'settings.resolvers.toolsActions', hint: 'settings.resolvers.toolsActionsHint' },
      ],
      body: [
        'settings.resolvers.toolsYtdlpMissingHint',
        'settings.resolvers.toolsFfmpegMissingHint',
        'settings.resolvers.toolsShadowed',
        'settings.resolvers.toolsShadowedOlder',
        'settings.resolvers.toolsManagedBroken',
      ],
      also: ['settings.resolvers.toolsCheck', 'settings.resolvers.toolsFetch', 'settings.resolvers.toolsRevert'],
    },
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
        // The host box and the jar box used to be two rows sitting open on the
        // page. They are inside CookieJarDialog now, behind the Add button, so
        // the search may no longer offer to jump to them: the jump resolves a
        // label in the DOM and neither is in it until somebody presses Add.
        // What stays findable is the card and the button that opens the dialog.
        { key: 'cookies.add' },
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

  health: [
    {
      title: 'settings.health.title',
      hint: 'settings.health.titleHint',
      rows: [
        { key: 'settings.health.version' },
        { key: 'settings.health.uptime', hint: 'settings.health.uptimeHint' },
        { key: 'settings.health.startedAt' },
      ],
      // The card's own status badge is the summary word itself (health.state.*,
      // looked up from a server id), so there is nothing fixed to index for it -
      // the same reason disk.role.* is absent from the downloads page's entry.
      body: [
        // The (i) beside that badge, and the one piece of prose on this page
        // that has to be findable: somebody who reads a failure here and then
        // sees /api/health still answering "ok" needs to be able to search for
        // why. It is a `tip=`, which the check script does not scan, so it is
        // named here on purpose rather than as a row.
        'settings.health.oldHealthHint',
        'settings.health.sampled',
        'settings.health.loadFailed',
      ],
    },
    {
      title: 'settings.health.parts',
      hint: 'settings.health.partsHint',
      // Every row on this card is a part the SERVER named (health.part.*) with
      // a state and a remedy it also named (health.state.*, health.remedy.*).
      // None of them appears literally in the page's source - they are built
      // from ids - so none can be indexed, exactly as disk.role.* cannot.
      rows: [],
    },
    {
      title: 'settings.health.tasks',
      hint: 'settings.health.tasksHint',
      rows: [
        { key: 'settings.health.running' },
        { key: 'settings.health.waiting' },
        { key: 'settings.health.failed' },
      ],
      also: ['settings.health.waitingWhy', 'settings.health.failedWhy'],
      body: ['settings.health.nothingWaiting', 'settings.health.nothingFailed', 'settings.health.diskLink'],
    },
    {
      title: 'settings.health.scrape',
      rows: [
        { key: 'settings.health.scrapeSwitch', hint: 'settings.health.scrapeHint' },
        { key: 'settings.health.scrapeUrl' },
      ],
      also: ['settings.health.scrapeCopy', 'settings.health.scrapeCopied'],
      body: ['settings.health.scrapeOffHint'],
    },
  ],

  diagnostics: [
    // The system card, indexed at last: its title key reached en.ts with this
    // wave, which is exactly what the check script's `waitingOn` exclusion was
    // waiting for. The exclusion came out in the same commit.
    {
      title: 'settings.diagnostics.systemTitle',
      hint: 'settings.diagnostics.subtitle',
      rows: [
        { key: 'settings.diagnostics.version' },
        { key: 'settings.diagnostics.deployment' },
        { key: 'settings.diagnostics.goVersion' },
        { key: 'settings.diagnostics.platform' },
        { key: 'settings.diagnostics.goroutines' },
      ],
    },
    // The start report has no rows of its own: every line on it is a check the
    // SERVER named, so there is no caption in the catalogue to jump to. The
    // button and the three empty-state sentences are what make it findable.
    {
      title: 'settings.diagnostics.startupTitle',
      hint: 'settings.diagnostics.startupHint',
      rows: [],
      also: ['settings.diagnostics.startupRecheck'],
      body: [
        'settings.diagnostics.startupNotRun',
        'settings.diagnostics.startupOff',
        'settings.diagnostics.startupStatOnly',
        'settings.diagnostics.startupRecheckHint',
      ],
    },
    // The self-test's two cards carry no ROWS in this sense: every line on them
    // is a readout with no caption in the DOM, so there is nothing for a jump to
    // scroll to. The seven check names and the four proxy checks go in `also`
    // instead, which lands on the card - and the advice, which is where the
    // words somebody would actually search for live ("nginx", "Upgrade", "TZ",
    // "uid"), goes in `body`.
    {
      title: 'settings.selftest.title',
      hint: 'settings.selftest.hint',
      rows: [],
      also: [
        'settings.selftest.run',
        'settings.selftest.jd',
        'settings.selftest.ytdlp',
        'settings.selftest.folders',
        'settings.selftest.accounts',
        'settings.selftest.relay',
        'settings.selftest.clock',
        'settings.selftest.torrentPort',
      ],
      body: [
        'settings.selftest.never',
        'settings.selftest.accounts.readOnlyHint',
        'settings.selftest.jd.missingAdvice',
        'settings.selftest.ytdlp.oldAdvice',
        'settings.selftest.folders.notWritableAdvice',
        'settings.selftest.clock.utcAdvice',
        'settings.selftest.clock.skewAdvice',
        'settings.selftest.relay.notConnectedAdvice',
        'settings.selftest.torrentPort.notCheckedAdvice',
      ],
    },
    {
      title: 'settings.selftest.proxy.title',
      hint: 'settings.selftest.proxy.hint',
      rows: [],
      also: [
        'settings.selftest.proxy.host',
        'settings.selftest.proxy.proto',
        'settings.selftest.proxy.prefix',
        'settings.selftest.proxy.ws',
      ],
      body: [
        'settings.selftest.proxy.desktopSkip',
        'settings.selftest.proxy.host.rewrittenAdvice',
        'settings.selftest.proxy.proto.missingAdvice',
        'settings.selftest.proxy.prefix.underPathAdvice',
        'settings.selftest.proxy.ws.failedAdvice',
      ],
    },
    // The log itself, now its own file (diagnostics/LogViewerCard.tsx). The
    // gap banner is indexed as body text although it only appears after lines
    // have actually been lost: somebody searching for "fell out" is searching
    // in exactly that state.
    {
      title: 'settings.diagnostics.logTitle',
      hint: 'settings.diagnostics.logHint',
      rows: [
        { key: 'settings.diagnostics.logSearch', hint: 'settings.diagnostics.logSearchHint' },
        { key: 'settings.diagnostics.logSource', hint: 'settings.diagnostics.logSourceHint' },
        { key: 'settings.diagnostics.logFollow', hint: 'settings.diagnostics.logFollowHint' },
      ],
      body: ['settings.diagnostics.logEmpty', 'settings.diagnostics.logNoMatches', 'settings.diagnostics.logGap'],
    },
    // The optional copy on disk. The problem sentence and its advice are body
    // text for the same reason as the gap banner above, and for the same reason
    // the settings.json row on the maintenance card below indexes its own
    // conditional (i).
    {
      title: 'settings.diagnostics.fileTitle',
      rows: [
        { key: 'settings.diagnostics.fileOn', hint: 'settings.diagnostics.fileOnHint' },
        { key: 'settings.diagnostics.fileSize', hint: 'settings.diagnostics.fileSizeHint' },
        { key: 'settings.diagnostics.fileKeep', hint: 'settings.diagnostics.fileKeepHint' },
        { key: 'settings.diagnostics.fileWhere', hint: 'settings.diagnostics.fileWhereHint' },
      ],
      body: [
        'settings.diagnostics.fileStateOff',
        'settings.diagnostics.fileProblem',
        'settings.diagnostics.fileProblemHint',
        'settings.diagnostics.fileEmpty',
        'settings.diagnostics.fileDownload',
      ],
    },
    {
      title: 'settings.dbmaint.title',
      hint: 'settings.dbmaint.hint',
      rows: [
        { key: 'settings.dbmaint.storeSize', hint: 'settings.dbmaint.storeSizeHint' },
        { key: 'settings.dbmaint.reclaimable', hint: 'settings.dbmaint.reclaimableHint' },
        // The (i) beside settings.json only appears while the file is not there
        // yet, which is the one state that needs explaining. Indexed by that
        // hint anyway: somebody searching for why it says "not written yet" is
        // searching in exactly that state.
        { key: 'settings.dbmaint.settingsSize', hint: 'settings.dbmaint.settingsMissingHint' },
        { key: 'settings.dbmaint.check', hint: 'settings.dbmaint.checkHint' },
        { key: 'settings.dbmaint.compact', hint: 'settings.dbmaint.compactHint' },
        { key: 'settings.dbmaint.analyze', hint: 'settings.dbmaint.analyzeHint' },
        { key: 'settings.dbmaint.interval', hint: 'settings.dbmaint.intervalHint' },
        { key: 'settings.dbmaint.compactOnSchedule', hint: 'settings.dbmaint.compactOnScheduleHint' },
      ],
      also: [
        'settings.dbmaint.inBundle',
        'settings.dbmaint.settingsMissing',
        'settings.dbmaint.running',
        'settings.dbmaint.intervalNever',
        'settings.dbmaint.interval30',
        'settings.dbmaint.interval90',
        'settings.dbmaint.interval180',
        'settings.dbmaint.confirmTitle',
      ],
      body: [
        'settings.dbmaint.neverRun',
        'settings.dbmaint.lastRun',
        'settings.dbmaint.nextRun',
        'settings.dbmaint.checkOk',
        'settings.dbmaint.checkFailed',
        'settings.dbmaint.checkFailedHelp',
        'settings.dbmaint.compacted',
        'settings.dbmaint.compactedNothing',
        'settings.dbmaint.analyzed',
        'settings.dbmaint.failed',
        'settings.dbmaint.diskFullHelp',
        'settings.dbmaint.skippedBusy',
        'settings.dbmaint.busy',
        'settings.dbmaint.confirmBody',
        'settings.dbmaint.loadFailed',
      ],
    },
    // The identity strip, drawn after the maintenance card. uid/gid/umask are
    // in `also` and not in `rows`: they are drawn by a local Stat component,
    // which emits no data-glim-label, so calling them rows would make every one
    // of them fail the row lookup and fall through to "that row is not on
    // screen" - the search telling the reader something untrue about their own
    // page.
    {
      title: 'settings.owner.identityTitle',
      hint: 'settings.owner.identityHint',
      rows: [],
      also: ['settings.owner.uid', 'settings.owner.gid', 'settings.owner.umask', 'settings.owner.asked'],
      body: [
        'settings.owner.umaskUnknown',
        'settings.owner.envIgnored',
        'settings.owner.envHow',
        'settings.owner.desktopNote',
        'settings.owner.noOwners',
      ],
    },
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

  eventtargets: [
    {
      title: 'settings.eventTargets.title',
      hint: 'settings.eventTargets.titleHint',
      rows: [
        { key: 'settings.eventTargets.enabled', hint: 'settings.eventTargets.enabledHint' },
        { key: 'settings.eventTargets.name', hint: 'settings.eventTargets.nameHint' },
        { key: 'settings.eventTargets.url', hint: 'settings.eventTargets.urlHint' },
        { key: 'settings.eventTargets.method', hint: 'settings.eventTargets.methodHint' },
        { key: 'settings.eventTargets.headers', hint: 'settings.eventTargets.headersHint' },
        { key: 'settings.eventTargets.body', hint: 'settings.eventTargets.bodyHint' },
        { key: 'settings.eventTargets.events', hint: 'settings.eventTargets.eventsHint' },
        { key: 'settings.eventTargets.placeholders', hint: 'settings.eventTargets.placeholdersHint' },
        { key: 'settings.eventTargets.attempts', hint: 'settings.eventTargets.attemptsHint' },
        { key: 'settings.eventTargets.timeout', hint: 'settings.eventTargets.timeoutHint' },
        { key: 'settings.eventTargets.status', hint: 'settings.eventTargets.droppedHint' },
        { key: 'settings.eventTargets.testResult', hint: 'settings.eventTargets.testHint' },
      ],
      also: [
        'settings.eventTargets.add',
        'settings.eventTargets.remove',
        'settings.eventTargets.test',
        'settings.eventTargets.testBusy',
        'settings.eventTargets.testSent',
        'settings.eventTargets.lastAttempt',
        'settings.eventTargets.lastOk',
        'settings.eventTargets.attemptsDefault',
      ],
      // The settings.eventTargets.problem.* sentences are deliberately NOT here.
      // They are built as `settings.eventTargets.problem.${code}` from whatever
      // the server classified the last failure as, so no source mentions any of
      // them literally and the reverse check in check-settings-search.mjs would
      // (correctly) call every one of them a result pointing at a row that is
      // not there.
      body: [
        'settings.eventTargets.empty',
        'settings.eventTargets.emptyHint',
        'settings.eventTargets.eventsNone',
        'settings.eventTargets.eventsBurst',
        'settings.eventTargets.eventsReplay',
        'settings.eventTargets.placeholderUnused',
        'settings.eventTargets.leavesTheBox',
        'settings.eventTargets.statusUnknown',
        'settings.eventTargets.lastOkNever',
        'settings.eventTargets.sentCount',
        'settings.eventTargets.dropped',
        'settings.eventTargets.testDuration',
        'settings.eventTargets.testTruncated',
        'settings.eventTargets.testEmptyBody',
        'settings.eventTargets.testNoAnswer',
      ],
    },
  ],

  shortcuts: [
    // The card titles only. Its rows ARE the command names, and the command
    // palette (mod+k) already searches exactly those - a second box beside it
    // offering the same list is one search too many. The per-group cards below
    // it are titled from commands.group.*, which the palette also already
    // groups by.
    { title: 'settings.nav.shortcuts', hint: 'settings.shortcuts.subtitle', rows: [] },
    // The list keys are the exception to the paragraph above, and for the
    // reason it gives: they are NOT commands, so the palette does not carry
    // them and this card is the only place they are written down. Its own rows
    // are a static table of key/meaning pairs rather than controls, so the card
    // is indexed by its title and hint and the table is left alone - the same
    // shape the per-group command cards have.
    { title: 'settings.shortcuts.listTitle', hint: 'settings.shortcuts.listHint', rows: [] },
  ],
};
