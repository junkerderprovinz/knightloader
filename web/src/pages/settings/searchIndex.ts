// What the settings search knows about: every page, every card on it, and the
// caption and (i) text of every row, as translation keys. The search resolves
// them through `t`, so the index is in the reader's language.
//
// It is written by hand rather than derived, because settings pages reach the
// catalogue through several helpers, keep keys in other files, share files
// between rail entries and do not map key prefixes to pages.
// web/check-settings-search.mjs keeps it in step with the pages; `--dump`
// prints what the pages draw.
//
// The import is type-only, since the check script reads this file as text.
import type { TranslationKey } from '../../lib/i18n';

/** One control's caption and its (i) text; the caption is what a result shows and where it jumps. */
export interface SettingsRow {
  /** The key passed as `label`, which Caption emits as `data-glim-label` for the jump. */
  key: TranslationKey;
  /** The exact key passed to its `hint`, when it has one. */
  hint?: TranslationKey;
}

/** One Card; `title` is its SectionTitle key, unique within the card. */
export interface SettingsCard {
  title: TranslationKey;
  /** SectionTitle's own optional `hint`. */
  hint?: TranslationKey;
  rows: SettingsRow[];
  /**
   * Short names on the card that are not rows, such as a badge, a caption
   * beside a bare Toggle or an aria-label. They show as results and jump to the
   * card, since there is no caption to scroll to.
   */
  also?: TranslationKey[];
  /** Prose on the card; searched, never shown as a result's text. */
  body?: TranslationKey[];
}

/**
 * SETTINGS_INDEX is keyed by the page id from featurePages, with the cards in
 * the order the page draws them. Only pages the server sends are offered.
 *
 * A module switch is a row keyed settings.module.<id>, except on a card titled
 * by that module: there the switch's caption is the title, so it has no row of
 * its own and its (i) goes in `body`. Modules.tsx finds the switch either way.
 */
