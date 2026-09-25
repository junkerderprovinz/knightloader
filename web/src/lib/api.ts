// Type-only imports: these types belong to the modules that own their data, so
// they are named here rather than restated.
import type { BarLabelMode, NavLabelMode } from './navLabels';
import type { EventProgramRow } from './eventprograms';
import type { EventTargetRow } from './eventtargets';
import type { Shape } from './appearance';
import { socketURL, withBase } from './basePath';

export type TaskStatus =
  | 'collected'
  | 'queued'
  | 'running'
  | 'paused'
  | 'extracting'
  | 'done'
  | 'error';

// Availability is what a check said about the link itself, separate from
// whether a download has been attempted. 'uncheckable' means the host refused
// to say (a 429, a 503, a transport error); treating it as 'offline' would let
// one flaky minute get a live link deleted.
export type Availability = '' | 'online' | 'offline' | 'uncheckable';

// Reason is the typed cause of a failure; Task.error is the sentence beside it.
// It stays an open string because the taxonomy grows on the server, and a
// union would turn every new value into a compile error. Readers map the
// values they know and show nothing for the rest (see reasonKey in
// components/columns.tsx).
export type Reason = string;

// Origin is the intake path a link arrived by: the paste box, the watch folder,
// Click'n'Load, a container upload. Open for the same reason as Reason.
export type Origin = string;

export interface Task {
  id: string;
  url: string;
  name: string;
  package: string;
  resolver: string;
  /** Absent when no hoster is on the other end of the link. */
  mode?: 'free' | 'premium';
  /** The backend the user pinned this task to, as a service id. Absent leaves
   *  the choice to the ranking. */
  resolverPin?: string;
  /** What the backend is doing for a running task that is not moving bytes,
   *  such as "Waiting for reconnect". Not a failure. */
  note?: string;
  size: number;
  loaded: number;
  speed: number;
  status: TaskStatus;
  error?: string;
  createdAt: string;
  dir?: string;
  password?: string;
  online?: Availability;
  retries?: number;
  /**
   * The attempt ceiling as the server resolved it, shown as "retry {retries} of
   * {maxTries}". Zero means no ceiling is known and the denominator is dropped.
   * It is not recomputed from settings.maxRetries, because host rules and the
   * per-reason table can override that, and the row may belong to a peer
   * instance. A snapshot: a raised retry count shows once the task fails again.
   */
  maxTries?: number;
  nextTry?: string;
  priority: number;
  position: number;
  checksum?: 'ok' | 'failed';

  /** What a Packagizer rule attached; nothing in the app acts on it. */
  comment?: string;
  /** Connections this download opens. 0 hands the count to the global setting;
   *  a resolver's limit can only lower it. */
  chunks?: number;
  /**
   * Per-task override of the global extraction switch. `undefined` means the
   * global decides, so a control bound to this has to offer inherit, on and
   * off rather than a checkbox.
   */
  autoExtract?: boolean;
  /** The Packagizer rules that shaped this task, in the order they fired. */
  matchedRules?: string[];

  /** When the download finished. Go sends the zero time
   *  "0001-01-01T00:00:00Z" for an unfinished task; fmtDate shows it empty. */
  finishedAt?: string;
  /** The user's own switch for one link. Always sent, and true unless switched off. */
  enabled: boolean;
  /** Parked without failing: not started, not an error either. */
  skipped?: boolean;
  skipReason?: string;
  /** Parked by the user; "resume everything" leaves it alone. */
  hold?: boolean;
  /** Runs now, past the concurrency and per-host limits. */
  forced?: boolean;
  /** The password the hoster asks for. `password` is the archive password
   *  tried when unpacking. */
  downloadPassword?: string;
  /** A checksum supplied with the link rather than found beside the file. */
  expectedHash?: string;
  /** The outbound connection this download is routed over; empty = the machine's own. */
  connection?: string;
  /** The file host, which differs from the resolver behind a debrid service. */
  host?: string;
  /** The page a crawl found this link on. */
  source?: string;
  /** The task this one is a second copy of, when the mirror policy staged it. */
  mirrorOf?: string;
  /** Whether an interrupted transfer can resume. `undefined` means nobody has
   *  asked yet and must not be shown as "no". */
  resumable?: boolean;
  /** The name to write the file under when it is not the one the backend would choose. */
  filename?: string;
  /** Which form of the same resource was picked: a yt-dlp format, a quality. */
  variant?: string;
  /** A variant row its host's preset leaves out: kept, but not shown in the
   *  collector and never started, until the preset lists its kind again. */
  variantOff?: boolean;
  /** File extension shown before the download starts, set only where it is
   *  certain ahead of time (see core.Task.Ext). */
  ext?: string;
  /** Height caps the probed video offers for when no format is chosen; absent
   *  falls back to the full menu. */
  availableQualities?: string[];
  /** The formats the probed video comes in, "best" first, as
   *  "<container> <codec>" (ytdlp.VideoContainers); absent until a probe answers. */
  availableVideoFormats?: string[];
  /** Every distinct video track, as "<height>p[<fps>] <container> <codec>"
   *  (ytdlp.VideoTracks). The quality picker offers a chosen format's own. */
  availableVideoTracks?: string[];
  /** The formats the probed audio comes in, "best" first (ytdlp.AudioFormatsOf);
   *  absent until a probe answers. */
  availableAudioFormats?: string[];
  /** Every distinct audio track, as "<format> <kbps>k" (ytdlp.AudioTracks). The
   *  bitrate picker offers a chosen format's own. */
  availableAudioTracks?: string[];
  /** Bitrates a conversion may encode to, up to the source's best track. */
  availableAudioBitrates?: string[];
  /** The bitrate of a format the source has no track in, which is converted
   *  to (yt-dlp's --audio-quality, such as "192"), or a preset's bitrate no
   *  probe has matched to a track yet. */
  audioBitrate?: string;
  /** A package the user chose by hand; automatic re-packaging leaves it alone. */
  manualPackage?: boolean;
  reason?: Reason;
  /**
   * Why a queued task has not started. Absent means nothing is holding it
   * back. The server recomputes it on every dispatch pass. A collected link
   * can carry 'premium' too, for what starting it would run into, which the
   * server updates whenever an account or the settings change. Callers fall
   * back for values a newer server may add.
   */
  waiting?: 'slot' | 'host' | 'forced' | 'disabled' | 'hold' | 'captcha' | 'account' | 'halted' | 'disk' | 'volumeCap' | 'module' | 'premium';
  /**
   * When the bytes stopped, so the age of a stall is computed on every render.
   * Not persisted: it describes a connection this process holds open.
   */
  stalledSince?: string;
  /** How often the watcher has restarted this task for standing still. */
  stallRestarts?: number;
  /** Set when nothing will be retried by itself (a never rule, a captcha, a
   *  full disk), but not for exhausted attempts, which a higher retry count cures. */
  gaveUp?: boolean;
  origin?: Origin;
  /** When this task last changed. Zero-timestamp caveat as for finishedAt. */
  changedAt?: string;
  /** Volume number inside a multi-volume set, 0 for a file that is not in one. */
  archivePart?: number;
  /** How the last unpacking of this file's archive ended, on every part of the
   *  set. Kept by the server, so it outlives the job after a restart. */
  unpack?: 'done' | 'error' | 'password';

  /** The file selection of a multi-file torrent; absent for everything else. */
  torrentFiles?: TorrentFile[];
  /** A debrid service's progress on a torrent it is still fetching for this
   *  task, before any of it comes here. Absent at every other time. */
  remote?: RemoteFetch;

  // The swarm fields are absent for non-torrent tasks and never persisted,
  // because a peer count is only true for the second it was read.
  /** How many peers the swarm has shown us, seeding or not. */
  peers?: number;
  /** How many of those are connected and complete. */
  seeds?: number;
  /** Uploaded over downloaded, what a seed target is measured against. */
  ratio?: number;
  /** Bytes sent to the swarm. */
  uploaded?: number;
  /** A finished torrent still uploading. A flag beside status 'done' rather
   *  than a status, so the existing status mappings stay exhaustive. */
  seeding?: boolean;

  /** Set once at stage time and persisted, unlike the swarm fields. */
  infoHash?: string;
  trackers?: string[];
}

/** core.RemoteFetch: progress runs from 0 to 1. */
export interface RemoteFetch {
  progress: number;
  speed?: number;
  seeds?: number;
}

/** One file inside a multi-file torrent. path is inside the torrent and
 *  forward-slashed, never a path on this machine. */
export interface TorrentFile {
  path: string;
  size: number;
  selected: boolean;
}

/** One RSS or Atom subscription, mirroring feed.Subscription. */
export interface FeedSubscription {
  /** http or https only. Also the subscription's identity, so a changed
   *  address starts over. */
  url: string;
  /** 0 means the server's default of 15; other values are clamped to 1..10080. */
  intervalMinutes: number;
  /**
   * An RE2 pattern an entry's title has to match before it is staged. It
   * decides what is downloaded, not what is remembered, so widening it later
   * does not dump the feed's current window into the collector. A pattern that
   * does not compile is refused at save.
   */
  titleFilter?: string;
  /** Absent for the app's download folder. May be a pathvars template. */
  dir?: string;
  /** -3..3. Absent and 0 differ: 0 is a real priority. */
  priority?: number;
}

/**
 * One level of the retry chain, mirroring settings.RetryRule. Numbers are
 * seconds or counts. Zero on any field hands the question to the next level
 * (host rule, reason table, instance backoff, built-in 15s/10min), so a row
 * that sets only `delay` changes only the delay.
 */
export interface RetryRule {
  /** Wait before the first retry, in seconds. 0 takes the level below. */
  delay?: number;
  /** Where the doubling stops, in seconds. 0 takes the level below. */
  max?: number;
  /** How many attempts this gets at all. 0 takes the global maxRetries. */
  tries?: number;
  /** Settles the task without any retry. Merged with OR, so `false` never
   *  clears a `true` from the reason table. */
  never?: boolean;
}

/** The instance-wide backoff plus the per-failure table layered over it. */
export interface RetryPolicy {
  /** Seconds. 0 keeps the built-in 15 seconds to 10 minutes. */
  delay: number;
  max: number;
  /** Keyed by the server's failure reason. Null when empty, as Go writes it. */
  byReason: Record<string, RetryRule> | null;
}

/** What one host pattern may differ in, mirroring settings.HostRule. */
export interface HostRule {
  /** 0 takes the global maxPerHost. */
  maxPerHost?: number;
  /** Connections one download from this host opens. 0 takes the global
   *  chunks. An override, so it may exceed the global number. */
  chunks?: number;
  /** Absent rather than `{}` when nothing is set. */
  retry?: RetryRule;
  /** The service asked first for this host, as ResolverOrder names it.
   *  Absent is Automatic. */
  prefer?: string;
  /** Services never used for this host. A pinned task still goes to its pin. */
  exclude?: string[];
}

/**
 * One named drawer, mirroring settings.Category. Every field but id and name
 * overrides a level above, and an absent value means "use the level above".
 * That is why priority and extract are optional: 0 is the middle priority and
 * false is a drawer that keeps archives packed.
 */
export interface Category {
  /** Empty on a new row: the server derives it from the name once, on save,
   *  and never again. Renaming does not change it. */
  id: string;
  name?: string;
  /** Absolute, and may be a pathvars template. Empty = the global download folder. */
  dir?: string;
  /** -3..3; absent differs from 0. */
  priority?: number;
  extract?: boolean;
  /** Bytes per second, 0 = no opinion. Stored and resolved; nothing enforces it yet. */
  speedLimit?: number;
  /** 'rename' | 'skip' | 'overwrite', empty = the instance's policy. A string
   *  because the menu comes from /api/options.collisionPolicies. */
  collision?: string;
  /** The media hook called when a package in this drawer finishes. An unknown
   *  id is refused on save. */
  notify?: string;
  /** This drawer's premium only switch; absent follows the instance's. */
  premiumOnly?: boolean;
  /** This drawer's own torrent file selection, in place of the Torrents page's
   *  as a whole. Absent is no opinion. */
  torrentFiles?: TorrentFileRules;
}

/** settings.TorrentFileRules: the files a torrent fetches when nobody ticked
 *  them by hand. The server sends null for an empty list. */
export interface TorrentFileRules {
  /** Bytes; 0 = no minimum. */
  minFileSize: number;
  /** Regular expressions, one per line. */
  includeFiles: string[] | null;
  excludeFiles: string[] | null;
}

export interface Settings {
  maxConcurrent: number;
  maxPerHost: number;
  /** Connections one download opens when neither the task nor a rule named a
   *  number. 0 lets the server's own fallback decide. */
  chunks: number;
  speedLimit: number; // bytes/s, 0 = unlimited
  extract: boolean;
  /**
   * Confirms an added batch the moment it stages, skipping the collector. This
   * is the settings page's "start added links immediately" toggle; `autoStart`
   * decides what a confirmed batch does next.
   */
  autoConfirm: boolean;
  /** Seconds before an unconfirmed batch confirms itself; 0 disables the countdown. */
  autoConfirmDelay: number;
  /** The modules switched off on the modules page, by their ids there. */
  modulesOff: string[] | null;
  /** Whether a confirmed batch starts right away (the default) or waits on Hold. */
  autoStart: boolean;
  /** confirm.Policy ("include"|"exclude"|"exclude-and-remove"|"ask") for a
   *  link that duplicates one already in the list. */
  onDupes: string;
  /** Same shape as onDupes, for a link already known offline at confirm time. */
  onOffline: string;
  /** Places a newly confirmed batch at the front of the queue. */
  addAtTop: boolean;
  downloadDir: string;
  subfolderByPackage: boolean;
  /**
   * Where bytes are written while they arrive; the file moves to downloadDir
   * after the checksum and any extraction. '' writes straight to the
   * destination. Always absolute and never a pathvars template, because the
   * parts of a multi-volume archive have to share one folder.
   */
  workDir: string;
  /** The named drawers Task.category refers to. Read through `?? []`, since
   *  an older server does not send the key. */
  categories: Category[];
  /**
   * Addresses called once a package has finished and its files are in place.
   * Read through `?? []` for older servers. The card that edits these saves on
   * its own, so this can be one save behind GET /api/mediahooks.
   */
  mediaHooks: MediaHook[];
  archivePasswords: string[];

  /** Where extractions go; empty means beside the archive. May be a pathvars
   *  template, so the folder chooser has to keep the tail. */
  extractTo: string;
  /** Each package in its own folder below extractTo. Does nothing without one. */
  extractSubfolder: boolean;
  /**
   * Where the files of a finished extraction are moved afterwards; '' leaves
   * them where they unpacked. May be a pathvars template. A Packagizer rule
   * that named a folder wins over it.
   */
  extractMoveTo: string;
  /** What an extraction does when its destination folder is already there. */
  extractCollision: string;
  /**
   * 'keep' | 'trash' | 'delete' for an archive that unpacked cleanly. It
   * replaced the boolean `deleteArchive`, which must never be sent again or it
   * would undo the server's one-time migration on every save.
   */
  archiveDisposal: string;
  /** How long a trashed archive stays before the sweep takes it. 0 never sweeps. */
  trashRetentionDays: number;
  /** Sweep the .nfo/.sfv/.diz/.url that came with the same package. */
  deleteInfoFiles: boolean;
  maxRetries: number;

  /**
   * Per-host exceptions keyed by host pattern, matched on a dot boundary with
   * the longest match winning. Both `null` and `{}` occur on the wire and mean
   * no rows, so read it as `cfg.hostRules ?? {}`.
   */
  hostRules: Record<string, HostRule> | null;
  /** The level a host rule's retry values fall through to. */
  retry: RetryPolicy;

  /**
   * Seconds a running download may move no bytes before it is marked as
   * standing still; 0 never marks. The server raises 1..59 to 60, so a
   * control must not offer them. The ceiling is 86400.
   */
  stallTimeout: number;
  /**
   * Drops a marked download's connections and asks for the rest of the file
   * on new ones, keeping its slot, and its bytes where the server can send
   * part of a file. On by default. Only the built-in engine can; JD, yt-dlp
   * and torrent rows are only marked.
   */
  stallReconnect: boolean;
  /**
   * Restarts a marked download from the top, once new connections have not
   * helped.
   * A switch of its own because the restart throws away the bytes already
   * fetched. Torrents are exempt.
   */
  stallRestart: boolean;
  /** Restarts one download gets. 0 means the default of 3, not unlimited; the cap is 20. */
  stallMaxRestarts: number;

  /**
   * Bytes kept free beyond what a download still has to fetch. Defaults to
   * 0.5 GiB; 0 is off. The server clamps rather than refuses.
   */
  diskReserve: number;
  /** Free bytes under which no new download starts. 0 is off. Raised to
   *  diskCriticalSpace on save when that one is higher. */
  diskLowSpace: number;
  /**
   * Free bytes under which running downloads are stopped and requeued, checked
   * every 15 seconds. 0 is off. Dearer than diskLowSpace, because a
   * non-resumable transfer loses what it had fetched.
   */
  diskCriticalSpace: number;

