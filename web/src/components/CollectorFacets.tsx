// The collector's facet sidebar: narrow the staged links by host, file type or
// package, the way JD's own LinkGrabber sidebar does (docs/jd-feature-census.md,
// section 6 — "Ansichten (Sidebar)" / "Hoster" / "Dateitypen", all three missing
// there today).
//
// Deliberately NOT a fourth, availability facet: Online / Offline / Uncheckable
// / Not checked is already a facet, just a horizontal one — ListToolbar's own
// COLLECTOR_FILTERS chips (components/ListToolbar.tsx) — and a second, vertical
// copy of the same four values here would be exactly the "third, inconsistent
// summary component" this wave was told to build instead of. Host, file type
// and package have no such chip anywhere, which is the whole reason they are
// the three groups below.
//
// Everything is computed client-side from the tasks already on screen —
// core.Task carries host and package as real fields, and a file type is one
// regex away from the name already rendered in the list — so there is no new
// server route here and nothing in this file can drift from what the tasks
// stream actually holds.
import { useCallback, useMemo } from 'react';
import type { Task } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button, Card, SectionTitle } from './ui';
import { hostOf } from './columns';
import { IconCheck } from '../lib/icons';

// New UI text for this wave, kept out of en.ts on purpose: the locale files are
// one writer's lane and it runs after 8A–8D/8G land (build-plan.md section 8's
// Wave 8 amendment). Same arrangement as pages/settings/Captcha.tsx and
// pages/settings/Connections.tsx — t() is asked first, so the day these keys
// land for real in en.ts (and in all the other locales) this table stops being
// consulted and can be deleted without touching anything else here.
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
 * them: checking two hosts means "either of these", checking a host AND a file
 * type means "both" — the same rule ListToolbar's matchesQuickFilters already
 * uses within its one dimension, extended the only way that makes sense once
 * there is more than one.
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

// Multi-volume archives name their tail parts .r00…/.r99, .001…/.999 or
// .z01…/.z99 rather than repeating a real extension, so the lookup above would
// file every part after the first one as "other" — the one case where a
// "checkbox list of the file types present" (docs/jd-feature-census.md's own
// words for this facet) would be actively misleading for exactly the kind of
// link a JDownloader refugee pastes in bulk.
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
 * buildFacet turns the staged tasks into one dimension's checkbox list.
 *
 * An active value that no longer matches any task is kept, at a count of 0,
 * rather than dropped — the same rule ListToolbar's own quick filters use
 * (components/ListToolbar.tsx, the `offered` memo in ListToolbar): a checked
 * box that disappears from the panel the moment its count hits zero is a
 * checked box nobody can ever reach again to uncheck.
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

/**
 * The row itself is the control — a real checkbox nested inside it would be
 * two interactive elements answering for one click. The mark is drawn to match
 * columns.tsx's exported Checkbox exactly (the selection mark everywhere else
 * in GlimStone), as a plain span rather than that component, because here it is
 * not a second focus stop of its own.
 */
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
      className="flex w-full items-center gap-2 rounded-[var(--radius-control)] px-1.5 py-1 text-start text-[12.5px]
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
      <span className="min-w-0 flex-1 truncate" title={label}>
        {label}
      </span>
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
      {/* Scrolls rather than growing the panel without bound — a paste of two
          hundred links from eighty hosts must not turn the sidebar into the
          page's own scrollbar. */}
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
 * CollectorFacetSidebar is the panel itself.
 *
 * `tasks` is the collected set before search, quick filters or these very
 * facets narrow it — the same basis ListToolbar counts its own chips against —
 * so unchecking a box always widens what is on screen, and a count next to a
 * box never shrinks just because another box in the same panel is checked.
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
    <Card className="flex w-full shrink-0 flex-col gap-4 lg:w-64">
      <SectionTitle
        hue={2}
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

