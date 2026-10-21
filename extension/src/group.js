/**
 * The group this browser belongs to.
 *
 * Only the phrase is stored. The keys are cheap to derive again, and a stored
 * copy could go stale if the derivation changed; who is online comes from the
 * relay on every connect.
 *
 * The phrase lives in the extension's own IndexedDB rather than in
 * storage.local, which the Click'n'Load content scripts on every site can read.
 * Only the extension's pages and its background share that origin.
 *
 * `defaultInstance` holds a relay instance id, so renaming a member keeps the
 * choice on the same machine.
 */

const SECRET_DB = 'knightloader';
const SECRET_STORE = 'secrets';

/** Runs one request against the secret store and resolves with its result once the transaction commits. */
function secretStore(mode, request) {
  return new Promise((resolve, reject) => {
    const open = indexedDB.open(SECRET_DB, 1);
    open.onupgradeneeded = () => open.result.createObjectStore(SECRET_STORE);
    open.onerror = () => reject(open.error);
    open.onsuccess = () => {
      const db = open.result;
      const tx = db.transaction(SECRET_STORE, mode);
      const req = request(tx.objectStore(SECRET_STORE));
      tx.oncomplete = () => {
        db.close();
        resolve(req.result);
      };
      tx.onabort = () => {
        db.close();
        reject(tx.error);
      };
    };
  });
}

/** Reads the stored phrase, or '' when this browser has not joined a group. */
async function readPhrase() {
  const kept = await secretStore('readonly', (s) => s.get('phrase'));
  if (typeof kept === 'string') return kept;
  // A phrase found in storage.local moves out of the content scripts' reach
  // the first time it is read.
  const { phrase } = await chrome.storage.local.get('phrase');
  if (typeof phrase !== 'string' || !phrase) return '';
  await secretStore('readwrite', (s) => s.put(phrase, 'phrase'));
  await chrome.storage.local.remove('phrase');
  return phrase;
}

/**
 * Stores the phrase after checking that it decodes, so a typo is reported
 * instead of turning into a relay that never connects. Throws PhraseError.
 */
async function writePhrase(phrase) {
  const normalised = String(phrase).trim().toLowerCase().split(/\s+/).filter(Boolean).join(' ');
  await decodePhrase(normalised);
  await secretStore('readwrite', (s) => s.put(normalised, 'phrase'));
  return normalised;
}

/** Leaves the group, dropping the phrase, the member key, and with it the member id, and the default target. */
async function forgetGroup() {
  await secretStore('readwrite', (s) => {
    s.delete('memberKey');
    return s.delete('phrase');
  });
  await chrome.storage.local.remove(['phrase', 'defaultInstance']);
}

/** The relay instance id this browser sends to when it is not asked. */
async function readDefaultTarget() {
  const stored = await chrome.storage.local.get('defaultInstance');
  return typeof stored.defaultInstance === 'string' ? stored.defaultInstance : '';
}

/**
 * defaultOf resolves the current default instance of the group. Until someone
 * chooses, or when the stored choice has left, the first instance stands in.
 * The fallback is not stored, since the order changes as instances come and go.
 */
function defaultOf(siblings, stored) {
  if (stored && siblings.some((s) => s.instanceId === stored)) return stored;
  return siblings[0]?.instanceId ?? '';
}

async function writeDefaultTarget(instanceId) {
  await chrome.storage.local.set({ defaultInstance: String(instanceId || '') });
}

/** The longest name the options page takes, well inside what the relay keeps. */
const DEVICE_NAME_MAX = 60;

/**
 * The name this browser announces to the group and the instances list it
 * under. Until one is set it is the browser and its system.
 */
async function readDeviceName() {
  const { deviceName } = await chrome.storage.local.get('deviceName');
  return typeof deviceName === 'string' && deviceName.trim() ? deviceName.trim() : defaultDeviceName();
}

/** Stores a name; an empty one goes back to the default. Returns the name in force. */
async function writeDeviceName(name) {
  const kept = String(name ?? '').trim().slice(0, DEVICE_NAME_MAX);
  if (kept) await chrome.storage.local.set({ deviceName: kept });
  else await chrome.storage.local.remove('deviceName');
  return kept || defaultDeviceName();
}

/**
 * defaultDeviceName names the browser and its system, such as "Brave on
 * Windows". Brave lists itself among the client hints' brands and sets
 * navigator.brave in a page; Firefox has no client hints, so the user agent
 * decides there.
 */
