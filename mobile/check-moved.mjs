// The phone follows its group into a fortress: an instance of the group hands
// it the keys of the pairing there, which take the place of the saved ones on
// every connection into that group, the key of the local network included.
//
// src/api/moved.ts imports nothing at run time, so node strips its types and
// these checks call the real functions.
//
// Run by hand and by CI, from mobile/: `node check-moved.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const { MOVED_PATH, parseMoved, followMove, rememberMove, currentConnection, askDue } = await import(
  pathToFileURL(join(here, 'src', 'api', 'moved.ts')).href
);

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

expect('the route the instance answers on', MOVED_PATH, '/api/group/peer/moved');

const moved = { groupKey: '1a'.repeat(32), frameKey: '2b'.repeat(32), peerAuthKey: '3c'.repeat(32) };
const keys = { relayKey: moved.groupKey, relayFrameKey: moved.frameKey, peerAuthKey: moved.peerAuthKey };
expect('the keys an instance hands over', parseMoved(JSON.stringify(moved)), keys);
expect('a frame key too short', parseMoved(JSON.stringify({ ...moved, frameKey: '2b'.repeat(16) })), null);
expect('a group key that is no hex', parseMoved(JSON.stringify({ ...moved, groupKey: 'not hex' })), null);
expect('no peer-auth key', parseMoved(JSON.stringify({ groupKey: moved.groupKey, frameKey: moved.frameKey })), null);
expect('the refusal before a move', parseMoved('{"code":"notMoved"}'), null);
expect('an answer that is no JSON', parseMoved('moved'), null);
expect('an answer of null', parseMoved('null'), null);

const old = { relayUrl: 'wss://relay', relayKey: 'aa'.repeat(32), relayFrameKey: 'bb'.repeat(32), peerAuthKey: 'cc'.repeat(32) };
const bottich = { kind: 'relay', id: 'r1', name: 'Bottich', ...old, instanceId: 'i1' };
const attic = { kind: 'relay', id: 'r2', name: 'Attic', ...old, peerAuthKey: undefined, instanceId: 'i2' };
const other = { kind: 'relay', id: 'r3', name: 'Elsewhere', ...old, relayKey: 'dd'.repeat(32), instanceId: 'i3' };
expect('every connection of the group follows, and no other', followMove([bottich, attic, other], bottich, keys), [
  { ...bottich, ...keys },
  { ...attic, ...keys },
  other,
]);
expect('the same keys again move nothing', followMove([{ ...bottich, ...keys }], { ...bottich, ...keys }, keys), null);

// A screen that read its connection before the move reaches the group on the
// keys it moved with, through one move and the next.
expect('a connection nobody moved', currentConnection(other), other);
rememberMove(bottich, keys);
expect('a connection read before the move', currentConnection(attic), { ...attic, ...keys });
const again = { relayKey: '4d'.repeat(32), relayFrameKey: '5e'.repeat(32), peerAuthKey: '6f'.repeat(32) };
rememberMove({ ...bottich, ...keys }, again);
expect('a connection read before two moves', currentConnection(bottich), { ...bottich, ...again });
expect('a connection read between them', currentConnection({ ...attic, ...keys }), { ...attic, ...again });

const t0 = 1_000_000;
expect('the first call of a group asks', askDue(other, t0), true);
expect('a second call soon after does not', askDue({ ...other, id: 'r4', instanceId: 'i4' }, t0 + 1000), false);
expect('a call of another group asks', askDue({ ...other, relayKey: 'ee'.repeat(32) }, t0 + 1000), true);
expect('a call five minutes on asks again', askDue(other, t0 + 5 * 60 * 1000), true);

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('ok: the phone takes the keys of a group that moved into a fortress for every connection into it, and only well formed ones');
