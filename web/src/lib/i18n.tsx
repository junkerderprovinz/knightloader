import { createContext, useCallback, useContext, useState, type ReactNode } from 'react';

// English is the source of truth: every other locale is typed against it, so a
// missing or stray key is a compile error rather than a runtime fallback.
const en = {
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
  'downloads.subtitle': 'Active and finished transfers.',
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
  'accounts.keyStored': 'A key is stored. Enter a new one to replace it. Applied on restart.',
  'accounts.keyHint': 'Applied on restart; stored encrypted on this instance.',
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
  'settings.speedHint': 'Applies to yt-dlp and JDownloader.',
  'settings.extract': 'Extract archives after download',
  'settings.deleteArchive': 'Delete archive after extraction',
  'settings.autoStart': 'Start added links immediately (skip the collector)',
  'settings.save': 'Save',
  'settings.saved': 'Saved.',
} as const;

export type TranslationKey = keyof typeof en;

const de: Record<TranslationKey, string> = {
  'nav.overview': 'Übersicht',
  'nav.collector': 'Sammler',
  'nav.downloads': 'Downloads',
  'nav.instances': 'Instanzen',
  'nav.accounts': 'Konten',
  'nav.settings': 'Einstellungen',
  'nav.workingTitle': 'Arbeitstitel',
  'theme.dark': 'Dunkel',
  'theme.light': 'Hell',
  'theme.toggle': 'Design wechseln',
  'lang.label': 'Sprache',

  'status.collected': 'Gesammelt',
  'status.queued': 'Wartet',
  'status.running': 'Läuft',
  'status.paused': 'Pausiert',
  'status.extracting': 'Entpackt',
  'status.done': 'Fertig',
  'status.error': 'Fehler',

  'task.pause': 'Pausieren',
  'task.resume': 'Fortsetzen',
  'task.start': 'Starten',
  'task.restart': 'Neu starten',
  'task.remove': 'Entfernen',
  'task.ready': 'Bereit zum Laden',
  'task.left': 'übrig',
  'task.file': 'Datei',
  'task.files': 'Dateien',
  'task.ungrouped': 'Ohne Paket',

  'overview.title': 'Übersicht',
  'overview.subtitle': 'Alles auf einen Blick.',
  'overview.totalSpeed': 'Gesamtgeschwindigkeit',
  'overview.active': 'Aktiv',
  'overview.queued': 'Wartet',
  'overview.inCollector': 'Im Sammler',
  'overview.done': 'Fertig',
  'overview.errors': 'Fehler',
  'overview.recent': 'Zuletzt',
  'overview.noDownloads': 'Noch keine Downloads.',
  'overview.instances': 'Instanzen',

  'collector.title': 'Sammler',
  'collector.subtitle': 'Links einfügen oder hineinziehen — sie werden analysiert und gesammelt, dann startest du sie.',
  'collector.placeholder': 'Links einfügen — eine URL pro Zeile — oder hier ablegen…  (Strg+Enter zum Hinzufügen)',
  'collector.package': 'Paket (optional)',
  'collector.add': 'Zum Sammler',
  'collector.empty': 'Der Sammler ist leer. Füge oben Links ein, um sie zu sammeln.',
  'collector.staged': 'gesammelt',
  'collector.selected': 'ausgewählt',
  'collector.startSelected': 'Auswahl starten',
  'collector.startAll': 'Alle starten',
  'collector.remove': 'Entfernen',
  'collector.toastStaged': '{n} Link(s) gesammelt',
  'collector.toastSkipped': '{n} Link(s) gesammelt, {skipped} bereits bekannt',
  'collector.toastNone': 'Keine gültigen Links gefunden',
  'collector.toastStarted': '{n} Download(s) gestartet',
  'collector.selectAll': 'Alle auswählen',
  'collector.selectNone': 'Auswahl aufheben',
  'collector.removeOffline': 'Offline entfernen',
  'collector.movePrompt': 'In welches Paket sollen die ausgewählten Links?',
  'collector.toastMoved': '{n} Link(s) nach „{pkg}“ verschoben',
  'collector.move': 'Ins Paket',

  'downloads.title': 'Downloads',
  'downloads.subtitle': 'Laufende und abgeschlossene Übertragungen.',
  'downloads.thisInstance': 'Diese Instanz',
  'downloads.totalSpeed': 'Gesamtgeschwindigkeit',
  'downloads.pauseAll': 'Pausieren',
  'downloads.resumeAll': 'Fortsetzen',
  'downloads.retryFailed': 'Fehler erneut',
  'downloads.clearFinished': 'Aufräumen',
  'downloads.filterPlaceholder': 'Nach Namen filtern…',
  'downloads.filterAll': 'Alle',
  'downloads.filterActive': 'Aktiv',
  'downloads.filterDone': 'Fertig',
  'downloads.filterErrors': 'Fehler',
  'downloads.noMatch': 'Nichts passt zu diesem Filter.',
  'downloads.emptyLead': 'Noch nichts am Laden. Füge Links im',
  'downloads.emptyTail': 'hinzu und starte sie.',
  'downloads.finished': '{name} fertig',
  'downloads.failed': '{name} fehlgeschlagen',

  'instances.title': 'Instanzen',
  'instances.subtitle': 'Alle KnightLoader von einer Oberfläche aus sehen und steuern.',
  'instances.thisInstance': 'Diese Instanz',
  'instances.add': 'Instanz hinzufügen',
  'instances.name': 'Name',
  'instances.url': 'URL',
  'instances.addButton': 'Hinzufügen',
  'instances.offlineWarning': 'Hinzugefügt, aber die Instanz antwortet nicht (offline?).',
  'instances.open': 'Öffnen',
  'instances.online': 'Online',
  'instances.offline': 'Offline',
  'instances.metricActive': 'Aktiv',
  'instances.metricTasks': 'Aufgaben',
  'instances.metricSpeed': 'Tempo',
  'instances.removeTitle': '{name} entfernen',

  'accounts.title': 'Konten',
  'accounts.subtitle': 'Premium- und Debrid-Konten bleiben deine — verschlüsselt auf dieser Instanz gespeichert.',
  'accounts.connected': 'Verbunden',
  'accounts.notConnected': 'Nicht verbunden',
  'accounts.connect': 'Verbinden',
  'accounts.replace': 'Schlüssel ersetzen',
  'accounts.disconnect': 'Trennen',
  'accounts.saved': 'Gespeichert.',
  'accounts.keyLabel': '{service} API-Schlüssel',
  'accounts.keyStored': 'Ein Schlüssel ist hinterlegt. Neuen eingeben zum Ersetzen. Gilt nach Neustart.',
  'accounts.keyHint': 'Gilt nach Neustart; verschlüsselt auf dieser Instanz gespeichert.',
  'accounts.placeholder': 'Schlüssel einfügen…',
  'accounts.more': 'Verbinde einen der unterstützten Dienste — Links werden automatisch zu dem geleitet, der den Hoster abdeckt.',
  'accounts.blurb.torbox': 'Debrid — schaltet einen großen Hoster-Katalog zu Direkt-Downloads frei.',
  'accounts.blurb.alldebrid': 'Debrid — Ein-Klick-Freischaltung für viele Hoster.',
  'accounts.blurb.realdebrid': 'Debrid — wandelt Hoster-Links in Direkt-Downloads um.',

  'settings.title': 'Einstellungen',
  'settings.subtitle': 'Parallelität, Tempo und Nachbearbeitung.',
  'settings.maxConcurrent': 'Gleichzeitig max.',
  'settings.maxPerHost': 'Pro Host max.',
  'settings.speedLimit': 'Tempolimit (KiB/s, 0 = ∞)',
  'settings.speedHint': 'Gilt für yt-dlp und JDownloader.',
  'settings.extract': 'Archive nach dem Download entpacken',
  'settings.deleteArchive': 'Archiv nach dem Entpacken löschen',
  'settings.autoStart': 'Hinzugefügte Links sofort starten (Sammler überspringen)',
  'settings.save': 'Speichern',
  'settings.saved': 'Gespeichert.',
};