function defaultDeviceName() {
  const nav = globalThis.navigator ?? {};
  const ua = nav.userAgent ?? '';
  const brands = (nav.userAgentData?.brands ?? []).map((b) => b.brand);
  const byBrand = [
    ['Brave', 'Brave'],
    ['Microsoft Edge', 'Edge'],
    ['Opera', 'Opera'],
    ['Vivaldi', 'Vivaldi'],
    ['Google Chrome', 'Chrome'],
    ['Chromium', 'Chromium'],
  ].find(([brand]) => brands.includes(brand))?.[1];
  const browser =
    (nav.brave ? 'Brave' : byBrand) ??
    (/Firefox\//.test(ua) ? 'Firefox' : /Edg\//.test(ua) ? 'Edge' : /OPR\//.test(ua) ? 'Opera' : 'Chrome');
  // Android first, since its user agent also says Linux.
  const where = `${nav.userAgentData?.platform ?? ''} ${ua}`;
  const system = [
    [/Windows|Win32|Win64/, 'Windows'],
    [/Android/, 'Android'],
    [/CrOS|Chrome OS/, 'ChromeOS'],
    [/macOS|Mac OS X|Macintosh/, 'macOS'],
    [/Linux/, 'Linux'],
  ].find(([rule]) => rule.test(where))?.[1];
  return system ? t('options.deviceNameDefault', { browser, system }) : browser;
}

/**
 * A fresh relay id for every session. The relay takes a second join under an
 * id it already holds for a reconnect and drops the first socket, and the
 * popup, the options page and a send often have sessions open at once. The
 * group recognises the browser by its member id instead (readMember).
 */
function sessionInstanceId() {
  const bytes = crypto.getRandomValues(new Uint8Array(20));
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}

/**
 * The member key this browser signs its calls with, and the id the group
 * knows it by, made from the key (relayMemberId): "browser-" and the hex of
 * the key's SHA-256. It is sealed into every announce and call, and an
 * instance lists the browser once under it, turns it away once removed and
 * takes a call under it only when that key signed it. The private half never
 * leaves WebCrypto. A browser without Ed25519 in WebCrypto gets null and calls
 * unsigned, which every instance refuses with a reason.
 */
async function readMember() {
  let kept = await secretStore('readonly', (s) => s.get('memberKey'));
  if (!(kept && kept.privateKey && kept.publicKey)) {
    let pair;
    try {
      pair = await crypto.subtle.generateKey({ name: 'Ed25519' }, false, ['sign', 'verify']);
    } catch {
      return null;
    }
    const made = {
      privateKey: pair.privateKey,
      publicKey: new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey)),
    };
    // Two sessions starting together each made a key; the first one stored
    // wins and both sign with it.
    await secretStore('readwrite', (s) => {
      const got = s.get('memberKey');
      got.onsuccess = () => {
        if (!(got.result && got.result.privateKey)) s.put(made, 'memberKey');
      };
      return got;
    });
    kept = (await secretStore('readonly', (s) => s.get('memberKey'))) ?? made;
  }
  return { id: await relayMemberId('extension', kept.publicKey), key: kept };
}

/**
 * withGroup opens a relay session for the stored phrase and hands it to `work`,
 * like relaySession. Without a phrase it throws an Error with code 'no-phrase'
 * for callers to translate.
 */
async function withGroup(work) {
  const phrase = await readPhrase();
  if (!phrase) {
    const err = new Error('no-phrase');
    err.code = 'no-phrase';
    throw err;
  }
  const { key, frameKey } = await keysFromPhrase(phrase);
  const member = await readMember();
  return relaySession(
    {
      url: DEFAULT_RELAY_URL,
      key,
      frameKey,
      selfId: sessionInstanceId(),
      memberId: member?.id,
      memberKey: member?.key,
      selfName: await readDeviceName(),
      // An instance took this browser out of the group. It starts over like a
      // fresh install: the phrase entered again joins it as a new member.
      onRemoved: forgetGroup,
    },
    work,
  );
}

/** The instances in the group. relaySession already leaves out clients such as
 *  other browsers and the phone, which cannot take a download. */
async function groupInstances() {
  return withGroup(async ({ siblings }) => siblings);
}

/**
 * groupStatus is the group plus what each instance is doing, asked in parallel
 * over one relay session through routes a member may reach (groupRoutes in
 * internal/api/group_routes.go). An instance that does not answer gets
 * `status: null`, so its card can say offline.
 *
 * The web address is asked for inside the encrypted frame rather than
 * announced, where the relay could read it.
 */
