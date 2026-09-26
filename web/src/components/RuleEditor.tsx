import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useT, type TranslationKey } from '../lib/i18n';
// A Categories-page drawer, aliased because this file's own Category is the
// file-type shorthand a condition offers.
import type { Category as Drawer } from '../lib/api';
import { en } from '../lib/locales/en';
import { IconFolder, IconPlus, IconTrash } from '../lib/icons';
import { Button, FIELD_BOX, IconBadge, InfoBubble, TextInput } from './ui';
import { Dropdown } from './Dropdown';
import { FolderPicker } from './FolderPicker';
import { Tabs } from './Tabs';

// The rule editor, shared by the Packagizer and the link filter, which are one
// engine; the flavour only decides which actions are offered. There is no
// post-extraction section as in JDownloader, because extraction here has no
// later step to hang Move and Rename on. The form is built from
// GET /api/rules/grammar, so fields and operators exist in one place only.

// Wire types mirroring internal/rules.

export type Flavour = 'packagizer' | 'filter';

export interface Condition {
  field: string;
  op: string;
  value?: string;
  min?: number;
  max?: number;
}

export interface RuleAction {
  packageName?: string;
  downloadDir?: string;
  /** Where the unpacked files move once unpacking has finished, for these links only. */
  extractDir?: string;
  filename?: string;
  comment?: string;
  priority?: number;
  autoExtract?: boolean;
  chunks?: number;
  /**
   * The Categories-page drawer to file the link into, by id, so later edits to
   * the drawer still apply. Absent means no opinion, not "clear it".
   */
  category?: string;
  reject?: boolean;
  reason?: string;
}

export interface Rule {
  name?: string;
  disabled?: boolean;
  conditions?: Condition[];
  action: RuleAction;
}

export interface RuleSet {
  rules?: Rule[];
  disabled?: boolean;
  stopAfterMatch?: boolean;
}

export interface Problem {
  index: number;
  rule: string;
  message: string;
  /** Which condition, counting from 1; absent when it is about the action. */
  condition?: number;
}

export interface FieldGrammar {
  id: string;
  ops: string[];
  numeric?: boolean;
  groups?: boolean;
}

export interface OpGrammar {
  id: string;
  value?: boolean;
  range?: boolean;
  regex?: boolean;
}

export interface ActionGrammar {
  id: keyof RuleAction;
  // 'category' is a pick, not a template: a template would let the link choose
  // its drawer and escape settings.ValidateCategories. The server refuses
  // "<jd:" in that field.
  kind: 'template' | 'int' | 'bool' | 'category' | 'reject';
  flavour?: string;
  min?: number;
  max?: number;
}

export interface Variable {
  tag: string;
  id: string;
  params?: string[];
}

export interface Category {
  id: string;
  extensions: string[];
  pattern: string;
}

export interface Grammar {
  fields: FieldGrammar[];
  operators: OpGrammar[];
  actions: ActionGrammar[];
  variables: Variable[];
  // File-type shorthands for conditions, not Categories-page drawers.
  categories: Category[];
  limits: { priorityMin: number; priorityMax: number; maxChunks: number; maxPattern: number };
}

/** What a rule dry-runs to, mirroring rules.Report. */
export interface RuleReport {
  index: number;
  name: string;
  disabled?: boolean;
  problems?: Problem[];
  matched: number;
}

export interface LinkReport {
  url: string;
  filename: string;
  matched: number[];
  effect: {
    package?: string;
    dir?: string;
    extractDir?: string;
    filename?: string;
    comment?: string;
    priority?: number;
    autoExtract?: boolean;
    chunks?: number;
    matched?: string[];
  };
  verdict: { rejected: boolean; rule?: string; reason?: string; code?: string; params?: Record<string, string> };
  result: { package: string; filename: string };
}

export interface Report {
  problems: Problem[];
  rules: RuleReport[];
  links: LinkReport[];
  disabled?: boolean;
}

type Translate = ReturnType<typeof useT>['t'];

// named falls back to the raw id for a grammar entry the catalogue does not
// know, since the grammar comes from the server.
function named(t: Translate, prefix: string, id: string): string {
  const key = (prefix + id) as TranslationKey;
  return key in en ? t(key) : id;
}