  /** Bytes that may finish downloading in one period. 0 is no cap and also the
   *  off state, so there is no separate switch. */
  volumeCap: number;
  /** Day of the month the counter resets, 1..31. A shorter month uses its last day. */
  volumeCapResetDay: number;
  /** What happens once the cap is reached. Does nothing while volumeCap is 0. */
  volumeCapAction: VolumeCapAction;
  /** Bytes per second while capped, for the 'throttle' action. A ceiling beside
   *  the other limits, so a schedule asking for less still wins. */
  volumeCapThrottle: number;

  /**
   * When two different URLs count as the same file (dedupe.Policy). A string
   * because the menu comes from /api/options.mirrorPolicies. The same URL
   * twice is always refused.
   */
  mirrorPolicy: string;
  /** Keeps a folded-away copy as a parked sibling row instead of dropping it.
   *  On its own it starts nothing. */
  keepMirrors: boolean;
  /** Releases that parked sibling when the download it copies has finished
   *  failing. Does nothing without keepMirrors. */
  mirrorFailover: boolean;
  /** How much the "already on the disk" pass trusts a file it did not see
   *  arrive: "checksum" | "record" | "size". */
  reclaimTrust: string;

  /**
   * What a restart does with in-flight downloads: 'never' | 'running' | 'all'.
   * 'never' is the default because no backend handle survives the process, so
   * a resumed transfer starts over and its partial meets the collision policy.
   */
  resumeOnStart: string;
  /**
   * 'rename' | 'skip' | 'overwrite' when the destination name is taken. Only
   * the built-in engine can be told a name, so JDownloader, TorBox and yt-dlp
   * honour skip and nothing else.
   */
  collisionPolicy: string;
  /** Names "rename" tries before giving up. 0 means 1000. Omitted from the JSON
   *  when 0, so read it as `cfg.collisionMaxAttempts ?? 0`. */
  collisionMaxAttempts: number;
  /** Days a finished download stays in the list. 0 keeps it forever. */
  keepFinishedDays: number;
  /** How many entries the history keeps. 0 keeps every one. */
  historyMax: number;
  /** Days between automatic database maintenance runs. 0 runs it only from the
   *  diagnostics page. The server clamps anything above 365. */
  maintenanceIntervalDays: number;
  /**
   * Whether the scheduled run also compacts. Off by default, because compacting
   * needs room for a second copy of the database on the temporary volume, which
   * in a container is not the data volume.
   */
  maintenanceCompactOnSchedule: boolean;

  /**
   * The optional log file. Off by default so an update does not start writing
   * files, which on Unraid usually land on the array. The server clamps maxMb
   * to 1..1024 and keep to 0..20; keep = 0 keeps only the current file. There
   * is no path setting: the file sits beside the database and KL_LOG_DIR moves
   * it.
   */
  logFile: { enabled: boolean; maxMb: number; keep: number };

  crawl: boolean;
  /**
   * Pages deep a crawl goes: 1 is the pasted page alone. Defaults to 1 so an
   * update never starts deep crawls of someone else's forum.
   */
  crawlDepth: number;
  /** How many pages one crawl may fetch. It counts requests, not links found. */
  crawlMaxPages: number;
  /** Keeps a deep crawl on the pasted page's exact host. It never restricts the
   *  files that come back. */
  crawlSameHost: boolean;
  /**
   * Patterns matched against the URLs a crawl meets. Exclude keeps the crawl
   * away from pages and files; include only narrows what is staged, since
   * applied to pages it would stop the walk at the first hop.
   */
  crawlInclude: string[];
  crawlExclude: string[];
  watchDir: string;
  /** The RSS and Atom subscriptions. `null` rather than an empty array when there are none. */
  feeds: FeedSubscription[] | null;
  /**
   * Where this instance reports events. Optional as well as nullable, because
   * a settings file written before the feature carries no key; read it with
   * `?? []`. Header values arrive masked and are sent back untouched; the
   * server merges the real ones in while the row keeps its address.
   */
  eventTargets?: EventTargetRow[] | null;
  /**
   * What this instance starts on events, read with `?? []` for the same
   * reason. The program and its arguments arrive masked and are sent back
   * untouched; the server merges the real ones in by row id.
   */
  eventPrograms?: EventProgramRow[] | null;
  verifyChecksums: boolean;
  /** Finds links anywhere in a paste instead of reading one line as one link
   *  (JDownloader's AddLinksPreParserEnabled). */
  preParserEnabled: boolean;
  shape: Shape;
  accent: string;
  rainbow: boolean;
  rainbowReactive: boolean;
  rainbowRotate: boolean;
  rainbowSeed: number;
  rainbowPalette: string[] | null;

  /** Hides the sidebar's "Konten" entry; the settings tab still reaches the page. */
  hideAccountsFromSidebar: boolean;
  /** Hides the sidebar's "Instanzen" entry in the same way. */
  hideInstancesFromSidebar: boolean;
  /** How much of a navigation entry is drawn, in the sidebar and the settings rail. */
  navLabels: NavLabelMode;
  /** How much of an entry the phone layout's bottom bar draws, or 'follow' for navLabels'. */
  bottomBarLabels: BarLabelMode;
  autoUpdateCheck: boolean;
  /** Needs autoUpdateCheck, and only the desktop build acts on it. */
  autoUpdateInstall: boolean;
  /** Holds off sleep while a download runs. Only the desktop build acts on it. */
  keepAwake: boolean;

  /** One request to api.github.com when the Resolvers page loads. It never
   *  installs anything: replacing the extractor unattended would change what
   *  every download produces. */
  ytdlpVersionCheck: boolean;

  /**
   * The captcha solvers to try before a human is asked, as catalogue ids in
   * try order; an id not in the list is never tried. `null` on a fresh
   * install.
   */
  captchaSolverOrder: string[] | null;
  /** Holds the solvers back while a prompt is watched: they take over once
   *  nobody watches or captchaSolverWait seconds pass without an answer. */
  captchaSolverOnlyUnwatched: boolean;
  /** Seconds, 10 to 600. */
  captchaSolverWait: number;

  /** Holds a link whose only way down is a free download until an account
   *  can fetch it. A category may say otherwise. */
  premiumOnly: boolean;

  /** What happens after a cancellable countdown once the queue runs dry. The
   *  live countdown comes from fetchIdleAction. */
  idleAction: IdleActionConfig;

  ytdlp: YtdlpOptions;
  /** Per-host "Variante" defaults, keyed by the lower-cased host without www.
   *  A host with no entry gets all five variants, best quality, best audio. */
  ytdlpPresets: Record<string, YtdlpHosterPreset>;

  /** This instance's name, offered when pairing and in its QR code. Optional. */
  instanceName: string;
  /** Full URLs such as "https://kl.example.com", remembered the first time a
   *  request arrives on one or added by hand. */
  knownDomains: string[];
}

/**
 * One configured account, mirroring app.AccountState: one per stored or
 * container-supplied credential. The secret never travels; `configured` is all
 * the page learns about it.
 */
export interface Account {
  /** What every account/label/enabled call sends back (app.metaKey). */
  id: string;
  /** Catalogue id, as in CatalogueService. */
  service: string;
  /** "" for a service's default account, a caller-chosen id otherwise. */
  account: string;
  label: string;
  enabled: boolean;
  configured: boolean;
  /** Together with envVar, why a credential is read-only on the page. */
  fromEnv: boolean;
  envVar?: string;
  ok: boolean;
  detail: string;
  hosts: number;
  /**
   * When the host list that routing matches links against was last fetched,
   * RFC3339, absent before the first success. `hosts` counts the last manual
   * refresh instead; a failing service can leave this older without the list
   * going empty.
   */
  hostsFetchedAt?: string;
  /** The health ticker's cached reading. "unknown" until its first read, and
   *  never to be shown as "free". */
  tier: string;
  /** Check `unlimited` before computing a percentage: used and limit are both
   *  zero then, and a bar would read as "out of traffic". */
  traffic: TrafficState;
  /** RFC3339; empty until the health refresher has fetched it. */
  expiry?: string;
  trafficLeft?: string;
  /** Whether the service can list the account's downloads for the import. */
  canImport: boolean;
  /** Whether what the user adds on the service's website is imported. */
  import: boolean;
}

/** One account's traffic allowance, mirroring app.TrafficState. */
export interface TrafficState {
  used: number;
  limit: number;
  unlimited: boolean;
  /** RFC3339, or absent when the service does not say when this resets. */
  resetsAt?: string;
}

/** Which shape of secret a service needs (accounts.Kind). */
export type ServiceKind = 'apiKey' | 'usernamePassword';

/**
 * Which section of the accounts page a service belongs to (accounts.Group).
 * Accounts.tsx renders neither 'captchaSolver', which settings/Captcha.tsx
 * configures, nor 'remoteServer', whose logins are stored with the server's
 * hostname as the account id. Both are listed so the type matches the wire.
 */
export type ServiceGroup = 'debrid' | 'hoster' | 'captchaSolver' | 'remoteServer';

/**
 * One entry of GET /api/accounts/catalogue, mirroring accounts.Service. The
 * accounts page reads the list of services from here and nowhere else.
 */
export interface CatalogueService {
  id: string;
  label: string;
  kind: ServiceKind;
  group: ServiceGroup;
  /** The container env var that can supply this credential, if there is one. */
  env?: string;
  whereUrl: string;
  /** What the two fields of a username-and-password service hold when they are
   *  not a website login; absent means username and password. */
  userLabel?: CredentialField;
  passLabel?: CredentialField;
}

export type CredentialField = 'apiUser' | 'apiKey' | 'customerId' | 'email';

/** The body of a credential POST or verify call. */
export interface AccountCredential {
  apiKey?: string;
  username?: string;
  password?: string;
}

export interface VerifyResult {
  ok: boolean;
  hosts: number;
  detail: string;
}

export interface QueueState {
  halted: boolean;
  stopMark?: string;
  running: number;
}

export interface AuthState {
  enabled: boolean;
  authenticated: boolean;
  /** Absent for a caller without a session; the sign-in screen learns what it
   *  needs from the login answer. */
  twoFactor?: boolean;
  recoveryLeft?: number;
}

/**
 * The answer to POST /api/auth/login. `twoFactorRequired` alone means the
 * password was right and the code is still needed (answered with 200).
 * `codeRejected` beside it means the code was wrong, so the screen can tell a
 * typo from the first prompt.
 */
export interface LoginResult extends AuthState {
  twoFactorRequired?: boolean;
  codeRejected?: boolean;
}

export interface Instance {
  /**
   * The key proxied calls are built from (`/api/instances/${name}`). For a
   * relay peer it is the stable instance ID, so render displayName instead.
   */
  name: string;
  url: string;
  /** What a relay peer calls itself, when it differs from `name`. A label
   *  only; two peers may share one. */
  displayName?: string;
  /**
   * The instance ID a relay peer is addressed by, set only while the relay
   * reaches it. Stored peers never carry one, and a relay peer has an empty
   * `url` because it has no address of its own.
   */
  relayId?: string;
}

/**
 * GET /api/diagnostics: everything a bug report needs. The page's preview and
 * its download button both read this shape, so the saved file carries all
 * that the page shows. `settings` is the same redacted document GET
 * /api/settings sends, kept untyped because it is only carried along.
 */
export interface Diagnostics {
  generatedAt: string;
  version: string;
  /** "container" or "desktop": which binary produced this bundle. */
  deployment: string;
  goVersion: string;
  os: string;
  arch: string;
  goroutines: number;
  /** Which yt-dlp and ffmpeg run, where each came from and its version. */
  mediaTools: MediaToolsStatus;
  settings: Record<string, unknown>;
  /** The number of archive passwords; the values are never in the bundle. */
  archivePasswordCount: number;
  logLines: string[];
  logCapacity: number;
  /** `path` is always empty here, because the bundle gets attached to public
   *  reports. fetchLogFileState has the real path. */
  logFile: LogFileState;
  /**
   * Samples in the speed record, out of 120 (one a second) and 360 (one per
   * ten seconds). Zero on both means nothing is recording. Memory only, so
   * both start empty after a restart.
   */
  speedSamplesRecent: number;
  speedSamplesHour: number;
  speedRecordingSince?: string;
  /** The store file plus any journal beside it. No paths, for the same reason as logFile. */
  storeBytes: number;
  /** Space deleted rows left inside the file. A floor: compaction usually gives back more. */
  storeReclaimableBytes: number;
  settingsBytes: number;
  /** False until a settings page has been saved once; settings.Load never writes. */
  settingsPresent: boolean;
  /** The GET /api/fileowner answer, so a bug report has the uid. */
  ownership: FileOwnerIdentity;
  /** Every configured folder as a stat saw it, without paths: the desktop
   *  build's download folder sits in the user's home directory. */
  ownedFolders: FolderOwnership[];
  /**
   * The start checks as they read just after boot. Null when none ran, which
   * must not be shown as "everything passed". runStartupCheck does not replace
   * it, because a bug report needs the state at start.
   */
  startup: StartupReport | null;
}

/** What an operation on a selection answers: the ids it touched. */
export interface BulkResult {
  ids: string[];
  count: number;
  /** The token that undoes a removal. Only deleteTasks sets it, and only when
   *  the files were kept. */
  undo?: string;
  /** How long the undo token stays valid, in milliseconds. The server owns the window. */
  undoMs?: number;
}

/**
 * The "clean up…" classes, typed so a request cannot carry a misspelt one.
 * Menus are built from fetchOptions().cleanupClasses instead, because the
 * server decides which classes it implements.
 */
export const CLEANUP_CLASSES = [
  'finished',
  'offline',
  'disabled',
  'duplicates',
  'incompleteArchives',
] as const;

export type CleanupClass = (typeof CLEANUP_CLASSES)[number];

/** A link that never became a task, kept so the collector can say what happened to it. */
export interface SkippedLink {
  url: string;
  /** What the mirror set decided: "duplicate" or "mirror". */
  kind: string;
  /** The sentence to show, which names what the match rests on. */
  reason: string;
  /** The task it was folded into. */
  ofId?: string;
  /** The signal the match rests on (file name, byte count, …). */
  signal?: string;
  at: string;
}

/**
 * The choices the settings forms offer. The server serves them so no menu
 * offers a value this build cannot honour.
 */
export interface ApiOptions {
  mirrorPolicies: string[];
  collisionPolicies: string[];
  /** confirm.Policy values valid as an instance default. UseGlobal is left
   *  out, because a global default cannot defer to itself. */
  confirmPolicies: string[];
  /** reclaim.Trust tiers, strictest first: "checksum" | "record" | "size". */
  reclaimTrustModes: string[];
  /** The size limit of the category table (settings.MaxCategories). */
  maxCategories: number;
  /** The archive page's lists. Extraction honours different collision
   *  policies from a download, and the formats depend on the build. */
  archiveCollisions: string[];
  archiveDisposals: string[];
  archiveFormats: string[];
  /** What a restart may do with what was in flight. */
  resumeModes: string[];
  /** The folder "trash" really means, so the help text can name it. */
  archiveTrashFolder: string;
  proxyKinds: string[];
  // The rule editor builds its form from GET /api/rules/grammar, which the
  // engine generates, so no rule vocabulary lives here.
  scheduleActions: string[];
  cleanupClasses: CleanupClass[];
  ytdlpQualities: string[];
  /** The video formats a host preset offers, "best" first. Absent on an older
   *  server. */
  ytdlpVideoFormats?: string[];
  ytdlpAudioFormats: string[];
  /** The --audio-quality menu; "" means no opinion. */
  ytdlpAudioBitrates: string[];
  /** Methods a media library call may use. Absent on an older server, and the
   *  method choice stays hidden then. */
  mediaHookMethods?: string[];
}

/** A container that was a plain link list: parsed here and staged like any paste. */
export interface ContainerStaged {
  kind: string;
  links: number;
  created: Task[];
  handedTo?: undefined;
}

/**
 * An encrypted container. Its key is issued only to registered clients, so it
 * goes to the headless JDownloader backend. Nothing is staged yet when this
 * comes back; the links appear once JD has fetched it.
 */
export interface ContainerHandedOver {
  kind: string;
  handedTo: 'jd';
  /** Seconds the handover address stays fetchable. */
  expiresIn: number;
}

/**
 * An .nzb, sent to the TorBox or Premiumize account named in `service`.
 * Nothing is staged yet; its files appear once the service has fetched them.
 */
export interface ContainerSentToUsenet {
  kind: 'nzb';
  handedTo: 'usenet';
  service: string;
}

export type ContainerResult = ContainerStaged | ContainerHandedOver | ContainerSentToUsenet;

/**
 * ApiError is a refusal the server explained. `code` and `params` make it
 * translatable; the server's own sentence is English.
 */
