/**
 * The group this browser belongs to.
 *
 * Only the phrase is stored. The keys are cheap to derive again, and a stored
 * copy could go stale if the derivation changed; who is online comes from the
 * relay on every connect.
 *
 * `defaultInstance` holds a relay instance id, so renaming a member keeps the
 * choice on the same machine.
 */

/** Reads the stored phrase, or '' when this browser has not joined a group. */
async function readPhrase() {
  const stored = await chrome.storage.local.get('phrase');
  return typeof stored.phrase === 'string' ? stored.phrase : '';
}

/**
 * Stores the phrase after checking that it decodes, so a typo is reported
 * instead of turning into a relay that never connects. Throws PhraseError.
 */
async function writePhrase(phrase) {
  const normalised = String(phrase).trim().toLowerCase().split(/\s+/).filter(Boolean).join(' ');
  await decodePhrase(normalised);
  await chrome.storage.local.set({ phrase: normalised });
  return normalised;
}

/** Leaves the group, dropping the phrase, the default target and the random
 *  member id, so the relay cannot link this browser to its next group. */
async function forgetGroup() {
  await chrome.storage.local.remove(['phrase', 'defaultInstance', 'selfId']);
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
 * A stable id for this browser inside the group, generated once, so a
 * reconnect is recognised as the same member.
 */
async function selfInstanceId() {
  const stored = await chrome.storage.local.get('selfId');
  if (typeof stored.selfId === 'string' && stored.selfId) return stored.selfId;
  const bytes = crypto.getRandomValues(new Uint8Array(20));
  const id = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
  await chrome.storage.local.set({ selfId: id });
  return id;
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
      selfId: await selfInstanceId(),
      // A plain label rather than anything identifying the browser.
      selfName: 'Browser',
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

/** Halt or release one instance's queue, through the relay. */
async function setQueueHalted(instanceId, halted) {
  return withGroup(async ({ call }) => {
    const res = await call(instanceId, 'POST', '/api/queue', JSON.stringify({ halted }));
    return !!res && res.status >= 200 && res.status < 300;
  });
}

/** What an instance is called in a list, falling back to the start of its id so
 *  two unnamed instances still differ. */
function instanceLabel(inst) {
  return inst.name && inst.name.trim() ? inst.name.trim() : inst.instanceId.slice(0, 8);
}
