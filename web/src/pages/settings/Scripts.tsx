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
import { useTriggerLabel } from '../../lib/triggers';
import { IconCode, IconPlay, IconPlus, IconTrash } from '../../lib/icons';

/**
 * Scripts edits the event scripts run by internal/script, with syntax
 * highlighting and a test run. Each script saves through its own POST or PUT
 * rather than a whole-list PUT, so two tabs editing different scripts cannot
 * erase each other's code.
 */

/**
 * CodeEditor is loaded lazily because CodeMirror doubles the main bundle, and
 * most visitors never open a script.
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

// Every ScriptInput field, timeoutMs included, or the dirty check against the
// saved row would stay true once the time limit is touched.
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
 * PENDING holds the English strings until the catalogue has them; the lookup
 * asks the catalogue first.
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
      // These keys are not in the union yet; only PENDING keys can be passed.
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
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

  // From the server's trigger registry (fetchScriptTriggers), never empty.
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
      // Through cx, so the starter comment follows the catalogue's language.
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
    // No reload of the list, which would drop other rows' unsaved drafts.
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
      <PageHeader title={cx('settings.scripts.title')} />

      <Card hue={0} className="flex flex-col gap-4">
        <SectionTitle
          right={
            <Button icon={<IconPlus width={16} height={16} />} onClick={add}>
              {cx('settings.scripts.add')}
            </Button>
          }
        >
          {cx('settings.scripts.listTitle')}
        </SectionTitle>

        {rows.length === 0 ? (
          <EmptyState nested icon={<IconCode width={26} height={26} />} title={cx('settings.scripts.empty')} hint={cx('settings.scripts.emptyHint')} />
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
  const [removing, setRemoving] = useState(false);
  const [running, setRunning] = useState(false);
  const [runResult, setRunResult] = useState<ScriptRunResult | null>(null);
  // One counter per control, so a refusal shakes the button that was pressed.
  const [removeShake, setRemoveShake] = useState(0);
  const [runShake, setRunShake] = useState(0);

  const dirty = row.saved === null || !same(draft, inputOf(row.saved));
  // A new row counts as dirty at once; touched keeps the autosave from
  // creating an empty script before anything is typed.
  const touched = useRef(false);
  const update = (fields: Partial<ScriptInput>) => {
    touched.current = true;
    setDraft((d) => ({ ...d, ...fields }));
  };

  async function onSave() {
    if (saving) return;
    setSaving(true);
    try {
      const script = row.saved ? await updateScript(row.saved.id, draft) : await createScript(draft);
      onSaved(row.key, script);
      toast(t('settings.saved'), 'ok');
    } catch (e) {
      // Only a toast: the debounced save has no button to shake.
      toast(cx('settings.scripts.saveFailed', { error: e instanceof ScriptApiError ? e.message : String(e) }), 'fail');
    } finally {
      setSaving(false);
    }
  }

  // Saves itself like every settings tab, after 900ms rather than 600ms,
  // since a keystroke may be mid-line in code.
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
    try {
      await deleteScript(row.saved.id);
      onRemoved(row.key, row.saved.id);
    } catch (e) {
      // Its own sentence rather than the save one, and the trash badge shakes.
      toast(cx('settings.scripts.removeFailed', { error: e instanceof ScriptApiError ? e.message : String(e) }), 'fail');
      setRemoveShake((n) => n + 1);
      setRemoving(false);
    }
  }

  async function onRun() {
    if (!row.saved) return;
    setRunning(true);
    setRunResult(null);
    try {
      setRunResult(await runScript(row.saved.id));
    } catch (e) {
      // The run could not start. A verdict about the script is runResult below.
      toast(cx('settings.scripts.runFailed', { error: e instanceof ScriptApiError ? e.message : String(e) }), 'fail');
      setRunShake((n) => n + 1);
    } finally {
      setRunning(false);
    }
  }

  const triggerLabel = useTriggerLabel();
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
                <span className="shrink-0 rounded-[var(--radius-control)] bg-statusInfoBg px-1.5 py-0.5 text-[11px] text-statusInfo">
                  {cx('settings.scripts.unsaved')}
                </span>
              )}
            </span>
            <span className="block truncate text-[11px] text-carbon-textMuted">
              {triggerLabel(draft.trigger)}
              {/* No "last run" line: internal/script keeps no run history. */}
            </span>
          </span>
        </button>
        {/* `labelled`, so the actions follow the Beschriftung setting; the
            name truncates instead. */}
        <div className="flex items-center gap-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          <IconBadge
            key={removeShake}
            className={removeShake > 0 ? 'glim-shake' : ''}
            labelled
            icon={<IconTrash width={16} height={16} />}
            hue={index}
            title={row.saved ? cx('settings.scripts.remove') : cx('settings.scripts.removeNew')}
            aria-label={row.saved ? cx('settings.scripts.remove') : cx('settings.scripts.removeNew')}
            disabled={removing}
            onClick={() => void onRemove()}
          />
        </div>
      </div>

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
              <TriggerSelect value={draft.trigger} options={triggers} onChange={(t) => update({ trigger: t })} />
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

          {/* The placeholder uses the house pulse and the editor fades in, so
              both follow the motion level and reduced motion. */}
          <Field label={cx('settings.scripts.code')}>
            <Suspense fallback={<div className="glim-well glim-live" style={{ minHeight: '220px' }} />}>
              <div className="glim-content-in">
                <CodeEditor value={draft.code} onChange={(code) => update({ code })} ariaLabel={cx('settings.scripts.code')} />
              </div>
            </Suspense>
          </Field>

          <div className="flex flex-wrap items-center gap-3">
            <span className="flex-1" />
            <Button
              key={runShake}
              className={runShake > 0 ? 'glim-shake' : ''}
              kind="secondary"
              icon={<IconPlay width={16} height={16} />}
              disabled={runDisabled}
              onClick={() => void onRun()}
            >
              {running ? cx('settings.scripts.running') : cx('settings.scripts.run')}
            </Button>
          </div>
          {runHint && !running && <p className="text-end text-[11px] text-carbon-textMuted">{runHint}</p>}

          {/* The run's verdict stays inline, even when the news is bad; a run
              that could not start is handled in onRun. */}
          {runResult && (
            <div className="glim-well flex flex-col gap-1.5 p-3 text-xs">
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
}: {
  value: ScriptTrigger;
  options: ScriptTrigger[];
  onChange: (t: ScriptTrigger) => void;
}) {
  // Labels from lib/triggers.ts, shared with the event targets page.
  const triggerLabel = useTriggerLabel();
  // A value the registry does not list stays an option of its own rather than
  // being swapped for the first known one, as in Schedule.tsx's ActionSelect.
  const shown = options.includes(value) ? options : [value, ...options];
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="glim-select appearance-none pe-6 w-full rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text
        outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
    >
      {shown.map((t) => (
        <option key={t} value={t}>
          {triggerLabel(t)}
        </option>
      ))}
    </select>
  );
}