export class ApiError extends Error {
  code?: string;
  params?: Record<string, string | number>;
  /** The HTTP status, when there was one. It tells a peer that refused a call
   *  from one that could not be reached. */
  status?: number;
  /**
   * Where in the settings a refusal is, when it is about one place: a key, or
   * a dotted path below one such as "reconnect.checkUrl" or "connections.2".
   */
  field?: string;

  // The name stays "Error", so String(e) reads "Error: …" like any other
  // failure and the call sites that strip that prefix show the server's words.
  constructor(message: string, code?: string, params?: Record<string, string | number>, status?: number) {
    super(message);
    this.code = code;
    this.params = params;
    this.status = status;
  }
}

/**
 * refusal reads a refused response. A JSON envelope keeps its code, so the
 * refusal can be translated; any other body is the server's sentence.
 */
export async function refusal(r: Response): Promise<ApiError> {
  const body = (await r.text()).trim();
  try {
    const p = JSON.parse(body) as {
      error?: string;
      code?: string;
      params?: Record<string, string | number>;
      field?: string;
    };
    if (p && typeof p.error === 'string') {
      const e = new ApiError(p.error, p.code, p.params, r.status);
      e.field = p.field;
      return e;
    }
  } catch {
    // Plain text.
  }
  return new ApiError(body || String(r.status), undefined, undefined, r.status);
}

/**
 * json decodes a response and throws the server's refusal instead of feeding
 * an error body to the JSON parser.
 */
export async function json<T>(r: Response): Promise<T> {
  if (!r.ok) throw await refusal(r);
  return (await r.json()) as T;
}

// ok throws with the server's own words, for routes whose refusal tells the
// user what to change.
async function ok(r: Response): Promise<Response> {
  if (!r.ok) throw await refusal(r);
  return r;
}

const post = (path: string, body: unknown) =>
  fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });

// apiBase returns the API prefix for an instance ('' = this instance).
export const apiBase = (instance: string): string =>
  instance ? `/api/instances/${encodeURIComponent(instance)}` : '/api';

export async function fetchTasks(base = '/api'): Promise<Task[]> {
  return (await json<Task[]>(await fetch(`${base}/tasks`))) ?? [];
}

/**
 * taskFileURL is where a task's file streams from; the server decides between
 * inline and a download prompt. It is opened directly as a link so the
 * browser's own download and viewer handling applies.
 */
export const taskFileURL = (id: string, base = '/api'): string =>
  withBase(`${base}/tasks/${encodeURIComponent(id)}/file`);

/**
 * isLocalBase reports whether a base points at this instance rather than a
 * peer. Anything that streams bytes checks it first: the federation proxy
 * reads at most 32 MB into memory, forwards no Range header and relabels the
 * answer as application/json.
 */
export const isLocalBase = (base: string): boolean => base === '/api';

/**
 * What GET /api/tasks/{id}/file would answer, asked with HEAD. The status is
 * kept as a number because each refusal needs its own sentence: 404 is nothing
 * on disk yet, 400 is a file this app does not serve (fetched by the JD
 * sidecar), 403 is a stored path outside its folder. `bytes` is what is on
 * disk this second, and `contentType` comes from the server's extension
 * allowlist, never from sniffing.
 */
export interface TaskFileHead {
  ok: boolean;
  status: number;
  /** Bytes on disk right now; 0 when the server sent no length. */
  bytes: number;
  /** Lower-cased, parameters stripped: "video/mp4", not "text/plain; charset=utf-8". */
  contentType: string;
  /** Whether byte ranges were offered, which is what a player seeks with. */
  ranges: boolean;
}

export async function taskFileHead(id: string, base = '/api'): Promise<TaskFileHead> {
  const r = await fetch(taskFileURL(id, base), { method: 'HEAD' });
  const len = Number(r.headers.get('Content-Length') ?? '');
  return {
    ok: r.ok,
    status: r.status,
    bytes: Number.isFinite(len) ? len : 0,
    contentType: (r.headers.get('Content-Type') ?? '').split(';')[0].trim().toLowerCase(),
    ranges: (r.headers.get('Accept-Ranges') ?? '').toLowerCase() === 'bytes',
  };
}

/**
 * hosterIconURL is a host's site icon, cached by the server. It is used as an
 * <img src> because a 404 is the normal answer for a host without a favicon,
 * and the component's onError turns that into a monogram.
 */
export const hosterIconURL = (host: string, base = '/api'): string =>
  withBase(`${base}/hosters/icon?host=${encodeURIComponent(host)}`);

export async function addLinks(links: string, pkg: string, base = '/api'): Promise<Task[]> {
  const r = await fetch(`${base}/links`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ links, package: pkg }),
  });
  return (await json<Task[]>(r)) ?? [];
}

/**
 * The add-links form's per-batch options; an empty object behaves like plain
 * addLinks. A matching Packagizer rule wins over priority, autoExtract and
 * comment unless `overrule` is set. `downloadPassword` is the hoster's
 * password, `password` the archive's.
 */
export interface AddLinksOptions {
  package?: string;
  origin?: string;
  dir?: string;
  password?: string;
  downloadPassword?: string;
  comment?: string;
  priority?: number;
  autoExtract?: boolean;
  overrule?: boolean;
}

/**
 * addLinksWithOptions is addLinks plus the per-batch fields. An unusable
 * destination refuses the whole batch with an ApiError, which callers have to
 * show to whoever typed the path.
 */
export async function addLinksWithOptions(
  links: string,
  opts: AddLinksOptions,
  base = '/api',
): Promise<Task[]> {
  const r = await fetch(`${base}/links`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ links, ...opts }),
  });
  return (await json<Task[]>(r)) ?? [];
}

/**
 * What a start did, mirroring app.StartResult, so the UI can explain why
 * nothing started: a halted queue, a filter holding the tasks, or ids that
 * matched nothing.
 */
export interface StartResult {
  started: number;
  skipped: number;
  /** Passed over because their own switch is off. */
  disabled?: number;
  /** The queue was taken off a halt the user had set by hand. */
  released: boolean;
  /** A schedule window is holding the queue; the tasks are queued and waiting. */
  blocked: boolean;
}

// startTasks moves collected tasks into the download queue (empty = start all).
export const startTasks = async (ids: string[], base = '/api'): Promise<StartResult> => {
  const r = await fetch(`${base}/tasks/start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids }),
  });
  // An older instance answers 204 with no body.
  return (
    (await json<StartResult>(r)) ?? { started: ids.length, skipped: 0, released: false, blocked: false }
  );
};

// setPackage moves tasks into a package (empty name = ungrouped).
export const setPackage = (ids: string[], pkg: string, base = '/api') =>
  fetch(`${base}/tasks/package`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids, package: pkg }),
  });

/**
 * renamePackage gives the package these tasks are in a new name. The folder
 * follows it only while nothing in the package has started (app.RenamePackage);
 * a name that is empty or holds a separator is refused with the reason.
 */
export const renamePackage = async (ids: string[], name: string, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/package/rename`, { ids, name })));

/**
 * restartTasks re-runs finished or failed tasks (empty ids = all failed).
 * `reasons` narrows that to the named causes, '' being the unclassified group;
 * with ids as well, the server intersects the two.
 */
export const restartTasks = (ids: string[], base = '/api', reasons?: Reason[]) =>
  fetch(`${base}/tasks/restart`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    // Omitted when empty, so an older instance sees the body it expects.
    body: JSON.stringify(reasons && reasons.length > 0 ? { ids, reasons } : { ids }),
  });

export const pause = (id: string, base = '/api') =>
  fetch(`${base}/tasks/${id}/pause`, { method: 'POST' });
export const resume = (id: string, base = '/api') =>
  fetch(`${base}/tasks/${id}/resume`, { method: 'POST' });
// remove drops a task from the list; withFiles also deletes what was downloaded.
export const remove = (id: string, base = '/api', withFiles = false) =>
  fetch(`${base}/tasks/${id}${withFiles ? '?files=1' : ''}`, { method: 'DELETE' });

// recheckTasks re-resolves collected links and refreshes their online state
// (empty = every collected link).
export const recheckTasks = (ids: string[], base = '/api') =>
  post(`${base}/tasks/recheck`, { ids });

/**
 * ExtractJob is one unpacking. `status` is an open string for the same reason
 * as Reason. `taskId` is the volume the job started on, which for a
 * multi-volume set is the first part.
 */
export interface ExtractJob {
  id: string;
  taskId: string;
  name: string;
  dir: string;
  package?: string;
  status: string;
  /** The file open right now, which at depth is one found inside the output. */
  archive?: string;
  depth?: number;
  files: number;
  bytes: number;
  /** How far through the archive open now the job is, against what its
   *  headers say it holds. `size` is absent when the format does not say. */
  unpacked?: number;
  size?: number;
  volumes: number;
  /** The task of every file in the set, in reading order. A peer on an older
   *  build sends none, and then only `taskId` is known. */
  parts?: string[];
  nested?: number;
  error?: string;
  /** The failure was a missing password, which is the one with an obvious remedy. */
  password?: boolean;
  queuedAt: string;
  startedAt?: string;
  endedAt?: string;
}

export async function fetchExtractJobs(base = '/api'): Promise<ExtractJob[]> {
  return (await json<ExtractJob[]>(await fetch(`${base}/extract`))) ?? [];
}

/**
 * startExtraction unpacks finished downloads now, whatever the unpacking
 * switch says. When some rows are refused the server answers 207; the refusal
 * is thrown while the started jobs show up on the stream as usual.
 */
export async function startExtraction(ids: string[], base = '/api'): Promise<ExtractJob[]> {
  const r = await fetch(`${base}/extract/start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids }),
  });
  if (r.status === 207) {
    const p = (await r.json()) as { refused?: string };
    throw new ApiError(p.refused || String(r.status));
  }
  return (await json<ExtractJob[]>(r)) ?? [];
}

// abortExtraction stops one unpacking and removes its half-written output. It
// throws when the job has already finished, so the button does not look dead.
export async function abortExtraction(id: string, base = '/api'): Promise<void> {
  const r = await post(`${base}/extract/${id}/abort`, {});
  if (!r.ok) throw new ApiError((await r.text()).trim() || String(r.status));
}

// setPriority lifts or drops tasks in the wait queue (-2..2, higher runs first).
export const setPriority = (ids: string[], priority: number, base = '/api') =>
  post(`${base}/tasks/priority`, { ids, priority });

// moveTasks reorders the queue by hand.
export const moveTasks = (ids: string[], where: 'top' | 'bottom', base = '/api') =>
  post(`${base}/tasks/move`, { ids, where });

/**
 * The per-task overrides the properties panel sends. The server leaves any
 * field it was not sent alone, so a panel editing many rows sends only what
 * changed; an empty string clears. For `autoExtract`, `null` means inherit
 * the global switch, and unlike `undefined` it survives JSON.stringify.
 */
export interface TaskOptionsPatch {
  name?: string;
  dir?: string;
  password?: string;
  /** The hoster's password; `password` is the archive password. */
  downloadPassword?: string;
  comment?: string;
  priority?: number;
  /** 0 removes the override and hands the count back to the rule and the
   *  global setting, so send it only when the user typed it. */
  chunks?: number;
  autoExtract?: boolean | null;
  /** A video row's pick, a height cap or a track, or an audio row's, a format
   *  or a track (see VariantPicker.tsx). '' means no opinion, so send it only
   *  when a picker changed. */
  variantQuality?: string;
  /** The audio row's bitrate beside a format it is converted to. '' leaves
   *  it to the track or to ffmpeg's default. */
  audioBitrate?: string;
  /** The backend to pin these tasks to, an id from fetchPinChoices. '' takes
   *  the pin off, so send it only when the dropdown changed. */
  resolver?: string;
}

// setTaskOptions applies per-task overrides; omitted fields stay as they are.
export const setTaskOptions = (ids: string[], opts: TaskOptionsPatch, base = '/api') =>
  post(`${base}/tasks/options`, { ids, ...opts });

/** One backend a selection can be pinned to (app.PinChoice). */
export interface PinChoice {
  id: string;
  /** A debrid service's own name; the others are named by resolverLabel. */
  label?: string;
}

/** fetchPinChoices lists the backends every one of `ids` can be pinned to, best first. */
export async function fetchPinChoices(ids: string[], base = '/api'): Promise<PinChoice[]> {
  return (await json<PinChoice[] | null>(await post(`${base}/tasks/backends`, { ids }))) ?? [];
}

// Operations on a whole selection go out as one request, since a route per id
// would turn a hundred rows into a hundred store writes and broadcasts. They
// answer with the ids touched. Everything under /api/tasks/ is forwarded to a
// peer, so these take a base.

/** setEnabled switches a selection of links on or off. */
export const setEnabled = async (ids: string[], enabled: boolean, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/enabled`, { ids, enabled })));

/** setHold parks a selection, or lets it go again. */
export const setHold = async (ids: string[], hold: boolean, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/hold`, { ids, hold })));

/** setForced marks a selection to run ahead of the concurrency limits. */
export const setForced = async (ids: string[], forced: boolean, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/force`, { ids, forced })));

/** pauseTasks pauses the running and waiting links of a selection and leaves the rest alone. */
export const pauseTasks = async (ids: string[], base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/pause`, { ids })));

/** resumeTasks puts the paused links of a selection back in the wait queue. */
export const resumeTasks = async (ids: string[], base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/resume`, { ids })));

/** deleteTasks removes a selection from the list; `withFiles` also erases what was downloaded. */
export const deleteTasks = async (ids: string[], withFiles = false, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/delete`, { ids, files: withFiles })));

/**
 * undoDelete puts back the rows one removal took. An expired or spent token
 * restores nothing without an error; the returned count says what happened.
 */
export const undoDelete = async (token: string, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/undo-delete`, { token })));

// The cleanup routes are not forwarded to peers, so they take no base.

/** cleanupPreview reports which tasks a class would take, so the confirmation can list them. */
export const cleanupPreview = async (cls: CleanupClass) =>
  json<BulkResult>(await ok(await fetch(`/api/cleanup/${encodeURIComponent(cls)}`)));

/** runCleanup removes everything in a class and reports what it removed. */
export const runCleanup = async (cls: CleanupClass, withFiles = false) =>
  json<BulkResult>(
    await ok(
      await fetch(`/api/cleanup/${encodeURIComponent(cls)}${withFiles ? '?files=1' : ''}`, {
        method: 'POST',
      }),
    ),
  );

/**
 * fetchSkipped lists the links that were folded into one already in the list,
 * oldest first, so a link that vanished on paste has an explanation.
 */
export async function fetchSkipped(): Promise<SkippedLink[]> {
  return (await json<SkippedLink[]>(await fetch('/api/collector/skipped'))) ?? [];
}

/** clearSkipped empties that trace. */
export const clearSkipped = () => fetch('/api/collector/skipped', { method: 'DELETE' });

/**
 * uploadContainer sends a .txt/.dlc/.ccf/.rsdf/.nzb file. A plain link list
 * comes back staged in `created`; an encrypted container is handed to the JD
 * backend and an .nzb to a Usenet-capable account, and their links arrive
 * later over the websocket, which the caller has to say rather than report
 * "0 links added". A failure throws the server's sentence, with a code when
 * nothing here can open the file.
 */
export async function uploadContainer(file: File, pkg = ''): Promise<ContainerResult> {
  const form = new FormData();
  form.append('file', file);
  if (pkg) form.append('package', pkg);
  // No Content-Type header: the browser has to set the multipart boundary.
  return json<ContainerResult>(await fetch('/api/containers', { method: 'POST', body: form }));
}

/** The preview POST /api/torrents/parse returns: the file tree, with the files
 *  the file selection on the Torrents page chooses selected, and the `uri`
 *  stageTorrent needs. Nothing is staged yet. */
export interface TorrentTree {
  uri: string;
  infoHash: string;
  name: string;
  private: boolean;
  totalSize: number;
  pieceLength: number;
  pieces: number;
  files: TorrentFile[];
  trackers: string[];
  droppedTrackers: number;
}

/** parseTorrentUpload sends a .torrent file and gets back its file tree. A
 *  failure throws the server's sentence. */
export async function parseTorrentUpload(file: File): Promise<TorrentTree> {
  const form = new FormData();
  form.append('file', file);
  return json<TorrentTree>(await fetch('/api/torrents/parse', { method: 'POST', body: form }));
}

/**
 * stageTorrent turns a parsed `uri` into a task. `selectedPaths` names the
 * files to keep; omitted, the torrent file selection chooses when the torrent
 * starts, which with none set keeps every file. The server parses `uri` again
 * and only narrows against the list, so an invented path has no effect.
 * Returns null when the mirror set folded it into an existing task, and a
 * task with `skipped` set when it was held back.
 */
export async function stageTorrent(
  uri: string,
  pkg: string,
  selectedPaths?: string[],
): Promise<Task | null> {
  return json<Task | null>(
    await fetch('/api/torrents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ uri, package: pkg, selectedPaths }),
    }),
  );
}

