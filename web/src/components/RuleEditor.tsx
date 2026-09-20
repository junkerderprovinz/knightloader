import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useT, type TranslationKey } from '../lib/i18n';
// A Categories-page drawer, aliased because this file's own Category is the
// file-type shorthand a condition offers.
import type { Category as Drawer } from '../lib/api';
import { en } from '../lib/locales/en';
import { IconPlus, IconTrash } from '../lib/icons';
import { Button, IconBadge, InfoBubble, TextInput } from './ui';
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
    filename?: string;
    comment?: string;
    priority?: number;
    autoExtract?: boolean;
    chunks?: number;
    matched?: string[];
  };
  verdict: { rejected: boolean; rule?: string; reason?: string };
  result: { package: string; filename: string };
}

export interface Report {
  problems: Problem[];
  rules: RuleReport[];
  links: LinkReport[];
  disabled?: boolean;
}

// English fallbacks behind the catalogue, which already has every key. RuleKey
// is derived from this table.
export const RULE_STRINGS = {
  // The page around the editor.
  'settings.rules.setupTitle': 'Rule set',
  'settings.rules.flavour.packagizer': 'Packagizer',
  'settings.rules.flavour.filter': 'Link filter',
  'settings.rules.flavourLabel': 'Which rule list',
  'settings.rules.packagizerHint':
    'Runs on every link as it is staged and rewrites what it can: package, folder, comment, priority, chunks, auto-extract. Every matching rule contributes and a later rule wins per field.',
  'settings.rules.filterHint':
    'Decides whether a link is taken into the collector at all. A rejected link is not deleted: it is held aside with the rule and the reason that stopped it, so nothing ever disappears without saying why.',
  'settings.rules.setOn': 'This list is being applied',
  'settings.rules.setOff': 'This list is switched off',
  'settings.rules.setSwitchHint':
    'The master switch for the whole list. Off, no rule below runs - but they are all still edited and dry-run normally, because a list cannot be repaired while it is off if being off also hides what is wrong with it.',
  'settings.rules.stopAfterMatch': 'Stop at the first matching rule',
  'settings.rules.stopHint':
    'Off, every matching rule contributes and a later rule wins per field, which is what the Packagizer wants. On, evaluation ends at the first match, which is what a filter usually wants: an accept placed above a broad reject then actually protects the link.',
  'settings.rules.listTitle': 'Rules, in the order they run',
  'settings.rules.add': 'Add rule',
  'settings.rules.import': 'Import',
  'settings.rules.export': 'Export',
  'settings.rules.exportTitle': 'Save this list as a JSON file',
  'settings.rules.importTitle': 'Replace this list from a JSON file',
  'settings.rules.importFailed': 'That file is not a rule list: {reason}',
  'settings.rules.importedCount': 'Loaded {n} rules. Nothing is saved until you save the page.',
  'settings.rules.empty': 'No rules yet',
  'settings.rules.emptyPackagizer':
    'Every link keeps the package, folder and options it arrived with. Add a rule to sort them as they come in.',
  'settings.rules.emptyFilter': 'Every link is taken. Add a rule to hold some of them aside.',
  'settings.rules.unnamed': 'Rule {n}',
  'settings.rules.duplicate': 'Duplicate this rule',
  'settings.rules.remove': 'Remove this rule',
  'settings.rules.moveUp': 'Move up',
  'settings.rules.moveDown': 'Move down',
  'settings.rules.ruleOn': 'This rule runs',
  'settings.rules.ruleOff': 'This rule is switched off',
  'settings.rules.problemCount': '{n} problems',
  'settings.rules.problemOne': '1 problem',
  'settings.rules.notRunning': 'This rule is not being applied. Fix what is listed and it starts working again.',
  'settings.rules.matchedCount': 'matched {n} of {total}',

  // The editor.
  'settings.rules.name': 'Name',
  'settings.rules.namePlaceholder': 'What this rule is for',
  'settings.rules.nameHint':
    'Only ever shown to you - in this list, on a problem, and on the reason a link was held aside. An unnamed rule is called by its position, which changes when you reorder the list.',
  'settings.rules.sectionIf': 'If all of these are true',
  'settings.rules.ifHint':
    'Every condition has to hold. An either/or is written as two rules, which keeps the list readable top to bottom. A rule with NO conditions matches every link, which is how a catch-all folder or a blanket reject at the end is written.',
  'settings.rules.fieldPicker': 'What to look at',
  'settings.rules.opPicker': 'How to compare it',
  'settings.rules.sectionThen': 'Then',
  'settings.rules.thenHintPackagizer':
    'An empty box means "leave this alone", never "clear it": a rule that only sets the folder must not wipe the package name an earlier rule chose.',
  'settings.rules.thenHintFilter':
    'A rule that rejects holds the link aside with its reason. A rule that accepts is worth having too: placed above a broad reject, with "stop at the first match" on, it is how one hoster is let through.',
  'settings.rules.addCondition': 'Add condition',
  'settings.rules.removeCondition': 'Remove this condition',
  'settings.rules.noConditions': 'No conditions - this rule matches every link.',
  'settings.rules.value': 'Value',
  'settings.rules.min': 'At least',
  'settings.rules.max': 'At most',
  'settings.rules.noUpperBound': 'no upper bound',
  'settings.rules.sizeHint':
    'A plain number is bytes. A unit is understood too - 700 MB, 1.5 GiB - and both are read as 1024-based, the same as every size the rest of the app prints.',
  'settings.rules.badSize': 'This is not a size.',
  'settings.rules.pattern': 'Pattern',
  'settings.rules.patternHint':
    'A Go regular expression, unanchored, and the one operator that does NOT ignore case - put (?i) at the front if you want it to. An unparsable pattern is refused with the reason rather than quietly matching nothing.',
  'settings.rules.category': 'File type',
  'settings.rules.categoryCustom': 'Custom pattern',
  'settings.rules.categoryHint':
    'A shortcut that fills in the pattern for a whole family of extensions. It is stored as an ordinary pattern, so you can pick one and then edit it - after which this goes back to "custom pattern", because it no longer says what the rule does.',
  'settings.rules.categoryExtensions': 'Covers: {list}',
  'settings.rules.unchanged': 'Unchanged',
  'settings.rules.yes': 'On',
  'settings.rules.no': 'Off',
  'settings.rules.reject': 'Reject the link',
  'settings.rules.accept': 'Accept the link',
  'settings.rules.priorityHint':
    'Higher runs earlier. The range is the one the queue itself accepts, so a rule cannot hand a task a priority you have no way to undo. Leave it empty to change nothing.',
  'settings.rules.chunksHint':
    'How many connections this one file is downloaded with. Leave it empty to use the global setting. Connections beyond a handful buy nothing on a hoster that limits per file and are a reliable way to get an account flagged.',
  'settings.rules.emptyMeansUnchanged': 'empty = unchanged',

  // The variables menu.
  'settings.rules.variables': 'Variables',
  'settings.rules.variablesTitle': 'Insert a variable',
  'settings.rules.variablesHint':
    'Every one of these resolves against the link AS IT ARRIVED, so rules do not chain onto each other: a folder template in rule four sees the name the hoster gave, not what rule two renamed it to. What a rule does can be read off that rule alone.',
  'settings.rules.varParams': 'replace {params}',

  // Field names.
  'settings.rules.field.filename': 'File name',
  'settings.rules.field.url': 'Link URL',
  'settings.rules.field.hoster': 'Hoster',
  'settings.rules.field.source': 'Source page',
  'settings.rules.field.filetype': 'File type',
  'settings.rules.field.filesize': 'File size',
  'settings.rules.field.package': 'Package',

  // Operators.
  'settings.rules.op.contains': 'contains',
  'settings.rules.op.contains-not': 'does not contain',
  'settings.rules.op.equals': 'is',
  'settings.rules.op.equals-not': 'is not',
  'settings.rules.op.matches': 'matches pattern',
  'settings.rules.op.is-between': 'is between',

  // Actions.
  'settings.rules.action.packageName': 'Package name',
  'settings.rules.action.downloadDir': 'Download folder',
  'settings.rules.action.comment': 'Comment',
  'settings.rules.action.priority': 'Priority',
  'settings.rules.action.autoExtract': 'Extract automatically',
  'settings.rules.action.chunks': 'Connections',
  // For a task detail rule chip whose rule was renamed or deleted.
  'settings.rules.notFound':
    'No rule here is called "{name}". It was renamed or deleted since the link that names it was added.',
  'settings.rules.action.category': 'Category',
  'settings.rules.action.categoryHint':
    'Files matching links in one of the drawers from the Categories page, which brings its own folder, queue position, unpacking switch and collision rule with it.',
  'settings.rules.action.categoryNone':
    'No categories yet. Create one on the Categories page first, otherwise this rule would file links in a drawer that does not exist.',
  'settings.rules.action.categoryMissing': '{id} (deleted)',
  'settings.rules.action.reject': 'Verdict',
  'settings.rules.action.reason': 'Reason',
  'settings.rules.action.reasonHint':
    'Shown next to the held-aside link. Left empty one is written for you, because a rejection nobody can explain is exactly what this list exists to avoid.',
  'settings.rules.action.folderHint':
    'The only box allowed to spell out path levels. Everything else is cut back to a single name, because a file name containing a slash is not a name, it is a way out of the folder you picked.',

  // File-type categories.
  'settings.rules.category.video': 'Video',
  'settings.rules.category.audio': 'Audio',
  'settings.rules.category.image': 'Images',
  'settings.rules.category.archive': 'Archives',
  'settings.rules.category.document': 'Documents and books',
  'settings.rules.category.subtitle': 'Subtitles',
  'settings.rules.category.disc': 'Disc images',
  'settings.rules.category.program': 'Programs and packages',

  // Variables. The source and match entries explain how they differ from
  // JDownloader's <jd:source:N>.
  'settings.rules.var.packagename': 'The package the link arrived in',
  'settings.rules.var.hoster': 'The hoster, without www.',
  'settings.rules.var.filename': 'The file name as it arrived',
  'settings.rules.var.orgfilename': 'The file name as it arrived',
  'settings.rules.var.orgfilenamewithoutext': 'The file name without its extension',
  'settings.rules.var.orgfiletype': 'The extension without its dot, empty when there is none',
  'settings.rules.var.date': 'Today, as YYYY-MM-DD',
  'settings.rules.var.year': 'The year, as YYYY',
  'settings.rules.var.month': 'The month, as MM',
  'settings.rules.var.day': 'The day, as DD',
  'settings.rules.var.simpledate': 'The date in a pattern you write, in Java’s date syntax',
  'settings.rules.var.source':
    'The Nth path segment of the source page’s URL, counting from 1: on https://site.org/tv/s01/list.html, 1 is tv and 2 is s01. NOT what JDownloader means by this tag - see the note below.',
  'settings.rules.var.match':
    'Capture group N of this rule’s "matches" pattern on FIELD. This is JDownloader’s <jd:source:N>, under a name that says which pattern it reads. A rule with no matching pattern on that field is refused when you save it, rather than quietly producing a folder called <jd:match:url:1>.',
  'settings.rules.var.append': 'Nothing the first time this value comes up, then _2, _3 and so on',
  'settings.rules.sourceDivergence':
    'Copying a template out of a JDownloader config? <jd:source:N> means something different here - a path segment of the source URL, not a capture group. JD’s meaning is spelled <jd:match:FIELD:N>. The two agree often enough to be dangerous, so a copied template is worth dry-running below before you save it.',

  // The test box.
  'settings.rules.testTitle': 'Try it on a link',
  'settings.rules.testHint':
    'Nothing here is downloaded, stored or saved. The list as it stands on this page is run against these samples, through the same code that runs at staging time.',
  'settings.rules.testUrl': 'Link URL',
  'settings.rules.testFilename': 'File name',
  'settings.rules.testSource': 'Source page',
  'settings.rules.testSourceHint':
    'The page a crawl found the link on. Only worth filling in for a rule that tests the source field or uses <jd:source:N>.',
  'settings.rules.testSize': 'Size',
  'settings.rules.testPackage': 'Package',
  'settings.rules.testPackageHint':
    'The package the link would arrive in, before any rule runs. It is what <jd:packagename> resolves to, which is worth knowing: a folder template does not see the package name a rule sets in the same pass.',
  'settings.rules.testAdd': 'Add a sample',
  'settings.rules.testRemove': 'Remove this sample',
  'settings.rules.testEmpty': 'Paste a link and a file name to see where it would land.',
  'settings.rules.testRunning': 'Checking…',
  'settings.rules.testFailed': 'The dry run could not be reached: {reason}',
  'settings.rules.resultMatched': 'Matched, in order',
  'settings.rules.resultNone': 'No rule matched',
  'settings.rules.resultPackage': 'Package',
  'settings.rules.resultFolder': 'Folder',
  'settings.rules.resultFilename': 'File name',
  'settings.rules.resultRejected': 'Held aside',
  'settings.rules.resultAccepted': 'Taken',
  'settings.rules.resultBy': 'by {rule}',
  'settings.rules.folderFromSettings': 'from the download settings',
  'settings.rules.folderFromSettingsHint':
    'No rule named a folder, so the link lands in the configured download folder - possibly inside a per-package subfolder, if that setting is on. This page will not guess the full path at you: what it can say for certain is that no rule changed it.',
  'settings.rules.alsoSets': 'Also sets',
} as const;

