// The wait-order verbs reach exactly as far as the server lets them, and not one
// state less.
//
// THE REGRESSION IT EXISTS FOR. The download list's selection row grew a
// "Reihenfolge" badge that opens the queue group, and the four page-level badges
// it replaced were dropped. The badge is drawn only when that group has entries,
// and the group was gated on one predicate reading
//
//     x.status === 'queued' || x.status === 'paused' || x.status === 'collected'
//
// so for a selection of RUNNING downloads the group came back empty: no badge,
// and the right-click menu had carried the same gate all along. The four move
// verbs and the seven priorities were reachable from nowhere at all. Measured on
// the running instance with two running downloads, and measured against the
// server in the same pass: it took `priority 0 -> 3` and renumbered positions to
// -2/-1 for exactly that selection. Nothing was a dead button; a live capability
// simply stopped being offered.
//
// The same round produced the weaker twin: a selection of finally failed tasks
// could no longer be given a priority, although the server writes one and
// RestartTasksIn leaves Priority alone, so the value takes effect the moment the
// row is restarted.
//
// THE PATTERN, and the reason this is a script rather than a note: a surface
// narrows a condition, and nothing notices, because no test builds the state in
// which the verb was still wanted. So the check is not "keep this list of
// statuses". It is: the interface's state set is READ OFF the server's, in both
// directions, and the two are compared per status.
//
// THE TWO REGRESSIONS THIS CHECK ITSELF LET THROUGH, and now does not.
//
// FIRST, it read four files and the verbs are offered from six. The "Reihenfolge"
// badge is drawn in web/src/pages/Downloads.tsx, gated on the group being
// non-empty, and the command palette offers the same four move verbs again in
// web/src/lib/commands/downloads.ts. Neither was read here. Measured on a running
// instance: with small.bin (done) and missing.bin (error) selected, the palette
// offered "Nach ganz oben  Alt+Pos1" ENABLED, pressing it sent
// POST /api/tasks/move -> 204, and GET /api/tasks was byte-identical before and
// after. A dead control, in a surface this check could not see.
//
// SECOND, it read the NAMES in the gates and not their SHAPE. The three sets can
// all be correct, no hand comparison anywhere, and the defect still on screen:
// hang the priority block back inside the move block, or write one condition
// covering both, and the narrower answer decides both questions again. Measured:
// that exact change passed this check with EXIT=0 while a selection of failed
// rows lost the badge entirely. So the entries are now read as a shape - which
// gate an entry sits in, and which sets that gate names.
//
// WHAT IT READS
//   internal/core/task.go        the seven statuses, the whole vocabulary
//   internal/app/app_queue.go    movable(), which is who may be MOVED, and
//                                SetPriorityIn's own filter, which is who may be
//                                given a PRIORITY - two different answers, which
//                                is the whole point
//   web/src/lib/api.ts           TaskStatus, so the browser's vocabulary cannot
//                                drift from the server's either
//   web/src/components/ListToolbar.tsx
//                                MOVE_STATES / PRIORITY_STATES / STOP_MARK_STATES,
//                                the sets queueMenuGroup gates its entries on,
//                                and the SHAPE of those three gates
//   web/src/pages/Downloads.tsx  the badge and the menu that draw that group, and
//                                what their visibility is gated on
//   web/src/lib/commands/downloads.ts
//                                the command palette's own four move verbs, the
//                                second entry point to the same server calls
//   internal/app/queue_reach_test.go
//                                the measurement those sets are justified by
//
// A status comparison written by hand inside one of those gates is refused even
// when it happens to be right: a literal beside the entries it gates is a second
// opinion about the server, and it is the form the regression arrived in. The
// scan runs on the source with its comments blanked out, so prose naming a
// status is prose, and the message says which gate the literal was found in -
// a check that cries wolf is a check somebody deletes.
//
// Run by hand or from CI: `node check-queue-reach.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(fileURLToPath(import.meta.url));
const read = (p) => readFileSync(join(root, p), 'utf8');

const problems = [];
const fail = (line) => problems.push(line);

// --- Reading source without reading its prose --------------------------------

