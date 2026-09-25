// What the settings autosave sends and what it keeps of the server's answer.
//
// Every settings page saves itself a moment after the last keystroke, and the
// answer comes back while the person may still be typing. Taking the answer
// wholesale would put the server's value into the box being typed in: a
// half-typed "C:" would come back empty, and "D:\My " trimmed just before the
// next word. A secret comes back masked, so its box would empty and the next
// keystrokes would be stored as the whole secret. The same goes for a box
// inside a list or an object, such as a category's name or the idle command.
// A refused value is also not sent again until it is edited, or one
// unusable folder would be refused on every save. The same holds for a save
// refused without naming a field, such as a reconnect method without an IP
// check URL: sent again by every autosave, it would fail every 600 ms with a
// toast each time.
//
// It drives the real functions: Node strips the types out of
// src/pages/settings/paths.ts, which is why that file carries no import.
//
// Run: `node web/check-settings-autosave.mjs`.

import {
  afterRound,
  foldAnswer,
  heldAfter,
  noAnswer,
  pendingFields,
  same,
  saveRound,
  settle,
  shownAt,
  toSend,
  typedIn,
} from './src/pages/settings/paths.ts';

const problems = [];

function check(what, got, want) {
  const a = JSON.stringify(got);
  const b = JSON.stringify(want);
  if (a !== b) problems.push(`${what}\n      got  ${a}\n      want ${b}`);
}

const stored = { watchDir: '/watch', maxConcurrent: 3, categories: [] };

check(
  'a changed field is sent',
  pendingFields({ ...stored, watchDir: 'C:' }, stored, {}),
  { watchDir: 'C:' },
);
check(
  'a refused value is not sent again',
  pendingFields({ ...stored, watchDir: 'C:' }, stored, { watchDir: 'C:' }),
  {},
);
check(
  'the field goes out again once it is edited',
  pendingFields({ ...stored, watchDir: 'C:\\' }, stored, { watchDir: 'C:' }),
  { watchDir: 'C:\\' },
);

check(
  'what was typed while the save was out stays in the box',
  foldAnswer({ ...stored, watchDir: 'C:\\Us' }, { ...stored, watchDir: '' }, { watchDir: 'C:' }, stored, []).watchDir,
  'C:\\Us',
);
check(
  'a value tidied while its box has focus stays as typed',
  foldAnswer({ ...stored, watchDir: 'D:\\My ' }, { ...stored, watchDir: 'D:\\My' }, { watchDir: 'D:\\My ' }, stored, [
    'watchDir',
  ]).watchDir,
  'D:\\My ',
);
check(
  'a value tidied in a box without focus is shown as stored',
  foldAnswer({ ...stored, watchDir: 'D:\\My ' }, { ...stored, watchDir: 'D:\\My' }, { watchDir: 'D:\\My ' }, stored, [])
    .watchDir,
  'D:\\My',
);
check(
  'an edit to a field the save did not send survives',
  foldAnswer({ ...stored, maxConcurrent: 4 }, stored, {}, stored, []).maxConcurrent,
  4,
);
check(
  'what the server adds to a sent value is taken, such as a new row id',
  foldAnswer(
    { ...stored, categories: [{ name: 'Serien' }] },
    { ...stored, categories: [{ name: 'Serien', id: 'serien' }] },
    { categories: [{ name: 'Serien' }] },
    stored,
    [],
  ).categories,
  [{ name: 'Serien', id: 'serien' }],
);

/** refusal is what lib/api.ts throws for a refused save. */
function refusal(message, field) {
  const e = new Error(message);
  if (field) e.field = field;
  return e;
}

/**
 * autosave plays the settings page for as many saves as it would start on its
 * own, the way pages/Settings.tsx does: what toSend offers goes out through
 * saveRound, and the answer decides what the next save offers. It stops once
 * nothing is left to send, or after ten saves, which is a loop.
 */
async function autosave(draft, saved, server, answer = noAnswer) {
  const patches = [];
  for (let i = 0; i < 10; i++) {
    answer = settle(answer, draft);
    const fields = toSend(draft, saved, {}, answer);
    if (Object.keys(fields).length === 0) break;
    const round = await saveRound(fields, async (patch) => {
      patches.push({ ...patch });
      return server(patch, saved);
    });
    answer = afterRound(answer, round);
    if (round.applied) {
      draft = foldAnswer(draft, round.applied, round.sent, saved, []);
      saved = round.applied;
    }
  }
  return { patches, answer, saved };
}

