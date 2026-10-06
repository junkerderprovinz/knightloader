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

/** Leaves the group, dropping the phrase, the member id and the default target. */
async function forgetGroup() {
  await secretStore('readwrite', (s) => {
    s.delete('member');
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

/**
 * A fresh relay id for every session. The relay takes a second join under an
 * id it already holds for a reconnect and drops the first socket, and the
 * popup, the options page and a send often have sessions open at once. The
 * group recognises the browser by readMemberId instead.
 */
function sessionInstanceId() {
  const bytes = crypto.getRandomValues(new Uint8Array(20));
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}

/**
 * The id the group knows this browser by across sessions, sealed into every
 * announce and call. An instance lists the browser once under it and turns it
 * away once removed. It is minted on first use, in the same transaction that
 * looks for it, so two sessions starting together agree on one.
 */
function readMemberId() {
  return secretStore('readwrite', (s) => {
    const out = {};
    const got = s.get('member');
    got.onsuccess = () => {
      out.result = typeof got.result === 'string' ? got.result : sessionInstanceId();
      if (out.result !== got.result) s.put(out.result, 'member');
    };
    return out;
  });
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
  return relaySession(
    {
      url: DEFAULT_RELAY_URL,
      key,
      frameKey,
      selfId: sessionInstanceId(),
      memberId: await readMemberId(),
      // A plain label rather than anything identifying the browser.
      selfName: 'Browser',
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
 * over one relay session through routes a member may reach (relayForwardable
 * in internal/api/routes_relay.go). An instance that does not answer gets
 * `status: null`, so its card can say offline.
 *
 * The web address is asked for inside the encrypted frame rather than
 * announced, where the relay could read it.
 */
async function groupStatus() {
  return withGroup(async ({ siblings, call }) => {
    const read = async (id, path) => {
      const res = await call(id, 'GET', path).catch(() => null);
      if (!res || res.status < 200 || res.status >= 300) return null;
      try {
        return JSON.parse(res.body);
      } catch {
        return null;
      }
    };
    return Promise.all(
      siblings.map(async (s) => {
        const [queue, counters, remote] = await Promise.all([
          read(s.instanceId, '/api/queue'),
          read(s.instanceId, '/api/queue/counters'),
          read(s.instanceId, '/api/remote-access'),
        ]);
        // Connected to the relay but not serving, which differs from idle.
        if (!queue && !counters) return { ...s, status: null };
        return { ...s, status: { queue, counters, webUrl: bestWebUrl(remote) } };
      }),
    );
  });
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

/** What an instance is called in a list, falling back to the start of its id so
 *  two unnamed instances still differ. */
function instanceLabel(inst) {
  return inst.name && inst.name.trim() ? inst.name.trim() : inst.instanceId.slice(0, 8);
}