export const fieldLabel = (t: Translate, id: string) => named(t, 'settings.rules.field.', id);
export const opLabel = (t: Translate, id: string) => named(t, 'settings.rules.op.', id);
export const actionLabel = (t: Translate, id: string) => named(t, 'settings.rules.action.', id);
export const categoryLabel = (t: Translate, id: string) => named(t, 'settings.rules.category.', id);
export const variableLabel = (t: Translate, id: string) => named(t, 'settings.rules.var.', id);

// The engine takes plain bytes, so sizes like "700 MB" are parsed here.
const SIZE_UNITS: Record<string, number> = {
  '': 1,
  b: 1,
  k: 1024,
  kb: 1024,
  kib: 1024,
  m: 1024 ** 2,
  mb: 1024 ** 2,
  mib: 1024 ** 2,
  g: 1024 ** 3,
  gb: 1024 ** 3,
  gib: 1024 ** 3,
  t: 1024 ** 4,
  tb: 1024 ** 4,
  tib: 1024 ** 4,
};

/**
 * parseSize reads "700 MB", "1.5GiB", "1,5 gb" or a bare byte count, all
 * 1024-based like fmtBytes. It returns null for anything else, since reading
 * garbage as 0 would make "at least 700 MB" match every file.
 */
export function parseSize(text: string): number | null {
  const s = text.trim().toLowerCase().replace(',', '.');
  if (s === '') return 0;
  const m = /^([0-9]+(?:\.[0-9]+)?)\s*([a-z]*)$/.exec(s);
  if (!m) return null;
  const factor = SIZE_UNITS[m[2]];
  if (factor === undefined) return null;
  return Math.round(Number(m[1]) * factor);
}

/** formatSize renders a stored byte count for the size box. */
export function formatSize(n: number | undefined): string {
  if (!n) return '';
  const units: [string, number][] = [
    ['GiB', 1024 ** 3],
    ['MiB', 1024 ** 2],
    ['KiB', 1024],
  ];
  for (const [label, factor] of units) {
    if (n >= factor && n % factor === 0) return `${n / factor} ${label}`;
  }
  return String(n);
}

const MARGIN = 8;

function VariablesMenu({
  t,
  variables,
  at,
  onPick,
  onClose,
}: {
  t: Translate;
  variables: Variable[];
  at: { x: number; y: number };
  onPick: (tag: string) => void;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ top: at.y, left: at.x });

  // Clamped after mount, since the tall menu can open near the bottom of a page.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    setPos({
      top: Math.max(MARGIN, Math.min(at.y, window.innerHeight - r.height - MARGIN)),
      left: Math.max(MARGIN, Math.min(at.x, window.innerWidth - r.width - MARGIN)),
    });
  }, [at.x, at.y]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) onClose();
    };
    window.addEventListener('keydown', onKey);
    window.addEventListener('mousedown', onDown, true);
    window.addEventListener('scroll', onClose, true);
    window.addEventListener('resize', onClose);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('mousedown', onDown, true);
      window.removeEventListener('scroll', onClose, true);
      window.removeEventListener('resize', onClose);
    };
  }, [onClose]);

  return createPortal(
    <div
      ref={ref}
      role="menu"
      aria-label={t('settings.rules.variablesTitle')}
      style={{ top: pos.top, left: pos.left }}
      className="glim-card glim-fade fixed z-50 max-h-[70vh] w-[26rem] max-w-[calc(100vw-1rem)] overflow-y-auto p-1.5"
    >
      <div className="flex items-center px-2 py-1.5 text-[11px] font-semibold text-carbon-textSub">
        {t('settings.rules.variablesTitle')}
        {/* How <jd:source:N> differs from JDownloader's, where a JD template gets pasted. */}
        <InfoBubble
          tip={
            <span className="flex flex-col gap-1.5">
              <span>{t('settings.rules.variablesHint')}</span>
              <span>{t('settings.rules.sourceDivergence')}</span>
            </span>
          }
          label={t('settings.rules.variablesHint')}
        />
      </div>
      {variables.map((v) => (
        <button
          key={v.tag}
          role="menuitem"
          onClick={() => onPick(v.tag)}
          className="flex w-full flex-col gap-0.5 rounded-[var(--radius-control)] px-2 py-1.5 text-start
            transition-colors hover:bg-carbon-hover"
        >
          <span className="glim-num text-[12px] text-carbon-text" dir="ltr">
            {v.tag}
          </span>
          <span className="text-[11px] leading-snug text-carbon-textMuted">
            {variableLabel(t, v.id)}
            {v.params?.length ? ` · ${t('settings.rules.varParams', { params: v.params.join(', ') })}` : ''}
          </span>
        </button>
      ))}
    </div>,
    document.body,
  );
}