/** fetchOptions returns the choices the settings and cleanup menus offer. */
export async function fetchOptions(): Promise<ApiOptions> {
  return json<ApiOptions>(await fetch('/api/options'));
}

// GET /api/diskspace takes no base: a peer answers 403, so a row built from it
// always describes this machine's disks, and the shell strip only draws it
// while the scope is local.

/** One target folder as the server measured it. */
export interface DiskVolume {
  /** The folder the app would write into. It may not exist yet, and for a path
   *  template it is the fixed head of that template. */
  dir: string;
  /**
   * The folder the figures describe: `dir`, or the nearest existing folder
   * above it. When a mount did not come up this is the volume root of another
   * disk, so show it whenever it differs from `dir`.
   */
  measured: string;
  /** Whether `dir` itself is a folder today. */
  exists: boolean;
  /** Whether this platform could be asked at all. When false, free, used and
   *  total are 0 and mean nothing, so draw no figure and no bar. */
  known: boolean;
  /** Bytes this process may still write here. 0 with `known` is a full volume
   *  and prints as "0 B", not through fmtBytes, which shows a dash for 0. */
  free: number;
  /** Bytes somebody's files occupy. */
  used: number;
  /** The volume's size. `free + used` can be less, because of a root reserve
   *  or a quota, so none of the three is derived from the other two. */
  total: number;
  /**
   * What the downloads still owed would add here, a floor since unknown sizes
   * add nothing. Never subtract it from `free`: a running transfer has usually
   * reserved its room already.
   */
  queued: number;
  /** How many downloads are aimed here, including those of unknown size. */
  tasks: number;
  /** Why this folder is listed: 'downloads' | 'category' | 'work' | 'task'. */
  role: string;
}

export interface DiskReport {
  /** One row per folder, not per disk. Two folders on one volume repeat its
   *  figures, so never sum `free` across rows. */
  volumes: DiskVolume[];
  /** Destinations outside the configured folders were cut to keep the route
   *  cheap. Configured folders are always present. */
  truncated: boolean;
  /** When the reading was taken. It is cached for a few seconds, so the UI
   *  should not present it as a live gauge. */
  sampledAt: string;
}

export async function fetchDiskSpace(): Promise<DiskReport> {
  return json<DiskReport>(await fetch('/api/diskspace'));
}

/**
 * What one part of this instance is doing. Open, because a newer server may
 * send a state this build has no word for; lookups render an unknown one as
 * its raw id. "unused" and "unknown" are not faults and never make `status`
 * worse than ok.
 */
export type HealthState = 'ok' | 'degraded' | 'failed' | 'unused' | 'unknown' | (string & {});

export interface HealthSubsystem {
  /** A stable id the UI translates: store, queue, disk, jd, ytdlp, accounts,
   *  feeds, relay, captcha. */
  id: string;
  state: HealthState;
  /** The failing service's own words, shown beside the translated state. */
  detail?: string;
  /** The id of a remedy sentence (health.remedy.<id>). */
  remedy?: string;
  /** When this row last changed state, RFC3339. Absent until a state has been
   *  seen twice, since the first reading cannot know when it started. */
  since?: string;
}

export interface HealthTaskCounts {
  running: number;
  waiting: number;
  paused: number;
  extracting: number;
  collected: number;
  /** Counted across the states above, so the buckets still add up to the list. */
  disabled: number;
  /** Rows with an error on them right now. A gauge, not a tally of failures:
   *  it falls when rows are cleared or trimmed. */
  failed: number;
  /** Counts by core.Waiting and core.Reason id, zero keys left out. Never
   *  null. The ids reuse the task.waiting.* and task.reason.* labels. */
  waitingBy: Record<string, number>;
  failedBy: Record<string, number>;
}

export interface HealthReport {
  /** The worst row, where "unused" and "unknown" count as ok. */
  status: HealthState;
  version: string;
  deployment: string;
  startedAt: string;
  /** Computed against the server's clock, so a skewed browser clock cannot distort it. */
  uptimeSeconds: number;
  /** Every part, in a fixed order. Never null. */
  subsystems: HealthSubsystem[];
  tasks: HealthTaskCounts;
  /** GET /api/diskspace's rows verbatim, so both pages agree. Check `known`
   *  before drawing a figure. */
  volumes: DiskVolume[];
  halted: boolean;
  quiet: boolean;
  /** When the probed rows were taken. Shared for half a minute, so not live. */
  sampledAt: string;
}

/**
 * The detailed health readout. /api/health stays a two-field answer because
 * the container health check, the Click'n'Load bridge and the phone app's
 * discovery read it. No base: this describes this machine and a peer
 * answers 403.
 */
export async function fetchHealthReport(): Promise<HealthReport> {
  return json<HealthReport>(await fetch('/api/health/detail'));
}

export type VolumeCapAction = 'report' | 'pause' | 'throttle';

/**
 * One bucket of the volume curve. `key` is bucketed in the server's local
 * calendar ('YYYY-MM-DD' or 'YYYY-MM'). Do not pass it to `new Date()`, which
 * reads it as UTC midnight and shifts bars a day early west of Greenwich.
 */
export interface VolumeBucket {
  key: string;
  /** Announced size of everything that finished in this bucket. */
  bytes: number;
  /** How many downloads that was. */
  count: number;
  /** Downloads without a size. They add nothing to `bytes`, so this says
   *  whether the total understates itself. */
  unsized: number;
  /** Bytes per file host and per backend. Null for an empty map, as Go writes it. */
  byHost: Record<string, number> | null;
  byResolver: Record<string, number> | null;
}

/** GET /api/stats/volume */
export interface VolumeStats {
  /** Newest last, one entry per day, gaps filled with a zero bucket. */
  days: VolumeBucket[];
  /** Newest last, one entry per month, gaps filled with a zero bucket. */
  months: VolumeBucket[];
  /** The zone the server bucketed by, so the chart can say whose calendar it is. */
  timeZone: string;
  /** The oldest finish time the history still holds; before it nothing is recorded. */
  oldest?: string;
  /** The history is at settings.historyMax, so the oldest months have been cut. */
  trimmed: boolean;
}

/** GET /api/stats/volume/usage, and the payload of the 'volume' websocket kind. */
export interface VolumeUsage {
  /** Bytes finished since periodStart. Moves when a download finishes, never
   *  while one runs. */
  used: number;
  /** 0 means no cap is set; `reached` is then always false. */
  cap: number;
  action: VolumeCapAction;
  throttle: number;
  periodStart: string;
  periodEnd: string;
  reached: boolean;
}

export async function fetchVolumeStats(): Promise<VolumeStats> {
  return json<VolumeStats>(await fetch('/api/stats/volume'));
}

export async function fetchVolumeUsage(): Promise<VolumeUsage> {
  return json<VolumeUsage>(await fetch('/api/stats/volume/usage'));
}

export async function fetchQueue(base = '/api'): Promise<QueueState> {
  return json<QueueState>(await fetch(`${base}/queue`));
}

/**
 * Whether the timetable is set aside, the part of GET /api/schedule's answer
 * that the suspend routes change. `suspendedUntil` is absent while it waits to
 * be lifted by hand.
 */
export interface ScheduleSuspension {
  suspended: boolean;
  suspendedUntil?: string;
}

/**
 * fetchScheduleSuspension reads the timetable's answer for its suspension and
 * for how many schedules there are to suspend.
 */
export async function fetchScheduleSuspension(): Promise<ScheduleSuspension & { schedules: number }> {
  const s = await json<ScheduleSuspension & { entries: unknown[] | null }>(await fetch('/api/schedule'));
  return { suspended: s.suspended, suspendedUntil: s.suspendedUntil, schedules: s.entries?.length ?? 0 };
}

/**
 * suspendSchedule sets every schedule aside and leaves the rows as they are:
 * for `minutes` on the server's clock, until an instant, or with neither until
 * resumeSchedule. It answers with the whole ScheduleState, which carries the
 * suspension.
 */
export async function suspendSchedule(span: { minutes: number } | { until: string } | null): Promise<ScheduleSuspension> {
  const r = await fetch('/api/schedule/suspend', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(span ?? {}),
  });
  return json<ScheduleSuspension>(r);
}

/** resumeSchedule lets the schedules apply again at once. */
export async function resumeSchedule(): Promise<ScheduleSuspension> {
  return json<ScheduleSuspension>(await fetch('/api/schedule/suspend', { method: 'DELETE' }));
}

/** setQueue toggles the master switch and/or arms the stop mark. */
export async function setQueue(
  patch: { halted?: boolean; stopMark?: string },
  base = '/api',
): Promise<QueueState> {
  return json<QueueState>(await post(`${base}/queue`, patch));
}

// The queue belongs to the task list it orders, so these routes are forwarded
// to a peer along with the list and take a base.

/**
 * Who a queue action is about. `package` reaches rows a filter hides, so a
 * package moves as a whole. `all` has to be asked for: the server refuses a
 * request that names nothing.
 */
export interface QueueSelection {
  ids?: string[];
  package?: string;
  all?: boolean;
}

/** The four steps the manual order understands. Anything finer is a drag. */
export type QueueMove = 'top' | 'up' | 'down' | 'bottom';

/** One of the seven priorities. The id is translated as `priority.<id>`. */
export interface PriorityChoice {
  id: string;
  value: number;
}

// The priority ladder is fetched once per session and shared, so the context
// menu and the properties panel offer the same steps.
let prioritiesOnce: Promise<PriorityChoice[]> | null = null;

export function priorityChoices(): Promise<PriorityChoice[]> {
  if (!prioritiesOnce) prioritiesOnce = fetchPriorities();
  return prioritiesOnce.then(
    (p) => p,
    (e) => {
      prioritiesOnce = null; // a failed load must not poison the next attempt
      throw e;
    },
  );
}

/** What the figures under the list say. `eta` is seconds, null when nothing is moving. */
export interface QueueCounters {
  files: number;
  disabled: number;
  running: number;
  remaining: number;
  speed: number;
  eta: number | null;
  /** Challenges waiting for an answer, as many as /api/captcha lists. */
  captchas: number;
}

/**
 * What stopping every transfer now would throw away. `unknown` counts
 * transfers nobody has asked about resuming, which need a different sentence
 * from a known loss.
 */
export interface StopCost {
  running: number;
  losing: string[];
  bytes: number;
  unknown: number;
  unknownBytes: number;
}

/** What the hard stop answers with: what it stopped, and the switch it left behind. */
export interface StopResult {
  ids: string[];
  count: number;
  queue: QueueState;
}

/** queueMove changes where a selection or a whole package sits in the wait order. */
export const queueMove = async (sel: QueueSelection, where: QueueMove, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/queue/move`, { ...sel, where })));

/**
 * reorderTasks writes the drag-and-drop order for one band, the tasks sharing
 * `priority` and `forced`; the server refuses a list that mixes bands. `ids`
 * may be a subset, read as "these tasks, in this order, in the slots they
 * already hold".
 */
export const reorderTasks = async (ids: string[], base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/reorder`, { ids })));

/** queuePriority puts a selection at one of the seven. */
export const queuePriority = async (sel: QueueSelection, priority: number, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/queue/priority`, { ...sel, priority })));

/** queueForce starts a selection now: front of the queue, switched on, released. */
export const queueForce = async (sel: QueueSelection, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/queue/force`, sel)));

