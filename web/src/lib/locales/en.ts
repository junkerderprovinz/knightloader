// English is the source of truth. Every other locale is typed as Dict, so a
// missing or stray key is a compile error rather than a silent fallback.
export const en = {
  'nav.overview': 'Overview',
  'nav.collector': 'Collector',
  'nav.downloads': 'Downloads',
  'nav.instances': 'Instances',
  'nav.accounts': 'Accounts',
  'nav.settings': 'Settings',
  'nav.workingTitle': 'working title',
  'theme.dark': 'Dark',
  'theme.light': 'Light',
  'theme.toggle': 'Toggle theme',
  'lang.label': 'Language',
  'lang.open': 'Change language',
  'lang.close': 'Close language picker',

  'status.collected': 'Collected',
  'status.queued': 'Queued',
  'status.running': 'Running',
  'status.paused': 'Paused',
  'status.extracting': 'Extracting',
  'status.done': 'Done',
  'status.error': 'Error',

  'task.pause': 'Pause',
  'task.resume': 'Resume',
  'task.start': 'Start',
  'task.restart': 'Restart',
  'task.remove': 'Remove',
  'task.ready': 'Ready to download',
  'task.left': 'left',
  'task.file': 'file',
  'task.files': 'files',
  'task.ungrouped': 'Ungrouped',

  // Badge-sized names for a typed failure. They sit next to a task's own error
  // text, so each one is the category and not the sentence.
  'task.reason.gone': 'Gone',
  'task.reason.auth': 'Sign-in required',
  'task.reason.limit': 'Hoster limit',
  'task.reason.unavailable': 'Host unavailable',
  'task.reason.network': 'Network',
  'task.reason.diskFull': 'Disk full',
  'task.reason.unsupported': 'No backend',
  'task.reason.captcha': 'Captcha',
  'task.reason.cancelled': 'Cancelled',

  'overview.title': 'Overview',
  'overview.subtitle': 'Everything at a glance.',
  'overview.totalSpeed': 'Total download speed',
  'overview.active': 'Active',
  'overview.queued': 'Queued',
  'overview.inCollector': 'In collector',
  'overview.done': 'Done',
  'overview.errors': 'Errors',
  'overview.recent': 'Recent',
  'overview.noDownloads': 'No downloads yet.',
  'overview.instances': 'Instances',
  'overview.idle': 'idle',
  'overview.peak': 'peak',
  'common.loading': 'Loading…',
  'common.loadFailed': 'Could not load this. Is the server reachable?',
  'common.retry': 'Try again',

  // FirstTouchHint's own copy (components/FirstTouchHint.tsx) — one honest
  // sentence or two about what a page actually does, shown once per surface
  // and never again once dismissed (see that file's own doc comment). Keyed
  // by page, not by component, because a hint is a fact about the page it
  // sits on rather than a reusable string.
  'hint.collector.title': 'Links are staged here, not downloaded yet',
  'hint.collector.body':
    'Everything you add lands in this collector first, where you can check names and sizes before anything starts. Start it here, or turn on auto-start in settings if you never want that pause.',
  'hint.downloads.title': 'What is actually running',
  'hint.downloads.body':
    'This is the transfer queue — links you have started, whether they are running, waiting their turn, or already finished. Nothing you have merely staged in the collector shows up here yet.',
  'hint.instances.title': 'A peer, not a copy',
  'hint.instances.body':
    'Adding another KnightLoader here does not move or sync anything — this instance simply calls that one’s own API to show and control its queue, the same way a browser would.',

  'empty.downloadsTitle': 'Nothing downloading yet',
  'empty.downloadsHint': 'Add links in the collector, then start them.',

  'collector.title': 'Collector',
  'collector.subtitle': 'Paste or drop links — they are analysed and staged, then you start them.',
  'collector.placeholder': 'Paste links — one URL per line — or drop them here…  (Ctrl+Enter to add)',
  'collector.package': 'Package (optional)',
  'collector.add': 'Add to collector',
  'collector.empty': 'The collector is empty. Paste some links above to stage them.',
  'collector.staged': 'staged',
  'collector.selected': 'selected',
  'collector.startSelected': 'Start selected',
  'collector.startAll': 'Start all',
  'collector.remove': 'Remove',
  'collector.toastStaged': 'Staged {n} link(s)',
  'collector.toastSkipped': 'Staged {n} link(s), {skipped} already known',
  'collector.toastNone': 'No valid links found',
  'collector.toastStarted': 'Started {n} download(s)',
  'collector.selectAll': 'Select all',
  'collector.selectNone': 'Clear selection',
  'collector.removeOffline': 'Remove offline',
  'collector.movePrompt': 'Move the selected links to which package?',
  'collector.toastMoved': 'Moved {n} link(s) to “{pkg}”',
  'collector.move': 'Move to package',

  'downloads.title': 'Downloads',
  'downloads.thisInstance': 'This instance',
  'downloads.totalSpeed': 'Total speed',
  'downloads.pauseAll': 'Pause all',
  'downloads.resumeAll': 'Resume all',
  'downloads.retryFailed': 'Retry failed',
  'downloads.clearFinished': 'Clear finished',
  'downloads.filterPlaceholder': 'Filter by name…',
  'downloads.filterAll': 'All',
  'downloads.filterActive': 'Active',
  'downloads.filterDone': 'Done',
  'downloads.filterErrors': 'Errors',
  'downloads.noMatch': 'Nothing matches this filter.',
  'downloads.emptyLead': 'Nothing downloading yet. Add links in the',
  'downloads.emptyTail': 'and start them.',
  'downloads.finished': '{name} finished',
  'downloads.failed': '{name} failed',

  'instances.title': 'Instances',
  'instances.subtitle': 'View and control every KnightLoader from one dashboard.',
  'instances.thisInstance': 'This instance',
  'instances.add': 'Add an instance',
  'instances.name': 'Name',
  'instances.url': 'URL',
  'instances.addButton': 'Add instance',
  'instances.offlineWarning': 'Added, but the instance did not answer (offline?).',
  'instances.open': 'Open',
  'instances.online': 'Online',
  'instances.offline': 'Offline',
  'instances.metricActive': 'Active',
  'instances.metricTasks': 'Tasks',
  'instances.metricSpeed': 'Speed',
  'instances.removeTitle': 'Remove {name}',

  'accounts.title': 'Accounts',
  'accounts.subtitle': 'Premium and debrid accounts stay yours, stored encrypted on this instance.',
  'accounts.connected': 'Connected',
  'accounts.notConnected': 'Not connected',
  'accounts.connect': 'Connect',
  'accounts.replace': 'Replace key',
  'accounts.disconnect': 'Disconnect',
  'accounts.saved': 'Saved.',
  'accounts.keyLabel': '{service} API key',
  'accounts.keyStored': 'A key is stored. Enter a new one to replace it.',
  'accounts.keyHint': 'Takes effect immediately; stored encrypted on this instance.',
  'accounts.placeholder': 'Paste your key…',
  'accounts.more': 'Connect any of the supported services — links are routed to whichever one covers the host.',
  'accounts.blurb.torbox': 'Debrid — unlocks a large hoster catalogue into direct downloads.',
  'accounts.blurb.alldebrid': 'Debrid — one-click unlock for a large hoster catalogue.',
  'accounts.blurb.realdebrid': 'Debrid — unrestricts hoster links into direct downloads.',

  'settings.title': 'Settings',
  'settings.subtitle': 'Concurrency, speed and post-processing.',
  'settings.maxConcurrent': 'Max simultaneous',
  'settings.maxPerHost': 'Max per host',
  'settings.speedLimit': 'Speed limit (KiB/s, 0 = ∞)',
  'settings.speedHint': 'A total for every download, applied while they run.',
  'settings.extract': 'Extract archives after download',
  'settings.deleteArchive': 'Delete archive after extraction',
  'settings.autoStart': 'Start added links immediately (skip the collector)',
  'settings.save': 'Save',
  'settings.saved': 'Saved.',

  'settings.sectionDownloads': 'Downloads',
  'settings.sectionArchives': 'Archives',
  'settings.sectionSecurity': 'Access',
  'settings.downloadDir': 'Download folder',
  'settings.downloadDirHint': 'Absolute path. Leave empty for the built-in folder.',
  'settings.subfolderByPackage': 'Put each package in its own subfolder',
  'settings.maxRetries': 'Automatic retries',
  'settings.resumeOnStart': 'After a restart',
  'settings.resumeOnStartHint': 'A restarted transfer begins again from the start, and the partial file already on disk meets the collision policy. That is why the cautious setting is the default.',
  'settings.resume.never': 'Nothing',
  'settings.resume.running': 'What was running',
  'settings.resume.all': 'Everything unfinished',
  'settings.keepFinishedDays': 'Keep finished for (days)',
  'settings.keepFinishedDaysHint': '0 keeps them in the list forever. The file on disk is never touched, and the history keeps the record.',
  'settings.historyMax': 'History entries',
  'settings.historyMaxHint': '0 keeps every entry.',
  'settings.maxRetriesHint': 'A failed download is tried again with a growing delay.',
  'settings.archivePasswords': 'Archive passwords',
  'settings.archivePasswordsHint': 'One per line. Tried in order when an archive is encrypted.',
  'settings.lockOn': 'A password is required to use this instance.',
  'settings.lockOff': 'Anyone who can reach this instance can use it.',
  'settings.passwordCurrent': 'Current password',
  'settings.passwordNew': 'New password',
  'settings.passwordHint': 'At least 8 characters. Leave empty to remove the lock.',
  'settings.setPassword': 'Set password',
  'settings.passwordSaved': 'Password updated.',
  'auth.title': 'This instance is locked',
  'auth.subtitle': 'Enter the password to continue.',
  'auth.password': 'Password',
  'auth.signIn': 'Sign in',
  'auth.signOut': 'Sign out',
  'auth.wrong': 'Wrong password.',
  'accounts.test': 'Test',
  'accounts.testing': 'Testing…',
  'accounts.ok': 'Working',
  'accounts.failed': 'Not working',
  'accounts.unchecked': 'Not checked',
  'accounts.notConfigured': 'No credential stored',
  'accounts.fromEnv': 'Set by the container',
  'accounts.hosts': 'supported hosters',
  'task.recheck': 'Recheck',
  'task.online': 'Online',
  'task.offline': 'Offline',
  'task.retryPending': 'Retrying automatically',
  'task.folder': 'Folder',
  'task.password': 'Archive password',
  'task.priorityUp': 'Raise priority',
  'task.priorityDown': 'Lower priority',
  'task.moveTop': 'Move to top',
  'task.moveBottom': 'Move to bottom',
  'task.removeWithFiles': 'Remove and delete files',
  'task.applied': 'Applied.',
  'select.all': 'Select all',
  'select.none': 'Clear selection',
  'select.count': 'selected',
  'settings.removePassword': 'Remove password',

  'common.cancel': 'Cancel',
  'pkg.moveTitle': 'Move to a package',
  'pkg.name': 'Package name',
  'pkg.merge': 'Merge into one package',
  'pkg.splitByHost': 'Split by hoster',
  'settings.pathVars': 'Variables: <jd:packagename>, <jd:hoster>, <jd:filename>, <jd:date>',
  'settings.watchDir': 'Watch folder',
  'settings.watchDirHint': 'Links dropped here as .txt or .crawljob files are picked up automatically.',
  'settings.crawl': 'Follow pages and collect the files they link to',
  'collector.crawling': 'Scanning the page…',
  'task.checksumOk': 'Checksum verified',
  'task.checksumFail': 'Checksum does not match',
  'settings.verifyChecksums': 'Verify a finished download against a checksum, when one came with it',
  'settings.preParser': 'Scan pasted or dropped text for links',
  'settings.preParserHint':
    'Finds links anywhere in what you paste or drop, not only one clean URL per line: it rejoins one a mail client wrapped across a line break, and reads a bare host and path with no http:// in front as a link too. Off takes each line exactly as typed, the way this always worked before.',
  'settings.sectionLook': 'Look',
  'settings.shape': 'Corners',
  'settings.shapeHint': 'Applies to cards, buttons, tabs, inputs and badges at once.',
  'settings.shape.round': 'Round',
  'settings.shape.soft': 'Soft',
  'settings.shape.square': 'Square',
  'settings.accent': 'Accent colour',
  'settings.accentHint': 'The one colour used for activity. Text on it is picked for contrast.',
  'settings.accentPresets': 'Presets',
  'settings.accentReset': 'Default',
  'queue.stop': 'Stop queue',
  'queue.start': 'Start queue',
  'queue.halted': 'Queue stopped. Running downloads finish; nothing new starts.',
  'queue.stopMark': 'Stop after this one',
  'queue.stopMarkOn': 'Stopping after this one',
  'queue.limit': 'Limit',
  'queue.play': 'Play',
  'queue.pause': 'Pause',
  'queue.hardStop': 'Stop',
  'queue.hardStopConfirmTitle': 'Stop every transfer now?',
  'queue.hardStopConfirmBody': '{n} transfer(s) in flight would stop immediately instead of finishing. {detail}',
  'queue.hardStopConfirmLoss': '{bytes} already downloaded cannot be resumed and would be fetched again.',
  'queue.hardStopConfirmUnknown': '{n} more have not been checked for resume support.',
  'queue.hardStopConfirmSafe': 'Everything running can resume from where it stopped.',
  'queue.hardStopConfirmCancel': 'Cancel',
  'queue.hardStopConfirmProceed': 'Stop now',
  'settings.rainbow': 'Rainbow mode',
  'settings.rainbowHint': 'Instead of one accent, a palette of eight handed out by position, so a long list reads as separate rows.',
  'settings.rainbowOn': 'Use the palette',
  'settings.rainbowReactive': 'Reactive mode',
  'settings.rainbowReactiveHint': 'Quiet until touched: colour on hover and on what is running',
  'settings.rainbowRotate': 'Color rotation',
  'settings.rainbowRotateHint': 'Shuffle where the palette starts',
  'settings.rainbowPalette': 'Palette colour',
  'settings.look.saveFailed': 'Could not save: {error}',
  'settings.theme': 'Theme',
  'queue.limitUnit': 'Unit',

  'common.show': 'Show',
  'common.hide': 'Hide',
  'common.dismiss': 'Dismiss',
  'select.row': 'Select this link',

  'columns.name': 'Name',
  'columns.size': 'Size',
  'columns.progress': 'Progress',
  'columns.speed': 'Speed',
  'columns.eta': 'ETA',
  'columns.status': 'Status',
  'columns.host': 'Host',
  'columns.added': 'Added',
  'columns.finished': 'Finished',
  'columns.comment': 'Comment',
  'columns.resolver': 'Backend',
  'columns.source': 'Source',
  'columns.enabled': 'Enabled',
  'columns.menuTitle': 'Columns',
  'columns.reset': 'Reset columns',
  'columns.headerHint': 'Drag a header to reorder a column, drag its edge to resize it, double-click the edge to put the width back. Right-click the header for the column list.',
  'columns.alwaysShown': 'This column is always shown.',
  'columns.lastVisible': 'A list needs at least one column.',
  'columns.resizeHint': 'Drag to resize, double-click to reset',
  'columns.sortHint': 'Sort by this column',

  'list.sortedView': 'Sorted view',
  'list.sortedViewTip': 'Sorting changes the order you see, not the order that runs. Downloads still start in queue order.',
  'list.queueOrder': 'Back to queue order',
  'list.controls': 'List controls',
  'list.actions': 'List actions',
  'list.failed': 'That did not work: {error}',
  'list.optionsFailed': 'Could not load the clean-up entries from the server.',

  'task.enable': 'Enable',
  'task.disable': 'Disable',
  'task.switchFailed': 'The switch could not be changed.',
  'task.uncheckable': 'Host would not say',
  'task.onlineRatio': '{online}/{total} online',
  'task.expand': 'Expand',
  'task.collapse': 'Collapse',

  'search.placeholder': 'Search this list…',
  'search.in': 'Search in',
  'search.any': 'Everything',
  'search.name': 'Name',
  'search.host': 'Host',
  'search.package': 'Package',
  'search.comment': 'Comment',
  'search.url': 'Link',
  'search.clear': 'Clear the search',
  'search.hint': 'Pick one field to search, or “Everything” to search all of them at once.',
  'search.shown': '{n} of {total} shown',

  'filter.label': 'Quick filters',
  'filter.clear': 'Show everything',
  'filter.running': 'Running',
  'filter.queued': 'Queued',
  'filter.paused': 'Paused',
  'filter.finished': 'Finished',
  'filter.failed': 'Failed',
  'filter.online': 'Online',
  'filter.offline': 'Offline',
  'filter.uncheckable': 'Could not be checked',
  'filter.unchecked': 'Not checked',
  'filter.disabled': 'Switched off',
  'filter.held': 'Held',

  'cleanup.menu': 'Clean up…',
  'cleanup.menuLabel': 'Clean-up entries',
  'cleanup.finished': 'Remove finished',
  'cleanup.offline': 'Remove offline',
  'cleanup.disabled': 'Remove switched-off links',
  'cleanup.duplicates': 'Remove duplicates',
  'cleanup.incompleteArchives': 'Remove incomplete archives',
  'cleanup.what.finished': 'Every download on this instance that has completed.',
  'cleanup.what.offline': 'Every link on this instance that a host answered about and said is gone. Links nobody could check are left alone.',
  'cleanup.what.disabled': 'Every link on this instance that is switched off.',
  'cleanup.what.duplicates': 'Second copies of a download already in the list. The copy that is furthest along is the one kept.',
  'cleanup.what.incompleteArchives': 'Multi-volume sets with a dead part. The rest of the set can never be unpacked.',
  'cleanup.finishedKeepsFiles': 'Finished downloads are only taken off the list. The files are the reason they were downloaded, so this entry never deletes anything on disk.',
  'cleanup.localOnly': 'Clean-up always runs on this instance, not on the one you are looking at. Switch back to “This instance” to use it.',
  'cleanup.nothing': 'Nothing matches “{what}” right now, so nothing was removed.',
  'cleanup.failed': 'Could not work out what this would remove: {error}',

  'remove.title': 'Delete the downloaded files too?',
  'remove.count': '{n} download(s) will be taken off the list.',
  'remove.fromList': 'Remove from the list',
  'remove.withFiles': 'Remove and delete the files',
  'remove.filesKept': 'The files already on disk stay where they are.',
  'remove.filesGone': 'This also erases {files} file(s), {bytes} on disk. It cannot be undone.',
  'remove.noFiles': 'Nothing has been written to disk yet.',
  'remove.done': 'Removed {n} download(s).',
  'remove.keys': 'Del takes the selected rows off the list. Shift+Del also deletes their files.',

  'menu.label': 'Actions for the selected downloads',
  'menu.packageLabel': 'Actions for this package',
  'menu.more': 'More',
  'menu.enable': 'Switch on',
  'menu.disable': 'Switch off',
  'menu.hold': 'Hold',
  'menu.release': 'Release',
  'menu.force': 'Force to the front',
  'menu.unforce': 'Stop forcing',
  'menu.setFolder': 'Set download folder…',
  'menu.collapseAll': 'Collapse all packages',
  'menu.expandAll': 'Expand all packages',
  'collector.checkAll': 'Check all',

  'skipped.summary': '{n} link(s) were not added',
  'skipped.info': 'Links the collector recognised as already staged. Nothing was lost — the copy already in the list is the one that will download. Clearing forgets this note; it does not add anything back.',
  'skipped.clear': 'Clear',
  'skipped.clearFailed': 'Could not clear the list. Is the server reachable?',

  'collector.filedrop.prompt': 'Have a .torrent or container file? Drop it here — or drop a link.',
  'collector.filedrop.info': 'A .torrent is read here — for more than one file inside it, you get to choose which ones to fetch before it is added. A plain link list (.txt) is read here too and staged like a paste; a .dlc, .ccf or .rsdf is encrypted and its key belongs to a service, so it is handed to the headless JDownloader backend, which has one. A link dropped here stages the same way the paste box above does.',

  'container.prompt': 'Have a container file? Drop a .txt, .dlc, .ccf or .rsdf here.',
  'container.info': 'A plain link list is read here and staged like a paste. A .dlc, .ccf or .rsdf is encrypted and its key belongs to a service, so it is handed to the headless JDownloader backend, which has one. Without that backend it cannot be opened, and KnightLoader says so rather than guessing.',
  'container.choose': 'Choose a file',
  'container.uploading': 'Uploading…',
  'container.tooBig': 'it is larger than {max}, which no container is',
  'container.staged': 'Staged {n} link(s) from {file}.',
  'container.stagedIn': 'Staged {n} link(s) from {file} in “{pkg}”.',
  'container.alsoKnown': '{n} more were already in the list.',
  'container.allKnown': 'All {n} link(s) in {file} were already in the list.',
  'container.handed': '{file} is encrypted. It was handed to the JDownloader backend; its links appear here once it has fetched it (within {n}s).',
  'container.failed': '{file} was not taken: {reason}',

  // Wave 11.5D: uploading a .torrent file, and the file-tree step a
  // multi-file torrent shows before it is added. A magnet link needs none of
  // this - it already stages through the paste box above, the moment
  // internal/resolver/torrent.Resolver.Match recognises the scheme.
  'torrent.prompt': 'Have a .torrent file? Drop it here.',
  'torrent.info': 'A magnet link is staged through the paste box above - it needs no upload. A .torrent file is read here: for more than one file inside it, you get to choose which ones to fetch before it is added.',
  'torrent.choose': 'Choose a .torrent file',
  'torrent.uploading': 'Reading…',
  'torrent.staging': 'Adding…',
  'torrent.staged': 'Added {file} to the collector.',
  'torrent.stagedIn': 'Added {file} to the collector in “{pkg}”.',
  'torrent.duplicate': '{file} is already in the list.',
  'torrent.failed': '{file} was not taken: {reason}',
  'torrent.onlyOne': 'only one .torrent can be reviewed at a time',
  'torrent.tree.summary': '{n} of {total} file(s) selected ({size})',
  'torrent.tree.private': 'Private tracker',
  'torrent.tree.selectAll': 'Select all',
  'torrent.tree.selectNone': 'Select none',
  'torrent.tree.cancel': 'Cancel',
  'torrent.tree.add': 'Add to collector',

  // The one-shot clipboard button: hidden by itself wherever
  // navigator.clipboard is undefined (an ordinary http:// LAN address is not
  // a secure context), so its own label is all it ever needs — there is no
  // paired "why is this missing" bubble to write, because where it cannot
  // work it is not there to ask about. Its own outcome reuses
  // collector.toastStaged/toastNone and list.failed rather than adding a
  // third phrasing of the same three states.
  'intake.pasteButton': 'Paste from clipboard',

  'settings.nav.general': 'General',
  'settings.nav.modules': 'Modules',
  'settings.nav.downloads': 'Downloads',
  'settings.nav.archives': 'Archives',
  'settings.nav.rules': 'Rules',
  'settings.nav.connections': 'Connections',
  'settings.nav.reconnect': 'Reconnect',
  'settings.nav.accounts': 'Accounts',
  'settings.nav.captcha': 'Captcha',
  'settings.nav.schedule': 'Schedule',
  'settings.nav.look': 'Look',
  'settings.nav.access': 'Access',
  'settings.nav.advanced': 'Advanced',

  'settings.unsaved': 'Unsaved changes',
  'settings.discard': 'Discard',
  'settings.railLabel': 'Settings sections',
  'settings.railReorderHint': 'Hold a tab and drag to put your own sections first.',
  'settings.empty': 'Nothing to configure here yet.',
  'settings.emptyHint': 'The page is registered so its address keeps working and so the controls land here when the subsystem does.',

  'settings.general.subtitle': 'Where files land and what happens to a link the moment it arrives.',
  'settings.sectionIntake': 'New links',

  'settings.downloads.watchOff': 'Folder watch is switched off on the Modules page, which is why this is not editable. Switching it back on restores the folder that was set here.',

  'settings.modules.subtitle': 'What this build contains, and what it does not.',
  'settings.modules.fixedAtBuild': 'The set of modules is fixed when the binary is built. Nothing can be installed into a running instance, so this list is the whole of it.',
  'settings.modules.sectionShipped': 'In this build',
  'settings.modules.sectionDesktop': 'Desktop builds only',
  'settings.modules.sectionNotBuilt': 'Not in this build',
  'settings.modules.on': 'On',
  'settings.modules.off': 'Off',
  'settings.modules.notBuilt': 'Not built',
  'settings.modules.desktopOnly': 'Desktop only',
  'settings.modules.noSwitch': 'No switch here',
  'settings.modules.configuredOn': 'Configured on {page}',
  'settings.modules.switchFailed': 'That switch was refused: {reason}',
  'settings.modules.configureFirst': 'There is nothing to switch on yet. Set this up on {page} first; after that the switch here stops and restarts it without losing what you set.',
  'settings.modules.saveFirst': 'Save or discard the pending changes first. Switching a module writes the settings straight away, and the two writes would overwrite each other.',

  'settings.module.extraction': 'Archive extraction',
  'settings.module.watch': 'Folder watch',
  'settings.module.crawler': 'Page crawler',
  'settings.module.checksums': 'Checksum verification',
  'settings.module.scheduler': 'Scheduler',
  'settings.module.reconnect': 'Reconnect',
  'settings.module.packagizer': 'Packagizer',
  'settings.module.linkfilter': 'Link filter',
  'settings.module.connections': 'Outbound connections',
  'settings.module.cnl': "Click'n'Load",
  'settings.module.federation': 'Peer instances',
  'settings.module.jd': 'JDownloader backend',
  'settings.module.captcha': 'Captcha',
  'settings.module.scripting': 'Event scripts',
  'settings.module.tray': 'Tray icon',
  'settings.module.windowpolicy': 'Window behaviour',
  'settings.module.myjd': 'My.JDownloader',
  'settings.module.updater': 'In-app updates',

  'settings.advanced.subtitle': 'Every setting this instance has, by name.',
  'settings.advanced.search': 'Filter by name or value',
  'settings.advanced.onlyModified': 'Only what differs from the default',
  'settings.advanced.colKey': 'Key',
  'settings.advanced.colType': 'Type',
  'settings.advanced.colValue': 'Value',
  'settings.advanced.reset': 'Reset',
  'settings.advanced.resetTitle': 'Put this key back to its default',
  'settings.advanced.modified': 'changed',
  'settings.advanced.noMatch': 'No key matches.',
  'settings.advanced.badJson': 'This has to be valid JSON, so it is not being applied.',
  'settings.advanced.secret': 'A stored secret is never sent to the browser. Leave the placeholder to keep it, or type a new value to replace it.',
  'settings.advanced.listHint': 'Edited as JSON here. The page for it has the proper editor.',
  'settings.advanced.defaultsUnavailable': 'The factory values could not be fetched, so nothing can be reset from here.',
  'settings.advanced.type.boolean': 'yes / no',
  'settings.advanced.type.number': 'number',
  'settings.advanced.type.text': 'text',
  'settings.advanced.type.list': 'list',
  'settings.advanced.type.object': 'group',

  'settings.access.subtitle': 'Who can reach this instance, and from where.',
  'settings.sectionIntakePorts': 'Ports and listeners',

  'settings.connections.add': 'Add connection',
  'settings.connections.import': 'Import list',
  'settings.connections.listTitle': 'Outbound connections',
  'settings.connections.empty': 'Everything goes out over this machine',
  'settings.connections.emptyHint': 'No outbound connections are configured, so every download uses this machine’s own connection. Add a proxy to route downloads through, or a direct row to keep certain hosts off one.',
  'settings.connections.use': 'Use this connection',
  'settings.connections.moveUp': 'Move up',
  'settings.connections.moveDown': 'Move down',
  'settings.connections.remove': 'Remove this connection',
  'settings.connections.edit': 'Edit this connection',
  'settings.connections.type': 'Type',
  'settings.connections.typeHint': 'None and direct are not the same row. None is inert: it names no connection and is never used, so it survives only until you finish filling it in. Direct is a real choice — go out over this machine’s own connection and deliberately bypass every proxy for the hosts named below, which is how a NAS is excluded from a whole-app proxy. A row whose filter matches the host beats a row with no filter, so a direct row with a filter always wins over a catch-all proxy.',
  'settings.connections.kind.none': 'None',
  'settings.connections.kind.direct': 'Direct',
  'settings.connections.stateNone': 'Inert. Nothing is ever sent through this row.',
  'settings.connections.stateDirect': 'Bypasses every proxy for the hosts below.',
  'settings.connections.warnDirectCatchAll': 'This direct row has no host filter, so it takes its turn in the rotation and sends downloads out unproxied at random. Name the hosts it should claim.',
  'settings.connections.stateSocks4': 'SOCKS4 carries a user id and has no password field at all, so no password is stored for this row.',
  'settings.connections.host': 'Host',
  'settings.connections.port': 'Port',
  'settings.connections.username': 'User name',
  'settings.connections.usernameHint': 'The proxy’s own credentials, not a hoster account. Clearing the user name clears the stored password with it.',
  'settings.connections.password': 'Password',
  'settings.connections.passwordStored': 'stored — leave empty to keep it',
  'settings.connections.passwordHint': 'A stored password is never sent to this page, which is why the box is empty. Leave it empty and the saved one is kept. A stored password does not follow the row to a different host, port or type: change one of those and this has to be set again.',
  'settings.connections.filter': 'Host filter',
  'settings.connections.filterHint': 'One host per line. A bare domain covers everything under it, so example.org is enough for dl2.example.org, and * ? [ ] work as wildcards. Empty means this row is a catch-all, which is weaker than a row whose filter matches the host.',
  'settings.connections.filterAll': 'all hosts',
  'settings.connections.filterCount': '{n} hosts',
  'settings.connections.cap': 'Downloads at once',
  'settings.connections.capHint': 'How many downloads may share this connection at the same time. 0 uses the default of 2 — spreading downloads is what the list is for, and one connection taking the whole queue would defeat it.',
  'settings.connections.capDefault': 'default',
  'settings.connections.test': 'Test',
  'settings.connections.testing': 'Testing…',
  'settings.connections.testTarget': 'Test against',
  'settings.connections.testTargetHint': 'Optional. Left empty the test only shows that the proxy answers. Name a host and the proxy is asked to forward to it, which is what actually checks the credentials and shows whether that one hoster is being refused.',
  'settings.connections.testFailed': 'The test could not be run: {error}',
  'settings.connections.importTitle': 'Import a proxy list',
  'settings.connections.importLabel': 'Proxy list',
  'settings.connections.importHint': 'One per line, as socks5://user:pass@host:port. https, socks4 and socks4a work too, and a line that cannot be read is listed below with the reason rather than dropped.',
  'settings.connections.importPlaceholder': 'socks5://user:pass@proxy.example.org:1080',
  'settings.connections.importRead': 'Read list',
  'settings.connections.importReading': 'Reading…',
  'settings.connections.importReady': '{n} ready to add',
  'settings.connections.importAdd': 'Add {n}',
  'settings.connections.importRefused': '{n} refused',
  'settings.connections.importNothing': 'Nothing in this list could be read.',
  'settings.connections.importLine': 'Line {n}',
  'settings.connections.importFailed': 'The list could not be read: {error}',
  'settings.connections.cancel': 'Cancel',

  'collector.filtered.summary': '{n} link(s) held by the link filter',
  'collector.filtered.info': 'Links a filter rule refused. They are kept here rather than in the list above, so a filter that is working does not look like a collector full of junk — nothing was lost. Restore puts a link back and lets it past the rule that caught it, which is what you want when the rule turned out to be too broad. Clear deletes it; no file has been downloaded either way.',
  'collector.filtered.restore': 'Restore',
  'collector.filtered.restoreAll': 'Restore all',
  'collector.filtered.clear': 'Clear',
  'collector.filtered.noRule': 'the link filter',
  'collector.filtered.originTitle': 'Where this link came from',
  'collector.filtered.origin.paste': 'pasted',
  'collector.filtered.origin.crawl': 'crawled',
  'collector.filtered.origin.cnl': "Click'n'Load",
  'collector.filtered.origin.watch': 'watch folder',
  'collector.filtered.origin.container': 'container',
  'collector.filtered.restoreFailed': 'Could not restore those links. Is the server reachable?',
  'collector.filtered.clearFailed': 'Could not clear those links. Is the server reachable?',
  'collector.filtered.toastHeld': 'Staged {n} link(s); {held} held by the link filter',
  'collector.filtered.toastAllHeld': 'Nothing was staged: the link filter is holding {held} link(s)',

  'settings.rules.flavour.packagizer': 'Packagizer',
  'settings.rules.flavour.filter': 'Link filter',
  'settings.rules.flavourLabel': 'Which rule list',
  'settings.rules.packagizerHint': 'Runs on every link as it is staged and rewrites what it can: package, folder, comment, priority, chunks, auto-extract. Every matching rule contributes and a later rule wins per field.',
  'settings.rules.filterHint': 'Decides whether a link is taken into the collector at all. A rejected link is not deleted: it is held aside with the rule and the reason that stopped it, so nothing ever disappears without saying why.',
  'settings.rules.setOn': 'This list is being applied',
  'settings.rules.setOff': 'This list is switched off',
  'settings.rules.setSwitchHint': 'The master switch for the whole list. Off, no rule below runs — but they are all still edited and dry-run normally, because a list cannot be repaired while it is off if being off also hides what is wrong with it.',
  'settings.rules.stopAfterMatch': 'Stop at the first matching rule',
  'settings.rules.stopHint': 'Off, every matching rule contributes and a later rule wins per field, which is what the Packagizer wants. On, evaluation ends at the first match, which is what a filter usually wants: an accept placed above a broad reject then actually protects the link.',
  'settings.rules.listTitle': 'Rules, in the order they run',
  'settings.rules.add': 'Add rule',
  'settings.rules.import': 'Import',
  'settings.rules.export': 'Export',
  'settings.rules.exportTitle': 'Save this list as a JSON file',
  'settings.rules.importTitle': 'Replace this list from a JSON file',
  'settings.rules.importFailed': 'That file is not a rule list: {reason}',
  'settings.rules.importedCount': 'Loaded {n} rules. Nothing is saved until you save the page.',
  'settings.rules.empty': 'No rules yet',
  'settings.rules.emptyPackagizer': 'Every link keeps the package, folder and options it arrived with. Add a rule to sort them as they come in.',
  'settings.rules.emptyFilter': 'Every link is taken. Add a rule to hold some of them aside.',
  'settings.rules.unnamed': 'Rule {n}',
  'settings.rules.duplicate': 'Duplicate this rule',
  'settings.rules.remove': 'Remove this rule',
  'settings.rules.moveUp': 'Move up',
  'settings.rules.moveDown': 'Move down',
  'settings.rules.ruleOn': 'This rule runs',
  'settings.rules.ruleOff': 'This rule is switched off',
  'settings.rules.problemCount': '{n} problems',
  'settings.rules.problemOne': '1 problem',
  'settings.rules.notRunning': 'This rule is not being applied. Fix what is listed and it starts working again.',
  'settings.rules.matchedCount': 'matched {n} of {total}',

  'settings.rules.name': 'Name',
  'settings.rules.namePlaceholder': 'What this rule is for',
  'settings.rules.nameHint': 'Only ever shown to you — in this list, on a problem, and on the reason a link was held aside. An unnamed rule is called by its position, which changes when you reorder the list.',
  'settings.rules.sectionIf': 'If all of these are true',
  'settings.rules.ifHint': 'Every condition has to hold. An either/or is written as two rules, which keeps the list readable top to bottom. A rule with NO conditions matches every link, which is how a catch-all folder or a blanket reject at the end is written.',
  'settings.rules.fieldPicker': 'What to look at',
  'settings.rules.opPicker': 'How to compare it',
  'settings.rules.sectionThen': 'Then',
  'settings.rules.thenHintPackagizer': 'An empty box means "leave this alone", never "clear it": a rule that only sets the folder must not wipe the package name an earlier rule chose.',
  'settings.rules.thenHintFilter': 'A rule that rejects holds the link aside with its reason. A rule that accepts is worth having too: placed above a broad reject, with "stop at the first match" on, it is how one hoster is let through.',
  'settings.rules.addCondition': 'Add condition',
  'settings.rules.removeCondition': 'Remove this condition',
  'settings.rules.noConditions': 'No conditions — this rule matches every link.',
  'settings.rules.value': 'Value',
  'settings.rules.min': 'At least',
  'settings.rules.max': 'At most',
  'settings.rules.noUpperBound': 'no upper bound',
  'settings.rules.sizeHint': 'A plain number is bytes. A unit is understood too — 700 MB, 1.5 GiB — and both are read as 1024-based, the same as every size the rest of the app prints.',
  'settings.rules.badSize': 'This is not a size.',
  'settings.rules.pattern': 'Pattern',
  'settings.rules.patternHint': 'A Go regular expression, unanchored, and the one operator that does NOT ignore case — put (?i) at the front if you want it to. An unparsable pattern is refused with the reason rather than quietly matching nothing.',
  'settings.rules.category': 'File type',
  'settings.rules.categoryCustom': 'Custom pattern',
  'settings.rules.categoryHint': 'A shortcut that fills in the pattern for a whole family of extensions. It is stored as an ordinary pattern, so you can pick one and then edit it — after which this goes back to "custom pattern", because it no longer says what the rule does.',
  'settings.rules.categoryExtensions': 'Covers: {list}',
  'settings.rules.unchanged': 'Unchanged',
  'settings.rules.yes': 'On',
  'settings.rules.no': 'Off',
  'settings.rules.reject': 'Reject the link',
  'settings.rules.accept': 'Accept the link',
  'settings.rules.priorityHint': 'Higher runs earlier. The range is the one the queue itself accepts, so a rule cannot hand a task a priority you have no way to undo. Leave it empty to change nothing.',
  'settings.rules.chunksHint': 'How many connections this one file is downloaded with. Leave it empty to use the global setting. Connections beyond a handful buy nothing on a hoster that limits per file and are a reliable way to get an account flagged.',
  'settings.rules.emptyMeansUnchanged': 'empty = unchanged',

  'settings.rules.variables': 'Variables',
  'settings.rules.variablesTitle': 'Insert a variable',
  'settings.rules.variablesHint': 'Every one of these resolves against the link AS IT ARRIVED, so rules do not chain onto each other: a folder template in rule four sees the name the hoster gave, not what rule two renamed it to. What a rule does can be read off that rule alone.',
  'settings.rules.varParams': 'replace {params}',

  'settings.rules.field.filename': 'File name',
  'settings.rules.field.url': 'Link URL',
  'settings.rules.field.hoster': 'Hoster',
  'settings.rules.field.source': 'Source page',
  'settings.rules.field.filetype': 'File type',
  'settings.rules.field.filesize': 'File size',
  'settings.rules.field.package': 'Package',

  'settings.rules.op.contains': 'contains',
  'settings.rules.op.contains-not': 'does not contain',
  'settings.rules.op.equals': 'is',
  'settings.rules.op.equals-not': 'is not',
  'settings.rules.op.matches': 'matches pattern',
  'settings.rules.op.is-between': 'is between',

  'settings.rules.action.packageName': 'Package name',
  'settings.rules.action.downloadDir': 'Download folder',
  'settings.rules.action.comment': 'Comment',
  'settings.rules.action.priority': 'Priority',
  'settings.rules.action.autoExtract': 'Extract automatically',
  'settings.rules.action.chunks': 'Connections',
  'settings.rules.action.reject': 'Verdict',
  'settings.rules.action.reason': 'Reason',
  'settings.rules.action.reasonHint': 'Shown next to the held-aside link. Left empty one is written for you, because a rejection nobody can explain is exactly what this list exists to avoid.',
  'settings.rules.action.folderHint': 'The only box allowed to spell out path levels. Everything else is cut back to a single name, because a file name containing a slash is not a name, it is a way out of the folder you picked.',

  'settings.rules.category.video': 'Video',
  'settings.rules.category.audio': 'Audio',
  'settings.rules.category.image': 'Images',
  'settings.rules.category.archive': 'Archives',
  'settings.rules.category.document': 'Documents and books',
  'settings.rules.category.subtitle': 'Subtitles',
  'settings.rules.category.disc': 'Disc images',
  'settings.rules.category.program': 'Programs and packages',

  'settings.rules.var.packagename': 'The package the link arrived in',
  'settings.rules.var.hoster': 'The hoster, without www.',
  'settings.rules.var.filename': 'The file name as it arrived',
  'settings.rules.var.orgfilename': 'The file name as it arrived',
  'settings.rules.var.orgfilenamewithoutext': 'The file name without its extension',
  'settings.rules.var.orgfiletype': 'The extension without its dot, empty when there is none',
  'settings.rules.var.date': 'Today, as YYYY-MM-DD',
  'settings.rules.var.year': 'The year, as YYYY',
  'settings.rules.var.month': 'The month, as MM',
  'settings.rules.var.day': 'The day, as DD',
  'settings.rules.var.simpledate': 'The date in a pattern you write, in Java’s date syntax',
  'settings.rules.var.source': 'The Nth path segment of the source page’s URL, counting from 1: on https://site.org/tv/s01/list.html, 1 is tv and 2 is s01. NOT what JDownloader means by this tag — see the note below.',
  'settings.rules.var.match': 'Capture group N of this rule’s "matches" pattern on FIELD. This is JDownloader’s <jd:source:N>, under a name that says which pattern it reads. A rule with no matching pattern on that field is refused when you save it, rather than quietly producing a folder called <jd:match:url:1>.',
  'settings.rules.var.append': 'Nothing the first time this value comes up, then _2, _3 and so on',
  'settings.rules.sourceDivergence': 'Copying a template out of a JDownloader config? <jd:source:N> means something different here — a path segment of the source URL, not a capture group. JD’s meaning is spelled <jd:match:FIELD:N>. The two agree often enough to be dangerous, so a copied template is worth dry-running below before you save it.',

  'settings.rules.testTitle': 'Try it on a link',
  'settings.rules.testHint': 'Nothing here is downloaded, stored or saved. The list as it stands on this page is run against these samples, through the same code that runs at staging time.',
  'settings.rules.testUrl': 'Link URL',
  'settings.rules.testFilename': 'File name',
  'settings.rules.testSource': 'Source page',
  'settings.rules.testSourceHint': 'The page a crawl found the link on. Only worth filling in for a rule that tests the source field or uses <jd:source:N>.',
  'settings.rules.testSize': 'Size',
  'settings.rules.testPackage': 'Package',
  'settings.rules.testPackageHint': 'The package the link would arrive in, before any rule runs. It is what <jd:packagename> resolves to, which is worth knowing: a folder template does not see the package name a rule sets in the same pass.',
  'settings.rules.testAdd': 'Add a sample',
  'settings.rules.testRemove': 'Remove this sample',
  'settings.rules.testEmpty': 'Paste a link and a file name to see where it would land.',
  'settings.rules.testRunning': 'Checking…',
  'settings.rules.testFailed': 'The dry run could not be reached: {reason}',
  'settings.rules.resultMatched': 'Matched, in order',
  'settings.rules.resultNone': 'No rule matched',
  'settings.rules.resultPackage': 'Package',
  'settings.rules.resultFolder': 'Folder',
  'settings.rules.resultFilename': 'File name',
  'settings.rules.resultRejected': 'Held aside',
  'settings.rules.resultAccepted': 'Taken',
  'settings.rules.resultBy': 'by {rule}',
  'settings.rules.folderFromSettings': 'from the download settings',
  'settings.rules.folderFromSettingsHint': 'No rule named a folder, so the link lands in the configured download folder — possibly inside a per-package subfolder, if that setting is on. This page will not guess the full path at you: what it can say for certain is that no rule changed it.',
  'settings.rules.alsoSets': 'Also sets',

  'settings.reconnect.method': 'Method',
  'settings.reconnect.methodHint':
    'How the router is told to drop the line and come back with a new address. Command runs a program, Requests replays a recorded HTTP conversation, UPnP asks the gateway over the network without any login at all, and Script hands a file to an interpreter. Only the fields the chosen method needs are shown; the others keep what you typed and come back when you switch to them.',
  'settings.reconnect.method.none': 'Off',
  'settings.reconnect.method.command': 'Command',
  'settings.reconnect.method.http': 'Requests',
  'settings.reconnect.method.upnp': 'UPnP',
  'settings.reconnect.method.script': 'Script',
  'settings.reconnect.offState':
    'Reconnect is off. Nothing asks the router for a new address, and a hoster limit is left to the ordinary retry backoff.',

  'settings.reconnect.command': 'Program',
  'settings.reconnect.commandHint':
    'The full path of the program to run. It is started directly rather than through a shell, so a pipe, a redirect or two commands in a row belong in the Script method instead. %%router%%, %%username%%, %%password%% and %%ip%% are filled in before it starts.',
  'settings.reconnect.args': 'Arguments',
  'settings.reconnect.argsHint':
    'One per line, so an argument containing a space stays one argument. The same four variables are filled in here.',

  'settings.reconnect.upnpState':
    'Nothing else is needed. The gateway is found by asking the network, which is what makes this the method that works without knowing anything about the router.',
  'settings.reconnect.upnpLocation': 'Gateway description URL',
  'settings.reconnect.upnpLocationHint':
    'Optional, and normally empty. Fill it in only where the multicast search is filtered but the gateway itself is perfectly reachable, which discovery on its own can never fix.',

  'settings.reconnect.interpreter': 'Interpreter',
  'settings.reconnect.interpreterHint':
    'The program the script is handed to, with its full path: /bin/sh, /bin/bash, /usr/bin/python3.',
  'settings.reconnect.interpreterArgs': 'Interpreter arguments',
  'settings.reconnect.interpreterArgsHint': 'One per line, placed before the script file. Usually empty.',
  'settings.reconnect.script': 'Script',
  'settings.reconnect.scriptHint':
    'Written to a private temporary file, and the interpreter is handed its path, so nothing here is ever quoted into a command line. The variables are filled in before the file is written, which means a password typed into this box reaches that file: %%password%% keeps it in the password field instead.',

  'settings.reconnect.router': 'Router address',
  'settings.reconnect.routerHint':
    'The router on your own network, without a scheme: 192.168.1.1, not http://192.168.1.1/. It is what %%router%% expands to. It is not the public address, and the two must never be swapped: a login request pointed at the public one sends your router password out to the internet.',
  'settings.reconnect.routerFind': 'Find my router',
  'settings.reconnect.routerFinding': 'Looking…',
  'settings.reconnect.routerFound': 'Found {address}.',
  'settings.reconnect.routerFoundVia':
    'Found {address} via {iface}. Worth a look: in a container this is often the bridge rather than the router.',
  'settings.reconnect.routerFailed': 'No router address could be read here: {reason}',
  'settings.reconnect.username': 'Router user name',
  'settings.reconnect.usernameHint':
    'The router login, not a hoster account. It is what %%username%% expands to.',
  'settings.reconnect.password': 'Router password',
  'settings.reconnect.passwordStored': 'stored, and never sent back here',
  'settings.reconnect.passwordHint':
    'A stored password is never sent back to this page, which is why the box is empty: leave it empty and the saved one is kept. Type a new one to replace it. To remove it altogether, type something and then clear the box again.',

  'settings.reconnect.requests': 'Requests',
  'settings.reconnect.requestsHint':
    'Replayed in order, and the run stops at the first one the router does not answer. This is JDownloader’s LiveHeader method: usually a login, then the reboot or the disconnect. Every field takes the variables.',
  'settings.reconnect.requestsEmpty': 'No requests yet. Add one, or paste a recorded script.',
  'settings.reconnect.requestAdd': 'Add a request',
  'settings.reconnect.requestStep': 'Step {n}',
  'settings.reconnect.requestUp': 'Move this request up',
  'settings.reconnect.requestDown': 'Move this request down',
  'settings.reconnect.requestRemove': 'Remove this request',
  'settings.reconnect.requestMethod': 'Method',
  'settings.reconnect.requestUrl': 'URL',
  'settings.reconnect.requestUrlHint':
    'The whole URL, scheme included. http://%%router%%/login.cgi is the usual shape.',
  'settings.reconnect.requestHeaders': 'Headers',
  'settings.reconnect.requestHeadersHint':
    'One per line, as Name: value. A Host header is honoured and reaches the wire, which is what router firmware that virtual-hosts its admin page needs.',
  'settings.reconnect.requestBody': 'Body',
  'settings.reconnect.requestBodyHint':
    'Sent as it stands. A body with no content type of its own is sent as a form post, which is what a recorded router login is.',

  'settings.reconnect.import': 'Import a script',
  'settings.reconnect.importLabel': 'LiveHeader or curl script',
  'settings.reconnect.importHint':
    'Paste a JDownloader reconnect script, markers and all. Its variables are translated on the way in, and %%%routerip%%% becomes the router address rather than the public one, because here those are two different things.',
  'settings.reconnect.importRead': 'Read script',
  'settings.reconnect.importReading': 'Reading…',
  'settings.reconnect.importClose': 'Close',
  'settings.reconnect.importMapped': '{n} mapped',
  'settings.reconnect.importRefusedCount': '{n} refused',
  'settings.reconnect.importUse': 'Replace the list with {n}',
  'settings.reconnect.importLine': 'Line {n}',
  'settings.reconnect.importBlocked':
    'Nothing is taken from a script with a refused line in it. Half a router script is a login with no reboot, and that shows up days later as an address that never changes.',
  'settings.reconnect.importFailed': 'The script could not be read: {reason}',

  'settings.reconnect.checkTitle': 'Checking the address',
  'settings.reconnect.checkUrl': 'IP check URL',
  'settings.reconnect.checkUrlHint':
    'Fetched before the method runs and again afterwards; the run counts as done once the address it prints has changed. There is no default, deliberately: a self-hosted download manager should not start reporting your address to a service you never chose. What it prints has to be the address the internet sees, because a page echoing the address on your own network cannot tell a reconnect from a no-op.',
  'settings.reconnect.checkPresets': 'Well-known services',
  'settings.reconnect.checkPresetsHint':
    'A shortcut for anyone with no preference, not a default: none of these is chosen for you, and any page that prints your public address does the job just as well.',
  'settings.reconnect.interval': 'Seconds between checks (1 to 60)',
  'settings.reconnect.intervalHint':
    'How long to wait between two looks at the check URL once the method has run. There is a floor because a loop without one turns the check service into a target.',
  'settings.reconnect.timeout': 'Seconds to keep checking (5 to 900)',
  'settings.reconnect.timeoutHint':
    'How long to keep looking before the run is called a failure. A reconnect still waiting a quarter of an hour later has failed whatever this says. A timeout below the interval is raised to it, or the run would be over before a single check happened.',
  'settings.reconnect.clamped': 'Saved as {n}.',

  'settings.reconnect.runTitle': 'Running one now',
  'settings.reconnect.run': 'Run it now',
  'settings.reconnect.running': 'Running…',
  'settings.reconnect.runMoved': 'The address moved from {from} to {to}.',
  'settings.reconnect.runDetail': '{n} checks, {secs} s',
  'settings.reconnect.runBusy':
    'A reconnect is already running, so this one was not started. The result belongs to whoever asked for the first one.',
  'settings.reconnect.runFailed': 'The reconnect did not finish: {reason}',
  'settings.reconnect.stateConfigured': 'Configured',
  'settings.reconnect.stateNotConfigured': 'Not configured',
  'settings.reconnect.stateIdle': 'Idle',
  'settings.reconnect.stateBusy': 'Running now',
  'settings.reconnect.stateUnreadable': 'The reconnect state could not be read. Is the server reachable?',
  'settings.reconnect.notReady': 'Not ready: {reason}',
  'settings.reconnect.reason.off': 'Reconnect is switched off',
  'settings.reconnect.reason.noCommand': 'The command method has no program to run',
  'settings.reconnect.reason.noRequests': 'The request method has no requests',
  'settings.reconnect.reason.requestNoURL': 'Request {n} has no URL',
  'settings.reconnect.reason.noInterpreter': 'The script method has no interpreter to run it with',
  'settings.reconnect.reason.noScript': 'The script method has no script',
  'settings.reconnect.reason.noCheckURL': 'No IP check URL',
  'settings.reconnect.reason.unknownMethod': 'Unknown reconnect method "{method}"',
  'settings.reconnect.reason.noRouter': 'The script uses %%{var}%% but no router address is set',
  'settings.reconnect.runUsesSaved': 'Run uses the saved settings, not what is on screen. Save first.',
  'settings.reconnect.policy': 'When this happens on its own',
  'settings.reconnect.policyHint':
    'Automatic reconnects are fired in one place, and this page is not it. One is started when a download backend itself asks for another attempt after a delay, which is how a hoster says the limit is tied to this address. It never runs while the queue is halted, because rebooting the router drops the downloads that are still going. It never runs while the reconnect is not fully configured. Only one runs at a time: a second request waits for the first one’s verdict instead of fighting it over the router. And if the address does not move, nothing is brought forward and the ordinary retry backoff is left to run.',

  'shell.bar': 'Queue and status bar',
  'shell.scope': 'Controlling',
  'shell.scopeHint':
    'The page below is showing this peer, so the bar points at it too. A peer keeps its own queue and its own speed limit, so anything this instance cannot pass on is shown as unavailable instead of being applied to this machine.',
  'queue.peerLimitLocal': 'The speed limit belongs to this machine; open the peer to change its own.',

  'task.moveUp': 'Move up',
  'task.moveDown': 'Move down',
  'menu.priority': 'Priority',
  'menu.forceStart': 'Start now',
  'menu.queueStopped': 'Queue stopped',
  'pkg.queueOrder': 'Move whole package',

  // The seven the queue backend reports, which is not the five the properties
  // panel offers below: one is JDownloader's ladder read off the server, the
  // other is what a task record can hold. Folding them into one set would tie
  // the panel to whichever backend answered first.
  'priority.highest': 'Highest',
  'priority.higher': 'Higher',
  'priority.high': 'High',
  'priority.default': 'Default',
  'priority.low': 'Low',
  'priority.lower': 'Lower',
  'priority.lowest': 'Lowest',

  'settings.chunks': 'Connections per download (0 = automatic)',
  'settings.chunksHint':
    'How many connections one download opens when nothing more specific applies. A rule, or a single download, can name its own number and outranks this. A hoster that tolerates fewer still gets fewer: a host limit can only lower the count, never raise it. 0 leaves the decision to the app. Connections beyond a handful buy nothing on a hoster that limits per file, and are a reliable way to get an account flagged.',
  'task.chunks': 'Connections (0 = the global setting)',
  'task.chunksHint':
    'How many connections this one download opens. It outranks the global setting and any rule that set it, but never the hoster: a host that tolerates fewer still gets fewer. 0 takes the override off again and hands the count back.',
  'columns.connection': 'Connection',

  'props.title': 'Properties',
  'props.mixed': 'Several values',
  'props.mixedHint':
    'These rows do not agree, so the box is empty. Left alone it changes nothing; type in it and every selected row gets what you typed.',
  'props.name': 'Name',
  'props.nameHint':
    'One file name, never a path: anything spelling out folders is cut back to a single name. A download that has finished is renamed on disk too, so the list and the folder cannot disagree. One that is running keeps the file its backend has open and takes the new name when it finishes. One that has not started takes it straight away. A name belongs to one file, so this box is only offered for a single row.',
  'props.comment': 'Comment',
  'props.commentHint': 'A note for whoever reads this list next month. Nothing in the app acts on it.',
  'props.priority': 'Priority',
  'props.priorityHint':
    'Higher runs first. It reorders what is still waiting; a download already running is not sent back to the queue.',
  'props.autoExtract': 'Unpack archives',
  'props.autoExtractHint':
    'Read when unpacking would happen rather than when the download ran, so switching it on now unpacks something that finished an hour ago. Inherit hands the decision back to the global setting, which is not the same answer as off.',
  'props.inherit': 'Inherit',
  'props.on': 'On',
  'props.off': 'Off',

  'strip.label': 'Download totals',
  'strip.of': 'of',
  'strip.hint':
    'Bytes fetched against bytes owed. Finished, failed and collected links stay out of the total, and so does anything whose size the host has not stated yet - a guess there would be a number nobody can act on.',
  'strip.scope': 'What the figures cover',
  'strip.total': 'Total',
  'strip.visible': 'Visible',
  'strip.selected': 'Selected',
  'strip.includeDisabled': 'Include disabled',
  'strip.includeDisabledHint':
    'Count the bytes of links that are switched off. They stay out of the time remaining either way: nothing is going to fetch them, so no amount of waiting works them off.',

  'quick.title': 'Quick settings',
  'quick.chunksHint':
    'Connections one download opens. It is not a count of connections that are open right now: a backend reports its speed and its bytes and never how many sockets it holds, so there is no such number to show. 0 hands the choice back to the app.',
  'quick.limitHint':
    'Switching this off lifts the limit and remembers it, so switching it back on puts the same number in force again. The number itself is typed in the bar.',
  'quick.noLimit': 'No speed limit',
  'quick.noLimitHint':
    'There is nothing to switch until a limit has been set. The field for it is in the bar beside the start button, and on the Downloads settings page.',

  // The server-side folder chooser. It browses the machine the backend runs on,
  // which in a container is the only one that knows what is mounted where, so
  // the wording talks about folders that are there rather than files you have.
  'folders.title': 'Choose a folder',
  'folders.browse': 'Browse folders',
  'folders.path': 'Path',
  'folders.up': 'Up one level',
  'folders.use': 'Use this folder',
  'folders.empty': 'No sub-folders here.',
  'folders.new': 'This folder does not exist yet. It is created when the first download lands in it.',
  'folders.tail': 'Kept',
  'folders.tailHint':
    'Browsing replaces only the fixed part of the folder. The variables are put back on the end, so your naming scheme is not lost.',
  'folders.roots': 'Roots',
  'folders.truncated': 'Only the first {n} folders are shown. Type a path to go straight to one.',

  // Unpacking as a job of its own, with progress and a stop button, rather than
  // a word the download wears for a while. The states below name what the job
  // is doing, so they read as verbs and not as a copy of the download statuses.
  //
  // Interpolation here is single-brace, like every other key in this file: t()
  // replaces {name}, and a doubled brace would survive into the UI.
  'archive.menu': 'Archive',
  'archive.unpackNow': 'Unpack now',
  'archive.stop': 'Stop unpacking',
  'archive.title': 'Archives',
  'archive.queued': 'Waiting to unpack',
  'archive.running': 'Unpacking',
  'archive.failed': 'Not unpacked',
  'archive.progress': '{files} files · {bytes}',
  'archive.volumes': '{volumes} volumes',
  'archive.needsPassword': 'Needs a password',

  // The archive settings page. The three policy strips are labelled by what the
  // extractor does and not by the id the server sends, but an id with no string
  // here still renders under its own name - see pages/settings/Archives.tsx.
  'settings.archives.handles': 'Opens',
  'settings.archives.destination': 'Unpack to',
  'settings.archives.destinationHint':
    'Where the unpacked files land. Leave it empty and they are put beside the archive.',
  'settings.archives.besideArchive': 'Beside the archive',
  'settings.archives.subfolder': 'A folder per package',
  'settings.archives.subfolderHint':
    'Puts every package in a folder of its own below the destination. It does nothing while the files land beside the archive, where the package folder is already where they are going.',
  'settings.archives.collision': 'If a file is already there',
  'settings.archives.collisionHint':
    'What the extractor does when a file of that name is already in the folder. It decides on its own, and for the whole folder: there is nobody to ask halfway through unpacking.',
  'settings.archives.collision.overwrite': 'Overwrite',
  'settings.archives.collision.rename': 'Keep both',
  'settings.archives.collision.skip': 'Skip',
  'settings.archives.afterwards': 'Afterwards',
  'settings.archives.disposal': 'The archive itself',
  'settings.archives.disposalHint':
    'What becomes of the archive once it has been unpacked. There is no recycle bin in a container, so the middle answer is a move into {folder} beside the files, swept later by age.',
  'settings.archives.disposal.keep': 'Keep',
  'settings.archives.disposal.trash': 'Move to trash',
  'settings.archives.disposal.delete': 'Delete',
  'settings.archives.retention': 'Empty the trash after (days)',
  'settings.archives.retentionHint':
    'How long an archive stays in {folder} before the sweep takes it. 0 sweeps nothing, so the folder is emptied by hand.',
  'settings.archives.infoFiles': 'Take the notes beside it too',
  'settings.archives.infoFilesHint':
    'Files that came with the archive rather than out of it, such as .nfo, .sfv and .txt. Only the package’s own files are touched and never the whole folder: on the default layout one folder holds several releases, and a sweep that read the folder would take the neighbours’ notes.',
  'settings.archives.optionsFailed':
    'The extractor’s own lists could not be fetched, so the choices that come from it are not shown.',

  // Wave 6: the accounts page grew a service catalogue and a real accounts
  // table with two sections (debrid, hoster logins), account health, priority
  // routing and a hoster-login reconciler that hands credentials to the
  // headless-JD sidecar rather than reimplementing a hoster's own login.
  'accounts.accountLabel': 'Account name',
  'accounts.accountLabelHint':
    'This service already has a default account. Name this one to add a second login beside it.',
  'accounts.accountLabelPlaceholder': 'e.g. work',
  'accounts.buyPremium': 'Buy Premium',
  'accounts.changeService': 'Choose a different service',
  'accounts.col.enabled': 'Enabled',
  'accounts.col.expiry': 'Expiry',
  'accounts.col.label': 'Label',
  'accounts.col.service': 'Service',
  'accounts.col.status': 'Status',
  'accounts.col.traffic': 'Traffic left',
  'accounts.credentialFromEnv':
    'Set by the container’s {env} environment variable. Remove it there to change this.',
  'accounts.debrid.empty': 'No debrid accounts yet',
  'accounts.debrid.emptyHint':
    'Add a TorBox, AllDebrid or Real-Debrid key to unlock hoster links automatically.',
  'accounts.debrid.note': 'The most convenient path - one API key covers many hosters at once.',
  'accounts.debrid.title': 'Debrid',
  'accounts.edit': 'Edit credential',
  'accounts.editTitle': '{service} account',
  'accounts.enableAccount': 'Enable {account}',
  'accounts.hoster.add': 'Add a hoster login',
  'accounts.hoster.empty': 'No hoster logins yet',
  'accounts.hoster.emptyHint': 'Native per-hoster accounts are coming in a later update.',
  'accounts.hoster.title': 'Hoster logins',
  'accounts.newAccount': 'New account',
  'accounts.newAccountTitle': 'Add an account',
  'accounts.noServicesFound': 'No services match your search.',
  'accounts.passwordField': 'Password',
  'accounts.pickService': 'Choose a service',
  'accounts.refresh': 'Refresh',
  'accounts.refreshing': 'Refreshing…',
  'accounts.remove': 'Remove',
  'accounts.removed': 'Removed.',
  'accounts.rename': 'Rename',
  'accounts.renew': 'Renew',
  'accounts.rowActions': 'Account actions',
  'accounts.save': 'Save',
  'accounts.saveAnyway': 'Save anyway',
  'accounts.saving': 'Saving…',
  'accounts.searchServices': 'Search services…',
  'accounts.usernameField': 'Username',
  'accounts.verifyFailed': 'Could not verify: {detail}',
  'accounts.verifying': 'Verifying…',
  'accounts.whereToFind': 'Where do I get this?',
  'accountStrip.label': 'Account status',
  'accountStrip.uncheckedHint':
    'Not checked yet - the account-health check runs in the background and updates here automatically.',
  'accountStrip.unlimitedHint': 'Unlimited traffic',
  'accountStrip.trafficHint': '{used} of {limit} used',
  'accountStrip.expiryHint': 'Expires {date}',
  'accounts.hostsRefreshed': 'Host list refreshed {when}',
  'accounts.routing.title': 'Routing',
  'accounts.routing.priorityTitle': 'Priority order',
  'accounts.routing.priorityHint':
    'When more than one configured service can fetch the same link, they are tried in this order.',
  'accounts.routing.priorityEmpty': 'No resolvers are registered yet.',
  'accounts.routing.jdTitle': 'JDownloader sidecar',
  'accounts.routing.jdNotConfigured': 'Not configured',
  'accounts.routing.jdReachable': 'Reachable, revision {version}',
  'accounts.routing.jdUnreachable': 'Unreachable',
  'accounts.routing.resolver.direct': 'Direct link',
  'accounts.routing.resolver.http': 'Plain HTTP fallback',
  'accounts.hoster.col.host': 'Host',
  'accounts.hoster.col.username': 'Username',
  'accounts.hoster.status.active': 'Active',
  'accounts.hoster.status.queued': 'Queued',
  'accounts.hoster.status.rejected': 'Rejected',
  'accounts.hoster.removed': 'Removed.',
  'accounts.hoster.pickHost': 'Pick a hoster',
  'accounts.hoster.searchHosts': 'Search hosters…',
  'accounts.hoster.loginTitle': '{host} login',
  'accounts.hoster.custodyNotice':
    'The password is sent to and stored by the headless JDownloader sidecar, which performs the actual login - not by KnightLoader itself.',

  // The prompt modal (components/CaptchaModal.tsx) - a hoster asking a human
  // something before a download can continue.
  'captcha.title': 'Captcha needed',
  'captcha.titleMore': 'Captcha needed ({n} more waiting)',
  'captcha.forHost': '{host} is asking for a captcha before this download can continue.',
  'captcha.answerLabel': 'Answer',
  'captcha.answerPlaceholder': 'Type what you see…',
  'captcha.clickHint': 'Click every point the image asks for, then press Continue.',
  'captcha.clickCount': '{n} point(s) marked',
  'captcha.clickClear': 'Clear points',
  'captcha.widgetHint': 'Solve the challenge below - it continues on its own once you do.',
  'captcha.widgetUnavailable': 'This challenge could not be loaded.',
  'captcha.unsupported': 'KnightLoader cannot show this kind of challenge (reported as {vendor}).',
  'captcha.unsupportedHint': 'Use Cancel below, or block this hoster’s captchas for this session.',
  'captcha.continue': 'Continue',
  'captcha.cancel': 'Cancel',
  'captcha.refresh': 'Refresh',
  'captcha.moreOptions': 'More options',
  'captcha.blockHoster': 'Also stop asking for {host} this session',
  'captcha.blockEverywhere': 'Also stop asking for every host this session',
  'captcha.tooLate': 'That answer arrived too late.',
  'captcha.networkError': 'Could not reach the server. Try again.',
  'captcha.timedOut': 'A captcha for {host} timed out.',
  'captcha.resolvedElsewhere': 'A captcha for {host} was resolved elsewhere.',

  // The captcha settings page (pages/settings/Captcha.tsx) - solver order and
  // each solver's own API key. Landed here verbatim from that file's own
  // PENDING table (see its doc comment) now that this wave's locale pass has
  // reached it; PENDING itself is left in place, unread once every key here
  // resolves through the real catalogue.
  'settings.captcha.title': 'Captcha',
  'settings.captcha.subtitle': 'Automatic solvers are tried in this order before a captcha is ever shown to you.',
  'settings.captcha.orderTitle': 'Solver order',
  'settings.captcha.orderHint':
    'Every enabled solver below is tried in the order shown, top to bottom. If none are enabled, or every one of them fails or declines, you are asked directly.',
  'settings.captcha.orderEmpty': 'No solver is enabled - every captcha comes straight to you.',
  'settings.captcha.use': 'Try this solver',
  'settings.captcha.enableSolver': 'Try {service} automatically',
  'settings.captcha.moveUp': 'Move up',
  'settings.captcha.moveDown': 'Move down',
  'settings.captcha.set': 'Key set',
  'settings.captcha.notSet': 'No key set',
  'settings.captcha.setKey': 'Set key',
  'settings.captcha.change': 'Change',
  'settings.captcha.remove': 'Remove',
  'settings.captcha.cancel': 'Cancel',
  'settings.captcha.save': 'Save',
  'settings.captcha.saving': 'Saving…',
  'settings.captcha.placeholder': 'Paste the API key',
  'settings.captcha.whereToFind': 'Get a key',
  'settings.captcha.saved': 'API key saved.',
  'settings.captcha.removed': 'API key removed.',
  'settings.captcha.saveFailed': 'Could not save the key: {error}',

  // AddLinksForm.tsx's own per-batch options (build-plan.md section 8A) - the
  // destination, its recent-use history, and the archive/link password pair,
  // landed here now that this wave's locale pass has reached the form.
  'collector.options': 'Options',
  'collector.destination': 'Destination',
  'collector.destinationRecent': 'Recently used',
  'collector.archivePasswordHint':
    'Tried before the list saved in Settings, when an archive in this batch is encrypted.',
  'collector.linkPassword': 'Link password',
  'collector.linkPasswordHint':
    'What the hoster’s own page asks for before it hands over the file, not the archive password above. Two different secrets, asked by two different parties.',
  'collector.overrule': 'Overrule a matching Packagizer rule',
  'collector.overruleHint':
    'Off, a matching Packagizer rule wins over priority, unpacking and the comment above. On, these values win instead. The destination is never part of this: it always applies as typed.',

  // The collector's facet sidebar (components/CollectorFacets.tsx) - landed
  // here verbatim from that file's own PENDING table (see its doc comment)
  // now that this wave's locale pass has reached it; PENDING itself is left
  // in place, unread once every key here resolves through the real catalogue.
  'collector.facets.title': 'Filters',
  'collector.facets.hint':
    'Narrow the staged list by where a link points, what kind of file it is, or which package it landed in. Hiding this panel does not clear what is checked here.',
  'collector.facets.fileType': 'File type',
  'collector.facets.clearAll': 'Clear',
  'collector.facets.hide': 'Hide filters',
  'collector.facets.show': 'Filters',
  'collector.facets.unknownHost': 'Unknown host',
  'collector.facets.type.archive': 'Archives',
  'collector.facets.type.video': 'Video',
  'collector.facets.type.audio': 'Audio',
  'collector.facets.type.image': 'Images',
  'collector.facets.type.document': 'Documents',
  'collector.facets.type.other': 'Other',

  // The collector's own totals strip (components/CollectorStats.tsx) - same
  // PENDING-table arrangement as CollectorFacets.tsx above, landed the same
  // way.
  'collector.stats.label': 'Collector totals',
  'collector.stats.packages': 'Packages',
  'collector.stats.links': 'Links',
  'collector.stats.totalSize': 'Total size',
  'collector.stats.hosts': 'Hosts',

  // The ambient-activity status strip (components/StatusStrip.tsx, Wave 9's
  // 9A) - its LABEL record and formatCount()/tooltip strings were left
  // hardcoded in English on purpose (see that file's own doc comment):
  // landed here now that this wave's locale pass has reached them.
  // StatusStrip.tsx itself still needs a follow-up pass to read these
  // through useT() instead of its literals - components/* is 9A's own file,
  // named here rather than taken (this wave's report).
  'activity.crawl': 'Crawling pages',
  'activity.linkcheck': 'Checking links',
  'activity.captcha': 'Captcha',
  'activity.autoconfirm': 'Auto-confirming',
  'activity.pending': '{n} pending',
  'activity.ofTotal': '{n} of {total}',
  'activity.tooltipHint': '{active} active of {total} this run',

  // The notification centre's quiet-mode row (lib/toast.tsx, Wave 9's 9B) -
  // landed here verbatim from that file's own PENDING table (see its doc
  // comment), same arrangement as CollectorFacets/CollectorStats above;
  // PENDING itself is left in place, unread once these resolve through the
  // real catalogue.
  'notifications.quiet': 'Quiet mode',
  'notifications.quietHint':
    'Hides success and info notifications. A failure, a captcha waiting on you, or a benched account still shows.',

  // The task list's row tooltip (components/columns.tsx, Wave 9's 9D) -
  // landed here verbatim from that file's own PENDING table, same
  // arrangement.
  'task.tooltip.url': 'URL',
  'task.tooltip.changed': 'Last changed',

  // The row tooltip's swarm detail and the three hidden-by-default swarm
  // columns (components/columns.tsx, Wave 11.5E) - the six tooltip strings
  // land here verbatim from that file's own PENDING table (see its doc
  // comment); columns.peers/seeds/ratio do not go through PENDING at all -
  // TaskList.tsx/ColumnMenu.tsx call t(col.labelKey) directly with no
  // fallback of their own, so these three are what resolves the labelKey
  // casts the moment this lands.
  'task.tooltip.infoHash': 'Info hash',
  'task.tooltip.trackers': 'Trackers',
  'task.tooltip.swarm': 'Peers / seeds / ratio',
  'task.tooltip.swarmDetail': '{peers} peers, {seeds} seeds, ratio {ratio}',
  'task.tooltip.uploaded': 'Uploaded',
  'task.tooltip.seeding': 'Still seeding',
  'columns.peers': 'Peers',
  'columns.seeds': 'Seeds',
  'columns.ratio': 'Ratio',

  // Reaching a task's own file (components/FileActions.tsx, Wave 10's 10G) -
  // "Open" streams it through the browser; the other two are desktop-only and
  // carry their reason in file.desktopOnly when they are shown disabled.
  'file.open': 'Open',
  'file.openNatively': 'Open with default app',
  'file.revealInFolder': 'Show in folder',
  'file.desktopOnly': 'Desktop app only',

  // The timetable editor (pages/settings/Schedule.tsx, Wave 10's 10A) -
  // landed here verbatim from that file's own PENDING table, same
  // arrangement as Connections.tsx and Captcha.tsx before it.
  'settings.schedule.title': 'Schedule',
  'settings.schedule.subtitle': 'Pause, resume or cap the download speed on a timetable.',
  'settings.schedule.listTitle': 'Timetable',
  'settings.schedule.orderHint':
    'Rows are applied in order, top to bottom, and a later row wins where two windows overlap - so a broad "pause every night" above a narrow exception leaves the exception in force, and the same two rows the other way round do not.',
  'settings.schedule.add': 'Add window',
  'settings.schedule.empty': 'The queue runs on its own schedule',
  'settings.schedule.emptyHint':
    'No windows are configured, so nothing here ever pauses or limits the queue by the clock. Add one to hold downloads overnight or cap the speed while you are on the connection yourself.',
  'settings.schedule.use': 'Use this window',
  'settings.schedule.moveUp': 'Move up',
  'settings.schedule.moveDown': 'Move down',
  'settings.schedule.remove': 'Remove this window',
  'settings.schedule.edit': 'Edit this window',
  'settings.schedule.name': 'Name',
  'settings.schedule.namePlaceholder': 'e.g. Night pause',
  'settings.schedule.days': 'Days',
  'settings.schedule.daysHint':
    'Which weekdays this window opens on. For a window that runs past midnight, tick the day it STARTS on - "Fri 22:00-06:00" ends Saturday morning without Saturday itself being ticked.',
  'settings.schedule.preset.every': 'Every day',
  'settings.schedule.preset.weekdays': 'Weekdays',
  'settings.schedule.preset.weekends': 'Weekends',
  'settings.schedule.preset.custom': 'Custom',
  'settings.schedule.start': 'Start',
  'settings.schedule.end': 'End',
  'settings.schedule.endHint':
    'Before the start time, this window runs past midnight and ends the following morning. Equal to the start time is refused - that could mean a whole day or no time at all, and guessing which one you meant is worse than asking.',
  'settings.schedule.action': 'Action',
  'settings.schedule.action.pause': 'Pause',
  'settings.schedule.action.resume': 'Resume',
  'settings.schedule.action.limit': 'Limit speed',
  'settings.schedule.limit': 'Speed limit',
  'settings.schedule.disabledOff': 'This window is parked and never fires. The queue behaves as if the row were not here at all.',
  'settings.schedule.activeNow': 'Active now, until {time}',
  'settings.schedule.next': 'Next: {when}',
  'settings.schedule.never': 'Never fires as configured',
  'settings.schedule.stateNow.paused': 'The queue is paused by the timetable right now.',
  'settings.schedule.stateNow.limited': 'The queue is capped at {rate} by the timetable right now.',
  'settings.schedule.stateNow.running': 'No window is in force right now.',
  'settings.schedule.nextChange': 'Next change: {when}',
  'settings.schedule.noNextChange': 'Nothing in the table will ever change the queue as configured.',
  'settings.schedule.save': 'Save timetable',
  'settings.schedule.discard': 'Discard',
  'settings.schedule.unsaved': 'Unsaved changes to the timetable',
  'settings.schedule.saveFailed': 'The timetable could not be saved: {error}',
  'settings.schedule.rowError': 'Row {row}: {error}',

  // The end-of-queue countdown banner (components/IdleActionBanner.tsx,
  // Wave 10's 10B) - its STRINGS object and the one hardcoded toast string
  // were left in plain English on purpose (see that file's own doc comment):
  // landed here now that this wave's locale pass has reached them.
  // IdleActionBanner.tsx itself still needs a follow-up pass to read these
  // through useT() instead of its literals - components/* is 10B's own file,
  // named here rather than taken (this wave's report). idleAction.cancelFailed
  // is re-cased to match this catalogue's sentence style; the source literal
  // itself was lowercase with no closing period.
  'idleAction.title': 'The queue is idle',
  'idleAction.action.pause': 'Pausing',
  'idleAction.actionFallback': '"{action}" running',
  'idleAction.in': 'in {countdown}',
  'idleAction.cancel': 'Cancel',
  'idleAction.cancelling': 'Cancelling…',
  'idleAction.cancelFailed': 'Could not cancel: the server did not answer.',

  // The end-of-queue action's own settings row (pages/settings/
  // DownloadsSettings.tsx, Wave 10's 10B) - IDLE_ACTION_LABELS and the
  // group's label/hint/InfoBubble were left hardcoded for the same reason as
  // IdleActionBanner.tsx above (see that file's own doc comment); landed
  // here verbatim. DownloadsSettings.tsx still needs the same follow-up pass
  // to read these through useT().
  'settings.idleAction.label': 'End-of-queue action',
  'settings.idleAction.hint':
    'What happens once nothing is left running, queued or waiting to start. A link you have switched off does not count - see the info bubble.',
  'settings.idleAction.info':
    "A link you have switched off is never counted as work left to do, so it cannot hold this off forever. A manually paused or held link still counts - both mean 'wait a bit', not 'never'.",
  'settings.idleAction.none': 'Do nothing',
  'settings.idleAction.pause': 'Pause the queue',
  'settings.idleAction.delay': 'Countdown (seconds)',
  'settings.idleAction.delayHint': 'How long you have to cancel before the action runs, once the queue actually goes idle.',

  // The diagnostics page (pages/settings/Diagnostics.tsx, Wave 10's 10C) -
  // landed here verbatim from that file's own PENDING table, same
  // arrangement as Schedule.tsx above. settings.nav.diagnostics is this
  // page's rail label (registry.tsx registers the id, tx.ts's label() looks
  // up settings.nav.<id>).
  'settings.nav.diagnostics': 'Diagnostics',
  'settings.diagnostics.subtitle':
    'What this build is, what it is running on, and its own recent log output - for attaching to a bug report.',
  'settings.diagnostics.version': 'Version',
  'settings.diagnostics.deployment': 'Build',
  'settings.diagnostics.deployment.container': 'Container',
  'settings.diagnostics.deployment.desktop': 'Desktop',
  'settings.diagnostics.goVersion': 'Go',
  'settings.diagnostics.platform': 'Platform',
  'settings.diagnostics.goroutines': 'Goroutines',
  'settings.diagnostics.download': 'Download diagnostics bundle',
  'settings.diagnostics.downloading': 'Preparing…',
  'settings.diagnostics.downloadHint':
    'A JSON file with the fields above, your settings with every password removed, and the log lines below.',
  'settings.diagnostics.downloadFailed': 'Could not build the bundle: {error}',
  'settings.diagnostics.logTitle': 'Recent log lines',
  'settings.diagnostics.logHint': 'The last {n} lines this process has logged, oldest first. Nothing here is written to disk.',
  'settings.diagnostics.logEmpty': 'Nothing logged yet.',
  'settings.diagnostics.refresh': 'Refresh',
  'settings.diagnostics.loadFailed': 'Could not load diagnostics. Is the server reachable?',

  // The help page (pages/settings/Help.tsx, Wave 10's 10C) - landed here
  // verbatim from that file's own PENDING table, same arrangement as
  // Diagnostics.tsx above. settings.nav.help is this page's rail label, the
  // same relationship settings.nav.diagnostics has to Diagnostics.tsx.
  'settings.nav.help': 'Help',
  'settings.help.intro':
    'What this build can do, organised by task rather than by settings page. Every section links to where it is configured.',

  'settings.help.intake.title': 'Adding downloads',
  'settings.help.intake.body':
    'Paste links into the Collector - one per line, or messy text: a scan finds links wherever they sit, inside a sentence, several to a line, or wrapped across two lines by a mail client.',
  'settings.help.intake.b1': 'Drop a link-container file (.dlc, .ccf, .rsdf) or a plain link list.',
  'settings.help.intake.b2':
    "Click'n'Load buttons on hoster and forum pages work unchanged - KnightLoader answers on 127.0.0.1:9666, the same port every other downloader uses.",
  'settings.help.intake.b3':
    'Paste a page URL instead of a file link and switch Crawl on to pull out every file it links to, instead of downloading the page itself.',
  'settings.help.intake.b4': 'A watch folder picks up .txt/.crawljob files dropped into it automatically.',
  'settings.help.intake.link1': 'Open Downloads settings',
  'settings.help.intake.link2': "Open Access settings (Click'n'Load)",

  'settings.help.collector.title': 'The collector, before anything downloads',
  'settings.help.collector.body':
    'New links land in the Collector first, not the queue: a staging area to check names, sizes and duplicate warnings before anything starts. Auto-confirm can skip that step, with an optional delay; once confirmed, a link either starts immediately or waits in Queued, depending on Auto-start.',
  'settings.help.collector.link': 'Open General settings',

  'settings.help.rules.title': 'Rules: packages, folders and what gets kept',
  'settings.help.rules.body':
    'The Packagizer renames a link, chooses its folder and sets its download options as it arrives, from conditions you write. The Link Filter decides whether a link is kept at all - and unlike a filter that just makes links disappear, a rejection always names the rule that made it and why.',
  'settings.help.rules.link': 'Open Rules',

  'settings.help.queue.title': 'Managing the queue',
  'settings.help.queue.body':
    'Pause, resume or reorder any link, alone or as a whole package. Stopping the queue is two different actions: halting it leaves anything already running untouched, so it finishes on its own, while stopping every transfer right now shows what would be lost first and only goes ahead once you confirm - a transfer that cannot resume where it left off is exactly what that warning is for.',
  'settings.help.queue.b1': 'A single link can override the global connection count or the extract-after-download switch.',
  'settings.help.queue.b2': 'A duplicate of a link already in the list is always refused, before it ever reaches the collector.',

  'settings.help.limits.title': 'Getting through hoster limits',
  'settings.help.limits.body':
    'Three independent ways to stop a free-user limit from being the ceiling:',
  'settings.help.limits.b1': 'Connections - spread downloads over more than one outbound path (a second line, a proxy, SOCKS) instead of always leaving by this machine’s own address.',
  'settings.help.limits.b2':
    'Reconnect - ask the router for a new public address, the only thing that actually lifts a limit keyed to the address itself (UPnP, an external program, a script, or replaying HTTP requests against the router’s admin page - existing JDownloader LiveHeader/curl scripts work unchanged).',
  'settings.help.limits.b3':
    'Accounts - store premium or debrid logins (Real-Debrid, AllDebrid, TorBox and others) so an eligible link is fetched at full speed instead of the free-user limit.',
  'settings.help.limits.link1': 'Open Connections',
  'settings.help.limits.link2': 'Open Reconnect',
  'settings.help.limits.link3': 'Open Accounts',

  'settings.help.captcha.title': 'Captcha',
  'settings.help.captcha.body':
    'When a hoster asks for a captcha, an automatic solver you have configured is tried first, in the order you set. Anything it cannot solve - or if none is configured - is put in front of you instead of failing silently.',
  'settings.help.captcha.link': 'Open Captcha settings',

  'settings.help.after.title': 'After the download',
  'settings.help.after.body':
    'Archives are extracted automatically: zip (including encrypted, both WinZip AES and the legacy ZipCrypto), rar with multi-volume sets, 7z, tar, and gzip/bzip2/xz/zstd whether or not they wrap a tar - pure Go, no external unrar or 7z binary involved. A list of passwords is tried in order for an encrypted archive. A finished file is checked against whatever checksum shipped with it: an .sfv listing, an md5/sha1/sha256sum file, or a CRC32 the release name itself carries.',
  'settings.help.after.link1': 'Open Archives settings',
  'settings.help.after.link2': 'Open Downloads settings',

  'settings.help.schedule.title': 'Running unattended',
  'settings.help.schedule.body':
    'A weekly timetable pauses or throttles the queue by the clock - a nightly window, correct across the daylight-saving change - the same idea as JDownloader’s Scheduler.',
  'settings.help.schedule.link': 'Open Schedule',

  'settings.help.instances.title': 'Running more than one instance',
  'settings.help.instances.body':
    'Add another KnightLoader as a peer and its queue shows up on this one’s dashboard too - self-hosted, no relay involved: this instance simply calls that instance’s own API, the same way a browser would.',
  'settings.help.instances.link': 'Open Instances',

  'settings.help.access.title': 'Access and troubleshooting',
  'settings.help.access.body':
    'A password locks the whole interface down to a session cookie. The Access page also lists the intake ports and access methods this build has, and why - so an unfamiliar open port has an answer instead of a guess. The Diagnostics page builds a file to attach to a bug report: version and build info, the current settings with every password removed, this process’s own recent log lines, and how many goroutines are running.',
  'settings.help.access.link1': 'Open Access settings',
  'settings.help.access.link2': 'Open Diagnostics',

  'settings.help.advanced.title': 'Everything else',
  'settings.help.advanced.body':
    'Every setting this build has can be read and changed by its raw name on the Advanced page, including a few - how a mirror of an already-downloaded file is treated, what happens when a download would land on a name already taken - that do not have a dedicated control anywhere else yet.',
  'settings.help.advanced.link': 'Open Advanced settings',

  // The script editor (pages/settings/Scripts.tsx) and its manual-invocation
  // menu entry (components/ScriptActions.tsx) - Wave 11B, JD's "Event
  // Scripter" (census family E). settings.nav.scripts is this page's rail
  // label, the same relationship every settings.nav.<id> key has to its own
  // page - not yet reachable from the rail itself (see that file's own doc
  // comment), landed regardless so the label is ready the moment it is.
  'settings.nav.scripts': 'Scripts',
  'settings.scripts.title': 'Scripts',
  'settings.scripts.subtitle': 'Automate KnightLoader with your own JavaScript, run on an event or on demand.',
  'settings.scripts.listTitle': 'Your scripts',
  'settings.scripts.add': 'Add script',
  'settings.scripts.empty': 'No scripts yet',
  'settings.scripts.emptyHint':
    'A script runs your own JavaScript when something happens - a download finishes, one fails, the queue goes idle - or on demand, from Test Run here and from the “Run script” entry this wave adds to the download list’s right-click menu. Add one to get started.',
  'settings.scripts.loadFailed':
    'Scripts could not be loaded. If this build does not yet include the automation engine, this page has nothing to show yet - try again once it does.',
  'settings.scripts.name': 'Name',
  'settings.scripts.namePlaceholder': 'e.g. Notify on failure',
  'settings.scripts.unnamed': 'Untitled script {n}',
  'settings.scripts.trigger': 'Runs on',
  'settings.scripts.triggerHint':
    'What starts this script. Manual only ever runs when you ask for it - from Test Run below, or from the “Run script” entry this wave adds to the download list’s right-click menu.',
  'settings.scripts.trigger.manual': 'Manual (on demand only)',
  'settings.scripts.trigger.taskDone': 'A download finishes',
  'settings.scripts.trigger.taskFailed': 'A download fails',
  'settings.scripts.trigger.queueIdle': 'The queue goes idle',
  'settings.scripts.use': 'Enable this script',
  'settings.scripts.code': 'Code',
  'settings.scripts.codeStarter':
    '// This script runs on the trigger picked above.\n// The sandbox API it runs against is still being finished - see Settings › Help once it lands.\n',
  'settings.scripts.timeout': 'Time limit',
  'settings.scripts.timeoutHint':
    'How long this script may run before it is stopped. Between 100 ms and 30 s; 0 uses the default of 5000 ms.',
  'settings.scripts.timeoutUnit': 'ms',
  'settings.scripts.save': 'Save',
  'settings.scripts.saving': 'Saving…',
  'settings.scripts.saveFailed': 'Could not save: {error}',
  'settings.scripts.discard': 'Discard changes',
  'settings.scripts.remove': 'Remove',
  'settings.scripts.removeNew': 'Cancel',
  'settings.scripts.removeFailed': 'Could not remove: {error}',
  'settings.scripts.unsaved': 'Unsaved',
  'settings.scripts.run': 'Test run',
  'settings.scripts.running': 'Running…',
  'settings.scripts.runNeedsSaveHint': 'Save this script once before testing it.',
  'settings.scripts.runDirtyHint': 'Save your changes to test the latest version.',
  'settings.scripts.runOk': 'Ran successfully',
  'settings.scripts.runOkDuration': 'Ran successfully in {ms} ms',
  'settings.scripts.runTimedOut': 'Stopped: ran longer than its time limit',
  'settings.scripts.runFailed': 'Failed: {error}',
  'settings.scripts.output': 'Output',

  // The manual-invocation half of Wave 11B (components/ScriptActions.tsx) -
  // the "Run script" entry on the download list's own right-click menu.
  'task.runScript': 'Run script',
  'task.runScriptUnnamed': 'Untitled script',
  'task.runScriptDone': 'Ran “{name}”',
  'task.runScriptFailed': '“{name}” failed: {error}',

  // The Remote access section and API tokens (pages/settings/Access.tsx,
  // Wave 11C) - build-plan.md section 8's Wave 11 amendment on 11C: named,
  // individually revocable tokens; the addresses this instance answers on,
  // with a QR code; the PWA install BrowserTools.tsx also offers; and the
  // loud warning when the server is reachable from off this machine with no
  // password set.
  'settings.access.tokens.title': 'API tokens',
  'settings.access.tokens.intro':
    'Named credentials for a script, a browser extension or a phone. Each one can be revoked on its own, without changing the shared password every other client uses.',
  'settings.access.tokens.empty': 'No tokens issued yet.',
  'settings.access.tokens.new': 'New token',
  'settings.access.tokens.namePlaceholder': 'e.g. my phone',
  'settings.access.tokens.cancel': 'Cancel',
  'settings.access.tokens.create': 'Create',
  'settings.access.tokens.creating': 'Creating…',
  'settings.access.tokens.created': 'Created',
  'settings.access.tokens.lastUsed': 'Last used',
  'settings.access.tokens.neverUsed': 'never',
  'settings.access.tokens.revoke': 'Revoke',
  'settings.access.tokens.secretTitle': 'Copy this token now',
  'settings.access.tokens.secretWarning':
    'This is the only time this token is shown. It is stored as a one-way hash on this instance, so if it is lost there is no way to read it back, only to revoke it and create a new one.',
  'settings.access.tokens.copy': 'Copy',
  'settings.access.tokens.copied': 'Copied',
  'settings.access.tokens.done': 'Done',
  'settings.access.tokens.howToUse': 'Send it as a header: Authorization: Bearer <token>',
  'settings.access.tokens.createFailed': 'Could not create the token: {error}',
  'settings.access.remote.title': 'Remote access',
  'settings.access.remote.desktopNote':
    'This is the desktop build. It does not serve the API over the network at all, so there is nothing here to reach from outside this application.',
  'settings.access.remote.exposedWarning':
    'This instance just answered a request from outside this machine, and no password protects it. Anyone who can reach it can see and control every download. Set a password above now.',
  'settings.access.remote.noRelayBody':
    'There is no account service and no pairing step, and there never will be: running one would mean an ongoing hosted service with real cost and liability, not a feature of a self-hosted binary. Reaching this instance from outside your own network is your own port forward, reverse proxy or VPN, the same as any other self-hosted server.',
  'settings.access.remote.addressesTitle': 'Addresses this instance answers on',
  'settings.access.remote.noAddresses': 'No address could be determined for this request.',
  'settings.access.remote.loopback': 'this machine only',
  'settings.access.remote.scanHint': 'Only works on the same network as this instance.',
  'settings.access.remote.installTitle': 'Install as an app',
  'settings.access.remote.installBody':
    'Add KnightLoader to a home screen or app list for a faster launch, without the browser chrome.',
  'settings.access.remote.install': 'Install',
  'settings.access.remote.installIOS':
    'On iPhone or iPad: open this page in Safari, tap Share, then "Add to Home Screen".',

  // Sending KnightLoader a link from outside the app - the bookmarklet, the
  // MV3 browser extension and the PWA install step (pages/settings/
  // BrowserTools.tsx, Wave 11D). settings.nav.browsertools is this page's
  // rail label.
  'settings.nav.browsertools': 'Browser tools',
  'settings.browsertools.subtitle':
    'Send a link to KnightLoader from anywhere else - another page, another app, or your phone’s Share menu.',
  'settings.browsertools.bookmarkletTitle': 'Bookmarklet',
  'settings.browsertools.bookmarkletHint':
    'Drag this to your bookmarks bar. On any page, click it to send that page (or whatever text you have selected) here.',
  'settings.browsertools.bookmarkletLink': 'Add to KnightLoader',
  'settings.browsertools.copyCode': 'Copy the code instead',
  'settings.browsertools.copied': 'Copied.',
  'settings.browsertools.extensionTitle': 'Browser extension',
  'settings.browsertools.extensionHint':
    'A right-click menu on any link, selection, or page, in Chrome, Edge, Brave, and other Chromium-based browsers. The download already points at this instance - nothing to configure.',
  'settings.browsertools.download': 'Download extension',
  'settings.browsertools.installTitle': 'Install as an app',
  'settings.browsertools.installHint':
    'Once installed, your device’s own Share menu can hand a link straight to KnightLoader - no browser tab required.',
  'settings.browsertools.install': 'Install',
  'settings.browsertools.installed': 'Already installed, or this browser offers its own install step in the address bar.',

  // /quickadd (pages/QuickAdd.tsx, Wave 11D) - the one page the bookmarklet,
  // the browser extension and the PWA share target all land on.
  'quickadd.title': 'Add to KnightLoader',
  'quickadd.manualLabel': 'Link (or paste several, one per line)',
  'quickadd.manualPlaceholder': 'https://example.com/file.zip',
  'quickadd.add': 'Add',
  'quickadd.adding': 'Adding…',
  'quickadd.emptyHint':
    'Nothing was shared - paste a link by hand, or use this page from the bookmarklet, the browser extension, or your device’s Share menu.',
  'quickadd.staged': 'Added to the collector.',
  'quickadd.stagedNamed': 'Added “{name}” to the collector.',
  'quickadd.stagedCount': 'Added {n} links to the collector.',
  'quickadd.none': 'Nothing was added - every link here was already in the collector.',
  'quickadd.failed': 'Could not add this: {error}',
  'quickadd.undo': 'Undo',
  'quickadd.undone': 'Removed.',
  'quickadd.openCollector': 'Open Collector',
  'quickadd.close': 'Close window',

  // Quit/restart/backup/restore (pages/settings/System.tsx) - build-plan.md's
  // Wave 10 (10D) shipped the whole backend with no page pointing at it at
  // all; found by that wave's own adversarial review. settings.nav.system is
  // this page's rail label.
  'settings.nav.system': 'System',
  'settings.system.subtitle': 'Quit, restart, and back up or restore this instance’s data.',
  'settings.system.deployment.container': 'Container',
  'settings.system.deployment.desktop': 'Desktop',
  'settings.system.lifecycleTitle': 'Quit & restart',
  'settings.system.quit': 'Quit',
  'settings.system.restart': 'Restart',
  'settings.system.quitConfirmTitle': 'Quit KnightLoader?',
  'settings.system.restartConfirmTitle': 'Restart KnightLoader?',
  'settings.system.quitConfirmBody': 'In-flight work is drained first, then the process exits. {note}',
  'settings.system.confirmCancel': 'Cancel',
  'settings.system.confirmProceed': 'Confirm',
  'settings.system.unavailable': 'This build has no way to do this from the browser.',
  'settings.system.acting': 'Working…',
  'settings.system.shuttingDown':
    'Shutting down. If this instance comes back on its own, the page will reconnect once it does; otherwise close this tab.',
  'settings.system.actionFailed': 'Could not do this: {error}',
  'settings.system.backupTitle': 'Backup',
  'settings.system.backupHint':
    'Downloads the database and settings as one archive, including passwords - keep it somewhere private.',
  'settings.system.backupButton': 'Download backup',
  'settings.system.restoreTitle': 'Restore',
  'settings.system.restoreHint': 'Replaces this instance’s data with a previously downloaded backup.',
  'settings.system.restoreButton': 'Upload backup…',
  'settings.system.restoreConfirmTitle': 'Restore from backup?',
  'settings.system.restoreConfirmBody':
    'This replaces the current database and settings with the contents of “{name}”. This cannot be undone.',
  'settings.system.restoring': 'Validating and staging…',
  'settings.system.restoreFailed': 'Could not restore: {error}',
  'settings.system.restoreStaged': '{status}',
  'settings.system.loadFailed': 'Could not load. Is the server reachable?',

  // Two more rail labels this wave's pages need: Resolvers.tsx (11E, yt-dlp
  // format/subtitle/output-template options) and the "ytdlp" module row
  // (routes_features.go) both already call label()/tx() against these keys
  // today, falling back to the raw id until now. Resolvers.tsx's own page
  // body is still hardcoded English throughout - see this wave's own report;
  // unlike every sibling page above it ships with no PENDING table at all, so
  // there are no ready-made keys to land verbatim here.
  'settings.nav.resolvers': 'Resolvers',
  'settings.module.ytdlp': 'yt-dlp',

  // The Torrents settings page (pages/settings/Torrents.tsx, Wave 11.5E) -
  // seed target, transfer limit, port + UPnP mapping, DHT/PEX. Landed here
  // verbatim from that file's own PENDING table (see its doc comment); two
  // more, settings.nav.torrents and settings.module.torrents, are not in
  // that table at all - registry.tsx and routes_features.go already call
  // label() against them (Settings.tsx's rail, Modules.tsx's row), falling
  // back to the raw "torrents" id until now, the same gap this wave's own
  // report closes the way 11G's report closed it for settings.nav.resolvers/
  // settings.module.ytdlp just above.
  'settings.nav.torrents': 'Torrents',
  'settings.module.torrents': 'Torrents',
  'settings.torrents.title': 'Torrents',
  'settings.torrents.subtitle':
    'Seed targets, transfer limits, port mapping and DHT/PEX for magnet links and .torrent files.',
  'settings.torrents.seedingTitle': 'Seeding',
  'settings.torrents.seedRatio': 'Seed ratio target',
  'settings.torrents.seedRatioHint':
    'Keep seeding a finished torrent until this much has gone back to the swarm, relative to its own size. 0 = no ratio target.',
  'settings.torrents.seedDuration': 'Seed duration target',
  'settings.torrents.seedDurationHint':
    'Keep seeding a finished torrent for this long after it completes. 0 = no time limit. Whichever of the two targets above is reached first stops seeding.',
  'settings.torrents.seedDurationUnit': 'hours',
  'settings.torrents.transferTitle': 'Transfer limit',
  'settings.torrents.uploadLimit': 'Upload limit',
  'settings.torrents.uploadLimitHint': 'Caps how fast a torrent uploads to the swarm while seeding. 0 = unlimited.',
  'settings.torrents.uploadLimitUnit': 'KiB/s',
  'settings.torrents.portTitle': 'Port & mapping',
  'settings.torrents.port': 'Port',
  'settings.torrents.portHint':
    'The port this instance listens for swarm connections on. 0 lets the torrent engine pick one.',
  'settings.torrents.portMapHint':
    'Asks the router to forward the port above to this machine over UPnP, so peers behind a different router can still reach it. Not every router supports this, and some accept the request without it actually working.',
  'settings.torrents.portMapButton': 'Attempt UPnP mapping',
  'settings.torrents.portMapping': 'Asking the router…',
  'settings.torrents.portMapNeedsPort':
    'Set a port above before mapping it - 0 leaves nothing for the router to forward to.',
  'settings.torrents.portMapConfirmed': 'Confirmed: port {port} is mapped and was verified reachable.',
  'settings.torrents.portMapUnconfirmed':
    'The router accepted the request, but the mapping could not be confirmed as actually working. Some routers do this silently - try a connectivity check from outside the network.',
  'settings.torrents.portMapFailed': 'Could not map the port: {error}',
  'settings.torrents.portMapUnavailable': 'This build does not expose port mapping yet.',
  'settings.torrents.networkTitle': 'Peer discovery',
  'settings.torrents.dht': 'DHT',
  'settings.torrents.dhtHint':
    'Finds peers with no tracker involved, using other BitTorrent clients as a distributed lookup.',
  'settings.torrents.pex': 'Peer exchange (PEX)',
  'settings.torrents.pexHint': 'Trades known peers with the ones already connected, so a swarm with few peers is found faster.',
  'settings.torrents.privateNote':
    'A private torrent switches both off automatically once its metadata is known, regardless of what is set here - immediately for an uploaded .torrent file, or as soon as a magnet link\'s own metadata arrives from the swarm. Most private trackers ban accounts that use either.',
  'settings.torrents.engineNote':
    'Seed ratio, seed duration and port now reach every torrent this engine starts - port only for the very first one since this instance’s last restart, because the engine’s own torrent client is built once and never rebuilt afterwards; a later save is still stored correctly and takes effect from the next restart on. Upload limit is still saved and validated only, with nowhere yet for the engine to carry it into a running download. DHT and PEX below are the same story for an ORDINARY torrent: this instance’s own default does not yet reach a running download either, so a torrent still seeds with both on regardless of what is set here - a PRIVATE torrent is a different case entirely, see the note further down. The mapping button further down still does a real thing: it asks the router to forward the port number typed above, honestly, whether or not a torrent is actually listening on it yet.',

  // The first-run tour (components/OnboardingWizard.tsx): a short walkthrough
  // shown once, gated on onboarding.done in the shared uistate bucket (see
  // that file's own doc comment) rather than a page of its own - it is an
  // overlay, mounted once beside CaptchaModal and IdleActionBanner, not a
  // route.
  'onboarding.step': 'Step {n} of {total}',
  'onboarding.skip': 'Skip',
  'onboarding.back': 'Back',
  'onboarding.next': 'Next',
  'onboarding.finish': 'Start using KnightLoader',
  'onboarding.welcome.title': 'Welcome to KnightLoader',
  'onboarding.welcome.body':
    "KnightLoader is a self-hosted download manager: paste or drop a link and it is fetched by whichever backend actually handles it - the built-in engine for plain file links, yt-dlp for media pages, a debrid service for hosters you already pay for, or a headless JDownloader for everything else. This short tour covers the one setting worth checking before you start.",
  'onboarding.welcome.langLabel': 'Interface language',
  'onboarding.folder.title': 'Where should files land?',
  'onboarding.folder.body':
    'This is the folder finished downloads are written into. Type a path or browse the server for one - you can change it again anytime from the General settings page.',
  'onboarding.accounts.title': 'Premium hosters, whenever you want them',
  'onboarding.accounts.body':
    'The queue works with no account at all. When you do want full speed on a hoster or debrid service you already pay for, add its login under Accounts - nothing here needs to be set up now.',
  'onboarding.accounts.link': 'Open Accounts',
  'onboarding.finished.title': "You're set",
  'onboarding.finished.body':
    'That covers the tour. The Help page under Settings has a full walkthrough of everything else this build does, and most controls carry their own explanation behind an (i) icon.',

  // The command registry core (lib/commands/, Wave 12A) - the first, small
  // set of commands visible on every surface: open the palette itself, the
  // theme switch (its own label already existed as theme.toggle, reused
  // rather than duplicated) and one "go to X" per main page. `group` on a
  // Command is a real TranslationKey string, not literal English - see
  // lib/commands/types.ts's own doc comment on why - so every later surface
  // file reuses commands.group.navigation below or adds its own key here,
  // never a bare word inline.
  'commands.openPalette': 'Open command palette',
  'commands.group.general': 'General',
  'commands.group.navigation': 'Go to',
  'commands.goOverview': 'Go to Overview',
  'commands.goDownloads': 'Go to Downloads',
  'commands.goCollector': 'Go to Collector',
  'commands.goInstances': 'Go to Instances',
  'commands.goAccounts': 'Go to Accounts',
  'commands.goSettings': 'Go to Settings',

  // lib/commands/settings.ts (Wave 12): one command per settings sub-page,
  // e.g. "Settings: Torrents" - see that file's own doc comment. Every
  // labelKey below reuses the page's existing settings.nav.<id> string
  // rather than minting a second name per page.
  'commands.group.settings': 'Settings',
  // lib/commands/queue.ts (Wave 12): the shell's own master switch
  // (QueueBar.tsx), reachable from every page rather than only Downloads'
  // own copy (commands/downloads.ts) - see that file's own doc comment.
  'commands.group.queue': 'Queue',
  // lib/commands/language.ts (Wave 12): open/close the sidebar's language
  // dropdown (components/LanguagePicker.tsx) from the palette.
  'commands.group.language': 'Language',
  // lib/commands/downloads.ts and lib/commands/collector.ts (Wave 12): the
  // bulk/page-level actions those two pages' toolbars already had a plain
  // onClick for - pause/resume/retry, select all, remove selected, clear
  // finished, start selected/all, check all. See either file's own doc
  // comment for which existing function each command calls.
  'commands.group.downloads': 'Downloads',
  'commands.group.collector': 'Collector',

  // components/CommandPalette.tsx (Wave 12): the overlay itself, not any one
  // command in it.
  'commands.paletteLabel': 'Command palette',
  'commands.searchPlaceholder': 'Type a command…',
  'commands.noResults': 'No matching commands',

  // The Shortcuts settings tab (pages/settings/Shortcuts.tsx, Wave 12): every
  // command with a default keyboard shortcut, grouped and rebindable. See
  // that file's own doc comment for why it reads lib/commands/allCommands.ts
  // rather than useCommands(), and why `group` is shown through a
  // fall-back-to-raw-string lookup instead of a plain t() call.
  'settings.nav.shortcuts': 'Shortcuts',
  'settings.shortcuts.subtitle':
    'Every command that ships with a default keyboard shortcut, grouped by where it applies. Change a binding, or reset it back to its default.',
  'settings.shortcuts.empty': 'No commands have a default shortcut yet.',
  'settings.shortcuts.change': 'Change',
  'settings.shortcuts.reset': 'Reset',
  'settings.shortcuts.resetAll': 'Reset all to default',
  'settings.shortcuts.resetAllConfirmTitle': 'Reset all shortcuts?',
  'settings.shortcuts.resetAllConfirmBody': 'Every rebound shortcut goes back to its default. This cannot be undone.',
  'settings.shortcuts.resetAllConfirm': 'Reset all',
  'settings.shortcuts.captureTitle': 'Press a new shortcut for “{name}”',
  'settings.shortcuts.captureHint': 'Press a key combination, or Escape to cancel.',
  'settings.shortcuts.conflict': '“{combo}” is already used by {command}.',
} as const;

export type TranslationKey = keyof typeof en;
export type Dict = Record<TranslationKey, string>;
