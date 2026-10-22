// A saved instance that is closed, such as a desktop app, reads Inactive with
// when it was last seen as long as the group lists it as away, and only then.
// The saved list follows the group: an instance that joined later or was away
// at the join is added, and one taken out of the group goes.
//
// src/api/away.ts imports nothing at run time, so node strips its types and
// these checks call the real functions.
//
// Run by hand and by CI, from mobile/: `node check-away.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const { awayFor, awayList, followGroup, memberList } = await import(pathToFileURL(join(here, 'src', 'api', 'away.ts')).href);

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const desktop = { id: 'a1b2', name: 'Zathallia', deployment: 'desktop', address: 'http://192.168.20.86:8749', lastSeen: 1790000000 };
const relayConn = (instanceId) => ({ kind: 'relay', id: 'c1', name: 'Zathallia', relayUrl: 'wss://r', relayKey: 'k', instanceId });

expect('a closed desktop app the group lists as away', awayFor(relayConn('a1b2'), [desktop]), desktop);
expect('an instance the group does not list as away', awayFor(relayConn('ffff'), [desktop]), undefined);
expect('a connection by address, which the group knows nothing of', awayFor({ id: 'c2', name: 'nas', url: 'http://nas' }, [desktop]), undefined);
expect('an instance from before the away list', awayList(undefined), null);
expect('entries without an id or a time are dropped', awayList([desktop, { name: 'x' }, null, { id: 'b', lastSeen: '1' }]), [desktop]);
expect('an instance from before the members list', memberList(undefined), null);
expect(
  'a member the answering instance sees is read as a sibling',
  memberList([{ id: 'lan', name: 'Keller', deployment: 'server', direct: true }, { name: 'x' }]),
  [{ instanceId: 'lan', name: 'Keller', deployment: 'server' }],
);

const saved = [relayConn('nas'), { ...relayConn('a1b2'), id: 'c3' }];
const nas = { instanceId: 'nas', name: 'Unraid', deployment: 'container' };
const laptop = { instanceId: 'lap', name: '', deployment: 'desktop' };
const round = (more) => followGroup({ saved, live: [nas], away: [desktop], answered: ['nas'], settled: true, dismissed: [], ...more });

expect('a group that matches the saved list changes nothing', round({}), { add: [], drop: [] });
expect('an instance that joined later is added', round({ live: [nas, laptop] }).add, [{ instanceId: 'lap', name: 'lap' }]);
expect(
  'an instance away at the join is added once',
  round({ saved: [relayConn('nas')], live: [nas, { instanceId: 'a1b2', name: 'Zathallia', deployment: 'desktop' }] }).add,
  [{ instanceId: 'a1b2', name: 'Zathallia' }],
);
expect('an instance removed on this phone stays off', round({ live: [nas, laptop], dismissed: ['lap'] }).add, []);
expect('an instance taken out of the group goes', round({ away: [] }).drop, ['c3']);
expect('nothing goes before the relay announced everybody', round({ away: [], settled: false }).drop, []);
expect('nothing goes when no instance sent the away list', round({ away: null }).drop, []);
expect('an instance that answered stays though the relay has not announced it', round({ live: [], away: [] }).drop, ['c3']);

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('away: ok');