/** A text box whose content is a template, with the variables menu attached. */
function TemplateInput({
  t,
  variables,
  value,
  onChange,
  placeholder,
  label,
  folder = false,
}: {
  t: Translate;
  variables: Variable[];
  value: string;
  onChange: (next: string) => void;
  placeholder?: string;
  /** The accessible name. There is no <label>, which would send a click on the
   *  variables button to the input. */
  label: string;
  /** The template names a folder, so the box gets the folder chooser too. */
  folder?: boolean;
}) {
  const input = useRef<HTMLInputElement>(null);
  const [menuAt, setMenuAt] = useState<{ x: number; y: number } | null>(null);
  const [browsing, setBrowsing] = useState(false);

  // Inserted at the caret, not appended.
  function insert(tag: string) {
    const el = input.current;
    const at = el?.selectionStart ?? value.length;
    const to = el?.selectionEnd ?? at;
    const next = value.slice(0, at) + tag + value.slice(to);
    onChange(next);
    setMenuAt(null);
    requestAnimationFrame(() => {
      el?.focus();
      const caret = at + tag.length;
      el?.setSelectionRange(caret, caret);
    });
  }

  return (
    <div className="flex items-center gap-2">
      <input
        ref={input}
        dir="ltr"
        aria-label={label}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        className={`${FIELD_BOX} w-full px-3 text-sm placeholder:text-carbon-textMuted`}
      />
      {folder && (
        <Button
          type="button"
          kind="secondary"
          className="shrink-0"
          icon={<IconFolder width={16} height={16} />}
          title={t('folders.browse')}
          aria-label={t('folders.browse')}
          onClick={() => setBrowsing(true)}
        />
      )}
      {/* The chooser keeps the <jd:…> tail and replaces only the real folder
          in front of it. */}
      {browsing &&
        createPortal(
          <FolderPicker
            value={value}
            title={label}
            onClose={() => setBrowsing(false)}
            onPick={(next) => {
              onChange(next);
              setBrowsing(false);
            }}
          />,
          document.body,
        )}
      <Button
        kind="secondary"
        className="shrink-0"
        onClick={(e) => {
          const r = e.currentTarget.getBoundingClientRect();
          setMenuAt({ x: r.left, y: r.bottom + 4 });
        }}
      >
        {t('settings.rules.variables')}
      </Button>
      {menuAt && (
        <VariablesMenu
          t={t}
          variables={variables}
          at={menuAt}
          onPick={insert}
          onClose={() => setMenuAt(null)}
        />
      )}
    </div>
  );
}

/** SizeInput is a size box that flags what it cannot parse instead of reading zero. */
function SizeInput({
  t,
  value,
  onChange,
  placeholder,
  label,
}: {
  t: Translate;
  value: number | undefined;
  onChange: (next: number) => void;
  placeholder?: string;
  label: string;
}) {
  // Text while typing, so reformatting does not fight the caret.
  const [text, setText] = useState(() => formatSize(value));
  const [touched, setTouched] = useState(false);
  const parsed = parseSize(text);

  useEffect(() => {
    if (!touched) setText(formatSize(value));
  }, [value, touched]);

  return (
    <div className="flex flex-col gap-1">
      <TextInput
        aria-label={label}
        dir="ltr"
        value={text}
        placeholder={placeholder}
        onChange={(e) => {
          setTouched(true);
          setText(e.target.value);
          const n = parseSize(e.target.value);
          if (n !== null) onChange(n);
        }}
        onBlur={() => setTouched(false)}
      />
      {parsed === null && <span className="text-[11px] text-statusFail">{t('settings.rules.badSize')}</span>}
    </div>
  );
}