/** queueEnabled is the bulk switch: a selection, a package, or `all` disabled links. */
export const queueEnabled = async (sel: QueueSelection, enabled: boolean, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/queue/enabled`, { ...sel, enabled })));

/** fetchPriorities is the menu's source of truth, so it cannot offer a value the server clamps away. */
export const fetchPriorities = async (base = '/api') =>
  json<PriorityChoice[]>(await fetch(`${base}/queue/priorities`));

export const fetchCounters = async (base = '/api') =>
  json<QueueCounters>(await fetch(`${base}/queue/counters`));

/** fetchStopCost weighs the hard stop and stops nothing. */
export const fetchStopCost = async (base = '/api') =>
  json<StopCost>(await fetch(`${base}/queue/stop`));

/** stopAll stops every transfer in flight and halts the queue behind them. */
export const stopAll = async (base = '/api') =>
  json<StopResult>(await ok(await post(`${base}/queue/stop`, {})));

export async function fetchSettings(): Promise<Settings> {
  return json<Settings>(await fetch('/api/settings'));
}

/**
 * patchSettings updates only the named top-level fields, so a concurrent edit
 * to another field survives. A named object field such as reconnect is still
 * replaced whole.
 */
export async function patchSettings(patch: Partial<Settings>): Promise<Settings> {
  const r = await fetch('/api/settings', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  });
  return json<Settings>(r);
}

export async function fetchAccounts(): Promise<Account[]> {
  return (await json<Account[]>(await fetch('/api/accounts'))) ?? [];
}

// fetchAccountCatalogue lists every service a credential can be stored for,
// configured or not.
export async function fetchAccountCatalogue(): Promise<CatalogueService[]> {
  return (await json<CatalogueService[]>(await fetch('/api/accounts/catalogue'))) ?? [];
}

// verifyAccountCredential checks a credential against its service without
// storing it, so a typo shows before it is saved.
export async function verifyAccountCredential(
  service: string,
  account: string,
  cred: AccountCredential,
): Promise<VerifyResult> {
  return json<VerifyResult>(await post('/api/accounts/verify', { service, account, ...cred }));
}

// saveAccountCredential stores one account's credential.
export async function saveAccountCredential(service: string, account: string, cred: AccountCredential): Promise<void> {
  await ok(await post('/api/accounts', { service, account, ...cred }));
}

// removeAccountCredential clears one account's credential: the server reads an
// empty credential as a delete.
export async function removeAccountCredential(service: string, account: string): Promise<void> {
  await ok(await post('/api/accounts', { service, account }));
}

export async function setAccountLabel(service: string, account: string, label: string): Promise<void> {
  await ok(await post('/api/accounts/label', { service, account, label }));
}

// setAccountEnabled switches an account off as if its credential were missing.
export async function setAccountEnabled(service: string, account: string, enabled: boolean): Promise<void> {
  await ok(await post('/api/accounts/enabled', { service, account, enabled }));
}

// setAccountImport switches whether what the user adds on the account's own
// website is picked up.
export async function setAccountImport(service: string, account: string, on: boolean): Promise<void> {
  await ok(await post('/api/accounts/import', { service, account, import: on }));
}

// testAccount re-checks a stored account, the per-row "Refresh".
export async function testAccount(service: string, account: string): Promise<Account> {
  return json<Account>(await post('/api/accounts/test', { service, account }));
}

// The end-of-queue action runs after a cancellable countdown once the queue has
// nothing enabled left to do. Its configuration is Settings.idleAction; the
// routes below report the live state and cancel a countdown.

/**
 * The program the "command" end-of-queue action runs. A program and its
 * arguments, not a shell line, so pipes and redirects belong in a script.
 * Stored values come back masked as '********' because the diagnostics bundle
 * is built from the same document. Send the mask back to keep what is stored,
 * a new string to replace it, an empty one to clear it.
 */
export interface IdleCommandSpec {
  program: string;
  args?: string[];
  timeoutSeconds: number;
}

/** settings.Settings.IdleAction on the wire. */
export interface IdleActionConfig {
  /** 'none' | 'pause' | 'quit' | 'command' | 'suspend', open for values a
   *  later build adds. */
  action: string;
  delaySeconds: number;
  command: IdleCommandSpec;
}

/**
 * What POST /api/idle-action/check answers. It resolves the stored command
 * without running it and answers 200 even when `problem` is set, so the page
 * can read the result.
 */
export interface IdleCommandCheck {
  problem?: 'empty' | 'notFound' | 'notExecutable' | 'permission' | 'timeout' | 'exit' | 'notSupported';
  /** What the program name resolves to, by the same lookup the run uses. */
  resolvedPath?: string;
  /** The exact argument vector, resolved program first. */
  argv?: string[];
  /** 'container' or 'desktop', to pick between "not in this image" and "not
   *  on this machine". */
  deployment: string;
}

/**
 * What the last end-of-queue action did, and what POST /api/idle-action/run
 * answers. It does not survive a restart, so a 'quit' only ever leaves a
 * failed run behind.
 */
export interface IdleRun {
  action: string;
  /** RFC3339. */
  at: string;
  ok: boolean;
  /** Empty exactly when `ok` is true. A code, never a sentence - translate it. */
  problem?: string;
  exitCode?: number;
  /** The program's output, capped, with the stored command line removed. */
  output?: string;
  /** The program as configured, unmasked: this never goes into the diagnostics bundle. */
  program?: string;
}

/** GET /api/idle-action, also the answer to POST .../cancel so a cancel
 *  button can repaint from it. */
export interface IdleActionState {
  config: IdleActionConfig;
  /** Whether the queue has nothing enabled left to do, read fresh on every request. */
  idle: boolean;
  armed: boolean;
  /** Which action is armed. Absent when `armed` is false. */
  action?: string;
  /** When the action fires, RFC3339. An instant rather than a duration, so a
   *  reloaded or woken page draws the server's deadline. */
  fireAt?: string;
  /** What the last action did, absent until one has run since the process
   *  started. */
  lastRun?: IdleRun;
}

export async function fetchIdleAction(): Promise<IdleActionState> {
  return json<IdleActionState>(await fetch('/api/idle-action'));
}

/** cancelIdleAction calls off a countdown in progress, without disarming the
 *  feature for the next time the queue goes idle. */
export async function cancelIdleAction(): Promise<IdleActionState> {
  return json<IdleActionState>(await ok(await post('/api/idle-action/cancel', {})));
}

/** fetchIdleActions is the menu's source of truth, so it cannot offer an
 *  action this build does not implement. */
export async function fetchIdleActions(): Promise<string[]> {
  return (await json<string[]>(await fetch('/api/idle-action/actions'))) ?? [];
}

/** checkIdleCommand resolves the stored command and reports what it found,
 *  without running it. */
export async function checkIdleCommand(): Promise<IdleCommandCheck> {
  return json<IdleCommandCheck>(await ok(await post('/api/idle-action/check', {})));
}

/** runIdleCommand runs the stored command once, as the countdown would. The
 *  server answers 409 unless the configured action is 'command', so a test
 *  never quits the process or suspends the machine. */
export async function runIdleCommand(): Promise<IdleRun> {
  return json<IdleRun>(await ok(await post('/api/idle-action/run', {})));
}

/**
 * abortActivity calls off the running background work of one kind and answers
 * how many runs that was. Zero is normal: the run may have finished before the
 * button was pressed.
 */
export async function abortActivity(kind: string): Promise<number> {
  const r = await ok(await post(`/api/activity/${encodeURIComponent(kind)}/abort`, {}));
  return (await json<{ cancelled: number }>(r))?.cancelled ?? 0;
}

/** One resolver's identity and priority (resolver.Info). */
export interface ResolverInfo {
  id: string;
  prio: number;
}

/**
 * fetchResolverPriority is the order configured services are asked in,
 * highest first, after the hand-arranged order and JD's per-host boost. With
 * `host` it narrows to the chain that host walks.
 */
export async function fetchResolverPriority(host?: string): Promise<ResolverInfo[]> {
  const q = host ? `?host=${encodeURIComponent(host)}` : '';
  return (await json<ResolverInfo[]>(await fetch(`/api/resolvers/priority${q}`))) ?? [];
}

/**
 * saveResolverPriority stores a hand-arranged order; an empty list restores
 * the automatic one. It answers with the server's re-read, since blanks and
 * repeats are dropped on the way in.
 */
export async function saveResolverPriority(order: string[]): Promise<ResolverInfo[]> {
  return (
    (await json<ResolverInfo[]>(
      await fetch('/api/resolvers/priority', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ order }),
      }),
    )) ?? []
  );
}

/** The headless JD sidecar's status (app.JDStatus). */
export interface JDStatus {
  configured: boolean;
  reachable: boolean;
  /** JDownloader's revision number, a plain integer; absent while unreachable. */
  version?: number;
  detail?: string;
}

export async function fetchJDStatus(): Promise<JDStatus> {
  return json<JDStatus>(await fetch('/api/resolvers/jd'));
}

/**
 * The instance-wide yt-dlp defaults, mirroring ytdlp.Options without Variant,
 * which is decided per task. Saved through Settings.ytdlp. Values are strings
 * because the menus come from ApiOptions. Which variant rows a host starts
 * with is a YtdlpHosterPreset. Every zero value keeps yt-dlp's plain
 * behaviour.
 */
export interface YtdlpOptions {
  /** 'best' | '2160p' | '1440p' | '1080p' | '720p' | '480p' | '360p' |
   *  'custom'. Read only on a video row. */
  quality: string;
  /** yt-dlp's -f selector, used when quality is 'custom'. */
  customFormat: string;
  /** The audio format ("mp3", "aac", "opus"), or "best". A source's own track
   *  in it is copied, and only a source without one is converted. Read only
   *  on an audio row. */
  audioFormat: string;
  /** yt-dlp's --sub-langs ("en,de"); empty defaults to "en". Read only on a
   *  subtitle row. */
  subtitleLangs: string;
  /** Also fetch auto-generated captions when no manual track exists. Read
   *  only on a subtitle row. */
  subtitleAuto: boolean;
  /** A playlist URL fetches every entry instead of only the linked one. */
  playlist: boolean;
  /** yt-dlp's -o template; empty uses "%(title)s.%(ext)s". The server
   *  sanitizes it against path traversal on save. */
  outputTemplate: string;
  /** yt-dlp's --audio-quality in kbit/s ("192"), or "". Only used when
   *  audioFormat transcodes. */
  audioBitrate: string;
  /** One spoken language, applied as a [language^=xx] filter inside the audio
   *  row's -f selector. "" passes no filter. */
  audioLang: string;
  /** Fail a subtitle row that wrote no file instead of settling it green. */
  subtitleStrict: boolean;
  /** Allow a stored cookies.txt to be handed to yt-dlp. The jar itself is never
   *  part of this document. */
  cookies: boolean;
  /** Tag and sort an audio row as music. */
  music: boolean;
  embed: YtdlpEmbed;
  measure: YtdlpMeasure;
  live: YtdlpLive;
}

/** ytdlp.Embed: what gets written into the finished file rather than beside it. */
export interface YtdlpEmbed {
  metadata: boolean;
  thumbnail: boolean;
  chapters: boolean;
  /** Video rows only. */
  subs: boolean;
  splitChapters: boolean;
  /** Not a yt-dlp flag: KnightLoader writes the NFO itself. */
  nfo: boolean;
}

/** Mirrors ytdlp.Measure: the ffprobe pass over a finished media file. */
export interface YtdlpMeasure {
  enabled: boolean;
  /** 1..100. Anything outside that, 0 included, is stored as 90. */
  shortPercent: number;
  failOnShort: boolean;
}

/** Mirrors ytdlp.Live: what a stream that has no end is allowed to do. */
export interface YtdlpLive {
  enabled: boolean;
  fromStart: boolean;
  /** Minutes of recording, 0 for no limit. */
  maxMinutes: number;
  /** MiB, 0 for no limit. */
  maxMB: number;
}

/** The "Variante" row kinds a yt-dlp link stages, in creation order. A fixed
 *  set, so it is not fetched from /api/options. */
export const YTDLP_VARIANT_KINDS = ['video', 'audio', 'thumbnail', 'subtitle', 'description'] as const;
export type YtdlpVariantKind = (typeof YTDLP_VARIANT_KINDS)[number];

/**
 * Which "Variante" rows a host's links start with enabled, and the format and
 * quality the video and audio rows start on (ytdlp.HosterPreset). Read and
 * written one host at a time through /api/ytdlp/preset, outside the settings
 * draft.
 */
export interface YtdlpHosterPreset {
  variants: YtdlpVariantKind[];
  /** "best", or a container with its codec ("mp4 avc1"). Absent on a preset
   *  saved by an older server, which reads as "best". */
  videoFormat?: string;
  /** A height cap, the most a video row starts at. */
  quality: string;
  audioFormat: string;
  /** kbit/s the audio row starts nearest to, '' for its best track. */
  audioBitrate?: string;
}

export async function fetchHosterPreset(host: string, base = '/api'): Promise<YtdlpHosterPreset> {
  return json<YtdlpHosterPreset>(await fetch(`${base}/ytdlp/preset?host=${encodeURIComponent(host)}`));
}

export async function saveHosterPreset(host: string, preset: YtdlpHosterPreset, base = '/api'): Promise<void> {
  const r = await post(`${base}/ytdlp/preset`, { host, ...preset });
  if (!r.ok) throw new ApiError((await r.text()).trim() || String(r.status));
}

/**
 * The formats a host's preset offers (ytdlp.HostMenus): what the site is known
 * to serve and what probes of its links found, "best" first. With `known`
 * false nothing is known of the host and the lists are the full ones.
 */
export interface YtdlpHostMenus {
  videoFormats: string[];
  audioFormats: string[];
  known: boolean;
}

export async function fetchHosterFormats(host: string, base = '/api'): Promise<YtdlpHostMenus> {
  return json<YtdlpHostMenus>(await fetch(`${base}/ytdlp/formats?host=${encodeURIComponent(host)}`));
}

// yt-dlp cookie jars are stored per site in the encrypted credential store and
// only ever travel into the server: a cookies.txt is a live session, so the
// list route answers names only.

/** The sites with a stored jar, normalised (lower-cased, "www." stripped), so
 *  render the answer rather than the request. */
export type YtdlpCookieHosts = string[];

export async function fetchYtdlpCookieHosts(base = '/api'): Promise<YtdlpCookieHosts> {
  return (await json<YtdlpCookieHosts>(await fetch(`${base}/ytdlp/cookies`))) ?? [];
}

/**
 * saveYtdlpCookieJar stores or replaces one site's cookies.txt and answers
 * the stored list. `text` is always sent: empty clears, and leaving it out is
 * refused, since the page never gets a jar back and a re-saved form would
 * otherwise delete a working session. `host` takes the export address or the
 * bare site name; anything else throws the server's ApiError.
 */
export async function saveYtdlpCookieJar(
  host: string,
  text: string,
  base = '/api',
): Promise<YtdlpCookieHosts> {
  return (await json<YtdlpCookieHosts>(await post(`${base}/ytdlp/cookies`, { host, text }))) ?? [];
}

/**
 * removeYtdlpCookieJar deletes one site's jar and answers the remaining list.
 * An unknown host throws with status 404, so a typo does not look like a
 * removed session.
 */
export async function removeYtdlpCookieJar(host: string, base = '/api'): Promise<YtdlpCookieHosts> {
  return (await json<YtdlpCookieHosts>(await post(`${base}/ytdlp/cookies/remove`, { host }))) ?? [];
}

// Header profiles hold a user's own request headers for one origin: a session
// cookie, a Referer, Basic auth. Values are sealed in the credential store and
// never come back, so an editing form re-sends REDACTED_HEADER for every line it
// keeps; an empty value clears the header.

/** One stored header profile (hostheaders.Listing). Names only, never a value. */
export interface HeaderProfile {
  /** The name a Packagizer rule uses to route a link through this profile.
   *  Letters, digits and - _ or . only, at most 64 characters. */
  id: string;
  /** scheme://host:port with the port spelled out. Another port, scheme or
   *  subdomain is a different origin. */
  origin: string;
  /** The header names this profile holds, in net/http's capitalisation. */
  headers: string[];
}

/** One header line on the way in. */
export interface HeaderProfileLine {
  name: string;
  /** A new value replaces what is stored, REDACTED_HEADER keeps it, and an
   *  empty string clears that header. */
  value: string;
}

/** The placeholder a stored value is re-sent as, the same string
 *  accounts.Redacted uses. */
export const REDACTED_HEADER = '********';

/** fetchHeaderProfiles is every stored profile: origin and header names. */
export async function fetchHeaderProfiles(): Promise<HeaderProfile[]> {
  return (await json<HeaderProfile[]>(await fetch('/api/hostheaders'))) ?? [];
}

/** saveHeaderProfile stores or replaces the profile for one origin and answers
 *  the whole listing. An empty id edits the profile that origin already has;
 *  a new one has to be named. */
export async function saveHeaderProfile(
  origin: string,
  headers: HeaderProfileLine[],
  id = '',
): Promise<HeaderProfile[]> {
  return json<HeaderProfile[]>(await post('/api/hostheaders', { id, origin, headers }));
}

/** deleteHeaderProfile removes one profile. An unknown name answers 404. */
export async function deleteHeaderProfile(id: string): Promise<void> {
  await ok(await fetch(`/api/hostheaders/${encodeURIComponent(id)}`, { method: 'DELETE' }));
}

// A media hook is the address a drawer calls once a package has finished and
// its files are in place. The one header value it may carry is sealed in the
// credential store, so an editing form sends REDACTED_HEADER back for a value
// it keeps; empty clears it.

/**
 * One stored address: mediahook.Hook plus the read-only fields the listing
 * adds. There is no value field; `hasValue` is all this side learns.
 */
export interface MediaHook {
  /** The key a drawer points at (Category.notify). Letters, digits and - _ or
   *  . It never changes, or drawers would point at nothing. */
  id: string;
  name?: string;
  /** Absolute http or https address, host and port included. */
  url: string;
  /** 'GET' or 'POST', from /api/options.mediaHookMethods. */
  method: string;
  /** The one header sent with the call. Empty sends no extra header. */
  headerName?: string;
  /** Seconds to wait after a package finishes, so a batch becomes one call.
   *  0 calls as soon as the files are in place. */
  waitSeconds: number;
  /** Whether a header value is stored. Never the value. */
  hasValue: boolean;
  /** host[:port] the call resolves to, for the line that says where it goes. */
  host: string;
  /** The target is loopback or on a private range. Decided without a DNS
   *  lookup, so a host name reads as false. */
  private: boolean;
  /** Category ids pointing at this address. Never null. */
  usedBy: string[];
  /** The last call, test calls included. Memory only, so null after a restart. */
  last: MediaHookResult | null;
}

/** What one call did. `code` keys the sentence that says what to try next. */
export interface MediaHookResult {
  at: string;
  /** The package that armed the call, or the first of several by name. Absent
   *  for a test call. */
  package?: string;
  /** How many packages were folded into this one call. Absent or 1 means one. */
  packages?: number;
  test?: boolean;
  status?: number;
  durationMs: number;
  ok: boolean;
  /** 'dns'|'refused'|'timeout'|'tls'|'auth'|'notFound'|'method'|'redirect'|
   *  'server'|'proxy'|'unknown'. An unknown code falls back to
   *  settings.mediahook.problem.unknown with `error`. */
  code?: string;
  params?: Record<string, string | number>;
  error?: string;
}

/** One save. `headerValue`: a new value replaces, REDACTED_HEADER keeps,
 *  empty clears. */
export interface MediaHookSave {
  id: string;
  name?: string;
  url: string;
  method: string;
  headerName?: string;
  headerValue: string;
  waitSeconds: number;
}

/** fetchMediaHooks is every stored address in stored order, which is the
 *  order the Categories picker offers. */
export async function fetchMediaHooks(): Promise<MediaHook[]> {
  return (await json<MediaHook[]>(await fetch('/api/mediahooks'))) ?? [];
}

/** saveMediaHook stores or replaces one address and answers the whole listing. */
export async function saveMediaHook(h: MediaHookSave): Promise<MediaHook[]> {
  return json<MediaHook[]>(await post('/api/mediahooks', h));
}

/** deleteMediaHook removes one address and its sealed header value. Refused
 *  with 409 while a category still points at it. */
export async function deleteMediaHook(id: string): Promise<void> {
  await ok(await fetch(`/api/mediahooks/${encodeURIComponent(id)}`, { method: 'DELETE' }));
}

/** testMediaHook calls one stored address once and reports what came back. A
 *  failed call still answers 200, so read `ok` on the result. */
export async function testMediaHook(id: string): Promise<MediaHookResult> {
  return json<MediaHookResult>(await post(`/api/mediahooks/${encodeURIComponent(id)}/test`, {}));
}

/**
 * FeedStatus is one subscription as GET /api/feeds reports it. The health
 * table lives in memory, so while `lastPolledAt` is absent, `seeded` and
 * `remembered` are unknown and must not be drawn.
 */
export interface FeedStatus {
  url: string;
  /** Whether this address is being polled. False with `error` is a refused
   *  row; false without one is the moment at startup before the runner starts. */
  polling: boolean;
  /** RFC3339. Absent when no poll has run since the server started. */
  lastPolledAt?: string;
  /** Why the last poll produced nothing, or why the row is not polled at all. */
  error?: string;
  seeded: boolean;
  remembered: number;
}

/** One entry of a test fetch, as a subscription would see it. */
export interface FeedTestEntry {
  title: string;
  /** What would be staged: the enclosure when the entry carries one. */
  link: string;
  /** Whether the title filter takes it. True for everything when there is none. */
  matches: boolean;
}

/** FeedTest is what POST /api/feeds/test found. With `error` set, `entries`
 *  is an empty list rather than null. */
export interface FeedTest {
  title: string;
  entries: FeedTestEntry[];
  /** Entries in the whole document; `entries` above is only the first few. */
  total: number;
  /** How many of `total` the filter takes. Equals `total` with no filter. */
  matched: number;
  error?: string;
}

/** fetchFeeds is the status table under the subscription rows. */
export async function fetchFeeds(): Promise<FeedStatus[]> {
  return (await json<FeedStatus[]>(await ok(await fetch('/api/feeds')))) ?? [];
}

/**
 * testFeed fetches one feed once and reports what is in it, staging and
 * remembering nothing, so a title filter can be tried before saving. An
 * invalid row throws the server's 400 sentence; an unreadable feed comes back
 * with `error` set.
 */
export async function testFeed(url: string, titleFilter = ''): Promise<FeedTest> {
  const r = await ok(await post('/api/feeds/test', { url, titleFilter }));
  return (await json<FeedTest>(r)) ?? { title: '', entries: [], total: 0, matched: 0 };
}

// Native hoster logins are written into the headless JD sidecar's account
// config through its Remote API, and JD's plugin does the login. One row per
// host from a list that runs into the hundreds, so this is its own API rather
// than part of /api/accounts.

/** One host the "add a login" picker offers (hosterauth.Host). */
export interface HosterHost {
  id: string;
  label: string;
  /** A service that unlocks other hosts rather than hosting files. Absent when
   *  false. The list is kept by hand on the server, because JDownloader's API
   *  cannot say. */
  multihoster?: boolean;
}

/**
 * The sync status of a stored login against JD. 'queued' means JD has not
 * checked it yet and must not read as a wrong password.
 */
export type HosterLoginStatus = 'queued' | 'active' | 'rejected' | 'off';

/** One stored native hoster login and its status - hosterauth.LoginState. Never the password. */
export interface HosterLogin {
  host: string;
  username: string;
  status: HosterLoginStatus;
  detail?: string;
  /** Names `detail`, so it can be translated. */
  code?: string;
  /** The user's own switch. `status` is what JD thinks of the login; this is
   *  whether JD was given it. */
  enabled: boolean;
  /** What JD says about the account, all optional because JD may say nothing,
   *  which has to stay distinct from zero. */
  tier?: string;
  expiry?: string;
  trafficLeft?: number;
  trafficMax?: number;
  /** A multihoster's login, listed on the debrid card rather than the hoster card. */
  multihoster?: boolean;
}

/** fetchHosterHosts is the "add a login" picker's host list. */
export async function fetchHosterHosts(): Promise<HosterHost[]> {
  return (await json<HosterHost[]>(await fetch('/api/hosterauth/hosts'))) ?? [];
}

/** fetchHosterLogins is every stored native hoster login and its sync status. */
export async function fetchHosterLogins(): Promise<HosterLogin[]> {
  return (await json<HosterLogin[]>(await fetch('/api/hosterauth/logins'))) ?? [];
}

/** saveHosterLogin stores (or updates) one host's native login. */
export async function saveHosterLogin(host: string, username: string, password: string): Promise<void> {
  await ok(await post('/api/hosterauth/logins', { host, username, password }));
}

/** setHosterLoginEnabled takes a login out of JD's list or puts it back,
 *  keeping the sealed credential so no password has to be retyped. */
export async function setHosterLoginEnabled(host: string, enabled: boolean): Promise<void> {
  await ok(await post('/api/hosterauth/logins/enabled', { host, enabled }));
}

/** removeHosterLogin clears one host's stored native login. */
export async function removeHosterLogin(host: string): Promise<void> {
  await ok(await post('/api/hosterauth/logins/remove', { host }));
}

// A captcha is a hoster or login gate asking a human something before a
// download can continue. CaptchaChallenge mirrors captcha.Challenge and
// CaptchaResolution mirrors app.CaptchaResolution.

export type CaptchaKind = 'image' | 'click' | 'widget' | 'unsupported';

/** The payload for 'image' and 'click'; the kind alone decides between a
 *  text box and a click surface. */
export interface CaptchaImagePayload {
  /** Always a complete "data:image/...;base64,..." string, ready for <img src>. */
  dataUrl: string;
}

/** The payload for 'widget': the sitekey data a reCAPTCHA, hCaptcha or
 *  Turnstile widget needs to render itself. See captchaWidgetUrl. */
export interface CaptchaWidgetPayload {
  /** Which script the widget page loads; the rest of the payload looks the
   *  same for every vendor. The page renders reCAPTCHA and hCaptcha only. */
  vendor: 'recaptcha' | 'hcaptcha' | 'turnstile';
  siteKey: string;
  siteUrl: string;
  contextUrl: string;
  type?: string;
  enterprise?: boolean;
  v3Action?: string;
  secureToken?: string;
}

/** The payload for 'unsupported'. */
export interface CaptchaUnsupportedPayload {
  /** JD's own challenge class name. */
  vendor: string;
}

/** One captcha blocking a download until a human answers or dismisses it, or
 *  it expires. */
export interface CaptchaChallenge {
  /** Opaque: pass back to answerCaptcha/skipCaptcha unchanged, never parsed. */
  id: string;
  source: string;
  host: string;
  /** The task this challenge blocks, when the server could work it out. */
  taskId?: string;
  kind: CaptchaKind;
  /** Instructions a human reads, in whatever language the hoster wrote them. */
  prompt?: string;
  payload?: CaptchaImagePayload | CaptchaWidgetPayload | CaptchaUnsupportedPayload;
  /** When this stops being answerable. The zero time "0001-01-01T00:00:00Z"
   *  means the source could not say, not that it has expired. */
  expiresAt: string;
  /** What the paid solvers have done with it, absent until one is set to
   *  work on it. */
  solver?: CaptchaSolverReport;
}

/** captcha.SolverReport. */
export interface CaptchaSolverReport {
  state: 'waiting' | 'solving' | 'stopped';
  /** The service at work, by its display name. */
  solver?: string;
  /** When a waiting solver takes over. */
  until?: string;
  refusals?: CaptchaSolverRefusal[];
}

/** One solver that did not deliver: 'unsupported', 'noAnswer', 'failed', or
 *  the provider's own error code. */
export interface CaptchaSolverRefusal {
  solver: string;
  code: string;
  detail?: string;
  /** The provider may hold the task and bill it, so no other solver was
   *  asked after it. */
  taken?: boolean;
}

/** How far a skipped challenge's effect reaches (captcha.AbortScope). */
export type CaptchaAbortScope = 'skip-once' | 'blacklist-hoster' | 'blacklist-everywhere';

/** One challenge's end, as broadcast over the hub. */
export interface CaptchaResolution {
  id: string;
  taskId?: string;
  host: string;
  reason: 'solved' | 'expired' | 'aborted' | 'timedOut' | 'switchedOff' | 'resolved';
}

/**
 * fetchCaptchas is every challenge this instance knows about, read from a
 * cache. The "captcha" and "captchaResolved" websocket events keep it current
 * afterwards.
 *
 * watch=0 because a plain read counts as somebody watching the captchas, for
 * a client that polls. This page says so over its socket, and a read after a
 * reconnect in a background tab must not count.
 */
export async function fetchCaptchas(): Promise<CaptchaChallenge[]> {
  return (await json<CaptchaChallenge[]>(await fetch('/api/captcha?watch=0'))) ?? [];
}

/** refreshCaptchas polls the source right now instead of waiting for the next
 *  automatic check, and returns what it found. */
export async function refreshCaptchas(): Promise<CaptchaChallenge[]> {
  return (await json<CaptchaChallenge[]>(await post('/api/captcha/refresh', {}))) ?? [];
}

/**
 * answerCaptcha submits text as id's solution. stillValid comes from JD and
 * says whether the answer arrived in time; trust it over any local countdown.
 */
export async function answerCaptcha(id: string, text: string): Promise<{ stillValid: boolean }> {
  return json<{ stillValid: boolean }>(await post(`/api/captcha/${encodeURIComponent(id)}/answer`, { text }));
}

/**
 * skipCaptcha gives up on one challenge. JD keeps its own blacklist for the
 * two blacklist scopes, so nothing here has to remember it.
 */
export async function skipCaptcha(id: string, scope: CaptchaAbortScope): Promise<void> {
  await ok(await post(`/api/captcha/${encodeURIComponent(id)}/skip`, { scope }));
}

/**
 * captchaWidgetUrl builds the widget page address. The rendering data goes in
 * the query string because the caller already holds it, which spares the
 * server a second lookup at JD. lang is the interface language, which the
 * vendor's widget then speaks too.
 */
export function captchaWidgetUrl(ch: CaptchaChallenge, lang: string): string {
  const p = (ch.payload ?? {}) as CaptchaWidgetPayload;
  const q = new URLSearchParams();
  if (p.vendor) q.set('vendor', p.vendor);
  if (p.siteKey) q.set('siteKey', p.siteKey);
  if (p.type) q.set('type', p.type);
  if (p.enterprise) q.set('enterprise', '1');
  if (p.v3Action) q.set('v3Action', p.v3Action);
  if (p.secureToken) q.set('secureToken', p.secureToken);
  if (lang) q.set('lang', lang);
  if (ch.host) q.set('host', ch.host);
  if (ch.prompt) q.set('prompt', ch.prompt);
  return withBase(`/api/captcha/${encodeURIComponent(ch.id)}/widget?${q.toString()}`);
}

export async function fetchAuth(): Promise<AuthState> {
  return json<AuthState>(await fetch('/api/auth'));
}

/**
 * login exchanges the password, and a code once a second factor is armed, for
 * a session cookie. A wrong password throws; a request for a code or a
 * rejected code are returned, since the sign-in is still in progress.
 */
export async function login(password: string, code = ''): Promise<LoginResult> {
  const r = await post('/api/auth/login', { password, code });
  if (r.ok) return json<LoginResult>(r);
  const text = (await r.text()).trim();
  try {
    const parsed = JSON.parse(text) as LoginResult;
    if (parsed && parsed.twoFactorRequired) return parsed;
  } catch {
    // A plain-text body, which is the wrong-password case.
  }
  throw new Error(text || String(r.status));
}

/** What POST /api/auth/2fa/begin hands back: shown once, armed by nothing. */
export interface TOTPEnrolment {
  /** The base32 secret, for somebody typing it in by hand. */
  secret: string;
  /** The otpauth:// URI behind the QR code. */
  uri: string;
  /** The same URI as a module grid for the QR code. Absent only if the encoder refused. */
  qr?: QRMatrix;
}

/** setupTOTP starts an enrolment. Nothing is armed until confirmTOTP succeeds. */
export async function setupTOTP(): Promise<TOTPEnrolment> {
  return json<TOTPEnrolment>(await post('/api/auth/2fa/begin', {}));
}

/** confirmTOTP arms the factor and answers with the recovery codes. The server
 *  keeps only their hashes, so this is the one time they are shown. */
export async function confirmTOTP(code: string): Promise<string[]> {
  const r = await json<{ recoveryCodes: string[] }>(await post('/api/auth/2fa/confirm', { code }));
  return r.recoveryCodes ?? [];
}

/** disableTOTP turns the factor off. It costs the same proof as using it. */
export async function disableTOTP(code: string): Promise<void> {
  await json<unknown>(await post('/api/auth/2fa/disable', { code }));
}

/** One registered credential, as the settings card lists it. */
export interface PasskeyView {
  id: string;
  name: string;
  /** The address this key is bound to. A key registered through a proxy does
   *  not work over the LAN IP, so the row names it. */
  rpId: string;
  /** Whether it can answer on the address the browser has open right now. */
  usableHere: boolean;
  /** Whether the authenticator says the key is synced to a keychain. */
  backedUp: boolean;
  createdAt: number;
  lastUsedAt: number;
  transports: string;
}

export interface PasskeyStatus {
  /**
   * Whether this address can carry a passkey at all. The UI reads only this
   * verdict; the server's English `reason` is for API callers and the log,
   * and the card shows its own translated text (see check-passkey-reason.mjs).
   */
  supported: boolean;
  /** The relying-party id this address resolves to, empty when it has none. */
  rpId: string;
  /** How many keys are registered in total, and how many for this address. */
  total: number;
  here: number;
  /** Only present for a caller with a session. */
  passkeys?: PasskeyView[];
}

export async function fetchPasskeys(): Promise<PasskeyStatus> {
  return json<PasskeyStatus>(await fetch('/api/auth/passkeys'));
}

/** passkeysInBrowser reports whether this browser can do WebAuthn at all. */
export function passkeysInBrowser(): boolean {
  return typeof window !== 'undefined' && typeof window.PublicKeyCredential === 'function';
}

// The base64url of the WebAuthn JSON, written out to keep a dependency off the
// sign-in path. The server omits padding, so it is added back before atob.
function fromB64url(s: string): ArrayBuffer {
  const padded = s.replace(/-/g, '+').replace(/_/g, '/') + '=='.slice(0, (4 - (s.length % 4)) % 4);
  const raw = atob(padded);
  // A plain ArrayBuffer, because WebAuthn wants a BufferSource that cannot be
  // a SharedArrayBuffer, which `new Uint8Array(n)` is typed to allow.
  const buf = new ArrayBuffer(raw.length);
  const out = new Uint8Array(buf);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return buf;
}

function toB64url(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let raw = '';
  for (const b of bytes) raw += String.fromCharCode(b);
  return btoa(raw).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

/** The fields of the server's options that arrive as base64url and have to
 *  reach the browser as bytes. */
type CreationOptions = {
  challenge: string;
  user: { id: string; name: string; displayName: string };
  excludeCredentials?: { id: string; type: string; transports?: string[] }[];
} & Record<string, unknown>;

type RequestOptions = {
  challenge: string;
  allowCredentials?: { id: string; type: string; transports?: string[] }[];
} & Record<string, unknown>;

/**
 * registerPasskey runs a registration: a challenge from the server, the
 * browser's answer back to it. The browser's own refusals arrive as a
 * DOMException and are left to the caller, whose message is the useful one.
 */
export async function registerPasskey(name: string): Promise<void> {
  const begun = await json<{ ceremonyId: string; options: { publicKey?: CreationOptions } & CreationOptions }>(
    await post('/api/auth/passkey/register/begin', {}),
  );
  const opts = (begun.options.publicKey ?? begun.options) as CreationOptions;
  const cred = (await navigator.credentials.create({
    publicKey: {
      ...(opts as unknown as PublicKeyCredentialCreationOptions),
      challenge: fromB64url(opts.challenge),
      user: {
        ...opts.user,
        id: fromB64url(opts.user.id),
      },
      excludeCredentials: (opts.excludeCredentials ?? []).map((c) => ({
        ...c,
        id: fromB64url(c.id),
        type: 'public-key' as const,
        transports: c.transports as AuthenticatorTransport[] | undefined,
      })),
    },
  })) as PublicKeyCredential | null;
  if (!cred) throw new Error('no credential');
  const response = cred.response as AuthenticatorAttestationResponse;
  await json<unknown>(
    await post('/api/auth/passkey/register/finish', {
      ceremonyId: begun.ceremonyId,
      name,
      credential: {
        id: cred.id,
        rawId: toB64url(cred.rawId),
        type: cred.type,
        response: {
          clientDataJSON: toB64url(response.clientDataJSON),
          attestationObject: toB64url(response.attestationObject),
        },
        clientExtensionResults: cred.getClientExtensionResults(),
      },
    }),
  );
}

/** signInWithPasskey is the other direction: a challenge, a signature, a
 *  session cookie. */
export async function signInWithPasskey(): Promise<AuthState> {
  const begun = await json<{ ceremonyId: string; options: { publicKey?: RequestOptions } & RequestOptions }>(
    await post('/api/auth/passkey/login/begin', {}),
  );
  const opts = (begun.options.publicKey ?? begun.options) as RequestOptions;
  const cred = (await navigator.credentials.get({
    publicKey: {
      ...(opts as unknown as PublicKeyCredentialRequestOptions),
      challenge: fromB64url(opts.challenge),
      allowCredentials: (opts.allowCredentials ?? []).map((c) => ({
        ...c,
        id: fromB64url(c.id),
        type: 'public-key' as const,
        transports: c.transports as AuthenticatorTransport[] | undefined,
      })),
    },
  })) as PublicKeyCredential | null;
  if (!cred) throw new Error('no credential');
  const response = cred.response as AuthenticatorAssertionResponse;
  return json<AuthState>(
    await post('/api/auth/passkey/login/finish', {
      ceremonyId: begun.ceremonyId,
      credential: {
        id: cred.id,
        rawId: toB64url(cred.rawId),
        type: cred.type,
        response: {
          clientDataJSON: toB64url(response.clientDataJSON),
          authenticatorData: toB64url(response.authenticatorData),
          signature: toB64url(response.signature),
          userHandle: response.userHandle ? toB64url(response.userHandle) : null,
        },
        clientExtensionResults: cred.getClientExtensionResults(),
      },
    }),
  );
}

export async function renamePasskey(id: string, name: string): Promise<void> {
  await ok(
    await fetch(`/api/auth/passkeys/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),
  );
}

