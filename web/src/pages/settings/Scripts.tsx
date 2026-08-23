import { Suspense, lazy, useCallback, useEffect, useRef, useState } from 'react';
import { Button, Card, EmptyState, ErrorCard, Field, IconBadge, LoadingCard, NumberInput, PageHeader, SectionTitle, TextInput } from '../../components/ui';
import { NeutralSwitch } from './controls';
import { useToast } from '../../lib/toast';
import {
  FALLBACK_TRIGGERS,
  ScriptApiError,
  createScript,
  deleteScript,
  fetchScriptTriggers,
  fetchScripts,
  runScript,
  updateScript,
  type Script,
  type ScriptInput,
  type ScriptRunResult,
  type ScriptTrigger,
} from '../../lib/scripts';
import { same } from './paths';
import { useResource } from '../../lib/useResource';
import { useT, type TranslationKey } from '../../lib/i18n';
import { IconCode, IconPlay, IconPlus, IconTrash } from '../../lib/icons';

/**
 * Scripts: JD's "Event Scripter" (census family E), split across two agents
 * this wave. This page is 11B's half: the editor surface (census row
 * "Script Editor: %s1") and, via components/ScriptActions.tsx, the
 * manual-invocation button (census row "Toolbar / Main Menu / Contextmenu /
 * Traymenu Button Pressed").
 *
 * internal/script (11A) and internal/api/routes_scripts.go are both real and
 * wired end to end - see lib/scripts.ts's file doc comment. Every field on
 * Script/ScriptRunResult below is typed from the actual Go structs in
 * internal/script/script.go, not guessed.
 *
 * SCOPE, against the census row's full wish list: syntax highlighting and a
 * Show Help pane's worth of intent are here (the trigger picker doubles as
 * that - see triggerHint). Auto Format and Test Compile are not: both need an
 * opinion about JavaScript formatting/parsing this build has no vendored tool
 * for yet, and a "format" button that silently does nothing would be worse
 * than the row staying missing. Test Run is here in full - see onRun below.
 *
 * PER-SCRIPT SAVE, NOT A WHOLE-LIST PUT - unlike this file's neighbours
 * Schedule.tsx and Rules.tsx. Both of those are defensible for what they hold
 * (a handful of short, rarely-conflicting rows); a script carries real
 * authored code, or the point of the code box above is aesthetic. Two browser
 * tabs open on the same script list, one adding a row while the other edits
 * one, must not let whichever PUT lands second silently erase the other's
 * work - internal/script/store.go's own doc comment gives the identical
 * reason for scripts.json being its own file rather than living in
 * settings.Settings, one layer further out. Every row here owns its own
 * POST/PUT and its own dirty flag, so the only thing two tabs can race is two
 * edits to the SAME script, which a normal HTTP response ordering answers
 * well enough for a single self-hosted user.
 *
 * Registered in the settings rail: internal/api/routes_features.go's
 * featurePages() lists a FeaturePage{id:"scripts"} entry, and the
 * "scripting" module row points its Page field at it (Verdict:
 * VerdictShipped) rather than at Advanced.
 */

/**
 * Lazy, not a static import - the one deviation from how every sibling
 * settings page in registry.tsx is loaded, and deliberately narrow: it is
 * CodeEditor that is heavy (CodeMirror plus its language/state/view/commands
 * packages - measured, not assumed: the production build's own app.js
 * nearly doubled the moment CodeEditor's static import landed here, and
 * Vite's own build output flagged the >500kB chunk warning on it), not this
 * page. Every other settings page stays a static import; only the one
 * component actually pulling in a third-party editor is deferred, fetched
 * the first time a row is opened rather than by everyone who ever loads
 * Settings. Matches the same reasoning lib/locales/index.ts already gives
 * for the exact same technique applied to locale dictionaries ("42 languages
 * eagerly bundled would make every visitor download 41 they will never
 * read") - this is that argument applied to the one other genuinely large,
 * often-unused piece the frontend now carries.
 */
