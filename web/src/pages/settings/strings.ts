/**
 * The English source strings for the settings tree, keyed by the translation
 * key each one is destined for.
 *
 * They live here and not in lib/locales/en.ts because the locale files are one
 * writer's lane per wave and this wave's writer is somebody else. The moment
 * those keys land in the catalogue, `tx` below starts returning the translated
 * string and nothing in this directory has to change — the table is the
 * fallback, not the source of truth.
 *
 * So: adding a string here is a debt, not a home. It is paid off by copying the
 * entries into en.ts and the other 41 locales and deleting them from here.
 */
export const PENDING = {
  // The rail.
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

  // The shell.
  'settings.unsaved': 'Unsaved changes',
  'settings.discard': 'Discard',
  'settings.railLabel': 'Settings sections',
  'settings.empty': 'Nothing to configure here yet.',
  'settings.emptyHint':
    'The page is registered so its address keeps working and so the controls land here when the subsystem does.',

  // General.
  'settings.general.subtitle': 'Where files land and what happens to a link the moment it arrives.',
  'settings.sectionIntake': 'New links',

  // Downloads.
  'settings.downloads.watchOff':
    'Folder watch is switched off on the Modules page, which is why this is not editable. Switching it back on restores the folder that was set here.',

  // Modules.
  'settings.modules.subtitle': 'What this build contains, and what it does not.',
  'settings.modules.fixedAtBuild':
    'The set of modules is fixed when the binary is built. Nothing can be installed into a running instance, so this list is the whole of it.',
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
  'settings.modules.configureFirst':
    'There is nothing to switch on yet. Set this up on {page} first; after that the switch here stops and restarts it without losing what you set.',
  'settings.modules.saveFirst':
    'Save or discard the pending changes first. Switching a module writes the settings straight away, and the two writes would overwrite each other.',

  // Module names. An id with no entry falls back to the id, so a module a later
  // wave adds shows up unlabelled rather than not at all.
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

  // Advanced.
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
  'settings.advanced.secret':
    'A stored secret is never sent to the browser. Leave the placeholder to keep it, or type a new value to replace it.',
  'settings.advanced.listHint': 'Edited as JSON here. The page for it has the proper editor.',
  'settings.advanced.defaultsUnavailable':
    'The factory values could not be fetched, so nothing can be reset from here.',
  'settings.advanced.type.boolean': 'yes / no',
  'settings.advanced.type.number': 'number',
  'settings.advanced.type.text': 'text',
  'settings.advanced.type.list': 'list',
  'settings.advanced.type.object': 'group',

  // Access.
  'settings.access.subtitle': 'Who can reach this instance, and from where.',
  'settings.sectionIntakePorts': 'Ports and listeners',
} as const;

export type PendingKey = keyof typeof PENDING;