export async function removePasskey(id: string): Promise<void> {
  await ok(await fetch(`/api/auth/passkeys/${encodeURIComponent(id)}`, { method: 'DELETE' }));
}

// logout ends the current session. It throws on a non-2xx answer as well as
// on a network failure, so callers can report a failed sign-out.
export async function logout(): Promise<void> {
  const r = await fetch('/api/auth/logout', { method: 'POST' });
  if (!r.ok) throw new Error(await r.text());
}

// setPassword sets, changes or (with an empty next) removes the password lock.
export async function setPassword(current: string, next: string): Promise<AuthState> {
  const r = await fetch('/api/auth/password', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ current, new: next }),
  });
  if (!r.ok) throw new Error(await r.text());
  return json<AuthState>(r);
}

// API tokens are named, individually revocable credentials, so a lost device
// costs one token rather than the shared password. Other clients present them
// as "Authorization: Bearer <secret>"; this UI never does.

/** One right a token can carry (apitoken.Scope). */
export type TokenScope = 'read' | 'add' | 'control' | 'admin';

/** Every right, in the server's canonical order. */
export const TOKEN_SCOPES: readonly TokenScope[] = ['read', 'add', 'control', 'admin'];

/** One token's metadata. Never the secret. */
export interface ApiToken {
  id: string;
  name: string;
  /** What the token may do, in canonical order. */
  scopes: TokenScope[];
  createdAt: string;
  /** Absent until this token's first successful use. */
  lastUsed?: string;
}