/**
 * The same text with every comment blanked out and every bracket inside a string
 * literal neutralised. Length and offsets are unchanged, so a line number taken
 * from this text is a line number in the file.
 *
 * Both halves matter. Blanking the comments is what keeps this check from firing
 * on a sentence that happens to spell `status === 'done'` while explaining why
 * it must not be written - the surest way to get a check switched off is to have
 * it cry wolf. Neutralising the brackets inside strings is what lets the nesting
 * be counted at all: `t('a {thing}')` would otherwise open a block that never
 * closes. The string's TEXT survives, because a status literal is a string and
 * this check has to see it.
 */
function maskNonCode(src) {
  const out = [...src];
  const blank = (i) => {
    if (i < out.length && out[i] !== '\n') out[i] = ' ';
  };
  let state = 'code';
  let i = 0;
  while (i < src.length) {
    const c = src[i];
    const d = src[i + 1];
    if (state === 'code') {
      if (c === '/' && d === '/') {
        blank(i), blank(i + 1), (state = 'line'), (i += 2);
        continue;
      }
      if (c === '/' && d === '*') {
        blank(i), blank(i + 1), (state = 'block'), (i += 2);
        continue;
      }
      if (c === "'" || c === '"' || c === '`') state = c;
      i += 1;
      continue;
    }
    if (state === 'line') {
      if (c === '\n') state = 'code';
      else blank(i);
      i += 1;
      continue;
    }
    if (state === 'block') {
      if (c === '*' && d === '/') {
        blank(i), blank(i + 1), (state = 'code'), (i += 2);
        continue;
      }
      blank(i);
      i += 1;
      continue;
    }
    // Inside a string literal.
    if (c === '\\') {
      blank(i), blank(i + 1), (i += 2);
      continue;
    }
    if (c === state) {
      state = 'code';
      i += 1;
      continue;
    }
    if ('{}()[]'.includes(c)) blank(i);
    i += 1;
  }
  return out.join('');
}

/** Whether the masked text still balances - if it does not, nothing below it can be trusted. */
function bracketsBalance(code) {
  const opener = { ')': '(', ']': '[', '}': '{' };
  const stack = [];
  for (const c of code) {
    if ('([{'.includes(c)) stack.push(c);
    else if (c in opener && stack.pop() !== opener[c]) return false;
  }
  return stack.length === 0;
}

/** 1-based line number of an offset, for a message that can be jumped to. */
const lineAt = (code, at) => code.slice(0, at).split('\n').length;

/** The '(' that matches a ')' at `closeAt`, or -1. */
function openerOf(code, closeAt) {
  let depth = 0;
  for (let i = closeAt; i >= 0; i--) {
    if (code[i] === ')') depth += 1;
    else if (code[i] === '(') {
      depth -= 1;
      if (depth === 0) return i;
    }
  }
  return -1;
}

/**
 * Every gate an offset sits inside, outermost first, as { cond, at }.
 *
 * Two shapes, which are the two this codebase writes: `if (cond) {` in the menu
 * builder, and `{cond && (` in the page's JSX. A block opened any other way
 * carries no condition and is simply not a gate, so an entry written under a
 * ternary or a switch reads here as UNGATED and is reported as such rather than
 * guessed at.
 */
function gatesAround(code, at) {
  const stack = [];
  for (let i = 0; i < at; i += 1) {
    const c = code[i];
    if (c === '(' || c === '[' || c === '{') stack.push({ char: c, at: i, cond: conditionOpening(code, c, i, stack) });
    else if (c === ')' || c === ']' || c === '}') stack.pop();
  }
  return stack.filter((f) => f.cond !== null);
}

function conditionOpening(code, char, at, stack) {
  let j = at - 1;
  while (j >= 0 && /\s/.test(code[j])) j -= 1;
  if (char === '{') {
    // `if (cond) {`
    if (code[j] !== ')') return null;
    const open = openerOf(code, j);
    if (open < 0 || !/\bif$/.test(code.slice(0, open).trimEnd())) return null;
    return code.slice(open + 1, j).replace(/\s+/g, ' ').trim();
  }
  if (char === '(') {
    // `{cond && (`
    if (!(code[j] === '&' && code[j - 1] === '&')) return null;
    const brace = [...stack].reverse().find((f) => f.char === '{');
    if (!brace) return null;
    return code
      .slice(brace.at + 1, j - 1)
      .replace(/\s+/g, ' ')
      .trim();
  }
  return null;
}

