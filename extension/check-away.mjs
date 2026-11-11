// An instance approved in the group that is away, such as a desktop app that
// was closed, is listed on the options page with when it was last seen, and
// once it is back on the relay it is listed once, as an instance. It reads
// Not reachable, in the web interface's words, which are GlimStone's.
//
// groupView runs for real against a stand-in relay session.
//
// Run by CI and by hand: `node extension/check-away.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const ctx = vm.createContext({});
vm.runInContext(readFileSync(join(here, 'src', 'group.js'), 'utf8'), ctx);

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const desktop = { id: 'desk', name: 'Zathallia', deployment: 'desktop', lastSeen: 1790000000 };
const nas = { instanceId: 'nas', name: 'Unraid', deployment: 'container' };

// Every instance answers its queue; the one asked for the roster sends `roster`.
async function view(siblings, roster) {
  ctx.session = {
    siblings,
    call: async (_id, _method, path) => {
      if (path === '/api/group/apps') return { status: 200, body: JSON.stringify(roster) };
      return { status: 200, body: '{}' };
    },
  };
  vm.runInContext('withGroup = (work) => work(session)', ctx);
  return ctx.groupView('');
}

const closed = await view([nas], { apps: [], away: [desktop] });
expect('a closed desktop app is listed as away', closed.away, [desktop]);

const back = await view([nas, { instanceId: 'desk', name: 'Zathallia', deployment: 'desktop' }], { apps: [], away: [desktop] });
expect('a desktop app back on the relay is not listed as away as well', back.away, []);
expect('a desktop app back on the relay is listed as an instance', back.instances.map((i) => i.instanceId), ['nas', 'desk']);

const old = await view([nas], { apps: [] });
expect('an instance from before the away list lists none', old.away, []);

const words = vm.createContext({ navigator: { language: 'en', languages: ['en'] } });
vm.runInContext(readFileSync(join(here, 'src', 'i18n.js'), 'utf8') + ';globalThis.MESSAGES = MESSAGES;', words);
for (const [lang, messages] of Object.entries(words.MESSAGES)) {
  const [web] = Object.values(await import(pathToFileURL(join(here, '..', 'web', 'src', 'lib', 'locales', `${lang}.ts`)).href));
  expect(`${lang}: an instance that is away`, messages['instance.notReachable'], web['instances.notReachable']);
  expect(`${lang}: the word for it before`, messages['instance.inactive'], undefined);
}

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('away: ok');