export const SETTINGS_INDEX: Record<string, SettingsCard[]> = {
  modules: [
    // The rows are the modules the server sent, which vary per deployment.
    { title: 'settings.modules.sectionShipped', hint: 'settings.modules.fixedAtBuild', rows: [] },
    { title: 'settings.modules.sectionDesktop', rows: [] },
    { title: 'settings.modules.sectionNotBuilt', rows: [] },
  ],

  collector: [
    {
      title: 'settings.sectionLinkIntake',
      hint: 'settings.linkIntakeHint',
      rows: [
        { key: 'settings.module.cnl', hint: 'settings.linkIntake.cnlHint' },
        { key: 'intake.clipboardWatch', hint: 'intake.clipboardWatchHint' },
        { key: 'settings.autoStart', hint: 'settings.autoStartHint' },
        { key: 'settings.module.watch', hint: 'settings.watchDirHint' },
      ],
      // What the watch folder's (i) adds while there is nothing to switch on,
      // or a parked folder to bring back.
      body: ['settings.linkIntake.watchPickFolder', 'settings.linkIntake.watchParked'],
    },
    {
      title: 'settings.downloads.collectorTitle',
      rows: [
        { key: 'settings.downloads.autoConfirmDelay', hint: 'settings.downloads.autoConfirmDelayHint' },
        { key: 'settings.downloads.onDupes', hint: 'settings.downloads.onDupesHint' },
        { key: 'settings.downloads.addAtTop', hint: 'settings.downloads.addAtTopHint' },
      ],
      // What the countdown row reads while its switch is off.
      body: ['settings.downloads.autoConfirmOff'],
    },
    {
      title: 'settings.module.crawler',
      rows: [
        { key: 'settings.crawl.depth', hint: 'settings.crawl.depthHint' },
        { key: 'settings.crawl.maxPages', hint: 'settings.crawl.maxPagesHint' },
        { key: 'settings.crawl.sameHost', hint: 'settings.crawl.sameHostHint' },
        { key: 'settings.crawl.include', hint: 'settings.crawl.includeHint' },
        { key: 'settings.crawl.exclude', hint: 'settings.crawl.excludeHint' },
      ],
      body: ['settings.crawl'],
    },
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
      title: 'settings.advanced.reclaimTitle',
      rows: [{ key: 'settings.advanced.reclaimTrust', hint: 'settings.advanced.reclaimTrustHint' }],
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
        { key: 'settings.maxConcurrent', hint: 'settings.maxConcurrentHint' },
        { key: 'settings.maxPerHost', hint: 'settings.maxPerHostHint' },
        { key: 'settings.chunks', hint: 'settings.chunksHint' },
        { key: 'settings.globalSpeedLimit', hint: 'settings.speedHint' },
        { key: 'settings.maxRetries', hint: 'settings.maxRetriesHint' },
        { key: 'settings.resumeOnStart', hint: 'settings.resumeOnStartHint' },
        { key: 'settings.keepFinishedDays', hint: 'settings.keepFinishedDaysHint' },
        { key: 'settings.historyMax', hint: 'settings.historyMaxHint' },
        { key: 'settings.module.checksums', hint: 'settings.verifyChecksums' },
        { key: 'settings.preParser', hint: 'settings.preParserHint' },
      ],
    },
    {
      title: 'settings.stall.title',
      rows: [
        { key: 'settings.stall.enabled', hint: 'settings.stall.enabledHint' },
        { key: 'settings.stall.timeout', hint: 'settings.stall.timeoutHint' },
        { key: 'settings.stall.reconnect', hint: 'settings.stall.reconnectHint' },
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
      title: 'settings.module.feeds',
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
    // The folder check draws no captioned rows, so the role names go in `also`
    // and the sentences, where words like "chown" live, in `body`.
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
  ],

  archives: [
    {
      title: 'settings.module.extraction',
      rows: [
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

  // The General tab, Look.tsx's `{general && …}` half.
  look: [
    // The event rows come from NOTIFY_EVENTS and never appear in the page's
    // source, so quiet mode is the only row indexed.
    {
      title: 'notifications.title',
      hint: 'notifications.titleHint',
      rows: [{ key: 'notifications.quiet', hint: 'notifications.quietHint' }],
    },
    { title: 'settings.dialogs.title', hint: 'settings.dialogs.hint', rows: [] },
    {
      title: 'settings.look.updatesTitle',
      hint: 'settings.look.updatesHint',
      rows: [{ key: 'settings.look.updatesAutoInstall', hint: 'settings.look.updatesAutoInstallHint' }],
      // The daily-check switch has a hand-built caption with no anchor.
      also: ['settings.look.updatesAuto'],
      // The container build uses its own sentence for the auto-install hint.
      body: ['settings.look.updatesAutoInstallContainerHint'],
    },
    { title: 'settings.system.lifecycleTitle', hint: 'settings.system.unavailable', rows: [] },
    // The archive and "settings only" share one card, so the old card titles
    // are `also` entries and a search for backup still lands here.
    {
      title: 'settings.transfer.cardTitle',
      hint: 'settings.transfer.cardHint',
      rows: [{ key: 'settings.transfer.withSecrets', hint: 'settings.transfer.withSecretsHint' }],
      // Row headings and buttons, with no anchor of their own.
      also: [
        'settings.transfer.archiveLabel',
        'settings.transfer.settingsLabel',
        'settings.system.backupButton',
        'settings.system.restoreButton',
        'settings.transfer.export',
        'settings.transfer.import',
      ],
      body: ['settings.transfer.archiveText', 'settings.transfer.settingsText'],
      // The import preview's strings stay out: its dialog exists only once a
      // file is chosen.
    },
    // Drawn by Help.tsx at the foot of this page.
    { title: 'settings.about.title', rows: [] },
  ],

  // The `{appearance && …}` half of the same component.
  appearance: [
    { title: 'settings.shape', hint: 'settings.shapeHint', rows: [] },
    { title: 'settings.navLabels.title', hint: 'settings.navLabels.titleHint', rows: [] },
    { title: 'settings.bottomBarLabels.title', hint: 'settings.bottomBarLabels.titleHint', rows: [] },
    { title: 'settings.motion.title', hint: 'settings.motion.hint', rows: [] },
    {
      title: 'settings.colours',
      rows: [],
      // Every row here is a caption beside a bare Toggle, with no anchor.
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
        // The accent's (i) while rainbow mode owns the colours.
        'settings.accentRainbowOwns',
        'settings.rainbowHint',
        'settings.rainbowReactiveHint',
        'settings.rainbowRotateHint',
        'settings.rainbowPaletteHint',
      ],
    },
    { title: 'settings.theme', rows: [] },
    { title: 'lang.label', rows: [] },
  ],

  accounts: [
    {
      title: 'settings.accounts.setupTitle',
      rows: [{ key: 'settings.accounts.showInSidebar', hint: 'settings.accounts.showInSidebarHint' }],
    },
    {
      title: 'settings.accounts.freeTitle',
      rows: [{ key: 'settings.accounts.premiumOnly', hint: 'settings.accounts.premiumOnlyHint' }],
    },
  ],

  instances: [
    {
      title: 'settings.instances.setupTitle',
      rows: [
        { key: 'settings.module.federation' },
        { key: 'settings.instances.showInSidebar', hint: 'settings.instances.showInSidebarHint' },
      ],
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
      title: 'auth.twoFactor.title',
      hint: 'auth.twoFactor.hint',
      rows: [
        { key: 'auth.twoFactor.confirmLabel' },
        { key: 'auth.twoFactor.secretManual' },
        { key: 'auth.twoFactor.disablePrompt' },
      ],
      also: ['auth.twoFactor.enable', 'auth.twoFactor.disable', 'auth.twoFactor.codesTitle'],
      // The (i) inside the Set up button.
      body: ['auth.twoFactor.beforeYouStart'],
    },
    {
      title: 'auth.passkey.title',
      hint: 'auth.passkey.hint',
      rows: [{ key: 'auth.passkey.nameLabel', hint: 'auth.passkey.nameHint' }],
      also: ['auth.passkey.add', 'auth.passkey.unavailableTitle', 'auth.passkey.rename'],
    },
    {
      title: 'settings.access.cardTitle',
      hint: 'settings.access.phrase.body',
      rows: [],
      // The three badges in the card's header.
      also: [
        'settings.access.phrase.howButton',
        'settings.access.phrase.statusConnected',
        'settings.access.phrase.statusDisconnected',
        'settings.access.relay.none',
        'settings.access.relay.own',
        'settings.access.relay.project',
      ],
      // The phrase's own (i), shown once the phrase is on screen.
      body: ['settings.access.phrase.pasteHint'],
    },
    {
      title: 'settings.access.relay.title',
      hint: 'settings.access.relay.body',
      rows: [{ key: 'settings.access.relay.use', hint: 'settings.access.relay.leadProject' }],
      also: ['settings.access.relay.seesButton'],
    },
    {
      title: 'settings.access.ownRelay.title',
      hint: 'settings.access.ownRelay.body',
      rows: [
        { key: 'settings.access.ownRelay.use', hint: 'settings.access.ownRelay.lead' },
        { key: 'settings.access.ownRelay.serveLabel', hint: 'settings.access.ownRelay.serveHint' },
      ],
    },
    {
      title: 'settings.access.tokens.title',
      hint: 'settings.access.tokens.intro',
      rows: [{ key: 'settings.module.downloadclient' }],
      // The rights picker in the window that creates a token.
      also: ['settings.access.tokens.rights'],
      // The (i) of the window that shows a new token, and of the rights picker.
      body: ['settings.access.tokens.howToUse', 'settings.access.tokens.rightsHint'],
    },
  ],

  advanced: [
    {
      title: 'settings.advanced.retryTitle',
      rows: [{ key: 'settings.advanced.retryTries', hint: 'settings.advanced.retryTriesHint' }],
    },
    // The key table has its own search box and stays out of the index.
    { title: 'settings.advanced.allSettings', rows: [] },
  ],

  rules: [
    {
      title: 'settings.rules.setupTitle',
      // One switch at a time, for the list the selector shows; Rules.tsx
      // shows the list a jump asks for.
      rows: [
        { key: 'settings.module.packagizer', hint: 'settings.rules.setSwitchHint' },
        { key: 'settings.module.linkfilter', hint: 'settings.rules.setSwitchHint' },
      ],
      also: ['settings.rules.flavourLabel', 'settings.rules.stopAfterMatch'],
      body: ['settings.rules.stopHint'],
    },
    {
      title: 'settings.rules.listTitle',
      rows: [],
      // The rule editor's fields carry aria-labels and exist only while a rule
      // is open.
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
      // The (i) inside the Import and Export buttons, and the variables menu's.
      body: ['settings.rules.importTitle', 'settings.rules.exportTitle', 'settings.rules.variablesHint'],
    },
    { title: 'settings.rules.testTitle', rows: [], body: ['settings.rules.testHint'] },
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
        { key: 'settings.categories.premiumOnly', hint: 'settings.categories.premiumOnlyHint' },
        { key: 'settings.categories.notify', hint: 'settings.categories.notifyHint' },
      ],
      also: ['settings.categories.notifyNone'],
      body: ['settings.pathVars', 'settings.categories.notifyEmpty', 'settings.categories.notifyMissing'],
    },
  ],

  network: [
    {
      title: 'settings.module.connections',
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
      // The user name's (i) on a SOCKS4 row.
      body: ['settings.connections.stateSocks4'],
    },
    {
      title: 'settings.module.reconnect',
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
      // The method's sub-heading belongs to this card.
      also: ['settings.reconnect.requests'],
      body: ['settings.reconnect.requestsHint', 'settings.reconnect.upnpState'],
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
        'settings.reconnect.runNow',
        'settings.reconnect.stateConfigured',
        'settings.reconnect.stateNotConfigured',
        'settings.reconnect.stateBusy',
        'settings.reconnect.stateIdle',
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
      title: 'settings.hostRules.title',
      hint: 'settings.hostRules.titleHint',
      rows: [
        { key: 'settings.hostRules.pattern', hint: 'settings.hostRules.patternHint' },
        { key: 'props.backend', hint: 'settings.hostRules.preferHint' },
        { key: 'settings.hostRules.backends', hint: 'settings.hostRules.backendsHint' },
        { key: 'settings.hostRules.maxPerHost', hint: 'settings.hostRules.maxPerHostHint' },
        { key: 'settings.hostRules.chunks', hint: 'settings.hostRules.chunksHint' },
        { key: 'settings.hostRules.never', hint: 'settings.hostRules.neverHint' },
        { key: 'settings.hostRules.retryDelay', hint: 'settings.hostRules.retryDelayHint' },
        { key: 'settings.hostRules.retryMax', hint: 'settings.hostRules.retryMaxHint' },
        { key: 'settings.hostRules.retryTries', hint: 'settings.hostRules.retryTriesHint' },
      ],
    },
  ],

  resolvers: [
    {
      title: 'settings.resolvers.toolsTitle',
      hint: 'settings.resolvers.toolsHint',
      rows: [
        { key: 'settings.module.ytdlp' },
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
      hint: 'settings.resolvers.about',
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
        // The host and jar boxes live in CookieJarDialog, so only the button
        // that opens it is a row.
        { key: 'cookies.add' },
      ],
    },
    {
      title: 'settings.resolvers.variantDefaults',
      hint: 'settings.resolvers.presetsHint',
      rows: [{ key: 'settings.resolvers.presetHost', hint: 'settings.resolvers.presetHostHint' }],
      // The per-host overrides are bare selects named by aria-label.
      also: ['settings.resolvers.moduleUnavailable'],
      body: ['settings.resolvers.moduleUnavailableHint'],
    },
  ],

  torrents: [
    {
      title: 'settings.torrents.seedingTitle',
      rows: [
        { key: 'settings.module.torrents' },
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
      // The port's second paragraph, and the (i) inside the mapping button.
      body: ['settings.torrents.portRestartHint', 'settings.torrents.portMapHint', 'settings.torrents.portMapNeedsPort'],
    },
    {
      title: 'settings.torrents.networkTitle',
      hint: 'settings.torrents.privateNote',
      rows: [
        { key: 'settings.torrents.dht', hint: 'settings.torrents.dhtHint' },
        { key: 'settings.torrents.pex', hint: 'settings.torrents.pexHint' },
      ],
    },
  ],

  captcha: [
    // The solver rows are named by the server.
    {
      title: 'settings.captcha.orderTitle',
      hint: 'settings.captcha.orderHint',
      rows: [{ key: 'settings.module.captcha' }],
      body: ['settings.captcha.orderEmpty'],
    },
    {
      title: 'settings.captcha.whenTitle',
      rows: [
        { key: 'settings.captcha.onlyUnwatched', hint: 'settings.captcha.onlyUnwatchedHint' },
        { key: 'settings.captcha.wait', hint: 'settings.captcha.waitHint' },
      ],
    },
  ],

  automation: [
    {
      title: 'settings.schedule.statusTitle',
      rows: [{ key: 'settings.schedule.suspend', hint: 'settings.schedule.suspendHint' }],
      also: [
        'settings.schedule.suspend.hour',
        'settings.schedule.suspend.threeHours',
        'settings.schedule.suspend.midnight',
        'settings.schedule.suspend.open',
      ],
      body: ['settings.schedule.stateNow.suspendedOpen'],
    },
    {
      title: 'settings.module.scheduler',
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
      // The menu entries and buttons have no caption, so they land on the card.
      also: [
        'settings.downloads.idleActionPause',
        'settings.downloads.idleActionQuit',
        'settings.downloads.idleActionCommand',
        'settings.downloads.idleActionSuspend',
        'settings.downloads.idleCommandCheck',
        'settings.downloads.idleCommandRun',
      ],
      // Prose the card carries without a caption of its own.
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
    {
      title: 'settings.module.eventtargets',
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
      // The problem.* sentences are built from a server code, so no source
      // names them.
      body: [
        'settings.eventTargets.empty',
        'settings.eventTargets.emptyHint',
        'settings.eventTargets.eventsNone',
        'settings.eventTargets.eventsBurst',
        'settings.eventTargets.replaysHint',
        'settings.eventTargets.placeholderUnused',
        'settings.eventTargets.leavesTheBox',
        'settings.eventTargets.statusUnknown',
        'settings.eventTargets.lastOkNever',
        'settings.eventTargets.sentCount',
        'settings.eventTargets.dropped',
        'settings.eventTargets.took',
        'settings.eventTargets.testTruncated',
        'settings.eventTargets.testEmptyBody',
        'settings.eventTargets.testNoAnswer',
      ],
    },
    {
      title: 'settings.module.scripting',
      rows: [
        { key: 'settings.scripts.name' },
        { key: 'settings.scripts.trigger', hint: 'settings.scripts.triggerHint' },
        { key: 'settings.scripts.timeout', hint: 'settings.scripts.timeoutHint' },
        { key: 'settings.scripts.code' },
      ],
      body: ['settings.scripts.empty', 'settings.scripts.emptyHint'],
    },
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
      // The card's badge is a state looked up from a server id.
      body: [
        // A `tip=`, which the check script does not scan.
        'settings.health.oldHealthHint',
        'settings.health.sampled',
        'settings.health.loadFailed',
      ],
    },
    {
      title: 'settings.health.parts',
      hint: 'settings.health.partsHint',
      // Every row is built from server ids, so none can be indexed.
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
      body: ['settings.health.nothingWaiting', 'settings.health.nothingFailed', 'settings.health.diskWhere'],
    },
    {
      title: 'settings.module.metrics',
      rows: [{ key: 'settings.health.scrapeUrl' }],
      also: ['settings.health.scrapeCopy', 'settings.health.scrapeCopied'],
      body: ['settings.health.scrapeHint', 'settings.health.scrapeOffHint'],
    },
  ],

  diagnostics: [
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
      // The (i) inside the download button.
      body: ['settings.diagnostics.downloadHint'],
    },
    // Every line of the start report is a check the server named.
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
    // The self-test lines have no captions, so the check names go in `also`
    // and the advice, with words like "nginx" or "TZ", in `body`.
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
        'settings.selftest.proxy.prefix.mismatchAdvice',
        'settings.selftest.proxy.ws.failedAdvice',
      ],
    },
    // The gap banner is indexed although it only shows after lines were lost.
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
    // The problem sentences only show while the file is not written, which is
    // when somebody searches for them.
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
        // Indexed with the (i) that only shows while the file is missing.
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
    // uid, gid and umask are local Stat readings without data-glim-label, so
    // they go in `also`.
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
    {
      title: 'settings.browsertools.phoneTitle',
      hint: 'settings.browsertools.phoneHint',
      rows: [],
      also: [
        'settings.browsertools.storeAndroid',
        'settings.browsertools.download',
        'settings.browsertools.qrCode',
        'settings.browsertools.installPwaLabel',
      ],
    },
    // Only one of the next two is drawn: the desktop app in a container, a
    // server install in the desktop app.
    {
      title: 'settings.browsertools.desktopTitle',
      hint: 'settings.browsertools.desktopHint',
      rows: [],
      body: ['settings.browsertools.desktopArchHint'],
    },
    {
      title: 'settings.browsertools.serverTitle',
      hint: 'settings.browsertools.serverHint',
      rows: [],
      also: ['settings.browsertools.sourceZip'],
      body: ['settings.browsertools.dockerHint'],
    },
    {
      title: 'settings.browsertools.bookmarkletTitle',
      rows: [],
      body: ['settings.browsertools.bookmarkletStep1', 'settings.browsertools.bookmarkletStep2'],
    },
    { title: 'settings.browsertools.extensionTitle', rows: [], also: ['settings.browsertools.installLabel'] },
  ],

  shortcuts: [
    // The command palette already searches the command names, so only the
    // card titles are here.
    { title: 'settings.nav.shortcuts', hint: 'settings.shortcuts.subtitle', rows: [] },
    // The list keys are not commands, so this card is the only place they are
    // written down; its table is left alone.
    { title: 'settings.shortcuts.listTitle', hint: 'settings.shortcuts.listHint', rows: [] },
  ],
};