export const LANGUAGES = [
  { code: 'en', label: 'English' },
  { code: 'de', label: 'Deutsch' },
] as const;

export type Lang = (typeof LANGUAGES)[number]['code'];

const DICTS: Record<Lang, Record<TranslationKey, string>> = { en, de };

const STORAGE_KEY = 'kl-lang';

function detect(): Lang {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === 'en' || stored === 'de') return stored;
  return navigator.language?.toLowerCase().startsWith('de') ? 'de' : 'en';
}

/** Applied at boot so <html lang> matches before first paint. */
export function applyStoredLanguage(): void {
  document.documentElement.setAttribute('lang', detect());
}

interface I18nAPI {
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string;
  lang: Lang;
  setLang: (l: Lang) => void;
}

const Ctx = createContext<I18nAPI>({ t: (k) => en[k], lang: 'en', setLang: () => {} });

export const useT = () => useContext(Ctx);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(detect);

  const setLang = useCallback((l: Lang) => {
    localStorage.setItem(STORAGE_KEY, l);
    document.documentElement.setAttribute('lang', l);
    setLangState(l);
  }, []);

  const t = useCallback(
    (key: TranslationKey, vars?: Record<string, string | number>) => {
      let s: string = DICTS[lang][key] ?? en[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [lang],
  );

  return <Ctx.Provider value={{ t, lang, setLang }}>{children}</Ctx.Provider>;
}

/** plural picks the singular or plural key by count (both locales are regular here). */
export function plural(n: number, one: TranslationKey, many: TranslationKey): TranslationKey {
  return n === 1 ? one : many;
}
