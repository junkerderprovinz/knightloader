// The collector's facet sidebar narrows staged links by host, file type or
// package, computed from the tasks on screen. Availability is not a facet here
// because ListToolbar's filter chips already cover it.
import { useCallback, useMemo } from 'react';
import type { Task } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button, Card, SectionTitle } from './ui';
import { Tip, hostOf } from './columns';
import { IconCheck } from '../lib/icons';

// English fallbacks for keys not yet in en.ts. t() is asked first, so the table
// can go once the catalogues carry them.
const PENDING = {
  'collector.facets.title': 'Filters',
  'collector.facets.hint': 'Narrow the staged list by where a link points, what kind of file it is, or which package it landed in.',
  'collector.facets.fileType': 'File type',
  'collector.facets.clearAll': 'Clear',
  'collector.facets.unknownHost': 'Unknown host',
  'collector.facets.type.archive': 'Archives',
  'collector.facets.type.video': 'Video',
  'collector.facets.type.audio': 'Audio',
  'collector.facets.type.image': 'Images',
  'collector.facets.type.document': 'Documents',
  'collector.facets.type.other': 'Other',
} as const;

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return useCallback(
    (key: PendingKey, vars?: Record<string, string | number>) => {
      // These keys are not in the union yet; only PENDING keys get through.
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
}

export type FacetKind = 'host' | 'fileType' | 'package';

export interface FacetSelection {
  host: ReadonlySet<string>;
  fileType: ReadonlySet<string>;
  package: ReadonlySet<string>;
}

export const EMPTY_FACETS: FacetSelection = { host: new Set(), fileType: new Set(), package: new Set() };

export function facetActiveCount(sel: FacetSelection): number {
  return sel.host.size + sel.fileType.size + sel.package.size;
}

/**
 * matchesFacets is a union within one dimension and an intersection across
 * them: two hosts mean either, a host and a file type mean both.
 */
export function matchesFacets(t: Task, sel: FacetSelection): boolean {
  if (sel.host.size > 0 && !sel.host.has(hostOf(t))) return false;
  if (sel.fileType.size > 0 && !sel.fileType.has(fileTypeOf(t))) return false;
  if (sel.package.size > 0 && !sel.package.has(t.package || '')) return false;
  return true;
}

type FileCategory = 'archive' | 'video' | 'audio' | 'image' | 'document' | 'other';

const EXT_CATEGORY: Record<string, FileCategory> = {
  zip: 'archive', rar: 'archive', '7z': 'archive', tar: 'archive', gz: 'archive', tgz: 'archive',
  bz2: 'archive', xz: 'archive', iso: 'archive', dlc: 'archive', dmg: 'archive', cbr: 'archive', cbz: 'archive',
  mp4: 'video', mkv: 'video', avi: 'video', mov: 'video', wmv: 'video', flv: 'video',
  webm: 'video', m4v: 'video', mpg: 'video', mpeg: 'video', ts: 'video',
  mp3: 'audio', flac: 'audio', wav: 'audio', aac: 'audio', ogg: 'audio', m4a: 'audio', wma: 'audio',
  jpg: 'image', jpeg: 'image', png: 'image', gif: 'image', webp: 'image', bmp: 'image', tiff: 'image', svg: 'image',
  pdf: 'document', epub: 'document', mobi: 'document', azw3: 'document',
  doc: 'document', docx: 'document', txt: 'document',
};

// Tail parts of multi-volume archives (.r00, .001, .z01) that the table misses.
const ARCHIVE_TAIL = /^(r\d{2,3}|z\d{2}|\d{3})$/;

function extOf(t: Task): string {
  const name = t.filename || t.name || t.url;
  const base = name.split(/[?#]/, 1)[0] ?? name;
  const m = /\.([a-z0-9]{1,8})$/i.exec(base);
  return m ? m[1].toLowerCase() : '';
}

function fileTypeOf(t: Task): FileCategory {
  const ext = extOf(t);
  return EXT_CATEGORY[ext] ?? (ARCHIVE_TAIL.test(ext) ? 'archive' : 'other');
}

const TYPE_LABEL: Record<FileCategory, PendingKey> = {
  archive: 'collector.facets.type.archive',
  video: 'collector.facets.type.video',
  audio: 'collector.facets.type.audio',
  image: 'collector.facets.type.image',
  document: 'collector.facets.type.document',
  other: 'collector.facets.type.other',
};

interface FacetOption {
  value: string;
  label: string;
  count: number;
}

/**
 * buildFacet turns the staged tasks into one dimension's options. An active
 * value with no tasks left stays at a count of 0, or nobody could uncheck it.
 */
function buildFacet(
  tasks: Task[],
  active: ReadonlySet<string>,
  keyOf: (t: Task) => { value: string; label: string },
): FacetOption[] {
  const m = new Map<string, FacetOption>();
  for (const t of tasks) {
    const { value, label } = keyOf(t);
    const cur = m.get(value);
    if (cur) cur.count++;
    else m.set(value, { value, label, count: 1 });
  }
  for (const v of active) if (!m.has(v)) m.set(v, { value: v, label: v, count: 0 });
  return [...m.values()].sort((a, b) => b.count - a.count || a.label.localeCompare(b.label));
}

// The row is the control; the mark copies columns.tsx's Checkbox as a plain
// span so it is not a second focus stop.
function FacetRow({
  checked,
  label,
  count,
  onToggle,
}: {
  checked: boolean;
  label: string;
  count: number;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      onClick={onToggle}
      className="flex w-full items-center gap-2 rounded-[var(--radius-control)] px-1.5 py-1 text-start text-xs
        text-carbon-textSub transition-colors hover:bg-carbon-hover"
    >
      <span
        aria-hidden
        className={`grid h-[1.125rem] w-[1.125rem] shrink-0 place-items-center rounded-[var(--radius-control)] transition-colors ${
          checked ? 'bg-accent text-accentContrast' : 'bg-carbon-surface3/60 text-transparent'
        }`}
      >
        <IconCheck width={12} height={12} />
      </span>
      <Tip tip={label} className="min-w-0 flex-1 truncate">
        {label}
      </Tip>
      <span className="glim-num shrink-0 text-[11px] text-carbon-textMuted">{count}</span>
    </button>
  );
}

function FacetGroup({
  title,
  options,
  active,
  onToggle,
}: {
  title: string;
  options: FacetOption[];
  active: ReadonlySet<string>;
  onToggle: (value: string) => void;
}) {
  if (options.length === 0) return null;
  return (
    <div className="flex flex-col gap-1">
      <h3 className="glim-eyebrow px-1.5">{title}</h3>
      {/* Scrolls, so a paste from eighty hosts does not stretch the sidebar. */}
      <div className="flex max-h-48 flex-col gap-0.5 overflow-y-auto">
        {options.map((o) => (
          <FacetRow key={o.value} checked={active.has(o.value)} label={o.label} count={o.count} onToggle={() => onToggle(o.value)} />
        ))}
      </div>
    </div>
  );
}

function toggled<T>(set: ReadonlySet<T>, value: T): Set<T> {
  const next = new Set(set);
  if (next.has(value)) next.delete(value);
  else next.add(value);
  return next;
}

/**
 * CollectorFacetSidebar is the facet panel. `tasks` is the collected set before
 * any narrowing, so a count never shrinks because another box is checked.
 */
export function CollectorFacetSidebar({
  tasks,
  selection,
  onChange,
}: {
  tasks: Task[];
  selection: FacetSelection;
  onChange: (next: FacetSelection) => void;
}) {
  const { t } = useT();
  const cx = useCx();

  const hostOptions = useMemo(
    () =>
      buildFacet(tasks, selection.host, (x) => ({
        value: hostOf(x),
        label: hostOf(x) || cx('collector.facets.unknownHost'),
      })),
    [tasks, selection.host, cx],
  );
  const typeOptions = useMemo(
    () =>
      buildFacet(tasks, selection.fileType, (x) => {
        const cat = fileTypeOf(x);
        return { value: cat, label: cx(TYPE_LABEL[cat]) };
      }),
    [tasks, selection.fileType, cx],
  );
  const pkgOptions = useMemo(
    () =>
      buildFacet(tasks, selection.package, (x) => ({
        value: x.package || '',
        label: x.package || t('task.ungrouped'),
      })),
    [tasks, selection.package, t],
  );

  const set = (kind: FacetKind, value: string) => onChange({ ...selection, [kind]: toggled(selection[kind], value) });
  const activeCount = facetActiveCount(selection);

  return (
    <Card hue={2} className="flex w-full shrink-0 flex-col gap-4 lg:w-64">
      <SectionTitle
        hint={cx('collector.facets.hint')}
        right={
          activeCount > 0 && (
            <Button kind="ghost" className="px-2 py-1 text-[11px]" onClick={() => onChange(EMPTY_FACETS)}>
              {cx('collector.facets.clearAll')}
            </Button>
          )
        }
      >
        {cx('collector.facets.title')}
      </SectionTitle>

      <FacetGroup title={t('columns.host')} options={hostOptions} active={selection.host} onToggle={(v) => set('host', v)} />
      <FacetGroup
        title={cx('collector.facets.fileType')}
        options={typeOptions}
        active={selection.fileType}
        onToggle={(v) => set('fileType', v)}
      />
      <FacetGroup title={t('search.package')} options={pkgOptions} active={selection.package} onToggle={(v) => set('package', v)} />
    </Card>
  );
}

