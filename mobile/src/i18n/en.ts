// English is the source of truth, same convention as the web UI's own
// lib/locales/en.ts. Every other locale is typed as Dict, so a missing or
// stray key is a compile error rather than a silent fallback.
export const en = {
  'connect.title': 'Connect to KnightLoader',
  'connect.hint':
    'Create a token on the Access tab of the KnightLoader web UI and paste it in here. The address can be scanned from the QR code on that same page.',
  'connect.nameLabel': 'Name (optional)',
  'connect.namePlaceholder': 'My KnightLoader',
  'connect.addressLabel': 'Server address',
  'connect.qrButton': 'QR',
  'connect.tokenLabel': 'Token',
  'connect.tokenPlaceholder': 'paste it here',
  'connect.errorMissing': 'Enter a server address and a token.',
  'connect.errorTokenRejected': 'Reached the server, but it did not accept the token.',
  'connect.connectButton': 'Connect',
  'connect.qrHintAddress': 'Scan the QR code from the Access tab',
  'connect.qrAutofillNotice': 'Name and address taken from the code. This kind of code never carries a token - paste that in by hand.',

  'qr.cancel': 'Cancel',
  'qr.cameraPermissionHint': 'Camera access is needed to scan the QR code.',
  'qr.grantAccess': 'Allow access',

  'connections.addButton': '+ Connection',
  'connections.empty': 'No connection saved yet.',
  'connections.emptyButton': 'Add your first connection',
  'connections.remove': 'Remove',

  'instances.title': 'Instances',
  'instances.subtitle': 'Every KnightLoader that {name} knows about.',
  'instances.empty': 'No other instances known.',
  'instances.manualTitle': 'Add manually',
  'instances.namePlaceholder': 'Name',
  'instances.urlPlaceholder': 'http://host:port',
  'instances.addButton': 'Add',
  'instances.addOfflineWarning': 'Added, but not reachable right now.',
  'instances.addError': 'Could not add that instance.',
  'instances.pairingTitle': 'By pairing code',
  'instances.pairingHint':
    'Generate a pairing code on the Access tab of the other instance, then paste it in here or scan its QR code.',
  'instances.codePlaceholder': 'Paste the code',
  'instances.redeemButton': 'Redeem code',
  'instances.redeeming': 'Pairing…',
  'instances.pairError': 'That code is invalid or has expired.',
  'instances.pairSuccess': '{name} added.',
  'instances.pairSuccessOffline': ' (not reachable right now)',
  'instances.scanHint': "Scan the pairing QR from the other instance's Access tab",
  'instances.remove': 'Remove',

  'downloads.connected': 'connected',
  'downloads.connecting': 'connecting…',
  'downloads.instancesLink': 'Instances',
  'downloads.switchLink': 'Switch',
  'downloads.queueHalted': 'Halted',
  'downloads.queueRunning': 'Running',
  'downloads.queueActive': '{n} active',
  'downloads.empty': 'No downloads.',
  'downloads.emptyConnecting': 'Connecting to the server…',

  'addDownload.title': 'Add links',
  'addDownload.titlePeer': 'Add links – {name}',
  'addDownload.hint': 'One link per line, same as the paste box on the web UI.',
  'addDownload.placeholder': 'https://…',
  'addDownload.errorEmpty': 'Paste at least one link.',
  'addDownload.errorServer': 'Server: {message}',
  'addDownload.errorGeneric': 'Could not send the links.',
  'addDownload.cancel': 'Cancel',
  'addDownload.button': 'Add',

  'status.queued': 'queued',
  'status.running': 'running',
  'status.paused': 'paused',
  'status.finished': 'finished',
  'status.failed': 'failed',
  'status.extracting': 'extracting',
} as const;

export type Dict = { [K in keyof typeof en]: string };
export type TranslationKey = keyof Dict;