/** What POST /api/tokens answers: the metadata plus the plaintext secret,
 *  which can never be shown again. */
export interface NewApiToken extends ApiToken {
  secret: string;
}

export async function fetchTokens(): Promise<ApiToken[]> {
  return (await json<ApiToken[]>(await fetch('/api/tokens'))) ?? [];
}

export async function createToken(name: string, scopes: readonly TokenScope[]): Promise<NewApiToken> {
  const r = await fetch('/api/tokens', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, scopes }),
  });
  return json<NewApiToken>(r);
}

/** revokeToken pulls one device's credential without touching the shared
 *  password or any other token. */
export const revokeToken = (id: string) => fetch(`/api/tokens/${encodeURIComponent(id)}`, { method: 'DELETE' });

/** One URL this instance might answer on (api.ReachableAddress). */
export interface ReachableAddress {
  /** "this connection" for the address the request arrived on, otherwise a
   *  local interface's IP. */
  label: string;
  url: string;
  /** Reachable only from this machine, so never what the QR code encodes. */
  loopback: boolean;
  /** A hostname behind a reverse proxy or VPN rather than a LAN IP; the kind
   *  that still works once the phone has left this network. */
  domain: boolean;
}

/** A QR code as the module grid the server computed, drawn as inline SVG by
 *  components/QRCode.tsx. */
export interface QRMatrix {
  size: number;
  /** One string per row, '1' for a dark module, '0' for a light one. */
  bits: string[];
}

/** What GET /api/remote-access answers with - api.RemoteAccessInfo. */
export interface RemoteAccessInfo {
  /** "container" or "desktop". The desktop build opens no TCP port, so the
   *  fields below are empty for it. */
  deployment: string;
  passwordSet: boolean;
  /** Every address this instance can name, the one the request arrived on first. */
  addresses: ReachableAddress[];
  /**
   * No password is set and this request came from another machine. Judged
   * from the request rather than from the listen address, which would read
   * as exposed for nearly every container install.
   */
  exposed: boolean;
  /** addresses[0] as a QR code; absent when there is nothing to encode. */
  qr?: QRMatrix;
}

export async function fetchRemoteAccess(): Promise<RemoteAccessInfo> {
  return json<RemoteAccessInfo>(await fetch('/api/remote-access'));
}

/**
 * Liveness, the running version and the revision it was built from. `commit`
 * is optional because the page may talk to an older server or a peer; empty
 * and absent are handled alike.
 */
export async function fetchHealth(): Promise<{ status: string; version: string; commit?: string }> {
  return json(await fetch('/api/health'));
}

export async function fetchExtensionVersion(): Promise<{ version: string }> {
  return json(await fetch('/api/browser-extension/version'));
}

export interface UpdateCheck {
  checked: boolean;
  available: boolean;
  current: string;
  latest?: string;
  url?: string;
}

export async function fetchUpdateCheck(): Promise<UpdateCheck> {
  return json(await fetch('/api/system/update-check'));
}

/** One external program a media download runs, as it stands on this machine. */
export interface MediaTool {
  found: boolean;
  path?: string;
  version?: string;
  /** "managed" | "env" | "path" | "none", for yt-dlp only; ffmpeg and ffprobe
   *  always come from PATH. */
  source?: string;
  /** Why it was not found or is unusable. English, because it names paths,
   *  errnos and program output. */
  detail?: string;
}

/** What was fetched, when, and what it turned out to be. */
export interface YtdlpManagedRecord {
  tag: string;
  asset: string;
  sha256: string;
  /** What the staged file printed at its smoke test, not what the tag claims. */
  version: string;
  fetchedAt: string;
}

export interface MediaToolsStatus {
  ytdlp: MediaTool;
  ffmpeg: MediaTool;
  ffprobe: MediaTool;
  managed?: YtdlpManagedRecord;
  /** Where a fetched copy lives. Separate from ytdlp.path, which names the
   *  fallback when the recorded copy no longer starts. */
  managedPath?: string;
  /** What would run without the fetched copy, so the page can warn when the
   *  system's copy is newer. */
  shadowed?: MediaTool;
}

export interface YtdlpLatest {
  checked: boolean;
  tag?: string;
  url?: string;
  /** "newer" | "same" | "older" | "unknown". yt-dlp versions are dates and a
   *  same-day rerelease adds a fourth segment, so some pairs cannot be
   *  ordered; "unknown" must not render as "current". */
  compare: string;
  /** GitHub's refusal verbatim when checked is false, since a rate limit and
   *  "Not Found" need different fixes. */
  detail?: string;
}

/** Which yt-dlp, ffmpeg and ffprobe this instance runs. Makes no outside call,
 *  since every settings load and diagnostics bundle reads it. */
export async function fetchMediaTools(): Promise<MediaToolsStatus> {
  return json(await fetch('/api/mediatools'));
}

/** Asks GitHub for yt-dlp's latest release. Downloads and replaces nothing. */
export async function fetchYtdlpLatest(): Promise<YtdlpLatest> {
  return json(await fetch('/api/mediatools/ytdlp/latest'));
}

/** Slow: downloads yt-dlp's newest release, verifies it against the release's
 *  SHA2-256SUMS, runs it once and only then swaps it in. A rejection leaves
 *  the working copy untouched. */
export async function updateYtdlp(): Promise<{ tag: string; version: string; path: string; asset: string; sha256: string }> {
  return json(await fetch('/api/mediatools/ytdlp/update', { method: 'POST' }));
}

/** Deletes the fetched copy and its record and answers the resulting status. */
export async function revertYtdlp(): Promise<MediaToolsStatus> {
  return json(await fetch('/api/mediatools/ytdlp/revert', { method: 'POST' }));
}

/**
 * Downloads and applies the latest release, then relaunches. Desktop only;
 * the container build answers 501. The process is already exiting when the
 * request resolves, so a network error here most likely means it worked.
 */
export async function installUpdate(): Promise<{ status: string }> {
  return json(await fetch('/api/system/update-install', { method: 'POST' }));
}

// fetchDiagnostics is called again right before a download, so the bundle
// reflects the moment it was pulled.
export async function fetchDiagnostics(): Promise<Diagnostics> {
  return json<Diagnostics>(await fetch('/api/diagnostics'));
}

// The start report, the log, the file owners and the self-test answer the
// questions asked while the diagnostics page is still open.

/**
 * One thing the start check looked at (startupcheck.Check). `id`, `role`,
 * `verdict` and `code` are stable ids that the UI translates.
 */
export interface StartupCheck {
  /** "data" | "java" | "ytdlp" | "ffmpeg" | "ffprobe" | "folder" | "clock",
   *  open so an unknown id still draws as a row. */
  id: string;
  /** Folder rows only: "downloads" | "work" | "category" | "extract" | "extractMove" | "watch". */
  role?: string;
  /** "ok" | "warn" | "fail" | "skipped", where "skipped" means not needed on
   *  this install. */
  verdict: string;
  /** What was checked: a folder, the binary found, the raw TZ value. The data
   *  directory is masked to "<data>", since the report goes into bug reports. */
  subject?: string;
  /** The fact worth reading, such as a version line or the zone and its
   *  offset, clamped by the server. */
  detail?: string;
  /** Folder rows: the deepest existing folder above `subject`. When it differs,
   *  the folder is missing, which usually means a share is not mounted. */
  measured?: string;
  /** Folder rows: a test file was written and removed. False is normal, since
   *  the boot pass writes nothing (see StartupReport.probed). */
  probed?: boolean;
  /** Which failure, so startupAdvice can offer the remedy that helps. Open string. */
  code?: string;
  /** The system's own message, verbatim and clamped. Never translated, so it
   *  stays searchable. */
  err?: string;
}

/** One whole pass of the start checks - startupcheck.Report. */
export interface StartupReport {
  /** "running" | "done" | "off". "off" means KL_STARTUP_CHECK=0, and the page
   *  says so rather than showing an empty list as a clean result. */
  state: string;
  startedAt: string;
  finishedAt?: string;
  /**
   * Whether this pass wrote test files: false at boot, true for a pass started
   * by the button. The boot only looks, because writing to a folder on an
   * Unraid array would wake its disk on every container restart.
   */
  probed: boolean;
  /** Never null. */
  checks: StartupCheck[];
}

/**
 * runStartupCheck runs the start checks again with test writes and answers a
 * fresh report. A POST because it writes, which a prefetch or link scanner must
 * never trigger. The boot reading in Diagnostics.startup is kept.
 */
export async function runStartupCheck(): Promise<StartupReport> {
  return json<StartupReport>(await fetch('/api/diagnostics/startup', { method: 'POST' }));
}

/**
 * One log line with the part of the app it came from and the download it
 * names. The server derives source and taskId, so the rules live beside the
 * log calls they describe.
 */
export interface LogLine {
  seq: number;
  line: string;
  source?: string;
  taskId?: string;
}

export interface LogTail {
  entries: LogLine[];
  /** Lines the memory buffer dropped between two polls. Non-zero means the
   *  follow view has a gap and must show it. */
  dropped: number;
  /** The cursor for the next poll. Resets on restart; the server handles a
   *  cursor from a process that is gone by starting over. */
  newest: number;
  capacity: number;
  /** The source buckets the server offers, in its own order. There are no
   *  log levels. */
  sources: string[];
}

export interface LogGeneration {
  /** 0 is the file being written; 1 the newest renamed one. */
  index: number;
  bytes: number;
  modifiedAt: string;
}

export interface LogFileState {
  /** Whether lines are reaching a file right now, which differs from the
   *  setting when the sink failed; `problem` then says why. */
  enabled: boolean;
  /** Where the file is or would be, answered even while off. Always empty in
   *  the diagnostics bundle. */
  path: string;
  bytes: number;
  maxBytes: number;
  keep: number;
  /** Never null. */
  generations: LogGeneration[];
  /** Empty while the file is being written; otherwise the sentence the card
   *  shows, with freeKnown and freeBytes for advice. */
  problem?: string;
  /** Only measured while `problem` is set. */
  freeBytes?: number;
  /** False means the platform could not be asked, which differs from zero bytes free. */
  freeKnown: boolean;
}

export interface TaskLog {
  lines: LogLine[];
  /** Where the lines were looked for. Always 'memory', so a double-click does
   *  not read rotated files off an array volume. */
  scanned: 'memory' | 'memory+file';
  /** Always true: only some log calls record which download they are about. */
  partial: boolean;
}

/**
 * fetchLogTail returns the log lines newer than `since` (0 for the whole
 * buffer) and what dropped out in between. `limit` takes the oldest matching
 * lines, so the cursor advances without skipping.
 */
export async function fetchLogTail(since: number, limit?: number): Promise<LogTail> {
  const q = new URLSearchParams({ since: String(since) });
  if (limit !== undefined) q.set('limit', String(limit));
  return json<LogTail>(await fetch(`/api/diagnostics/log?${q.toString()}`));
}

/** Whether the log is being written to disk, where, how big, and what to try
 *  when it is not. */
export async function fetchLogFileState(): Promise<LogFileState> {
  return json<LogFileState>(await fetch('/api/diagnostics/logfile'));
}

/**
 * fetchTaskLog returns the log lines that name one download. Local only:
 * neither the relay nor the federation proxy forwards /api/diagnostics,
 * because a log line can carry an indexer key in a feed URL. Check
 * isLocalBase first.
 */
export async function fetchTaskLog(id: string): Promise<TaskLog> {
  return json<TaskLog>(await fetch(`/api/diagnostics/task/${encodeURIComponent(id)}`));
}

/** logFileHref is the download address of one log file, used as a plain href
 *  so a file of up to a gigabyte never passes through a Blob. */
export function logFileHref(gen: number): string {
  return withBase(`/api/diagnostics/logfile/${gen}`);
}

/**
 * Who this instance writes files as, and what the environment asked for.
 * With `known` false (Windows has no unix owners) the numbers mean nothing,
 * not uid 0, and the UI has to say so.
 */
export interface FileOwnerIdentity {
  known: boolean;
  /** "container" | "desktop". PUID is a container idea; the desktop app runs
   *  as the person using it. */
  deployment: string;
  /** The effective ids, which the kernel stamps on a new file. */
  uid: number;
  gid: number;
  /** "" when the id has no passwd or group entry, which is normal under
   *  --user 99:100. */
  user: string;
  group: string;
  /** Four octal digits, "0022". Empty when umaskKnown is false. */
  umask: string;
  /** False on a kernel without the Umask: line in /proc/self/status (before
   *  Linux 4.7). Setting and restoring the umask to read it would race with
   *  the goroutines creating files. */
  umaskKnown: boolean;
  /** What the operator set, verbatim. "" means unset, which is not 0. */
  env: { puid: string; pgid: string; umask: string };
  /**
   * Whether this build acts on those three. It does not: the image runs as
   * USER knight (uid 1000), which cannot switch to another uid. Shown so an
   * operator can tell an ignored PUID from a mistyped one; never render it as
   * "PUID would work if you set it".
   */
  envRead: boolean;
}

/** One folder as the probe measured it by creating, stat-ing and removing a
 *  real file and sub-folder. Computing it from uid and umask would be wrong on
 *  set-group-id folders, root-squashed NFS and mounts with their own umask. */
export interface FolderOwnerProbe {
  dir: string;
  /** "downloads" | "work" | "category" | "watch", translated locally. */
  role: string;
  exists: boolean;
  /** False where files have no owner. Every number below then means nothing. */
  known: boolean;
  dirUid: number;
  dirGid: number;
  dirUser: string;
  dirGroup: string;
  /** Four octal digits, so a set-group-id folder reads "2775". */
  dirMode: string;
  /** The probe file, which is what a finished download will look like. */
  fileUid: number;
  fileGid: number;
  fileUser: string;
  fileGroup: string;
  fileMode: string;
  /** What a new per-package folder comes out as. With a tight umask the files
   *  can be fine while their folder cannot be entered. */
  subdirMode: string;
  /** A stable id:
   *  ok | ownerMismatch | groupUnreadable | dirUnreadable | notWritable | missing | unknown */
  verdict: string;
  /** The raw OS error, present only for the verdicts that have one. */
  detail?: string;
}

export interface FolderOwnerReport {
  checkedAt: string;
  /** Never null. */
  folders: FolderOwnerProbe[];
}

/** One configured folder as a stat saw it, for the diagnostics bundle. No path
 *  and no raw error, since the bundle is attached to public reports. */
export interface FolderOwnership {
  role: string;
  exists: boolean;
  known: boolean;
  uid: number;
  gid: number;
  user: string;
  group: string;
  mode: string;
}

/** fetchFileOwner reports who this instance writes files as. It only reads,
 *  so a page may refresh it freely. */
export async function fetchFileOwner(): Promise<FileOwnerIdentity> {
  return json<FileOwnerIdentity>(await fetch('/api/fileowner'));
}

/**
 * checkFolderOwners measures what a file written into each configured folder
 * comes out as. A POST because it writes a probe file and sub-folder per
 * folder. `dirs` narrows the run to folders this instance already writes
 * into; anything else is refused with a 400 rather than skipped, so a report
 * is never silently partial.
 */
export async function checkFolderOwners(dirs?: string[]): Promise<FolderOwnerReport> {
  return json<FolderOwnerReport>(await post('/api/fileowner/check', dirs ? { dirs } : {}));
}

/**
 * internal/selftest.Status. `skipped` means nothing is configured to check;
 * `unknown` means it is configured and this build cannot find out. Neither is
 * "off".
 */
export type SelfTestStatus = 'pass' | 'warn' | 'fail' | 'skipped' | 'unknown';

/** One check, mirroring internal/selftest.Result field for field. */
export interface SelfTestResult {
  /** "jd" | "ytdlp" | "folders" | "accounts" | "relay" | "clock" | "torrentPort",
   *  or inside `rows` the folder path or account key the row describes. */
  id: string;
  status: SelfTestStatus;
  /** The key of the sentence to render, such as "ytdlp.old". */
  code: string;
  /** That sentence's substitutions. Byte counts arrive as decimal strings and
   *  are formatted here with fmtBytes, in the reader's locale. */
  params?: Record<string, string>;
  /** The other side's own words (a Go error, a provider's refusal), left untranslated. */
  detail?: string;
  /** One level only: one row per debrid account, one per folder. */
  rows?: SelfTestResult[];
  /** RFC3339. */
  at: string;
}