export type RuleKey = keyof typeof RULE_STRINGS;

/** useRx returns `t` for RuleKey, falling back to RULE_STRINGS. */
export function useRx() {
  const { t } = useT();
  return useCallback(
    (key: RuleKey, vars?: Record<string, string | number>) => {
      // Only RULE_STRINGS keys get through this cast.
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? RULE_STRINGS[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
}

export type Rx = ReturnType<typeof useRx>;

// named falls back to the raw id for a grammar entry the catalogue does not
// know, since the grammar comes from the server.
function named(rx: Rx, prefix: string, id: string): string {
  const key = (prefix + id) as RuleKey;
  return key in en ? rx(key) : id;
}

export const fieldLabel = (rx: Rx, id: string) => named(rx, 'settings.rules.field.', id);
export const opLabel = (rx: Rx, id: string) => named(rx, 'settings.rules.op.', id);
export const actionLabel = (rx: Rx, id: string) => named(rx, 'settings.rules.action.', id);
export const categoryLabel = (rx: Rx, id: string) => named(rx, 'settings.rules.category.', id);
export const variableLabel = (rx: Rx, id: string) => named(rx, 'settings.rules.var.', id);

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
  rx,
  variables,
  at,
  onPick,
  onClose,
}: {
  rx: Rx;
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
      aria-label={rx('settings.rules.variablesTitle')}
      style={{ top: pos.top, left: pos.left }}
      className="glim-card glim-fade fixed z-50 max-h-[70vh] w-[26rem] max-w-[calc(100vw-1rem)] overflow-y-auto p-1.5"
    >
      <div className="flex items-center px-2 py-1.5 text-[11px] font-semibold text-carbon-textSub">
        {rx('settings.rules.variablesTitle')}
        <InfoBubble tip={rx('settings.rules.variablesHint')} />
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
            {variableLabel(rx, v.id)}
            {v.params?.length ? ` - ${rx('settings.rules.varParams', { params: v.params.join(', ') })}` : ''}
          </span>
        </button>
      ))}
      {/* How <jd:source:N> differs from JDownloader's, where a JD template gets pasted. */}
      <div className="mt-1 border-t border-carbon-border/60 px-2 pb-1 pt-2 text-[11px] leading-snug text-carbon-textSub">
        {rx('settings.rules.sourceDivergence')}
      </div>
    </div>,
    document.body,
  );
}

