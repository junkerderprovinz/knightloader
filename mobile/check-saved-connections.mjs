// The app keeps only the instances it reaches with the twelve words. A
// connection an older build saved by its address leaves the list on the first
// start, and its name is kept so the app can tell the owner once.
//
// src/api/savedConnections.ts imports nothing at run time, so node strips its
// types and these checks call the real function.
//
// Run by hand and by CI, from mobile/: `node check-saved-connections.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const { splitSaved } = await import(pathToFileURL(join(here, 'src', 'api', 'savedConnections.ts')).href);

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const relay = { kind: 'relay', id: 'r1', name: 'Bottich', relayUrl: 'wss://r', relayKey: 'k', relayFrameKey: 'ff', instanceId: 'a1b2' };
const byAddress = { id: 'd1', name: 'Home KL', baseUrl: 'https://kl.example.lan', token: 'secret' };
const markedDirect = { kind: 'direct', id: 'd2', name: 'Attic', baseUrl: 'http://192.168.1.9:8749', token: 'secret' };

expect('a list of the phrase alone stays', splitSaved([relay]), { kept: [relay], dropped: [] });
expect('connections by address go and are named', splitSaved([byAddress, relay, markedDirect]), { kept: [relay], dropped: ['Home KL', 'Attic'] });
expect('nothing saved yet', splitSaved(null), { kept: [], dropped: [] });
expect('a list that does not read', splitSaved('not a list'), { kept: [], dropped: [] });
expect('an entry that is no connection', splitSaved([relay, null, 7]), { kept: [relay], dropped: [] });

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('ok: connections saved by address leave the list, named once, and the phrase connections stay');