/** RuleEditor edits one rule's name, conditions and actions. */
export function RuleEditor({
  rule,
  flavour,
  grammar,
  problems,
  categories,
  onChange,
}: {
  rule: Rule;
  flavour: Flavour;
  grammar: Grammar;
  /** This rule's own problems, from the dry run. */
  problems: Problem[];
  /**
   * The drawers to pick from, taken from the unsaved settings draft that
   * PUT /api/settings will validate, so an unsaved drawer is accepted.
   */
  categories: Drawer[];
  onChange: (next: Rule) => void;
}) {
  const { t } = useT();
  const conditions = rule.conditions ?? [];

  const setAction = (fields: Partial<RuleAction>) =>
    onChange({ ...rule, action: { ...rule.action, ...fields } });

  const setCondition = (i: number, next: Condition) =>
    onChange({ ...rule, conditions: conditions.map((c, j) => (j === i ? next : c)) });

  const addCondition = () => {
    const first = grammar.fields[0];
    onChange({
      ...rule,
      conditions: [...conditions, { field: first.id, op: first.ops[0], value: '' }],
    });
  };

  const actions = grammar.actions.filter((a) => !a.flavour || a.flavour === flavour);

  return (
    <div className="flex flex-col gap-5">
      <label className="flex flex-col gap-1.5">
        <span className="flex items-center text-xs text-carbon-textSub">
          {t('settings.rules.name')}
          <InfoBubble tip={t('settings.rules.nameHint')} />
        </span>
        <TextInput
          value={rule.name ?? ''}
          placeholder={t('settings.rules.namePlaceholder')}
          onChange={(e) => onChange({ ...rule, name: e.target.value })}
        />
      </label>

      {/* Eyebrow captions rather than SectionTitle, whose badge needs a card
          edge that this inline editor does not have. */}
      <section className="flex flex-col gap-2.5">
        <h3 className="glim-eyebrow flex items-center">
          {t('settings.rules.sectionIf')}
          <InfoBubble tip={t('settings.rules.ifHint')} />
        </h3>

        {conditions.length === 0 && (
          <p className="text-[11px] text-carbon-textMuted">{t('settings.rules.noConditions')}</p>
        )}

        {conditions.map((c, i) => (
          <ConditionRow
            key={i}
            t={t}
            grammar={grammar}
            condition={c}
            index={i}
            // Problem.condition counts from 1.
            problems={problems.filter((p) => p.condition === i + 1)}
            onChange={(next) => setCondition(i, next)}
            onRemove={() => onChange({ ...rule, conditions: conditions.filter((_, j) => j !== i) })}
          />
        ))}

        <div>
          <Button kind="secondary" icon={<IconPlus width={14} height={14} />} onClick={addCondition}>
            {t('settings.rules.addCondition')}
          </Button>
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <h3 className="glim-eyebrow flex items-center">
          {t('settings.rules.sectionThen')}
          <InfoBubble
            tip={
              flavour === 'packagizer'
                ? t('settings.rules.thenHintPackagizer')
                : t('settings.rules.thenHintFilter')
            }
          />
        </h3>
        <div className="grid gap-3 sm:grid-cols-2">
          {actions.map((a) => (
            <ActionField
              key={a.id}
              t={t}
              grammar={grammar}
              action={a}
              value={rule.action}
              categories={categories}
              onChange={setAction}
            />
          ))}
        </div>
      </section>

      {/* Problems not tied to a condition, such as a bad priority. */}
      {problems.filter((p) => !p.condition).length > 0 && (
        <ul className="flex flex-col gap-1">
          {problems
            .filter((p) => !p.condition)
            .map((p, i) => (
              <li key={i} className="text-[11px] text-statusFail">
                {p.message}
              </li>
            ))}
        </ul>
      )}
    </div>
  );
}