/** A text box whose content is a template, with the variables menu attached. */
function TemplateInput({
  rx,
  variables,
  value,
  onChange,
  placeholder,
  label,
}: {
  rx: Rx;
  variables: Variable[];
  value: string;
  onChange: (next: string) => void;
  placeholder?: string;
  /** The accessible name. There is no <label>, which would send a click on the
   *  variables button to the input. */
  label: string;
}) {
  const input = useRef<HTMLInputElement>(null);
  const [menuAt, setMenuAt] = useState<{ x: number; y: number } | null>(null);

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
        className="w-full rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text
          placeholder:text-carbon-textMuted outline-none transition-shadow
          focus:shadow-[0_0_0_2px_var(--focus-ring)]"
      />
      {/* Not the shared Button, which is taller than a form control. */}
      <button
        type="button"
        onClick={(e) => {
          const r = e.currentTarget.getBoundingClientRect();
          setMenuAt({ x: r.left, y: r.bottom + 4 });
        }}
        className="shrink-0 rounded-[var(--radius-control)] bg-carbon-surface2 px-2.5 py-2 text-xs font-medium
          text-carbon-textSub transition-colors select-none hover:bg-carbon-surface3 hover:text-carbon-text"
      >
        {rx('settings.rules.variables')}
      </button>
      {menuAt && (
        <VariablesMenu
          rx={rx}
          variables={variables}
          at={menuAt}
          onPick={insert}
          onClose={() => setMenuAt(null)}
        />
      )}
    </div>
  );
}