async function groupStatus() {
  return withGroup(({ siblings, call }) => statusOf(siblings, call));
}

/**
 * groupView is groupStatus with the group's phones and browsers, asked in the
 * same session of the default instance, or of the first that answered when
 * the default did not.
 */
async function groupView(defaultTarget) {
  return withGroup(async ({ siblings, call }) => {
    const instances = await statusOf(siblings, call);
    const live = instances.filter((s) => s.status !== null);
    const ask = defaultOf(live, defaultTarget);
    return { instances, apps: ask ? await groupApps(call, ask) : [] };
  });
}

/** readJSON is one GET over `call`, parsed, or null for any answer but a 2xx. */
async function readJSON(call, id, path, refused) {
  const res = await call(id, 'GET', path).catch(() => null);
  if (!res || res.status < 200 || res.status >= 300) {
    if (res && refused) refused.key = refusalKey(res);
    return null;
  }
  try {
    return JSON.parse(res.body);
  } catch {
    return null;
  }
}

/** statusOf asks every sibling what it is doing, in parallel. */
function statusOf(siblings, call) {
  return Promise.all(
    siblings.map(async (s) => {
      const refused = {};
      const [queue, counters, remote] = await Promise.all([
        readJSON(call, s.instanceId, '/api/queue', refused),
        readJSON(call, s.instanceId, '/api/queue/counters'),
        readJSON(call, s.instanceId, '/api/remote-access'),
      ]);
      // Connected to the relay but not serving, which differs from idle. An
      // instance that turned this browser away says why.
      if (!queue && !counters) return { ...s, status: null, refusal: refused.key };
      return { ...s, status: { queue, counters, webUrl: bestWebUrl(remote) } };
    }),
  );
}

/**
 * groupApps is the group's phones and browsers as `instanceId` lists them. An
 * instance from before the list, or one that keeps it from this browser,
 * answers with none.
 */
async function groupApps(call, instanceId) {
  const apps = (await readJSON(call, instanceId, '/api/group/apps'))?.apps;
  return Array.isArray(apps) ? apps.filter((a) => a && typeof a.id === 'string') : [];
}

/**
 * bestWebUrl picks the reported address most likely to work from this browser.
 * Loopback addresses are dropped, since here they mean this machine, and a
 * domain wins over a bare IP because someone set it up for outside access.
 */
function bestWebUrl(remote) {
  const list = Array.isArray(remote?.addresses) ? remote.addresses : [];
  const usable = list.filter((a) => a && a.url && !a.loopback);
  const domain = usable.find((a) => a.domain);
  return (domain ?? usable[0])?.url ?? '';
}

/**
 * The refusals an instance answers a browser with when it will not take its
 * key (internal/federation/members.go) or a call a member may not make
 * (internal/api/group_routes.go), each with its sentence.
 * check-member-refusals.mjs holds this map to the server's codes.
 */
const REFUSAL_KEYS = {
  memberPending: 'refusal.memberPending',
  memberKeyChanged: 'refusal.memberKeyChanged',
  memberUnsigned: 'refusal.memberUnsigned',
  memberBusy: 'refusal.memberBusy',
  groupFull: 'refusal.groupFull',
  groupJoinsBusy: 'refusal.groupJoinsBusy',
  memberNotPinned: 'refusal.memberNotPinned',
  ownLoginNeeded: 'refusal.ownLoginNeeded',
};

/** refusalKey is the catalogue key for why an instance refused a call: its
 *  own sentence where the code is in REFUSAL_KEYS, else the plain one. An
 *  instance that was reset answers the old words with a plain 410 "reset"
 *  (internal/api/retired_group.go). */
function refusalKey(res) {
  if (res.status === 410 && res.body === 'reset') return 'refusal.groupReset';
  try {
    const code = JSON.parse(res.body)?.code;
    if (typeof code === 'string' && Object.hasOwn(REFUSAL_KEYS, code)) return REFUSAL_KEYS[code];
  } catch {
    // A plain-text refusal.
  }
  return 'send.refused';
}

/** What an instance is called in a list, falling back to the start of its id so
 *  two unnamed instances still differ. */
function instanceLabel(inst) {
  return inst.name && inst.name.trim() ? inst.name.trim() : inst.instanceId.slice(0, 8);
}