// The server refuses a reconnect method without a check URL and names no field.
const refusesReconnect = (patch, saved) => {
  if (patch.reconnect && !patch.reconnect.checkUrl) throw refusal('reconnect.noCheckURL');
  return { ...saved, ...patch };
};
const withMethod = { ...stored, reconnect: { method: 'command', command: '/bin/true' } };

{
  const run = await autosave(withMethod, stored, refusesReconnect);
  check('a save refused without a field goes out once', run.patches.length, 1);

  const again = await autosave(withMethod, stored, refusesReconnect, run.answer);
  check('the refused draft is not sent again while nothing changes', again.patches.length, 0);

  const edited = { ...withMethod, reconnect: { ...withMethod.reconnect, checkUrl: 'https://api.ipify.org' } };
  const fixed = await autosave(edited, stored, refusesReconnect, run.answer);
  check('an edit sends it again', fixed.patches.length, 1);
  check('and the fixed value is stored', fixed.saved.reconnect?.checkUrl, 'https://api.ipify.org');

  const elsewhere = { ...withMethod, maxConcurrent: 5 };
  const other = await autosave(elsewhere, stored, refusesReconnect, run.answer);
  check('an edit to another field sends the refused one with it, once', other.patches, [
    { maxConcurrent: 5, reconnect: withMethod.reconnect },
  ]);
}

{
  // The server names the field, so the rest of the edit is saved without it.
  const naming = (patch, saved) => {
    if (patch.reconnect && !patch.reconnect.checkUrl) throw refusal('reconnect.noCheckURL', 'reconnect.checkUrl');
    return { ...saved, ...patch };
  };
  const draft = { ...withMethod, maxConcurrent: 5 };
  const run = await autosave(draft, stored, naming);
  check('a refusal naming its field takes it out and saves the rest', run.patches, [
    { maxConcurrent: 5, reconnect: withMethod.reconnect },
    { maxConcurrent: 5 },
  ]);
  check('the rest is stored', run.saved.maxConcurrent, 5);
  check('the refusal keeps the place it named', run.answer.refused.reconnect?.field, 'reconnect.checkUrl');
}

{
  // A folder refused by name, then the rest failing as a whole: both are kept,
  // so neither is sent again on its own.
  const server = (patch) => {
    if ('watchDir' in patch) throw refusal('pathProblem.notAbsolute', 'watchDir');
    throw new TypeError('Failed to fetch');
  };
  const draft = { ...stored, watchDir: 'C:', maxConcurrent: 5 };
  const run = await autosave(draft, stored, server);
  check('a failure after a refusal ends the saves', run.patches.length, 2);
  check('the refusal from before the failure is kept', Object.keys(run.answer.refused), ['watchDir']);
  check('the failure holds what it left unsent', run.answer.failed, { maxConcurrent: 5 });
}

{
  const answer = {
    refused: { watchDir: { value: 'C:', field: 'watchDir', error: null } },
    failed: { maxConcurrent: 5 },
  };
  const moved = settle(answer, { ...stored, watchDir: 'C:\\', maxConcurrent: 5 });
  check('a refusal ends once its field holds something else', Object.keys(moved.refused), []);
  check('a failure stays while its fields hold what failed', moved.failed, { maxConcurrent: 5 });
  check('a failure ends once one of its fields changes', settle(answer, { ...stored, watchDir: 'C:', maxConcurrent: 6 }).failed, null);
}

/**
 * typing plays one box with focus the way pages/Settings.tsx handles it: each
 * pause sends what is pending, and the answer is held back from a field whose
 * box shows text the server changed, until the box is left.
 */
function typing(stored, server) {
  const s = { draft: stored, saved: stored, held: {}, answer: noAnswer, sent: [] };
  return {
    state: s,
    type(fields) {
      s.draft = { ...s.draft, ...fields };
    },
    async pause(boxText) {
      const fields = toSend(s.draft, s.saved, s.held, s.answer);
      if (Object.keys(fields).length === 0) return;
      s.sent.push(fields);
      const round = await saveRound(fields, async (patch) => server(patch, s.saved));
      s.answer = afterRound(s.answer, round);
      if (!round.applied) return;
      const keep = Object.keys(round.sent).filter((k) => typedIn(round.sent[k], round.applied[k], boxText));
      s.held = heldAfter(s.held, round.sent, keep);
      s.draft = foldAnswer(s.draft, round.applied, round.sent, s.saved, keep);
      s.saved = round.applied;
    },
    leave() {
      for (const [k, v] of Object.entries(s.held)) if (same(s.draft[k], v)) s.draft = { ...s.draft, [k]: s.saved[k] };
      s.held = {};
    },
  };
}