function ConditionRow({
  t,
  grammar,
  condition,
  index,
  problems,
  onChange,
  onRemove,
}: {
  t: Translate;
  grammar: Grammar;
  condition: Condition;
  /** The condition's 0-based position, which sets the badge hue. */
  index: number;
  problems: Problem[];
  onChange: (next: Condition) => void;
  onRemove: () => void;
}) {
  const field = grammar.fields.find((f) => f.id === condition.field) ?? grammar.fields[0];
  const op = grammar.operators.find((o) => o.id === condition.op);
  const broken = problems.length > 0;

  // A new field may not offer the current operator, so it falls back to the
  // field's first one rather than saving a condition Compile refuses.
  function pickField(id: string) {
    const next = grammar.fields.find((f) => f.id === id);
    if (!next) return;
    const keepOp = next.ops.includes(condition.op) ? condition.op : next.ops[0];
    onChange({ ...condition, field: id, op: keepOp });
  }

  const category = grammar.categories.find((c) => c.pattern === condition.value);
  const showCategories = field.id === 'filetype' && op?.regex;

  return (
    <div
      className={`flex flex-col gap-2 rounded-[var(--radius-control)] p-2.5 ${
        broken ? 'bg-statusFailBg' : 'bg-carbon-surface2/40'
      }`}
    >
      <div className="flex flex-wrap items-start gap-2">
        <Dropdown
          width="widest"
          label={t('settings.rules.fieldPicker')}
          value={condition.field}
          onChange={pickField}
          options={grammar.fields.map((f) => ({ value: f.id, label: fieldLabel(t, f.id) }))}
        />
        <Dropdown
          width="widest"
          label={t('settings.rules.opPicker')}
          value={condition.op}
          onChange={(id) => onChange({ ...condition, op: id })}
          options={field.ops.map((o) => ({ value: o, label: opLabel(t, o) }))}
        />

        <div className="flex min-w-[12rem] flex-1 flex-col gap-1.5">
          {op?.range ? (
            <div className="flex items-start gap-2">
              <SizeInput
                t={t}
                label={t('settings.rules.min')}
                value={condition.min}
                onChange={(n) => onChange({ ...condition, min: n })}
                placeholder={t('settings.rules.min')}
              />
              <SizeInput
                t={t}
                label={t('settings.rules.max')}
                value={condition.max}
                onChange={(n) => onChange({ ...condition, max: n })}
                // The engine reads an empty maximum as no upper bound.
                placeholder={t('settings.rules.noUpperBound')}
              />
            </div>
          ) : field.numeric ? (
            <SizeInput
              t={t}
              label={t('settings.rules.value')}
              value={Number(condition.value) || 0}
              onChange={(n) => onChange({ ...condition, value: String(n) })}
            />
          ) : op?.regex ? (
            <div className="flex items-center">
              <TextInput
                aria-label={t('settings.rules.pattern')}
                dir="ltr"
                className="min-w-0 flex-1"
                value={condition.value ?? ''}
                placeholder={t('settings.rules.pattern')}
                onChange={(e) => onChange({ ...condition, value: e.target.value })}
              />
              <InfoBubble tip={t('settings.rules.patternHint')} />
            </div>
          ) : (
            <TextInput
              aria-label={t('settings.rules.value')}
              dir="ltr"
              value={condition.value ?? ''}
              placeholder={t('settings.rules.value')}
              onChange={(e) => onChange({ ...condition, value: e.target.value })}
            />
          )}

          {showCategories && (
            <div className="flex flex-wrap items-center gap-2">
              <Dropdown
                look="dense"
                label={t('settings.rules.category')}
                value={category?.id ?? ''}
                onChange={(id) => {
                  const picked = grammar.categories.find((c) => c.id === id);
                  if (picked) onChange({ ...condition, value: picked.pattern });
                }}
                options={[
                  { value: '', label: t('settings.rules.categoryCustom') },
                  ...grammar.categories.map((c) => ({ value: c.id, label: categoryLabel(t, c.id) })),
                ]}
              />
              <InfoBubble
                tip={
                  category
                    ? t('settings.rules.categoryExtensions', { list: category.extensions.join(', ') })
                    : t('settings.rules.categoryHint')
                }
              />
            </div>
          )}
        </div>

        {/* `labelled` so the label setting applies (rule 13); the row wraps if
            the badge grows a label. */}
        <IconBadge
          icon={<IconTrash width={16} height={16} />}
          hue={index}
          labelled
          aria-label={t('settings.rules.removeCondition')}
          title={t('settings.rules.removeCondition')}
          onClick={onRemove}
        />
      </div>

      {/* Shown on the offending condition itself. */}
      {problems.map((p, i) => (
        <p key={i} className="text-[11px] text-statusFail">
          {p.message}
        </p>
      ))}

    </div>
  );
}