export interface SelfTestRun {
  /** Empty when the instance has never been checked. */
  id: string;
  startedAt: string;
  /** Absent while the run is still going; the page polls until it appears. */
  finishedAt?: string;
  /** Every check id this run will report, in order, so all rows can show as
   *  waiting before the first result. */
  planned: string[];
  results: SelfTestResult[];
}

/**
 * What this instance saw of the request that asked, the server half of the
 * reverse-proxy card. The browser half is window.location plus a probe
 * websocket; a probe run on the server would only reach its own listener.
 */
export interface SelfTestRequestView {
  /** r.Host as received; sameOrigin compares the browser's Origin against it. */
  host: string;
  /** X-Forwarded-Host, "" when absent. */
  forwardedHost: string;
  /** X-Forwarded-Proto, lowercased, "" when absent. */
  forwardedProto: string;
  /** Whether the connection into this process was TLS, which differs from the
   *  browser being on https. */
  tls: boolean;
  /** r.URL.Path, as evidence that nothing rewrote the path on the way in. */
  path: string;
  /** X-Forwarded-Prefix without a trailing slash, "" when absent. The only
   *  trace a stripped path prefix leaves. */
  forwardedPrefix: string;
  /** The prefix the instance served this request under, KL_BASE_PATH or a
   *  usable X-Forwarded-Prefix, "" at the root. */
  basePath: string;
  /** How many hops X-Forwarded-For names. The addresses themselves are not
   *  sent, since they map someone's internal network. */
  forwardedForHops: number;
  /** The server's clock when it answered, RFC3339 with offset. Compare it
   *  with the midpoint of the round trip (see lib/selftest.ts). */
  now: string;
  /** time.Local's name ("UTC", "Europe/Berlin") and its offset right now. */
  zone: string;
  zoneOffsetSeconds: number;
  /** False only when $TZ names a zone the database lacks, so the process has
   *  fallen back to UTC. */
  zoneReadable: boolean;
  /** buildinfo.Deployment, so the page can hide the proxy rows on desktop. */
  deployment: string;
}

/**
 * startSelfTest starts a run and returns at once with a 202, since logging in
 * to every provider can outlast a reverse proxy's read timeout. Poll
 * fetchSelfTest until finishedAt appears. A call during a run joins it, so
 * providers are not asked twice and rate-limited.
 */
export async function startSelfTest(): Promise<SelfTestRun> {
  return json<SelfTestRun>(await fetch('/api/selftest', { method: 'POST' }));
}

/** fetchSelfTest returns the current or last run; an unchecked instance
 *  answers a run with an empty id rather than a 404. */
export async function fetchSelfTest(): Promise<SelfTestRun> {
  return json<SelfTestRun>(await fetch('/api/selftest'));
}

/** The caller times this for the clock comparison. no-store, because a cached
 *  timestamp would be reported as drift. */
export async function fetchRequestView(): Promise<SelfTestRequestView> {
  return json<SelfTestRequestView>(await fetch('/api/selftest/request', { cache: 'no-store' }));
}

/**
 * What the store and settings files cost, and where a compaction's scratch
 * copy would go. The paths are here and not in the diagnostics bundle: this
 * only reaches someone on their own settings pages, while the bundle is
 * attached to public reports.
 */
export interface StorageInfo {
  storePath: string;
  /** The .db plus any -journal/-wal/-shm beside it. */
  storeBytes: number;
  /** freelist_count * page_size. A floor, since compacting also repacks half-filled pages. */
  storeReclaimableBytes: number;
  settingsPath: string;
  settingsBytes: number;
  /** False until a settings page has been saved once; the server never writes settings.json by itself. */
  settingsPresent: boolean;
  /**
   * Where SQLite writes the second copy a compaction needs. The container
   * image sets no TMPDIR, so it lands in the container's writable layer rather
   * than on the data volume, and "database or disk is full" names the wrong disk.
   */
  tempDir: string;
  /** 0 when the platform cannot be asked, which is not 0 bytes free. Render it with tempDir, never alone. */
  tempFreeBytes: number;
}

/** One completed maintenance pass, exactly as the server recorded it. */
export interface MaintenanceRun {
  kind: 'check' | 'compact' | 'analyze';
  at: string;
  durationMs: number;
  /** False for a check that found damage and for one that could not run; `error` tells them apart. */
  ok: boolean;
  /** integrity_check's findings, one per entry, never null and never containing the bare word "ok". */
  problems: string[];
  /** The file's size either side of a compaction, and 0 for the two kinds that do not change it. */
  bytesBefore: number;
  bytesAfter: number;
  error: string;
  /** '' on a pass that ran; 'downloads-running' on a scheduled one that stood down. */
  skipped: string;
}

/**
 * GET /api/system/maintenance. `last` and `nextRunAt` are nullable, because
 * "nothing has run" must not render as a clean result.
 */
export interface MaintenanceState {
  storage: StorageInfo;
  /** '' when idle. The page polls while this is non-empty and stops when it clears. */
  running: '' | 'check' | 'compact' | 'analyze';
  intervalDays: number;
  compactOnSchedule: boolean;
  nextRunAt: string | null;
  last: MaintenanceRun | null;
}

export async function fetchMaintenance(): Promise<MaintenanceState> {
  return json<MaintenanceState>(await fetch('/api/system/maintenance'));
}

/**
 * startMaintenance starts one pass and returns at once with a 202, since a
 * compaction of a large store outlasts any request timeout. Poll
 * fetchMaintenance while `running` is non-empty. A pass already in progress
 * throws an ApiError with status 409, whose body is the state document rather
 * than a sentence: check the status and show settings.dbmaint.busy.
 */
export async function startMaintenance(action: 'check' | 'compact' | 'analyze'): Promise<MaintenanceState> {
  return json<MaintenanceState>(
    await fetch('/api/system/maintenance', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action }),
    }),
  );
}

/** Which build this is and what quit and restart do here. */
export interface DeploymentInfo {
  deployment: string;
  canQuit: boolean;
  canRestart: boolean;
  note: string;
}

export async function fetchDeploymentInfo(): Promise<DeploymentInfo> {
  return json<DeploymentInfo>(await fetch('/api/system/deployment'));
}

export async function requestQuit(): Promise<{ status: string }> {
  return json(await fetch('/api/system/quit', { method: 'POST' }));
}

export async function requestRestart(): Promise<{ status: string }> {
  return json(await fetch('/api/system/restart', { method: 'POST' }));
}

/** Where a backup archive streams from. Opened directly so the browser handles
 *  a download of several hundred MB. */
export const BACKUP_DOWNLOAD_URL = '/api/system/backup';

export interface RestoreResult {
  manifest: { version: string; deployment: string; createdAt: string };
  restarting: boolean;
  status: string;
}

export async function uploadRestore(file: File): Promise<RestoreResult> {
  const body = new FormData();
  body.append('file', file);
  return json(await fetch('/api/system/restore', { method: 'POST', body }));
}

/**
 * A settings-only export (settings.PortableDoc). Unlike a backup, which
 * replaces a whole install at the next start, this is settings.json without
 * the identity keys, imported key by key and applied live.
 */
export interface SettingsExportDoc {
  kind: 'knightloader-settings';
  version: string;
  deployment: string;
  createdAt: string;
  /** What the file claims about itself. Anyone can edit it, so both sides
   *  inspect the settings instead of trusting this. */
  secrets: 'included' | 'omitted';
  /** settings.json's top-level keys, raw. Untyped so that keys an older or
   *  newer build knows can still be listed. */
  settings: Record<string, unknown>;
}

/**
 * settingsExportURL is where a settings export streams from, opened directly
 * so the browser owns the save dialog. The server treats anything but
 * "include" as "omit".
 */
export const settingsExportURL = (includeSecrets: boolean) =>
  withBase(`/api/settings/export?secrets=${includeSecrets ? 'include' : 'omit'}`);

/**
 * What POST /api/settings/import did, key by key. Several of these fields
 * describe things that save cleanly and fail later, so they all reach the
 * screen.
 */
export interface SettingsImportResult {
  /** The keys that reached the store. */
  applied: string[];
  /** Asked for and refused: the identity keys, and keys the file does not
   *  actually carry. */
  skipped: string[];
  /** Keys this build does not know. The server's migration only runs on
   *  settings.json, so a renamed key cannot be mapped here. */
  unknown: string[];
  /** Codes such as "reconnect.password" or "archivePasswords", which the UI translates. */
  incomplete: string[];
  /** Imported rules this build cannot compile. They save but never fire. */
  ruleProblems: number;
  /** The whole document as it now stands, for SettingsDraft.reseed. */
  settings: Settings;
}

/**
 * importSettings takes over exactly the named keys. The document travels with
 * the selection because the selection was made in a preview of that document.
 * A refusal throws an ApiError; a file from a newer build also carries the
 * code "transfer.tooNew" with { version, running }.
 */
export async function importSettings(
  document: SettingsExportDoc,
  keys: string[],
): Promise<SettingsImportResult> {
  const r = await fetch('/api/settings/import', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ document, keys }),
  });
  return json<SettingsImportResult>(r);
}

export async function fetchInstances(): Promise<Instance[]> {
  return (await json<Instance[]>(await fetch('/api/instances'))) ?? [];
}

/**
 * An instance announcing itself on this network. Being on the same network is
 * not consent: adding it stores the address only, and a peer with a password
 * still refuses until the connection phrase is shared.
 */
export interface DiscoveredInstance {
  id: string;
  name: string;
  url: string;
  deployment: string;
  /** Already a stored or relay peer. Shown greyed rather than hidden. */
  known: boolean;
}

export async function fetchDiscovered(): Promise<DiscoveredInstance[]> {
  return (await json<DiscoveredInstance[]>(await fetch('/api/discovery'))) ?? [];
}

/**
 * addInstance registers a peer by address, without a credential. `refused`
 * tells a peer that said no, which needs the connection phrase, from one that
 * could not be reached.
 */
export async function addInstance(
  name: string,
  url: string,
): Promise<{ online: boolean; refused: boolean }> {
  const r = await fetch('/api/instances', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, url }),
  });
  return json(r);
}

export const removeInstance = (name: string) =>
  fetch(`/api/instances/${encodeURIComponent(name)}`, { method: 'DELETE' });

/** Which relay an instance uses. 'off' suits instances that all sit on one
 *  network and find each other through local discovery. */
export type RelayMode = 'project' | 'own' | 'off';

/** GET and PUT /api/relay/config. The relay key is never in it, not even
 *  masked; `keySet` is all that is reported. */
export interface RelayConfig {
  /** Always one of the three; the server resolves an empty stored value. */
  mode: RelayMode;
  relayUrl: string;
  keySet: boolean;
  /** Whether the socket to the relay is up right now, not merely configured. */
  connected: boolean;
  /** Whether this instance runs the relay itself, under /relay/connect. */
  serve: boolean;
  /** Instances connected to the relay this one serves, itself included when it
   *  dials its own. Zero while `serve` is false. */
  serveClients: number;
}

export async function fetchRelayConfig(): Promise<RelayConfig> {
  return json<RelayConfig>(await fetch('/api/relay/config'));
}

/**
 * saveRelayConfig stores the address and answers with what is now stored.
 * `key` undefined keeps the stored key, '' clears it, anything else replaces
 * it. An unreachable relay is not a failure: the address is stored and the
 * client keeps dialling.
 */
export async function saveRelayConfig(
  relayUrl: string,
  key?: string,
  serve?: boolean,
  mode?: RelayMode,
): Promise<RelayConfig> {
  // Fields left undefined stay off the wire, so a save from one control does
  // not reset the others to what they were when its form was drawn.
  const body: Record<string, unknown> = { relayUrl };
  if (key !== undefined) body.key = key;
  if (serve !== undefined) body.serve = serve;
  if (mode !== undefined) body.mode = mode;
  return json<RelayConfig>(
    await fetch('/api/relay/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  );
}

/**
 * GET /api/connect. The connection phrase lets a person's own instances find
 * each other on the relay, whose address is compiled into the binary, so the
 * phrase carries only the secret. It does not give a public https address.
 */
export interface ConnectInfo {
  /** Whether this instance holds a connection secret at all. */
  active: boolean;
  /** Whether the relay socket is up; a stored phrase with an unreachable
   *  relay is active but not connected. */
  connected: boolean;
  /** Mirrors GET /api/auth, so the page can warn before minting a phrase
   *  that reaches every instance in the group. */
  passwordSet: boolean;
  /** Which relay this instance is pointed at. */
  relayUrl: string;
  /** Whether that is somebody's own relay rather than the default one. */
  selfHosted: boolean;
  /** The same three-way answer RelayConfig carries; `selfHosted` cannot say 'off'. */
  relayMode: RelayMode;
  /** Where the project relay is, whichever relay this instance uses. */
  projectRelayUrl: string;
}

export async function fetchConnect(): Promise<ConnectInfo> {
  return json<ConnectInfo>(await fetch('/api/connect'));
}

/** activateConnect mints this instance's phrase and answers with it. This is
 *  the one time it comes back without the password. */
export async function activateConnect(): Promise<{ phrase: string; qr?: QRMatrix; info: ConnectInfo }> {
  return json(await fetch('/api/connect/activate', { method: 'POST' }));
}

/**
 * PhraseRejected is a phrase the server would not take. The server sends a
 * reason code and specifics so the message can be translated: `word` and
 * `position` for 'unknown_word', `count` for 'word_count'.
 */
export class PhraseRejected extends Error {
  reason: 'word_count' | 'unknown_word' | 'checksum';
  word: string;
  position: number;
  count: number;

  constructor(body: { error?: string; reason?: string; word?: string; position?: number; count?: number }) {
    super(body.error ?? 'phrase rejected');
    this.name = 'PhraseRejected';
    // An unknown code falls back to 'checksum', the one reason that needs no
    // specifics and is true of every rejected phrase.
    this.reason =
      body.reason === 'word_count' || body.reason === 'unknown_word' ? body.reason : 'checksum';
    this.word = body.word ?? '';
    this.position = body.position ?? 0;
    this.count = body.count ?? 0;
  }
}

/** joinConnect is the other half: every instance after the first. */
export async function joinConnect(phrase: string): Promise<ConnectInfo> {
  const r = await fetch('/api/connect/join', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ phrase }),
  });
  if (!r.ok) {
    const raw = await r.text();
    // A rejected phrase answers with JSON; other failures (an expired session,
    // a proxy) answer with text.
    try {
      throw new PhraseRejected(JSON.parse(raw));
    } catch (e) {
      if (e instanceof PhraseRejected) throw e;
      throw new Error(raw.trim() || `${r.status}`);
    }
  }
  return json(r);
}

/**
 * revealConnect shows the phrase again. It needs the password whenever one is
 * set, since the phrase is the key to every instance in the group.
 */
export async function revealConnect(password: string): Promise<{ phrase: string; qr?: QRMatrix }> {
  const r = await fetch('/api/connect/reveal', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  });
  return json(r);
}

/** leaveConnect forgets the secret and stops dialling the relay. */
export async function leaveConnect(): Promise<void> {
  const r = await fetch('/api/connect', { method: 'DELETE' });
  if (!r.ok) throw new Error(await r.text());
}

/**
 * connectWS opens the live event stream, reconnects on its own and returns a
 * closer. `kinds` subscribes to just those broadcast types, and is sent again
 * on every reconnect because a fresh socket starts unfiltered; omitted, the
 * connection gets everything. Direct sends such as 'snapshot' arrive
 * regardless.
 *
 * `reportVisibility` tells the server whenever the page goes to the background
 * or comes back, which decides whether somebody is watching the captcha prompt
 * (hub.Watched). A connection that never reports is not counted as a viewer.
 */
export function connectWS(
  onMessage: (type: string, data: any) => void,
  kinds?: string[],
  reportVisibility = false,
): () => void {
  let ws: WebSocket | null = null;
  let closed = false;
  const sendVisibility = () => {
    if (ws?.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({ type: 'visibility', visible: document.visibilityState === 'visible' }));
  };
  const open = () => {
    // A reconnect timer can fire after the caller has closed the stream.
    if (closed) return;
    ws = new WebSocket(socketURL('/api/ws'));
    ws.onopen = () => {
      if (kinds && kinds.length > 0) ws?.send(JSON.stringify({ type: 'subscribe', kinds }));
      if (reportVisibility) sendVisibility();
    };
    ws.onmessage = (e) => {
      try {
        const m = JSON.parse(e.data);
        onMessage(m.type, m.data);
      } catch {
        /* ignore */
      }
    };
    ws.onclose = () => {
      if (!closed) setTimeout(open, 1500);
    };
  };
  if (reportVisibility) document.addEventListener('visibilitychange', sendVisibility);
  open();
  return () => {
    closed = true;
    if (reportVisibility) document.removeEventListener('visibilitychange', sendVisibility);
    ws?.close();
  };
}
