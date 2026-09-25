import { Suspense, lazy, useCallback, useEffect, useRef, useState } from 'react';
import { Button, Card, EmptyState, ErrorCard, Field, IconBadge, LoadingCard, NumberInput, SectionTitle, TextInput } from '../../components/ui';
import { Dropdown } from '../../components/Dropdown';
import { NeutralSwitch } from './controls';
import { ModuleToggle } from './ModuleToggle';
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
import { fmtUnit } from '../../lib/format';
import { useT } from '../../lib/i18n';
import { useTriggerLabel } from '../../lib/triggers';
import { IconCode, IconPlay, IconPlus, IconTrash } from '../../lib/icons';

/**
 * ScriptsCard edits the event scripts run by internal/script, with syntax
 * highlighting and a test run. Each script saves through its own POST or PUT
 * rather than a whole-list PUT, so two tabs editing different scripts cannot
 * erase each other's code.
 */

/**
 * CodeEditor is loaded lazily because CodeMirror doubles the main bundle, and
 * most visitors never open a script.
 */
const CodeEditor = lazy(() => import('../../components/CodeEditor').then((m) => ({ default: m.CodeEditor })));

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

export function ScriptsCard({ hue }: { hue: number }) {
  const { t } = useT();
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
      // Translated, so the starter comment follows the reader's language.
      draft: { name: '', trigger: 'manual', enabled: true, code: t('settings.scripts.starter') },
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
    return <ErrorCard message={t('settings.scripts.loadFailed')} retry={reload} retryLabel={t('common.retry')} />;
  }

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle
        right={
          <Button icon={<IconPlus width={16} height={16} />} onClick={add}>
            {t('settings.scripts.add')}
          </Button>
        }
      >
        {t('settings.module.scripting')}
      </SectionTitle>
      <ModuleToggle id="scripting" />

      {rows.length === 0 ? (
        <EmptyState nested icon={<IconCode width={26} height={26} />} title={t('settings.scripts.empty')} hint={t('settings.scripts.emptyHint')} />
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
              onSaved={handleSaved}
              onRemoved={handleRemoved}
            />
          ))}
        </ul>
      )}
    </Card>
  );
}