const ACTION_HINTS: Partial<Record<keyof RuleAction, TranslationKey>> = {
  downloadDir: 'settings.rules.action.downloadDirHint',
  extractDir: 'settings.rules.action.extractDirHint',
  reason: 'settings.rules.action.reasonHint',
  priority: 'settings.rules.priorityHint',
  chunks: 'settings.rules.chunksHint',
  category: 'settings.rules.action.categoryHint',
};

// The actions that name a folder, which get the folder chooser and a whole row.
const FOLDER_ACTIONS: ReadonlySet<keyof RuleAction> = new Set(['downloadDir', 'extractDir']);

/** ActionField renders one action in the control its grammar kind calls for. */
function ActionField({
  t,
  grammar,
  action,
  value,
  categories,
  onChange,
}: {
  t: Translate;
  grammar: Grammar;
  action: ActionGrammar;
  value: RuleAction;
  categories: Drawer[];
  onChange: (fields: Partial<RuleAction>) => void;
}) {
  const label = actionLabel(t, action.id);
  const hintKey = ACTION_HINTS[action.id];
  const hint = hintKey ? t(hintKey) : undefined;

  const head = (
    <span className="flex items-center text-xs text-carbon-textSub">
      {label}
      {hint && <InfoBubble tip={hint} />}
    </span>
  );

  if (action.kind === 'template') {
    return (
      <div className={`flex flex-col gap-1.5 ${FOLDER_ACTIONS.has(action.id) ? 'sm:col-span-2' : ''}`}>
        {head}
        <TemplateInput
          t={t}
          label={label}
          folder={FOLDER_ACTIONS.has(action.id)}
          variables={grammar.variables}
          value={(value[action.id] as string) ?? ''}
          onChange={(next) => onChange({ [action.id]: next } as Partial<RuleAction>)}
        />
      </div>
    );
  }

  if (action.kind === 'int') {
    const current = value[action.id] as number | undefined;
    return (
      <div className="flex flex-col gap-1.5">
        {head}
        <TextInput
          type="number"
          inputMode="numeric"
          aria-label={label}
          min={action.min}
          max={action.max}
          // Empty means unchanged; 0 is a real value.
          value={current === undefined ? '' : String(current)}
          placeholder={t('settings.rules.emptyMeansUnchanged')}
          onChange={(e) => {
            const raw = e.target.value.trim();
            onChange({ [action.id]: raw === '' ? undefined : Number(raw) } as Partial<RuleAction>);
          }}
        />
      </div>
    );
  }

  if (action.kind === 'bool') {
    const current = value[action.id] as boolean | undefined;
    return (
      <div className="flex flex-col gap-1.5">
        {head}
        <Segments
          value={current === undefined ? 'unset' : current ? 'on' : 'off'}
          onChange={(v) =>
            onChange({ [action.id]: v === 'unset' ? undefined : v === 'on' } as Partial<RuleAction>)
          }
          options={[
            { value: 'unset', label: t('settings.rules.unchanged') },
            { value: 'on', label: t('settings.rules.yes') },
            { value: 'off', label: t('settings.rules.no') },
          ]}
          label={label}
        />
      </div>
    );
  }

  if (action.kind === 'category') {
    const current = value.category ?? '';
    const known = categories.map((c) => ({ value: c.id, label: c.name || c.id }));
    // A deleted or imported drawer id stays as an option, named as missing, so
    // the rule shows what it points at and the wheel steps from there.
    const options =
      current && !categories.some((c) => c.id === current)
        ? [{ value: current, label: t('settings.rules.action.categoryMissing', { id: current }) }, ...known]
        : known;
    return (
      <div className="flex flex-col gap-1.5">
        {head}
        {categories.length === 0 && !current ? (
          <span className="text-xs text-carbon-textSub">{t('settings.rules.action.categoryNone')}</span>
        ) : (
          <Dropdown
            value={current}
            // Undefined rather than "", which ValidateCategories would refuse.
            onChange={(next) => onChange({ category: next === '' ? undefined : next })}
            options={[{ value: '', label: t('settings.rules.unchanged') }, ...options]}
            label={label}
          />
        )}
      </div>
    );
  }

  // kind === 'reject': segments rather than a switch, since accepting is a
  // deliberate choice rather than the absence of a rejection.
  return (
    <div className="flex flex-col gap-1.5">
      {head}
      <Segments
        value={value.reject ? 'reject' : 'accept'}
        onChange={(v) => onChange({ reject: v === 'reject' })}
        options={[
          { value: 'reject', label: t('settings.rules.reject') },
          { value: 'accept', label: t('settings.rules.accept') },
        ]}
        label={label}
      />
    </div>
  );
}