const CodeEditor = lazy(() => import('../../components/CodeEditor').then((m) => ({ default: m.CodeEditor })));

const DEFAULT_CODE_KEY: PendingKey = 'settings.scripts.codeStarter';

interface Row {
  key: string;
  saved: Script | null;
  draft: ScriptInput;
}

let keyCounter = 0;
const freshKey = () => `k${Date.now().toString(36)}${keyCounter++}`;

// Every ScriptInput field, timeoutMs included - dropping it here would make
// `same(draft, inputOf(row.saved))` compare a draft that HAS the key against
// a baseline that never does, leaving a row's dirty flag stuck true forever
// the moment anyone touches the time-limit field, saved or not.
function inputOf(s: Script): ScriptInput {
  return { name: s.name, trigger: s.trigger, enabled: s.enabled, code: s.code, timeoutMs: s.timeoutMs };
}

function toRows(list: Script[]): Row[] {
  return list
    .slice()
    .sort((a, b) => (a.name || a.id).localeCompare(b.name || b.id))
    .map((s) => ({ key: s.id, saved: s, draft: inputOf(s) }));
}

/**
 * The strings this page needs, keyed by where they are going.
 *
 * i18n for this wave lands in one later, dedicated pass across every locale
 * at once (the same one-writer-per-wave rule locales/* has followed since
 * Wave 1) - see Schedule.tsx and Connections.tsx's identical tables and
 * useCx for the precedent this mirrors. The lookup asks the real catalogue
 * first, so the day these keys land in en.ts this table stops being
 * consulted and can be deleted without touching anything else here.
 */
const PENDING = {
  'settings.scripts.title': 'Scripts',
  'settings.scripts.subtitle': 'Automate KnightLoader with your own JavaScript, run on an event or on demand.',
  'settings.scripts.listTitle': 'Your scripts',
  'settings.scripts.add': 'Add script',
  'settings.scripts.empty': 'No scripts yet',
  'settings.scripts.emptyHint':
    'A script runs your own JavaScript when something happens - a download finishes, one fails, the queue goes idle - or on demand, from Test Run here and from the “Run script” entry this wave adds to the download list’s right-click menu. Add one to get started.',
  'settings.scripts.loadFailed':
    'Scripts could not be loaded. If this build does not yet include the automation engine, this page has nothing to show yet - try again once it does.',
  'settings.scripts.name': 'Name',
  'settings.scripts.namePlaceholder': 'e.g. Notify on failure',
  'settings.scripts.unnamed': 'Untitled script {n}',
  'settings.scripts.trigger': 'Runs on',
  'settings.scripts.triggerHint':
    'What starts this script. Manual only ever runs when you ask for it - from Test Run below, or from the “Run script” entry this wave adds to the download list’s right-click menu.',
  'settings.scripts.trigger.manual': 'Manual (on demand only)',
  'settings.scripts.trigger.taskDone': 'A download finishes',
  'settings.scripts.trigger.taskFailed': 'A download fails',
  'settings.scripts.trigger.queueIdle': 'The queue goes idle',
  'settings.scripts.use': 'Enable this script',
  'settings.scripts.code': 'Code',
  'settings.scripts.codeStarter':
    '// This script runs on the trigger picked above.\n// The sandbox API it runs against is still being finished - see Settings › Help once it lands.\n',
  'settings.scripts.timeout': 'Time limit',
  'settings.scripts.timeoutHint':
    'How long this script may run before it is stopped. Between 100 ms and 30 s; 0 uses the default of 5000 ms.',
  'settings.scripts.timeoutUnit': 'ms',
  'settings.scripts.saveFailed': 'Could not save: {error}',
  'settings.scripts.remove': 'Remove',
  'settings.scripts.removeNew': 'Cancel',
  'settings.scripts.removeFailed': 'Could not remove: {error}',
  'settings.scripts.unsaved': 'Unsaved',
  'settings.scripts.run': 'Test run',
  'settings.scripts.running': 'Running…',
  'settings.scripts.runNeedsSaveHint': 'Give it a name or some code to create it, then test it here.',
  'settings.scripts.runDirtyHint': 'Your changes are still saving - test run will use them in a moment.',
  'settings.scripts.runOk': 'Ran successfully',
  'settings.scripts.runOkDuration': 'Ran successfully in {ms} ms',
  'settings.scripts.runTimedOut': 'Stopped: ran longer than its time limit',
  'settings.scripts.runFailed': 'Failed: {error}',
  'settings.scripts.output': 'Output',
} as const;

