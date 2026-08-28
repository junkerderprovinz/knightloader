/**
 * The group this browser belongs to, and the one thing it stores about it.
 *
 * This replaces the old registry of hand-typed {name, url} entries. That model
 * predates the connection phrase, and kept standing in a product that had moved
 * on — the WebUI and the phone had used the phrase for weeks while the options
 * page still asked for a name and an address (jdp, 2026-08-28: "Wieso muss man
 * eine Instanz per Name & Adresse hinzufügen? Das soll doch jetzt alles
 * ausschliesslich via Phrase laufen.").
 *
 * What is stored is the PHRASE and nothing else. Not the derived keys: they are
 * cheap to recompute (two SHA-256 hashes) and storing them would mean two
 * copies of the same secret in two shapes, one of which could go stale if the
 * derivation ever changed. Not the roster either — who is online is a fact about
 * right now, and the relay tells us on every connect.
 *
 * `defaultInstance` holds a relay instance id, so a group whose members were
 * renamed keeps pointing at the same machine.
 */

/** Reads the stored phrase, or '' when this browser has not joined a group. */
async function readPhrase() {
  const stored = await chrome.storage.local.get('phrase');
  return typeof stored.phrase === 'string' ? stored.phrase : '';
}

/**
 * Stores the phrase after checking it decodes.
 *
 * Checked here rather than trusted from the caller, because this is the one
 * door: a phrase that does not decode cannot reach anything, and storing it
 * would turn a typo into "the relay never connects" with nothing to look at.
 * Throws PhraseError, which the options page turns into a translated sentence.
 */
async function writePhrase(phrase) {
  const normalised = String(phrase).trim().toLowerCase().split(/\s+/).filter(Boolean).join(' ');
  await decodePhrase(normalised); // throws PhraseError on anything unusable
  await chrome.storage.local.set({ phrase: normalised });
  return normalised;
}

/** Leaves the group: the phrase and the remembered target both go. */
async function forgetGroup() {
  await chrome.storage.local.remove(['phrase', 'defaultInstance']);
}

/** The relay instance id this browser sends to when it is not asked. */
async function readDefaultTarget() {
  const stored = await chrome.storage.local.get('defaultInstance');
  return typeof stored.defaultInstance === 'string' ? stored.defaultInstance : '';
}

/**
 * defaultOf resolves which instance in THIS group is the default, right now.
 *
 * There is always exactly one, and that is the point: nothing is stored until
 * somebody chooses, so a fresh join would otherwise show a group of cards with
 * no badge on any of them and no answer to "where does a send go". The first
 * instance stands in until a choice is made — and a stored choice that has
 * since left the group falls back the same way rather than pointing at
 * something that is not there.
 *
 * Deliberately NOT written back. Storing the fallback would turn "whichever is
 * first" into a decision somebody has to undo, and the order can change on its
 * own as instances come and go.
 */
function defaultOf(siblings, stored) {
  if (stored && siblings.some((s) => s.instanceId === stored)) return stored;
  return siblings[0]?.instanceId ?? '';
}

async function writeDefaultTarget(instanceId) {
  await chrome.storage.local.set({ defaultInstance: String(instanceId || '') });
}

/**
 * A stable id for THIS browser inside the group.
 *
 * The relay tells instances apart by this, and joining twice under the same id
 * is what makes a reconnect a reconnect rather than a second member. Generated
 * once and kept, so a browser that reconnects is recognised as the same one.
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
 * withGroup opens one relay session for the stored phrase and hands it to
 * `work`, exactly like relaySession — this only supplies the stored parts.
 *
 * Throws a plain Error with a translatable key when no phrase is stored, so
 * every caller reports the same thing in the reader's language rather than
 * three variations of "not configured".
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
      // What the group's other members see this browser called. Deliberately
      // not the browser's name or anything identifying: it is a label in a
      // list, and the list belongs to somebody who already knows it is theirs.
      selfName: 'Browser',
    },
    work,
  );
}

/** The instances in the group, right now. Clients (other browsers, the phone)
 *  are already filtered out by relaySession — they are routable, but they are
 *  not somewhere to send a download to. */
async function groupInstances() {
  return withGroup(async ({ siblings }) => siblings);
}

/**
 * groupStatus is the group plus what each instance is currently doing.
 *
 * One relay session for the lot, and every instance asked in parallel: three
 * small reads each, all of them on the list a group member may reach
 * (relayForwardable in internal/api/routes_relay.go). An instance that does
 * not answer comes back as `status: null` rather than as a missing field, so
 * the card can say "offline" instead of quietly showing nothing.
 *
 * The web address is asked for rather than announced, and that is the whole
 * point: an address in the announce frame would be readable by the RELAY,
 * which is the one thing this design keeps out of everyone's business. A
 * proxied call travels inside the encrypted frame.
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
        // Nothing answered at all: the instance is in the roster (the relay
        // has a live socket for it) but is not serving. Distinct from "it
        // answered and has nothing to do".
        if (!queue && !counters) return { ...s, status: null };
        return { ...s, status: { queue, counters, webUrl: bestWebUrl(remote) } };
      }),
    );
  });
}

/**
 * bestWebUrl picks the address most likely to work from THIS browser, out of
 * the list an instance reports.
 *
 * A loopback address is dropped outright: 127.0.0.1 on the instance is this
 * machine here, and offering it would open the wrong thing or nothing at all.
 * A remembered domain beats a bare IP, because a domain is what somebody
 * deliberately set up to reach the instance from outside.
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

/** What one instance is called in a list. Falls back to the id's first octets
 *  so an instance that never set a name is still distinguishable from another
 *  that never set one either. */
function instanceLabel(inst) {
  return inst.name && inst.name.trim() ? inst.name.trim() : inst.instanceId.slice(0, 8);
}