function ScriptRow({
  row,
  index,
  last,
  open,
  onToggle,
  triggers,
  onSaved,
  onRemoved,
}: {
  row: Row;
  index: number;
  last: boolean;
  open: boolean;
  onToggle: () => void;
  triggers: ScriptTrigger[];
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
      toast(t('settings.scripts.saveFailed', { error: e instanceof ScriptApiError ? e.message : String(e) }), 'fail');
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
      toast(t('settings.scripts.removeFailed', { error: e instanceof ScriptApiError ? e.message : String(e) }), 'fail');
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
      toast(t('settings.scripts.runFailed', { error: e instanceof ScriptApiError ? e.message : String(e) }), 'fail');
      setRunShake((n) => n + 1);
    } finally {
      setRunning(false);
    }
  }

  const triggerLabel = useTriggerLabel();
  const title = draft.name.trim() || t('settings.scripts.unnamed', { n: index + 1 });
  const runDisabled = running || !row.saved || dirty;
  const runHint = !row.saved ? t('settings.scripts.runNeedsSaveHint') : dirty ? t('settings.scripts.runDirtyHint') : undefined;

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="grid grid-cols-[auto_1fr_auto] items-center gap-3 py-2.5">
        <NeutralSwitch
          on={draft.enabled}
          onChange={(v) => update({ enabled: v })}
          name={t('settings.scripts.use')}
          hue={index}
        />
        <button type="button" onClick={onToggle} aria-expanded={open} className="flex min-w-0 items-center gap-3 text-start">
          <span className="min-w-0 flex-1">
            <span className="flex items-center gap-2">
              <span className="truncate text-sm text-carbon-text">{title}</span>
              {dirty && (
                <span className="shrink-0 rounded-[var(--radius-control)] bg-statusInfoBg px-1.5 py-0.5 text-[11px] text-statusInfo">
                  {t('settings.scripts.unsaved')}
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
        <div className="flex items-center gap-1.5">
          <IconBadge
            key={removeShake}
            className={removeShake > 0 ? 'glim-shake' : ''}
            labelled
            icon={<IconTrash width={16} height={16} />}
            hue={index}
            title={row.saved ? t('settings.scripts.remove') : t('settings.scripts.removeNew')}
            aria-label={row.saved ? t('settings.scripts.remove') : t('settings.scripts.removeNew')}
            disabled={removing}
            onClick={() => void onRemove()}
          />
        </div>
      </div>

      {open && (
        <div className="glim-well mb-3 flex flex-col gap-4 p-4">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <Field label={t('settings.scripts.name')}>
              <TextInput
                value={draft.name}
                placeholder={t('settings.scripts.namePlaceholder')}
                onChange={(e) => update({ name: e.target.value })}
              />
            </Field>
            <Field label={t('settings.scripts.trigger')} hint={t('settings.scripts.triggerHint')}>
              <TriggerSelect
                label={t('settings.scripts.trigger')}
                value={draft.trigger}
                options={triggers}
                onChange={(t) => update({ trigger: t })}
              />
            </Field>
            <Field label={t('settings.scripts.timeout')} hint={t('settings.scripts.timeoutHint')}>
              <div className="flex items-center gap-2">
                <NumberInput
                  dir="ltr"
                  value={draft.timeoutMs ?? 0}
                  onValue={(n) => update({ timeoutMs: Math.max(0, n) })}
                  min={0}
                  max={30000}
                  step={100}
                />
                <span className="glim-num shrink-0 text-xs text-carbon-textMuted">{t('settings.scripts.timeoutUnit')}</span>
              </div>
            </Field>
          </div>

          {/* The placeholder uses the house pulse and the editor fades in, so
              both follow the motion level and reduced motion. */}
          <Field label={t('settings.scripts.code')}>
            <Suspense fallback={<div className="glim-well glim-live" style={{ minHeight: '220px' }} />}>
              <div className="glim-content-in">
                <CodeEditor value={draft.code} onChange={(code) => update({ code })} ariaLabel={t('settings.scripts.code')} />
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
              hint={running ? undefined : runHint}
              onClick={() => void onRun()}
            >
              {running ? t('settings.scripts.running') : t('settings.scripts.run')}
            </Button>
          </div>

          {/* The run's verdict stays inline, even when the news is bad; a run
              that could not start is handled in onRun. */}
          {runResult && (
            <div className="glim-well flex flex-col gap-1.5 p-3 text-xs">
              <p className={runResult.ok ? 'text-statusOk' : 'text-statusFail'}>
                {runResult.ok
                  ? t('settings.scripts.ranIn', { duration: fmtUnit(runResult.durationMs, 'ms') })
                  : runResult.timedOut
                    ? t('settings.scripts.runTimedOut')
                    : t('settings.scripts.runFailed', { error: runResult.error ?? '' })}
              </p>
              {runResult.output && runResult.output.length > 0 && (
                <>
                  <span className="text-[11px] text-carbon-textMuted">{t('settings.scripts.output')}</span>
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
  label,
}: {
  value: ScriptTrigger;
  options: ScriptTrigger[];
  onChange: (t: ScriptTrigger) => void;
  label: string;
}) {
  // Labels from lib/triggers.ts, shared with the event targets card.
  const triggerLabel = useTriggerLabel();
  // A value the registry does not list stays an option of its own rather than
  // being swapped for the first known one, as in Schedule.tsx's ActionSelect.
  const shown = options.includes(value) ? options : [value, ...options];
  return (
    <Dropdown
      label={label}
      value={value}
      onChange={onChange}
      options={shown.map((t) => ({ value: t, label: triggerLabel(t) }))}
    />
  );
}