/** The status literals compared by hand in one expression, e.g. `status === 'done'`. */
const handComparisons = (text) => [...new Set([...text.matchAll(/status\s*[!=]==\s*'([a-z]+)'/g)].map((m) => m[1]))].sort();

/**
 * The file-local functions whose body names `set`, so a gate may reach the set
 * through one of them instead of spelling it out. One level, deliberately: a
 * predicate named right there is readable, a chain of three is a place to hide
 * the same narrowing again.
 */
function helpersNaming(code, set) {
  const names = new Set();
  for (const m of code.matchAll(/function\s+([A-Za-z_$][\w$]*)\s*\(/g)) {
    const brace = code.indexOf('{', m.index + m[0].length);
    if (brace < 0) continue;
    let depth = 0;
    let end = brace;
    for (let i = brace; i < code.length; i += 1) {
      if (code[i] === '{') depth += 1;
      else if (code[i] === '}') {
        depth -= 1;
        if (depth === 0) {
          end = i;
          break;
        }
      }
    }
    if (code.slice(brace, end).includes(set)) names.add(m[1]);
  }
  return names;
}

/** Whether an expression reaches `set`, directly or through one of those helpers. */
const reaches = (text, set, helpers) =>
  text.includes(set) || [...helpers].some((h) => new RegExp(`\\b${h}\\s*\\(`).test(text));

// --- The vocabulary: every status the server has ---------------------------

const TASK_GO = 'internal/core/task.go';
const statusByConst = new Map(); // StatusRunning -> running
for (const m of read(TASK_GO).matchAll(/^\s*(Status[A-Za-z]+)\s+Status\s*=\s*"([a-z]+)"/gm)) {
  statusByConst.set(m[1], m[2]);
}
if (statusByConst.size === 0) {
  console.error(`${TASK_GO}: no Status constants found - this check cannot read the vocabulary any more.`);
  process.exit(1);
}
const ALL = [...statusByConst.values()].sort();

// The browser's own copy of that vocabulary. A status the server can send and
// the browser has never heard of is a row whose verbs are decided by a
// comparison that is false for every value.
const API_TS = 'web/src/lib/api.ts';
const apiSrc = read(API_TS);
const taskStatusDecl = apiSrc.match(/export type TaskStatus\s*=([\s\S]*?);/);
if (!taskStatusDecl) {
  fail(`${API_TS}: no \`export type TaskStatus\` to compare against the server's own list.`);
} else {
  const ui = [...taskStatusDecl[1].matchAll(/'([a-z]+)'/g)].map((m) => m[1]).sort();
  diff('the status vocabulary', `${TASK_GO} Status constants`, ALL, `${API_TS} TaskStatus`, ui);
}

// --- What the server does with each of them ---------------------------------

const QUEUE_GO = 'internal/app/app_queue.go';
const queueSrc = read(QUEUE_GO);

// movable() is the server's answer to "may this be moved". It is written as a
// pure exclusion list, and the check insists on that shape rather than trying to
// evaluate Go: a movable() that grows a condition this cannot read is a change
// somebody has to look at, not one to guess past.
const movableBody = queueSrc.match(/func movable\(t \*core\.Task\) bool \{\n\treturn ([^\n]+)\n\}/);
let serverMove = [];
if (!movableBody) {
  fail(
    `${QUEUE_GO}: movable() is not the single-return exclusion list this check can read.\n` +
      `        Read it yourself, then teach this check the new shape - do not delete the check.`,
  );
} else {
  const excluded = new Set();
  for (const part of movableBody[1].split('&&').map((s) => s.trim())) {
    const m = part.match(/^t\.Status != core\.(Status[A-Za-z]+)$/);
    if (!m || !statusByConst.has(m[1])) {
      fail(
        `${QUEUE_GO}: movable() holds a term this check cannot read: ${part}\n` +
          `        It must stay a run of \`t.Status != core.StatusX\`, or this check has to be taught the new shape.`,
      );
      continue;
    }
    excluded.add(statusByConst.get(m[1]));
  }
  serverMove = ALL.filter((s) => !excluded.has(s));
}

// SetPriorityIn's own filter. `nil` is pickLocked's "keep everything", so a nil
// here means the server writes a priority on every state there is - which is
// what it does, and what the interface therefore has to offer. Anything else is
// a narrowing the interface has to be narrowed to match.
const setPriority = queueSrc.match(/func \(a \*App\) SetPriorityIn\([\s\S]*?\n\}/);
let serverPriority = [];
if (!setPriority) {
  fail(`${QUEUE_GO}: SetPriorityIn not found.`);
} else {
  const pick = setPriority[0].match(/a\.pickLocked\(sel,\s*([A-Za-z]+)\)/);
  if (!pick) {
    fail(`${QUEUE_GO}: SetPriorityIn no longer resolves its selection through pickLocked in a way this check can read.`);
  } else if (pick[1] === 'nil') {
    serverPriority = [...ALL];
  } else {
    fail(
      `${QUEUE_GO}: SetPriorityIn now filters its selection through \`${pick[1]}\`.\n` +
        `        The seven priorities are offered for every status on the strength of that being \`nil\`.\n` +
        `        Narrow PRIORITY_STATES in web/src/components/ListToolbar.tsx to match, and re-measure\n` +
        `        internal/app/queue_reach_test.go.`,
    );
  }
}

// --- What the interface offers ----------------------------------------------

const TOOLBAR = 'web/src/components/ListToolbar.tsx';
const toolbar = read(TOOLBAR);

/** A module-level `const NAME: ... = ['a', 'b'];` read as its list of statuses. */
function declaredSet(name) {
  const m = toolbar.match(new RegExp(`\\bconst ${name}\\b[^=]*=\\s*\\[([^\\]]*)\\]`));
  if (!m) return null;
  return [...m[1].matchAll(/'([a-z]+)'/g)].map((x) => x[1]).sort();
}

const uiMove = declaredSet('MOVE_STATES');
const uiPriority = declaredSet('PRIORITY_STATES');
const uiStopMark = declaredSet('STOP_MARK_STATES');

// The body of the one function both surfaces build their queue entries with,
// with its prose blanked out: everything below reads code only.
const toolbarCode = maskNonCode(toolbar);
if (!bracketsBalance(toolbarCode)) {
  fail(`${TOOLBAR}: this check can no longer count the nesting in this file (unbalanced after masking) - teach it the new shape, do not delete it.`);
}
const group = toolbarCode.match(/export function queueMenuGroup\(\{[\s\S]*?\n\}\n/);
if (!group) {
  fail(`${TOOLBAR}: queueMenuGroup not found - the queue verbs have moved and this check is pointing at nothing.`);
}
const body = group ? group[0] : '';
const bodyAt = group ? group.index : 0;

// A status compared by hand in here is the shape the regression arrived in: a
// predicate written beside the entries it gates, agreeing with the server on the
// day it was written and answerable to nothing afterwards.
const strays = [...body.matchAll(/status\s*[!=]==\s*'([a-z]+)'/g)];
if (strays.length > 0) {
  const seen = [...new Set(strays.map((m) => m[1]))].sort();
  const where = strays.map((m) => `line ${lineAt(toolbarCode, bodyAt + m.index)}: ${m[0]}`);
  fail(
    `${TOOLBAR}: queueMenuGroup compares statuses by hand: ${seen.map((s) => `'${s}'`).join(', ')}\n` +
      `        ${where.join('\n        ')}\n` +
      `        Those literals are the interface's own opinion about the server. Gate the entries on\n` +
      `        MOVE_STATES / PRIORITY_STATES / STOP_MARK_STATES instead, which this check reads off\n` +
      `        ${QUEUE_GO}.\n` +
      `        For the record, the server's own answer right now:\n` +
      `          may be moved:      ${serverMove.join(', ') || '(unreadable)'}\n` +
      `          may be prioritised: ${serverPriority.join(', ') || '(unreadable)'}`,
  );
}

for (const [name, set] of [
  ['MOVE_STATES', uiMove],
  ['PRIORITY_STATES', uiPriority],
  ['STOP_MARK_STATES', uiStopMark],
]) {
  if (set === null) {
    fail(`${TOOLBAR}: no \`const ${name}\` - the set the queue entries are gated on has to be declared where this check can read it.`);
    continue;
  }
  const unknown = set.filter((s) => !ALL.includes(s));
  if (unknown.length > 0) fail(`${TOOLBAR}: ${name} names a status the server does not have: ${unknown.join(', ')}`);
  // Declared and not used is the failure mode that looks green: a set nothing
  // reads is a comment with brackets round it.
  const uses = body.match(new RegExp(`\\b${name}\\b`, 'g'));
  if (!uses) fail(`${TOOLBAR}: ${name} is declared but queueMenuGroup never reads it, so nothing is gated on it.`);
}

if (uiMove) diff('who may be moved', `${QUEUE_GO} movable()`, serverMove, `${TOOLBAR} MOVE_STATES`, uiMove);
if (uiPriority)
  diff('who may be given a priority', `${QUEUE_GO} SetPriorityIn`, serverPriority, `${TOOLBAR} PRIORITY_STATES`, uiPriority);

// --- The SHAPE of the three gates -------------------------------------------
//
// Three questions, three answers, three gates side by side. The regression that
// came back after the sets were already right was a shape: the priority block
// hung inside the move block, so movable() decided both questions and a
// selection the server writes priorities for all day was offered none. Written
// as one condition covering both, it reads differently and does the same thing.
//
// So each entry is checked against the gate it actually sits in: its own gate
// names its own set, and nothing it sits inside names any of the three.
const GATE_SETS = ['MOVE_STATES', 'PRIORITY_STATES', 'STOP_MARK_STATES'];
const ENTRY_SET = { move: 'MOVE_STATES', priority: 'PRIORITY_STATES', stopMark: 'STOP_MARK_STATES' };

if (body) {
  const seenEntries = new Set();
  for (const m of body.matchAll(/queueGroup\.items\.push\(\{\s*id:\s*'([A-Za-z]+)'/g)) {
    const entry = m[1];
    const line = lineAt(toolbarCode, bodyAt + m.index);
    const set = ENTRY_SET[entry];
    if (!set) {
      fail(
        `${TOOLBAR}: queueMenuGroup pushes an entry this check does not know: '${entry}' (line ${line}).\n` +
          `        Every entry in this group answers one of the server's questions and is gated on the set\n` +
          `        that question is read off. Add it to ENTRY_SET in check-queue-reach.mjs with its set, or\n` +
          `        say here why it needs none.`,
      );
      continue;
    }
    seenEntries.add(entry);
    const gates = gatesAround(body, m.index);
    const own = gates[gates.length - 1];
    const outer = gates.slice(0, -1);

    if (!own) {
      fail(
        `${TOOLBAR}: the '${entry}' entry (line ${line}) sits in no gate this check can read.\n` +
          `        It has to be \`if (<something naming ${set}>) { ... }\`, so that what the server answers\n` +
          `        decides whether the verb is offered. A ternary or a switch here is a shape this check\n` +
          `        cannot follow: write the plain gate, or teach this check the new one.`,
      );
      continue;
    }
    if (!own.cond.includes(set)) {
      fail(
        `${TOOLBAR}: the '${entry}' entry (line ${line}) is gated on a condition that never names ${set}:\n` +
          `          ${own.cond}\n` +
          `        That set is this interface's copy of the server's own answer for this verb. A gate that\n` +
          `        does not name it is answering the question from somewhere else.`,
      );
    }
    const foreign = GATE_SETS.filter((s) => s !== set && own.cond.includes(s));
    if (foreign.length > 0) {
      fail(
        `${TOOLBAR}: the '${entry}' entry (line ${line}) shares its gate with ${foreign.join(', ')}:\n` +
          `          ${own.cond}\n` +
          `        One condition, two questions - and the narrower of the two answers then decides both.\n` +
          `        That is the regression this file exists for, written as one line instead of as nesting.`,
      );
    }
    for (const g of outer) {
      const named = GATE_SETS.filter((s) => g.cond.includes(s));
      if (named.length === 0) continue;
      fail(
        `${TOOLBAR}: the '${entry}' entry (line ${line}) is NESTED inside a gate on ${named.join(', ')}:\n` +
          `          line ${lineAt(toolbarCode, bodyAt + g.at)}: ${g.cond}\n` +
          `        ${set} then only ever gets asked after ${named.join('/')} has already said yes, so the\n` +
          `        narrower answer decides this verb too. The three gates are siblings, never nested:\n` +
          `        that is exactly how the whole queue group vanished for a selection of running downloads.`,
      );
    }
  }
  for (const [entry, set] of Object.entries(ENTRY_SET)) {
    if (!seenEntries.has(entry)) {
      fail(
        `${TOOLBAR}: queueMenuGroup no longer pushes a '${entry}' entry, so nothing is offered off ${set}.\n` +
          `        Either the verb has moved somewhere this check cannot see it, or it has been dropped -\n` +
          `        both are the thing this check is for.`,
      );
    }
  }
}

// --- The page that draws the badge ------------------------------------------
//
// Half the regression sat here and this check could not see it. The four
// page-level badges were folded into one "Reihenfolge" badge whose visibility is
// a question about the GROUP - drawn when it has entries, gone when it has none -
// and a page that answers that question itself, with its own status comparison,
// is the old bug with a new address.
const PAGE = 'web/src/pages/Downloads.tsx';
const pageCode = maskNonCode(read(PAGE));
if (!bracketsBalance(pageCode)) {
  fail(`${PAGE}: this check can no longer count the nesting in this file (unbalanced after masking) - teach it the new shape, do not delete it.`);
}

// The two places the group reaches the screen. Both are matched on how they are
// WIRED (the group handed to a menu, the badge opening it), not on a label.
const PAGE_SITES = [
  ['the badge that opens the queue group', /orderMenu\.openAt\s*\(/g],
  ['the menu that draws the queue group', /groups=\{\[\s*queueGroup\s*\]\}/g],
];
for (const [what, re] of PAGE_SITES) {
  const hits = [...pageCode.matchAll(re)];
  if (hits.length === 0) {
    fail(
      `${PAGE}: ${what} is not where this check can see it any more (${re.source}).\n` +
        `        The queue verbs are drawn from somewhere else now, so this check is guarding nothing.\n` +
        `        Point it at the new wiring rather than removing it.`,
    );
    continue;
  }
  for (const hit of hits) {
    const line = lineAt(pageCode, hit.index);
    const gates = gatesAround(pageCode, hit.index);
    if (!gates.some((g) => g.cond.includes('queueGroup.items.length'))) {
      fail(
        `${PAGE}: ${what} (line ${line}) is not drawn off the group's own emptiness.\n` +
          `        ${gates.length === 0 ? '(it is drawn unconditionally)' : gates.map((g) => `line ${lineAt(pageCode, g.at)}: ${g.cond}`).join('\n        ')}\n` +
          `        \`queueGroup.items.length > 0\` is the only honest gate here: the group knows which of the\n` +
          `        three verbs the server will carry out for this selection, and the page does not.`,
      );
    }
    for (const g of gates) {
      const hand = handComparisons(g.cond);
      if (hand.length === 0) continue;
      fail(
        `${PAGE}: ${what} (line ${line}) is gated on a hand-written status comparison: ${hand.map((s) => `'${s}'`).join(', ')}\n` +
          `          line ${lineAt(pageCode, g.at)}: ${g.cond}\n` +
          `        Found in a VISIBILITY GATE around that element, not in prose and not in a counter: this\n` +
          `        page may count statuses all it likes, it may not decide from them whether the queue verbs\n` +
          `        are reachable. Ask the group: \`queueGroup.items.length > 0\`.`,
      );
    }
  }
}

// The page must not build the verbs either. One builder is what stops the badge
// and the right-click menu drifting apart, and the page building its own
// `setPriority(ids, 1)` is the defect that survived here for exactly that reason.
for (const verb of ['moveTasks', 'queueMove', 'queuePriority', 'setPriority']) {
  const call = new RegExp(`\\b${verb}\\s*\\(`, 'g');
  for (const m of [...pageCode.matchAll(call)]) {
    fail(
      `${PAGE}: calls ${verb}() directly (line ${lineAt(pageCode, m.index)}).\n` +
        `        The queue verbs have ONE builder, queueMenuGroup in ${TOOLBAR}, because a page that builds\n` +
        `        its own entries is how "raise priority" stayed a broken absolute write here long after it\n` +
        `        had been fixed in the menu.`,
    );
  }
}

// --- The second entry point: the command palette -----------------------------
//
// Same verbs, same server calls, a different file - and it was gated on nothing
// but "is something selected". Measured on a running instance with a done row and
// a failed row selected: "Nach ganz oben  Alt+Pos1" offered, enabled, pressed,
// POST /api/tasks/move -> 204, server state byte-identical. A command is read
// here by WHAT IT CALLS, so a fifth one added tomorrow is held to the same rule
// without anybody remembering to list it.
const PALETTE = 'web/src/lib/commands/downloads.ts';
const paletteCode = maskNonCode(read(PALETTE));
if (!bracketsBalance(paletteCode)) {
  fail(`${PALETTE}: this check can no longer count the nesting in this file (unbalanced after masking) - teach it the new shape, do not delete it.`);
}

if (!/import\s*\{[^}]*\bMOVE_STATES\b[^}]*\}\s*from\s*'[^']*ListToolbar'/.test(paletteCode)) {
  fail(
    `${PALETTE}: does not import MOVE_STATES from ${TOOLBAR}.\n` +
      `        The palette's move commands have to be gated on the SAME list the menu is gated on. A second\n` +
      `        copy of those statuses here is two lists that can disagree, which is the thing lib/commands/\n` +
      `        types.ts warns about in its own doc comment.`,
  );
}

const paletteHelpers = {
  MOVE_STATES: helpersNaming(paletteCode, 'MOVE_STATES'),
  PRIORITY_STATES: helpersNaming(paletteCode, 'PRIORITY_STATES'),
};

/** The top-level fields of one command record, e.g. { id, enabled, visible, run }. */
function recordFields(record) {
  const out = {};
  let depth = 0;
  let name = null;
  let start = 0;
  let i = 1;
  while (i < record.length - 1) {
    const c = record[i];
    if ('([{'.includes(c)) depth += 1;
    else if (')]}'.includes(c)) depth -= 1;
    if (depth === 0 && name === null) {
      const m = /^([A-Za-z_$][\w$]*)\s*:/.exec(record.slice(i));
      if (m) {
        name = m[1];
        i += m[0].length;
        start = i;
        continue;
      }
    }
    if (depth === 0 && name !== null && c === ',') {
      out[name] = record.slice(start, i).replace(/\s+/g, ' ').trim();
      name = null;
    }
    i += 1;
  }
  if (name !== null) out[name] = record.slice(start, record.length - 1).replace(/\s+/g, ' ').trim();
  return out;
}

/** The `{ ... }` an offset sits directly inside. */
function recordAround(code, at) {
  const gates = [];
  for (let i = 0; i < at; i += 1) {
    if (code[i] === '{') gates.push(i);
    else if (code[i] === '}') gates.pop();
  }
  const open = gates[gates.length - 1];
  if (open === undefined) return null;
  let depth = 0;
  for (let i = open; i < code.length; i += 1) {
    if (code[i] === '{') depth += 1;
    else if (code[i] === '}') {
      depth -= 1;
      if (depth === 0) return { text: code.slice(open, i + 1), at: open };
    }
  }
  return null;
}

// What a command DOES decides which set it has to be gated on. Both verbs land in
// the same app.MoveIn / SetPriorityIn the menu's own entries do.
const COMMAND_VERBS = [
  [/\b(moveTasks|queueMove)\s*\(/, 'MOVE_STATES', 'who the server will move'],
  [/\b(queuePriority|setPriority)\s*\(/, 'PRIORITY_STATES', 'who the server will give a priority'],
];

let moveCommands = 0;
for (const m of paletteCode.matchAll(/id:\s*'(downloads\.[A-Za-z]+)'/g)) {
  const record = recordAround(paletteCode, m.index);
  if (!record) continue;
  const f = recordFields(record.text);
  const run = f.run ?? '';
  const verb = COMMAND_VERBS.find(([re]) => re.test(run));
  if (!verb) continue;
  const [, set, question] = verb;
  if (set === 'MOVE_STATES') moveCommands += 1;
  const line = lineAt(paletteCode, m.index);
  for (const field of ['enabled', 'visible']) {
    const cond = f[field];
    if (cond === undefined) {
      fail(`${PALETTE}: ${m[1]} (line ${line}) has no ${field}() at all, so it is offered whatever the selection is.`);
      continue;
    }
    if (!reaches(cond, set, paletteHelpers[set])) {
      fail(
        `${PALETTE}: ${m[1]}.${field}() does not reach ${set} (line ${line}):\n` +
          `          ${cond}\n` +
          `        Its run() calls ${run.match(COMMAND_VERBS.find(([re]) => re.test(run))[0])[1]}(), so the question it has to answer is ${question}.\n` +
          `        A selection the server refuses must not be offered the verb: measured, the palette sent\n` +
          `        POST /api/tasks/move for a done+error selection and got a 204 with nothing moved.\n` +
          `        Name ${set} in the gate, or reach it through a helper in this file that names it.`,
      );
    }
    const wrong = ['MOVE_STATES', 'PRIORITY_STATES'].filter((s) => s !== set && cond.includes(s));
    if (wrong.length > 0) {
      fail(
        `${PALETTE}: ${m[1]}.${field}() is gated on ${wrong.join(', ')} (line ${line}), but its run() asks for ${set}.\n` +
          `          ${cond}\n` +
          `        The server answers "may be moved" and "may be given a priority" differently, and this is\n` +
          `        the palette's version of gating one on the other.`,
      );
    }
    const hand = handComparisons(cond);
    if (hand.length > 0) {
      fail(
        `${PALETTE}: ${m[1]}.${field}() compares statuses by hand: ${hand.map((s) => `'${s}'`).join(', ')} (line ${line}).\n` +
          `          ${cond}\n` +
          `        Found in a command's own gate, not in prose: those literals are a second opinion about\n` +
          `        the server, kept nowhere near it. Gate on ${set}.`,
      );
    }
  }
}
if (moveCommands === 0) {
  fail(
    `${PALETTE}: no command in here calls moveTasks()/queueMove() any more.\n` +
      `        Either the palette has lost the queue-move verbs, or they are being sent some other way that\n` +
      `        this check cannot see. Both need a look.`,
  );
}

// --- The measurement the sets stand on --------------------------------------

// The sets above say what the server accepts. That claim is a MEASUREMENT, and
// it lives in a Go test that builds one task per status and runs both verbs
// against it. Deleting or thinning that test would leave these two lists
// agreeing with a source file nobody has run.
const REACH_TEST = 'internal/app/queue_reach_test.go';
let reachTest = '';
try {
  reachTest = read(REACH_TEST);
} catch {
  fail(`${REACH_TEST} is missing - the sets above would then be read off source nobody has run.`);
}
if (reachTest) {
  const unmeasured = [...statusByConst.keys()].filter((c) => !new RegExp(`core\\.${c}\\b`).test(reachTest));
  if (unmeasured.length > 0) {
    fail(`${REACH_TEST} never builds a task in: ${unmeasured.join(', ')}\n` + `        Every status the server has is measured there, or the matrix has a hole in it.`);
  }
}

// --- Report -----------------------------------------------------------------

function diff(what, leftName, left, rightName, right) {
  const missing = left.filter((s) => !right.includes(s));
  const extra = right.filter((s) => !left.includes(s));
  if (missing.length === 0 && extra.length === 0) return;
  let msg = `${what}: ${leftName} and ${rightName} disagree.\n`;
  msg += `        server: ${left.join(', ') || '(none)'}\n`;
  msg += `        offered: ${right.join(', ') || '(none)'}\n`;
  if (missing.length > 0)
    msg += `        NOT OFFERED although the server takes it: ${missing.join(', ')}  <- a capability nobody can reach\n`;
  if (extra.length > 0) msg += `        OFFERED although the server refuses it: ${extra.join(', ')}  <- a dead control\n`;
  problems.push(msg.trimEnd());
}

if (problems.length > 0) {
  for (const p of problems) console.error(p);
  console.error(`\n${problems.length} disagreement(s) between the wait order's verbs and the server that carries them out.`);
  process.exit(1);
}
console.log(
  'queue reach: moved ' +
    `[${serverMove.join(' ')}], prioritised [${serverPriority.join(' ')}] - offered exactly that, measured in ${REACH_TEST}.`,
);
