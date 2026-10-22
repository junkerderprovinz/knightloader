// A saved instance that is closed, such as a desktop app, reads Inactive with
// when it was last seen as long as the group lists it as away, and only then.
//
// src/api/away.ts imports nothing at run time, so node strips its types and
// these checks call the real functions.
//
// Run by hand and by CI, from mobile/: `node check-away.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const { awayFor, awayList } = await import(pathToFileURL(join(here, 'src', 'api', 'away.ts')).href);

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
expect('an instance from before the away list', awayList(undefined), []);
expect('entries without an id or a time are dropped', awayList([desktop, { name: 'x' }, null, { id: 'b', lastSeen: '1' }]), [desktop]);

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('away: ok');
