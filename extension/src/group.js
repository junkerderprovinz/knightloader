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

/** What one instance is called in a list. Falls back to the id's first octets
 *  so an instance that never set a name is still distinguishable from another
 *  that never set one either. */
function instanceLabel(inst) {
  return inst.name && inst.name.trim() ? inst.name.trim() : inst.instanceId.slice(0, 8);
}