/**
 * Segments is a small segmented choice on a form row, drawn by Tabs' well
 * variant so every horizontal selector is one component.
 */
export function Segments<T extends string>({
  value,
  onChange,
  options,
  label,
}: {
  value: T;
  onChange: (next: T) => void;
  options: { value: T; label: string }[];
  label: string;
}) {
  return (
    <Tabs
      variant="well"
      size="sm"
      label={label}
      active={value}
      onSelect={(id) => onChange(id as T)}
      items={options.map((o) => ({ id: o.value, label: o.label }))}
    />
  );
}

/**
 * ruleSummary is a one-line reading of a rule for the collapsed row, built from
 * the editor's own labels.
 */
export function ruleSummary(t: Translate, rule: Rule, flavour: Flavour): string {
  const conds = (rule.conditions ?? []).map((c) => {
    const op = opLabel(t, c.op);
    if (c.op === 'is-between') {
      const max = c.max ? formatSize(c.max) : t('settings.rules.noUpperBound');
      return `${fieldLabel(t, c.field)} ${op} ${formatSize(c.min) || '0'} - ${max}`;
    }
    return `${fieldLabel(t, c.field)} ${op} ${c.value ?? ''}`.trim();
  });

  const acts: string[] = [];
  if (flavour === 'filter') {
    acts.push(rule.action.reject ? t('settings.rules.reject') : t('settings.rules.accept'));
  } else {
    for (const [key, label] of [
      ['packageName', actionLabel(t, 'packageName')],
      ['downloadDir', actionLabel(t, 'downloadDir')],
      ['extractDir', actionLabel(t, 'extractDir')],
      ['comment', actionLabel(t, 'comment')],
    ] as const) {
      const v = rule.action[key];
      if (v) acts.push(`${label}: ${v}`);
    }
    if (rule.action.priority !== undefined) acts.push(`${actionLabel(t, 'priority')} ${rule.action.priority}`);
    if (rule.action.chunks !== undefined) acts.push(`${actionLabel(t, 'chunks')} ${rule.action.chunks}`);
    if (rule.action.autoExtract !== undefined) {
      acts.push(
        `${actionLabel(t, 'autoExtract')} ${rule.action.autoExtract ? t('settings.rules.yes') : t('settings.rules.no')}`,
      );
    }
  }

  const left = conds.length ? conds.join(' · ') : t('settings.rules.noConditions');
  return acts.length ? `${left} → ${acts.join(' · ')}` : left;
}

/** emptyRule returns a new rule for the flavour being edited. */
export function emptyRule(flavour: Flavour): Rule {
  return {
    name: '',
    conditions: [],
    // A new filter rule rejects; an accepting one would do nothing useful.
    action: flavour === 'filter' ? { reject: true } : {},
  };
}