/**
 * wheelSteps lets the wheel step a closed <select> one option per notch,
 * clamped at both ends so a condition never jumps to a far field (design rule
 * 14). It is a native listener with `passive: false`, because React registers
 * onWheel passive and preventDefault would not stop the page scrolling.
 * QueueBar.tsx and SearchField.tsx carry the same listener.
 */
function wheelSteps(el: HTMLSelectElement | null) {
  if (!el) return;
  const onWheel = (e: WheelEvent) => {
    // Only the sign of deltaY counts; trackpads report fractions.
    if (el.disabled || el.options.length < 2 || e.deltaY === 0) return;
    e.preventDefault();
    const next = Math.min(el.options.length - 1, Math.max(0, el.selectedIndex + (e.deltaY > 0 ? 1 : -1)));
    if (next === el.selectedIndex) return;
    el.selectedIndex = next;
    // A real change event, so the element's onChange handles it like a click.
    el.dispatchEvent(new Event('change', { bubbles: true }));
  };
  el.addEventListener('wheel', onWheel, { passive: false });
  return () => el.removeEventListener('wheel', onWheel);
}

/**
 * Select is a native dropdown in the settings style. It stays native against
 * design rule 18 until the app has one shared listbox for every picker.
 */
function Select<T extends string>({
  value,
  onChange,
  options,
  label,
  className = '',
}: {
  value: T;
  onChange: (next: T) => void;
  options: { value: T; label: string }[];
  label: string;
  className?: string;
}) {
  return (
    <select
      ref={wheelSteps}
      aria-label={label}
      value={value}
      onChange={(e) => onChange(e.target.value as T)}
      className={`glim-select appearance-none pe-6 rounded-[var(--radius-control)] bg-carbon-surface2 px-2.5 py-2 text-sm text-carbon-text
        outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)] ${className}`}
    >
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}

/** SizeInput is a size box that flags what it cannot parse instead of reading zero. */
function SizeInput({
  rx,
  value,
  onChange,
  placeholder,
  label,
}: {
  rx: Rx;
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
      {parsed === null && <span className="text-[11px] text-statusFail">{rx('settings.rules.badSize')}</span>}
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
  const rx = useRx();
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
          {rx('settings.rules.name')}
          <InfoBubble tip={rx('settings.rules.nameHint')} />
        </span>
        <TextInput
          value={rule.name ?? ''}
          placeholder={rx('settings.rules.namePlaceholder')}
          onChange={(e) => onChange({ ...rule, name: e.target.value })}
        />
      </label>

      {/* Eyebrow captions rather than SectionTitle, whose badge needs a card
          edge that this inline editor does not have. */}
      <section className="flex flex-col gap-2.5">
        <h3 className="glim-eyebrow flex items-center">
          {rx('settings.rules.sectionIf')}
          <InfoBubble tip={rx('settings.rules.ifHint')} />
        </h3>

        {conditions.length === 0 && (
          <p className="text-[11px] text-carbon-textMuted">{rx('settings.rules.noConditions')}</p>
        )}

        {conditions.map((c, i) => (
          <ConditionRow
            key={i}
            rx={rx}
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
            {rx('settings.rules.addCondition')}
          </Button>
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <h3 className="glim-eyebrow flex items-center">
          {rx('settings.rules.sectionThen')}
          <InfoBubble
            tip={
              flavour === 'packagizer'
                ? rx('settings.rules.thenHintPackagizer')
                : rx('settings.rules.thenHintFilter')
            }
          />
        </h3>
        <div className="grid gap-3 sm:grid-cols-2">
          {actions.map((a) => (
            <ActionField
              key={a.id}
              rx={rx}
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
  rx,
  grammar,
  condition,
  index,
  problems,
  onChange,
  onRemove,
}: {
  rx: Rx;
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
        <Select
          label={rx('settings.rules.fieldPicker')}
          value={condition.field}
          onChange={pickField}
          options={grammar.fields.map((f) => ({ value: f.id, label: fieldLabel(rx, f.id) }))}
        />
        <Select
          label={rx('settings.rules.opPicker')}
          value={condition.op}
          onChange={(id) => onChange({ ...condition, op: id })}
          options={field.ops.map((o) => ({ value: o, label: opLabel(rx, o) }))}
        />

        <div className="flex min-w-[12rem] flex-1 flex-col gap-1.5">
          {op?.range ? (
            <div className="flex items-start gap-2">
              <SizeInput
                rx={rx}
                label={rx('settings.rules.min')}
                value={condition.min}
                onChange={(n) => onChange({ ...condition, min: n })}
                placeholder={rx('settings.rules.min')}
              />
              <SizeInput
                rx={rx}
                label={rx('settings.rules.max')}
                value={condition.max}
                onChange={(n) => onChange({ ...condition, max: n })}
                // The engine reads an empty maximum as no upper bound.
                placeholder={rx('settings.rules.noUpperBound')}
              />
            </div>
          ) : field.numeric ? (
            <SizeInput
              rx={rx}
              label={rx('settings.rules.value')}
              value={Number(condition.value) || 0}
              onChange={(n) => onChange({ ...condition, value: String(n) })}
            />
          ) : (
            <TextInput
              aria-label={op?.regex ? rx('settings.rules.pattern') : rx('settings.rules.value')}
              dir="ltr"
              value={condition.value ?? ''}
              placeholder={op?.regex ? rx('settings.rules.pattern') : rx('settings.rules.value')}
              onChange={(e) => onChange({ ...condition, value: e.target.value })}
            />
          )}

          {showCategories && (
            <div className="flex flex-wrap items-center gap-2">
              <Select
                label={rx('settings.rules.category')}
                className="text-xs"
                value={category?.id ?? ''}
                onChange={(id) => {
                  const picked = grammar.categories.find((c) => c.id === id);
                  if (picked) onChange({ ...condition, value: picked.pattern });
                }}
                options={[
                  { value: '', label: rx('settings.rules.categoryCustom') },
                  ...grammar.categories.map((c) => ({ value: c.id, label: categoryLabel(rx, c.id) })),
                ]}
              />
              <InfoBubble
                tip={
                  category
                    ? rx('settings.rules.categoryExtensions', { list: category.extensions.join(', ') })
                    : rx('settings.rules.categoryHint')
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
          aria-label={rx('settings.rules.removeCondition')}
          title={rx('settings.rules.removeCondition')}
          onClick={onRemove}
        />
      </div>

      {/* Shown on the offending condition itself. */}
      {problems.map((p, i) => (
        <p key={i} className="text-[11px] text-statusFail">
          {p.message}
        </p>
      ))}

      {op?.regex && !broken && (
        <p className="text-[11px] text-carbon-textMuted">
          {rx('settings.rules.patternHint')}
        </p>
      )}
    </div>
  );
}

/** ActionField renders one action in the control its grammar kind calls for. */
function ActionField({
  rx,
  grammar,
  action,
  value,
  categories,
  onChange,
}: {
  rx: Rx;
  grammar: Grammar;
  action: ActionGrammar;
  value: RuleAction;
  categories: Drawer[];
  onChange: (fields: Partial<RuleAction>) => void;
}) {
  const label = actionLabel(rx, action.id);
  const hint =
    action.id === 'downloadDir'
      ? rx('settings.rules.action.folderHint')
      : action.id === 'reason'
        ? rx('settings.rules.action.reasonHint')
        : action.id === 'priority'
          ? rx('settings.rules.priorityHint')
          : action.id === 'chunks'
            ? rx('settings.rules.chunksHint')
            : action.id === 'category'
              ? rx('settings.rules.action.categoryHint')
              : undefined;

  const head = (
    <span className="flex items-center text-xs text-carbon-textSub">
      {label}
      {hint && <InfoBubble tip={hint} />}
    </span>
  );

  if (action.kind === 'template') {
    return (
      <div className={`flex flex-col gap-1.5 ${action.id === 'downloadDir' ? 'sm:col-span-2' : ''}`}>
        {head}
        <TemplateInput
          rx={rx}
          label={label}
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
          placeholder={rx('settings.rules.emptyMeansUnchanged')}
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
            { value: 'unset', label: rx('settings.rules.unchanged') },
            { value: 'on', label: rx('settings.rules.yes') },
            { value: 'off', label: rx('settings.rules.no') },
          ]}
          label={label}
        />
      </div>
    );
  }

  if (action.kind === 'category') {
    const current = value.category ?? '';
    const known = categories.map((c) => ({ value: c.id, label: c.name || c.id }));
    // A deleted or imported drawer id stays as an option, because a <select>
    // with an unknown value shows its first option and would silently rewrite
    // the rule to it.
    const options =
      current && !categories.some((c) => c.id === current)
        ? [{ value: current, label: rx('settings.rules.action.categoryMissing', { id: current }) }, ...known]
        : known;
    return (
      <div className="flex flex-col gap-1.5">
        {head}
        {categories.length === 0 && !current ? (
          <span className="text-xs text-carbon-textSub">{rx('settings.rules.action.categoryNone')}</span>
        ) : (
          <Select
            value={current}
            // Undefined rather than "", which ValidateCategories would refuse.
            onChange={(next) => onChange({ category: next === '' ? undefined : next })}
            options={[{ value: '', label: rx('settings.rules.unchanged') }, ...options]}
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
          { value: 'reject', label: rx('settings.rules.reject') },
          { value: 'accept', label: rx('settings.rules.accept') },
        ]}
        label={label}
      />
    </div>
  );
}

/**
 * Segments is a small segmented choice on a form row, drawn by Tabs' well
 * variant so every horizontal selector is one component. `w-fit` keeps the
 * track hugging its segments.
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
      className="w-fit"
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
export function ruleSummary(rx: Rx, rule: Rule, flavour: Flavour): string {
  const conds = (rule.conditions ?? []).map((c) => {
    const op = opLabel(rx, c.op);
    if (c.op === 'is-between') {
      const max = c.max ? formatSize(c.max) : rx('settings.rules.noUpperBound');
      return `${fieldLabel(rx, c.field)} ${op} ${formatSize(c.min) || '0'} - ${max}`;
    }
    return `${fieldLabel(rx, c.field)} ${op} ${c.value ?? ''}`.trim();
  });

  const acts: string[] = [];
  if (flavour === 'filter') {
    acts.push(rule.action.reject ? rx('settings.rules.reject') : rx('settings.rules.accept'));
  } else {
    for (const [key, label] of [
      ['packageName', actionLabel(rx, 'packageName')],
      ['downloadDir', actionLabel(rx, 'downloadDir')],
      ['comment', actionLabel(rx, 'comment')],
    ] as const) {
      const v = rule.action[key];
      if (v) acts.push(`${label}: ${v}`);
    }
    if (rule.action.priority !== undefined) acts.push(`${actionLabel(rx, 'priority')} ${rule.action.priority}`);
    if (rule.action.chunks !== undefined) acts.push(`${actionLabel(rx, 'chunks')} ${rule.action.chunks}`);
    if (rule.action.autoExtract !== undefined) {
      acts.push(
        `${actionLabel(rx, 'autoExtract')} ${rule.action.autoExtract ? rx('settings.rules.yes') : rx('settings.rules.no')}`,
      );
    }
  }

  const left = conds.length ? conds.join(' · ') : rx('settings.rules.noConditions');
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