type PendingKey = keyof typeof PENDING;
type Cx = (key: PendingKey, vars?: Record<string, string | number>) => string;

function useCx(): Cx {
  const { t } = useT();
  return useCallback(
    (key: PendingKey, vars?: Record<string, string | number>) => {
      // The cast is the whole point: these keys are not in the union yet. It is
      // narrow — only keys in PENDING can be passed — and it goes with the table.
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
}

// Keyed by internal/script.Trigger's real values (script.go) - see
// lib/scripts.ts's ScriptTrigger for where "task.done" etc. come from.
const KNOWN_TRIGGER_KEY: Record<string, PendingKey> = {
  manual: 'settings.scripts.trigger.manual',
  'task.done': 'settings.scripts.trigger.taskDone',
  'task.failed': 'settings.scripts.trigger.taskFailed',
  'queue.idle': 'settings.scripts.trigger.queueIdle',
};

function triggerLabel(cx: Cx, trigger: ScriptTrigger): string {
  const key = KNOWN_TRIGGER_KEY[trigger];
  return key ? cx(key) : trigger;
}

export function Scripts() {
  const { t } = useT();
  const cx = useCx();
  const { data: loaded, failed, loading, setData: setLoaded, reload } = useResource<Script[]>(fetchScripts);
  const [rows, setRows] = useState<Row[] | null>(null);
  const [openKey, setOpenKey] = useState('');

  useEffect(() => {
    if (loaded) setRows(toRows(loaded));
  }, [loaded]);

  // Fetched once, from 11A's own trigger registry - see lib/scripts.ts's
  // fetchScriptTriggers for why this is asked for rather than hard-coded, and
  // why it never resolves to an unusable empty list.
  const [triggers, setTriggers] = useState<ScriptTrigger[]>(FALLBACK_TRIGGERS);
  useEffect(() => {
    let alive = true;
    fetchScriptTriggers().then((t) => alive && setTriggers(t));
    return () => {
      alive = false;
    };
  }, []);

  const add = () => {
    if (!rows) return;
    const row: Row = {
      key: freshKey(),
      saved: null,
      // cx(), not PENDING directly: once these keys land in en.ts a new
      // script's starter comment should read in whichever language the
      // catalogue is answering in, the same as everything else this page
      // shows - not permanently frozen at the English fallback text.
      draft: { name: '', trigger: 'manual', enabled: true, code: cx(DEFAULT_CODE_KEY) },
    };
    setRows([row, ...rows]);
    setOpenKey(row.key);
  };

  const handleSaved = useCallback((oldKey: string, script: Script) => {
    setRows((prev) =>
      prev ? prev.map((r) => (r.key === oldKey ? { key: script.id, saved: script, draft: inputOf(script) } : r)) : prev,
    );
    setOpenKey(script.id);
    // The list's own GET is not re-run: the row that just saved already holds
    // the server's answer, and reloading here would blow away every OTHER
    // row's unsaved draft that happened to be open at the same time.
    setLoaded((prev) => {
      if (!prev) return prev;
      const i = prev.findIndex((s) => s.id === script.id || s.id === oldKey);
      return i === -1 ? [...prev, script] : prev.map((s, j) => (j === i ? script : s));
    });
  }, [setLoaded]);

  const handleRemoved = useCallback((key: string, id: string | null) => {
    setRows((prev) => (prev ? prev.filter((r) => r.key !== key) : prev));
    if (openKey === key) setOpenKey('');
    if (id) setLoaded((prev) => (prev ? prev.filter((s) => s.id !== id) : prev));
  }, [openKey, setLoaded]);

  if (loading) return <LoadingCard label={t('common.loading')} />;
  if (failed || rows === null) {
    return <ErrorCard message={cx('settings.scripts.loadFailed')} retry={reload} retryLabel={t('common.retry')} />;
  }

  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={cx('settings.scripts.title')} subtitle={cx('settings.scripts.subtitle')} />

      <Card className="flex flex-col gap-4">
        <SectionTitle
          hue={0}
          right={
            <Button icon={<IconPlus width={16} height={16} />} onClick={add}>
              {cx('settings.scripts.add')}
            </Button>
          }
        >
          {cx('settings.scripts.listTitle')}
        </SectionTitle>

        {rows.length === 0 ? (
          <EmptyState icon={<IconCode width={26} height={26} />} title={cx('settings.scripts.empty')} hint={cx('settings.scripts.emptyHint')} />
        ) : (
          <ul className="flex flex-col">
            {rows.map((row, i) => (
              <ScriptRow
                key={row.key}
                row={row}
                index={i}
                last={i === rows.length - 1}
                open={openKey === row.key}
                onToggle={() => setOpenKey(openKey === row.key ? '' : row.key)}
                triggers={triggers}
                cx={cx}
                onSaved={handleSaved}
                onRemoved={handleRemoved}
              />
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}

function ScriptRow({
  row,
  index,
  last,
  open,
  onToggle,
  triggers,
  cx,
  onSaved,
  onRemoved,
}: {
  row: Row;
  index: number;
  last: boolean;
  open: boolean;
  onToggle: () => void;
  triggers: ScriptTrigger[];
  cx: Cx;
  onSaved: (oldKey: string, script: Script) => void;
  onRemoved: (key: string, id: string | null) => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [draft, setDraft] = useState<ScriptInput>(row.draft);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [removing, setRemoving] = useState(false);
  const [removeError, setRemoveError] = useState('');
  const [running, setRunning] = useState(false);
  const [runResult, setRunResult] = useState<ScriptRunResult | null>(null);
  const [runError, setRunError] = useState('');

  const dirty = row.saved === null || !same(draft, inputOf(row.saved));
  // A freshly-added row (row.saved === null) is "dirty" the instant it
  // exists, before anyone has typed a single character - the auto-save
  // effect below must not fire on that alone, or add() would create an
  // untitled, empty script the moment its row appears. touched flips true
  // only from a real field edit, so an unopened new row just sits there.
  const touched = useRef(false);
  const update = (fields: Partial<ScriptInput>) => {
    touched.current = true;
    setDraft((d) => ({ ...d, ...fields }));
    setSaveError('');
    setRemoveError('');
  };

  async function onSave() {
    if (saving) return;
    setSaving(true);
    setSaveError('');
    setRemoveError('');
    try {
      const script = row.saved ? await updateScript(row.saved.id, draft) : await createScript(draft);
      onSaved(row.key, script);
      toast(t('settings.saved'), 'ok');
    } catch (e) {
      const msg = e instanceof ScriptApiError ? e.message : String(e);
      setSaveError(msg);
      toast(cx('settings.scripts.saveFailed', { error: msg }), 'fail');
    } finally {
      setSaving(false);
    }
  }

  // Saves itself like every other settings tab (jdp: "In allen
  // Einstellungstabs soll alles was man einstellt automatisch sofort
  // gespeichert werden, ohne dass ein Speichern Button erscheint") - a
  // longer 900ms delay than the shared shell's 600ms since a keystroke
  // here can be mid-line of actual authored code, not a single field.
  const saveTimer = useRef<number | null>(null);
  useEffect(() => {
    if (!touched.current || !dirty) return;
    if (saveTimer.current !== null) window.clearTimeout(saveTimer.current);
    saveTimer.current = window.setTimeout(() => {
      saveTimer.current = null;
      void onSave();
    }, 900);
    return () => {
      if (saveTimer.current !== null) {
        window.clearTimeout(saveTimer.current);
        saveTimer.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft]);

  async function onRemove() {
    if (!row.saved) {
      onRemoved(row.key, null);
      return;
    }
    setRemoving(true);
    setRemoveError('');
    try {
      await deleteScript(row.saved.id);
      onRemoved(row.key, row.saved.id);
    } catch (e) {
      // Its own error state, not saveError - caught live: a failed delete
      // was rendering through settings.scripts.saveFailed ("Could not
      // save: …") because this used to reuse setSaveError, which is exactly
      // the wrong sentence for a Remove click. removeFailed already existed
      // in PENDING and was simply never wired to anything.
      setRemoveError(e instanceof ScriptApiError ? e.message : String(e));
      setRemoving(false);
    }
  }

  async function onRun() {
    if (!row.saved) return;
    setRunning(true);
    setRunResult(null);
    setRunError('');
    try {
      setRunResult(await runScript(row.saved.id));
    } catch (e) {
      setRunError(e instanceof ScriptApiError ? e.message : String(e));
    } finally {
      setRunning(false);
    }
  }

  const title = draft.name.trim() || cx('settings.scripts.unnamed', { n: index + 1 });
  const runDisabled = running || !row.saved || dirty;
  const runHint = !row.saved ? cx('settings.scripts.runNeedsSaveHint') : dirty ? cx('settings.scripts.runDirtyHint') : undefined;

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="group grid grid-cols-[auto_1fr_auto] items-center gap-3 py-2.5">
        <NeutralSwitch
          on={draft.enabled}
          onChange={(v) => update({ enabled: v })}
          name={cx('settings.scripts.use')}
          hue={index}
        />
        <button type="button" onClick={onToggle} aria-expanded={open} className="flex min-w-0 items-center gap-3 text-left">
          <span className="min-w-0 flex-1">
            <span className="flex items-center gap-2">
              <span className="truncate text-sm text-carbon-text">{title}</span>
              {dirty && (
                <span className="shrink-0 rounded-[var(--radius-control)] bg-statusInfoBg px-1.5 py-0.5 text-[10px] text-statusInfo">
                  {cx('settings.scripts.unsaved')}
                </span>
              )}
            </span>
            <span className="block truncate text-[11px] text-carbon-textMuted">
              {triggerLabel(cx, draft.trigger)}
              {/* No "last run" line: internal/script.Script (script.go) persists
                  no run history at all - Result only ever exists for the
                  duration of one Test Run, held below in this row's own
                  runResult state, never written back to the saved script. */}
            </span>
          </span>
        </button>
        <div className="flex items-center gap-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          <IconBadge
            kind="danger"
            icon={<IconTrash width={14} height={14} />}
            aria-label={row.saved ? cx('settings.scripts.remove') : cx('settings.scripts.removeNew')}
            disabled={removing}
            onClick={() => void onRemove()}
          />
        </div>
      </div>

      {/* Repeated below the fold too, same as Schedule.tsx's EntryRow: Remove
          is reachable from the collapsed row (the trash icon above is
          outside the `open` block), so a failed delete has to be visible
          without forcing the row open first - the one place in this row
          that can be acted on without opening it. */}
      {!open && removeError && (
        <p className="pb-2.5 ps-12 text-[11px] text-statusFail">{cx('settings.scripts.removeFailed', { error: removeError })}</p>
      )}

      {open && (
        <div className="glim-well mb-3 flex flex-col gap-4 p-4">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <Field label={cx('settings.scripts.name')}>
              <TextInput
                value={draft.name}
                placeholder={cx('settings.scripts.namePlaceholder')}
                onChange={(e) => update({ name: e.target.value })}
              />
            </Field>
            <Field label={cx('settings.scripts.trigger')} hint={cx('settings.scripts.triggerHint')}>
              <TriggerSelect value={draft.trigger} options={triggers} onChange={(t) => update({ trigger: t })} cx={cx} />
            </Field>
            <Field label={cx('settings.scripts.timeout')} hint={cx('settings.scripts.timeoutHint')}>
              <div className="flex items-center gap-2">
                <NumberInput
                  dir="ltr"
                  value={draft.timeoutMs ?? 0}
                  onValue={(n) => update({ timeoutMs: Math.max(0, n) })}
                  min={0}
                  max={30000}
                  step={100}
                />
                <span className="glim-num shrink-0 text-xs text-carbon-textMuted">{cx('settings.scripts.timeoutUnit')}</span>
              </div>
            </Field>
          </div>

          <Field label={cx('settings.scripts.code')}>
            <Suspense fallback={<div className="glim-well animate-pulse" style={{ minHeight: '220px' }} />}>
              <CodeEditor value={draft.code} onChange={(code) => update({ code })} ariaLabel={cx('settings.scripts.code')} />
            </Suspense>
          </Field>

          {saveError && <p className="text-xs text-statusFail">{cx('settings.scripts.saveFailed', { error: saveError })}</p>}
          {removeError && <p className="text-xs text-statusFail">{cx('settings.scripts.removeFailed', { error: removeError })}</p>}

          <div className="flex flex-wrap items-center gap-3">
            <span className="flex-1" />
            <Button
              kind="secondary"
              icon={<IconPlay width={14} height={14} />}
              title={runHint}
              disabled={runDisabled}
              onClick={() => void onRun()}
            >
              {running ? cx('settings.scripts.running') : cx('settings.scripts.run')}
            </Button>
          </div>
          {runHint && !running && <p className="text-end text-[11px] text-carbon-textMuted">{runHint}</p>}

          {runError && <p className="text-xs text-statusFail">{cx('settings.scripts.runFailed', { error: runError })}</p>}
          {runResult && (
            <div className="glim-card flex flex-col gap-1.5 p-3 text-xs">
              <p className={runResult.ok ? 'text-statusOk' : 'text-statusFail'}>
                {runResult.ok
                  ? cx('settings.scripts.runOkDuration', { ms: runResult.durationMs })
                  : runResult.timedOut
                    ? cx('settings.scripts.runTimedOut')
                    : cx('settings.scripts.runFailed', { error: runResult.error ?? '' })}
              </p>
              {runResult.output && runResult.output.length > 0 && (
                <>
                  <span className="text-[11px] text-carbon-textMuted">{cx('settings.scripts.output')}</span>
                  <pre dir="ltr" className="glim-well overflow-x-auto whitespace-pre-wrap p-2 text-[11px] text-carbon-textSub">
                    {runResult.output.join('\n')}
                  </pre>
                </>
              )}
            </div>
          )}
        </div>
      )}
    </li>
  );
}

function TriggerSelect({
  value,
  options,
  onChange,
  cx,
}: {
  value: ScriptTrigger;
  options: ScriptTrigger[];
  onChange: (t: ScriptTrigger) => void;
  cx: Cx;
}) {
  // A value this build's registry does not currently list is kept as an
  // option of its own rather than silently swapped for the first known one -
  // matching Schedule.tsx's ActionSelect for the identical reason: switching
  // a saved trigger on the strength of a menu that simply had nothing else to
  // offer would be exactly the kind of save-time surprise a picker exists to
  // prevent.
  const shown = options.includes(value) ? options : [value, ...options];
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="w-full rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text
        outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
    >
      {shown.map((t) => (
        <option key={t} value={t}>
          {triggerLabel(cx, t)}
        </option>
      ))}
    </select>
  );
}