// The server trims folders, as settings.Sanitize does.
const trims = (patch, saved) => {
  const out = { ...saved, ...patch };
  if (typeof out.downloadDir === 'string') out.downloadDir = out.downloadDir.trim();
  return out;
};

{
  const box = typing({ ...stored, downloadDir: 'D:\\Downloads' }, trims);
  box.type({ downloadDir: 'D:\\My ' });
  await box.pause('D:\\My ');
  box.type({ downloadDir: 'D:\\My F' });
  await box.pause('D:\\My F');
  box.type({ downloadDir: 'D:\\My ' });
  await box.pause('D:\\My ');
  check('deleting back to text that was held once is saved', box.state.sent.at(-1), { downloadDir: 'D:\\My ' });
  box.leave();
  check('leaving the box shows the stored folder', box.state.draft.downloadDir, 'D:\\My');
  check('and the server holds it', box.state.saved.downloadDir, 'D:\\My');
}

{
  // The answer masks the idle command, as Settings.Redacted does, and a
  // masked value sent back leaves the stored one in place.
  const MASK = '********';
  let program = '';
  const masks = (patch, saved) => {
    const sent = patch.idleAction?.command?.program;
    if (sent !== undefined && sent !== MASK) program = sent;
    const out = { ...saved, ...patch };
    if ('idleAction' in patch) out.idleAction = { ...patch.idleAction, command: { ...patch.idleAction.command, program: MASK } };
    return out;
  };
  const idle = (p) => ({ idleAction: { action: 'command', command: { program: p, args: [] } } });
  const box = typing({ ...stored, ...idle('') }, masks);
  // The box shows a masked program as empty, as IdleAction.tsx does.
  const shown = () => {
    const p = box.state.draft.idleAction.command.program;
    return p === MASK ? '' : p;
  };
  box.type(idle('/usr/bin/'));
  await box.pause(shown());
  check('a masked answer leaves the typed program in its box', shown(), '/usr/bin/');
  box.type(idle(shown() + 'systemctl'));
  await box.pause(shown());
  check('the whole program is stored, not the part typed after the pause', program, '/usr/bin/systemctl');
}

{
  const trimsNames = (patch, saved) => ({
    ...saved,
    ...patch,
    categories: (patch.categories ?? saved.categories).map((c) => ({ ...c, name: c.name.trim(), id: 'my' })),
  });
  const box = typing(stored, trimsNames);
  const shown = () => box.state.draft.categories[0].name;
  box.type({ categories: [{ name: 'My ' }] });
  await box.pause(shown());
  check('a name trimmed inside a list keeps its space while typed', shown(), 'My ');
  box.type({ categories: [{ name: shown() + 'Cat' }] });
  await box.pause(shown());
  check('so the next word is not glued on', box.state.saved.categories[0].name, 'My Cat');
}

check(
  'arguments edited one per line are recognised as the box being typed in',
  typedIn(['-c', 'echo '], ['********'], '-c\necho '),
  true,
);
check(
  'an empty box does not hold a value whose only change is elsewhere',
  typedIn({ checkUrl: '', username: 'me ' }, { checkUrl: '', username: 'me' }, ''),
  false,
);

check('a list row shows a refusal of one of its rows', shownAt('connections.1', 'connections', true), true);
check('a field shows only its own refusal', shownAt('connections.1', 'connections', false), false);
check('a refusal is shown at the field it names', shownAt('reconnect.checkUrl', 'reconnect.checkUrl', false), true);
check('a longer key is not under a shorter one', shownAt('reconnect.checkUrls', 'reconnect.checkUrl', true), false);

if (problems.length > 0) {
  console.error(`settings autosave: ${problems.length} problem(s)\n`);
  for (const p of problems) console.error(`  - ${p}\n`);
  process.exit(1);
}
console.log('settings autosave: ok');
