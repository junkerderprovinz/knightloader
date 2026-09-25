import { useEffect, useState } from 'react';
import {
  Button,
  Card,
  Field,
  FieldGroup,
  IconBadge,
  SectionTitle,
  TextInput,
  UnitNumberInput,
} from '../../components/ui';
import { Dropdown } from '../../components/Dropdown';
import { PathInput } from '../../components/FolderPicker';
import { Tabs } from '../../components/Tabs';
import {
  fetchMediaHooks,
  fetchOptions,
  priorityChoices,
  type ApiOptions,
  type Category,
  type MediaHook,
  type PriorityChoice,
} from '../../lib/api';
import { RATE_UNITS, fmtSpeed } from '../../lib/format';
import { IconArrowDown, IconArrowUp, IconPlus, IconTrash } from '../../lib/icons';
import { useT, type TranslationKey } from '../../lib/i18n';
import { COLLISION_LABEL } from './Archives';
import { useDraft, useFieldError } from './context';
import { RowRefusal } from './controls';
import { FileSelectionFields } from './Torrents';

/**
 * CategoriesCard edits the named drawers, each a folder plus defaults, that a
 * Packagizer rule files a download into. The folder, unpacking, collision rule,
 * queue position and torrent file selection are live; the speed limit has no
 * caller yet, since internal/throttle is one limiter for the whole app, and its
 * hint says so.
 *
 * The server derives a key from the name once, on save, so renaming is free;
 * this page only previews that key and never writes it. ValidateCategories
 * refuses the whole settings document over one bad row, so the controls offer
 * only what the server accepts and a duplicate key is marked before saving.
 */

/**
 * Mirrors settings.CategoryID to preview a key and find duplicates; the server
 * alone decides the stored key.
 */
const LETTER_OR_DIGIT = /[\p{L}\p{N}]/u;

/** The Go side cuts on a rune boundary at 64 bytes, so this counts bytes. */
const CATEGORY_ID_MAX_BYTES = 64;

function cutToBytes(s: string, max: number): string {
  const enc = new TextEncoder();
  if (enc.encode(s).length <= max) return s;
  let out = '';
  let used = 0;
  for (const ch of s) {
    const size = enc.encode(ch).length;
    if (used + size > max) break;
    out += ch;
    used += size;
  }
  return out;
}

function categoryID(raw: string): string {
  let out = '';
  // A run of non-alphanumerics becomes one dash, and only after something was
  // kept, so no leading or trailing dash appears.
  let pending = false;
  for (const ch of raw.trim().toLowerCase()) {
    if (LETTER_OR_DIGIT.test(ch)) {
      if (pending && out !== '') out += '-';
      pending = false;
      out += ch;
    } else {
      pending = true;
    }
  }
  // The cut can land on a dash, so trim again, as the Go side does.
  return cutToBytes(out, CATEGORY_ID_MAX_BYTES).replace(/-+$/, '');
}

/**
 * keyOf is the key a row will be stored under, folded like CategoryID, so
 * "Serien" and "serien" count as one drawer. The server refuses such a pair.
 */
const keyOf = (c: Category): string => (c.id.trim() !== '' ? categoryID(c.id) : categoryID(c.name ?? ''));

/**
 * The segment id for "no opinion". The leading space keeps it apart from any
 * id the server may send; it never leaves this file.
 */
const INHERIT = ' inherit';
const OWN = 'own';

// React keys for the rows: a new row's id is empty and array indexes shift on
// removal.
let uidSeq = 0;
const nextUid = () => `cat-${(uidSeq += 1)}`;

/** A new row; the server derives its id on save. */
const emptyCategory = (): Category => ({ id: '' });

/**
 * usePriorityTabs builds the priority ladder from priorityChoices(), highest
 * first, and stays empty until the server answers.
 */
function usePriorityTabs(): { id: string; label: string }[] {
  const { t } = useT();
  const [choices, setChoices] = useState<PriorityChoice[]>([]);
  useEffect(() => {
    let live = true;
    void priorityChoices().then(
      (p) => {
        if (live) setChoices(p);
      },
      () => {
        /* the strip stays empty rather than offering a guess */
      },
    );
    return () => {
      live = false;
    };
  }, []);
  return choices
    .slice()
    .reverse()
    .map((p) => ({ id: String(p.value), label: t(`priority.${p.id}` as TranslationKey) }));
}

/**
 * useMediaHooks fetches the stored media hooks rather than reading the draft,
 * since they save through their own route and one stored a minute ago would be
 * missing from the draft.
 */
function useMediaHooks(): MediaHook[] {
  const [hooks, setHooks] = useState<MediaHook[]>([]);
  useEffect(() => {
    let live = true;
    void fetchMediaHooks().then(
      (list) => {
        if (live) setHooks(list);
      },
      () => {
        /* No picker rather than a guessed one. */
      },
    );
    return () => {
      live = false;
    };
  }, []);
  return hooks;
}

/**
 * useCategoryOptions loads the menus and maxCategories from GET /api/options.
 * The sanitiser drops rows past that ceiling on save, so Add stops there.
 */
function useCategoryOptions(): ApiOptions | null {
  const [options, setOptions] = useState<ApiOptions | null>(null);
  useEffect(() => {
    let live = true;
    fetchOptions().then(
      (o) => live && setOptions(o),
      () => {
        /* Nothing is guessed; see the two call sites below. */
      },
    );
    return () => {
      live = false;
    };
  }, []);
  return options;
}

export function CategoriesCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const options = useCategoryOptions();
  const priorities = usePriorityTabs();
  const hooks = useMediaHooks();
  const listRefused = useFieldError('categories');

  const [openRow, setOpenRow] = useState(-1);
  // A row without a name waits here rather than in the draft (see writePending).
  const [pending, setPending] = useState<Category | null>(null);

  // An older server does not send the field.
  const cats = cfg.categories ?? [];

  const [uids, setUids] = useState<string[]>(() => cats.map(() => nextUid()));
  // The draft can change length elsewhere, so the keys are adjusted during
  // render to stay in step with the rows.
  if (uids.length !== cats.length) {
    setUids(cats.map((_, i) => uids[i] ?? nextUid()));
  }

  // Every write goes through here and spreads cfg.
  const write = (next: Category[], nextUids: string[]) => {
    patch({ categories: next });
    setUids(nextUids);
  };
  const writeRow = (index: number, next: Category) =>
    patch({ categories: cats.map((c, i) => (i === index ? next : c)) });

  const move = (index: number, by: number) => {
    const to = index + by;
    if (to < 0 || to >= cats.length) return;
    const next = [...cats];
    const ids = [...uids];
    [next[index], next[to]] = [next[to], next[index]];
    [ids[index], ids[to]] = [ids[to], ids[index]];
    write(next, ids);
    // The open editor follows its row; the order is what pickers offer.
    if (openRow === index) setOpenRow(to);
    else if (openRow === to) setOpenRow(index);
  };

  const removeAt = (index: number) => {
    // Tasks keep the dead key and behave as untagged until it exists again.
    write(
      cats.filter((_, i) => i !== index),
      uids.filter((_, i) => i !== index),
    );
    setOpenRow(-1);
  };

  const add = () => {
    // A nameless row already waiting is opened instead of adding another.
    if (pending) {
      setOpenRow(cats.length);
      return;
    }
    setPending(emptyCategory());
    setOpenRow(cats.length);
  };

  /**
   * writePending edits the waiting row and moves it into the draft once it has
   * an id or a name. The server refuses the whole document over a row with
   * neither, and the autosave would fire while it is typed.
   */
  const writePending = (next: Category) => {
    if (keyOf(next) === '') {
      setPending(next);
      return;
    }
    setPending(null);
    write([...cats, next], [...uids, nextUid()]);
    setOpenRow(cats.length);
  };

  // Both rows of a repeated key are marked, since the save would be refused.
  const duplicates = new Set<number>();
  const firstSeen = new Map<string, number>();
  cats.forEach((c, i) => {
    const key = keyOf(c);
    if (key === '') return;
    const first = firstSeen.get(key);
    if (first === undefined) firstSeen.set(key, i);
    else {
      duplicates.add(first);
      duplicates.add(i);
    }
  });

  // An unknown ceiling must not block Add.
  const max = options?.maxCategories ?? 0;
  const full = max > 0 && cats.length >= max;

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle
        hint={t('settings.categories.listHint')}
        right={
          <Button icon={<IconPlus width={16} height={16} />} disabled={full} onClick={add}>
            {t('settings.categories.add')}
          </Button>
        }
      >
        {t('settings.categories.listTitle')}
      </SectionTitle>

      {/* Shown at the ceiling, where the disabled Add would read as a fault. */}
      {full && <p className="text-xs text-carbon-textMuted">{t('settings.categories.full', { max })}</p>}
      {listRefused && <p className="text-xs text-statusWarn">{listRefused}</p>}

      {cats.length === 0 && !pending ? (
        // Inside the card rather than an EmptyState, which would hide Add.
        <p className="py-6 text-center text-sm text-carbon-textSub">
          {t('settings.categories.empty')}
          <span className="mt-1 block text-[11px] text-carbon-textMuted">
            {t('settings.categories.emptyHint')}
          </span>
        </p>
      ) : (
        <ul className="flex flex-col">
          {cats.map((cat, i) => (
            <CategoryRow
              key={uids[i] ?? `row-${i}`}
              cat={cat}
              index={i}
              last={i === cats.length - 1 && !pending}
              open={openRow === i}
              duplicate={duplicates.has(i)}
              priorities={priorities}
              collisions={options?.collisionPolicies ?? []}
              hooks={hooks}
              onToggle={() => setOpenRow(openRow === i ? -1 : i)}
              onChange={(next) => writeRow(i, next)}
              onMove={(by) => move(i, by)}
              onRemove={() => removeAt(i)}
            />
          ))}
          {/* Last and not movable until it has a name. */}
          {pending && (
            <CategoryRow
              key="pending"
              cat={pending}
              index={cats.length}
              last
              open={openRow === cats.length}
              duplicate={false}
              priorities={priorities}
              collisions={options?.collisionPolicies ?? []}
              hooks={hooks}
              onToggle={() => setOpenRow(openRow === cats.length ? -1 : cats.length)}
              onChange={writePending}
              onMove={() => {}}
              onRemove={() => {
                setPending(null);
                setOpenRow(-1);
              }}
            />
          )}
        </ul>
      )}
    </Card>
  );
}

/**
 * summarise lists what a drawer sets, in one line under its name. Fields
 * without an opinion are left out.
 */
function summarise(
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string,
  cat: Category,
  priorities: { id: string; label: string }[],
): string {
  const parts: string[] = [];
  const dir = cat.dir?.trim();
  if (dir) parts.push(dir);
  if (cat.priority !== undefined) {
    const step = priorities.find((p) => p.id === String(cat.priority));
    parts.push(`${t('props.priority')}: ${step ? step.label : String(cat.priority)}`);
  }
  if (cat.extract !== undefined) {
    parts.push(`${t('props.autoExtract')}: ${cat.extract ? t('props.on') : t('props.off')}`);
  }
  if (cat.premiumOnly !== undefined) {
    parts.push(`${t('settings.categories.premiumOnly')}: ${cat.premiumOnly ? t('props.on') : t('props.off')}`);
  }
  // fmtSpeed returns '' at 0, the "no opinion" case.
  if (cat.speedLimit) parts.push(fmtSpeed(cat.speedLimit));
  const collision = cat.collision?.trim();
  if (collision) {
    const key = COLLISION_LABEL[collision];
    parts.push(key ? t(key) : collision);
  }
  if (cat.torrentFiles) {
    parts.push(`${t('settings.categories.torrentFiles')}: ${t('settings.categories.torrentFilesOwn')}`);
  }
  return parts.join(' · ');
}

function CategoryRow({
  cat,
  index,
  last,
  open,
  duplicate,
  priorities,
  collisions,
  hooks,
  onToggle,
  onChange,
  onMove,
  onRemove,
}: {
  cat: Category;
  index: number;
  last: boolean;
  open: boolean;
  duplicate: boolean;
  priorities: { id: string; label: string }[];
  collisions: string[];
  hooks: MediaHook[];
  onToggle: () => void;
  onChange: (next: Category) => void;
  onMove: (by: number) => void;
  onRemove: () => void;
}) {
  const { t } = useT();

  const name = cat.name?.trim() ?? '';
  const derived = keyOf(cat);
  const title = name || cat.id.trim() || derived;

  /**
   * "No opinion" deletes the key, since 0 is the middle priority and false a
   * deliberate choice; the Go fields are pointers.
   */
  const setPriority = (p: number | undefined) => {
    const next = { ...cat };
    if (p === undefined) delete next.priority;
    else next.priority = p;
    onChange(next);
  };
  const setExtract = (v: boolean | undefined) => {
    const next = { ...cat };
    if (v === undefined) delete next.extract;
    else next.extract = v;
    onChange(next);
  };
  const setCollision = (v: string) => {
    const next = { ...cat };
    if (v === '') delete next.collision;
    else next.collision = v;
    onChange(next);
  };
  const setPremiumOnly = (v: boolean | undefined) => {
    const next = { ...cat };
    if (v === undefined) delete next.premiumOnly;
    else next.premiumOnly = v;
    onChange(next);
  };
  // An own selection starts empty, which fetches every file: the drawer that
  // wants one is usually the one the Torrents page's skips do not fit.
  const setOwnTorrentFiles = (own: boolean) => {
    const next = { ...cat };
    if (own) next.torrentFiles = { minFileSize: 0, includeFiles: [], excludeFiles: [] };
    else delete next.torrentFiles;
    onChange(next);
  };

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="grid grid-cols-[1fr_auto] items-center gap-3 py-2.5">
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          className="flex min-w-0 items-center gap-3 text-start"
        >
          <span className="glim-num w-5 shrink-0 text-xs text-carbon-textMuted">{index + 1}</span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm text-carbon-text">
              {/* A new row stands under the muted name placeholder. */}
              {title || <span className="text-carbon-textMuted">{t('settings.categories.name')}</span>}
            </span>
            <span className="block truncate text-[11px] text-carbon-textMuted">
              {summarise(t, cat, priorities)}
            </span>
          </span>
        </button>
        {/* `labelled`, so the actions follow the Beschriftung setting; the name
            and summary truncate instead. */}
        <div className="flex items-center gap-1.5">
          <IconBadge
            labelled
            icon={<IconArrowUp width={16} height={16} />}
            hue={index}
            title={t('settings.rules.moveUp')}
            aria-label={t('settings.rules.moveUp')}
            disabled={index === 0}
            onClick={() => onMove(-1)}
          />
          <IconBadge
            labelled
            icon={<IconArrowDown width={16} height={16} />}
            hue={index}
            title={t('settings.rules.moveDown')}
            aria-label={t('settings.rules.moveDown')}
            disabled={last}
            onClick={() => onMove(1)}
          />
          <IconBadge
            labelled
            icon={<IconTrash width={16} height={16} />}
            hue={index}
            title={t('settings.categories.remove')}
            aria-label={t('settings.categories.remove')}
            onClick={onRemove}
          />
        </div>
      </div>

      {/* Shown on a closed row too, since a repeated key blocks the whole save. */}
      {duplicate && !open && (
        <p className="pb-2.5 ps-8 text-xs text-statusWarn">{t('settings.categories.duplicate')}</p>
      )}
      <RowRefusal field={`categories.${index}`} explained={duplicate} className="ps-8" />

      {open && (
        <div className="glim-well mb-3 flex flex-col gap-4 p-4">
          {duplicate && <p className="text-xs text-statusWarn">{t('settings.categories.duplicate')}</p>}

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t('settings.categories.name')} hint={t('settings.categories.nameHint')}>
              <TextInput value={cat.name ?? ''} onChange={(e) => onChange({ ...cat, name: e.target.value })} />
            </Field>
            {/* The placeholder previews the derived key. */}
            <Field label={t('settings.categories.id')} hint={t('settings.categories.idHint')}>
              <TextInput
                dir="ltr"
                spellCheck={false}
                value={cat.id}
                placeholder={derived}
                onChange={(e) => onChange({ ...cat, id: e.target.value })}
              />
            </Field>
          </div>

          {/* The shared chooser browses the server and keeps a <jd:...> tail.
              `title`, or the dialog would read "Download folder". */}
          <Field
            label={t('settings.categories.dir')}
            hint={`${t('settings.categories.dirHint')} ${t('settings.pathVars')}`}
          >
            <PathInput
              value={cat.dir ?? ''}
              title={t('settings.categories.dir')}
              onValue={(dir) => onChange({ ...cat, dir })}
            />
          </Field>

          {/* FieldGroup, because a Field's label would pass a click on the
              caption to the first tab. */}
          <div className="grid gap-4 sm:grid-cols-2">
            <FieldGroup label={t('props.priority')} hint={t('settings.categories.priorityHint')}>
              {/* The server's ladder plus "no opinion"; ValidateCategories
                  refuses anything outside -3..3. */}
              <Tabs
                variant="well"
                size="sm"
                label={t('props.priority')}
                active={cat.priority === undefined ? INHERIT : String(cat.priority)}
                onSelect={(id) => setPriority(id === INHERIT ? undefined : Number(id))}
                items={[
                  // Its own word, which the field's hint quotes.
                  { id: INHERIT, label: t('settings.categories.priorityNone') },
                  ...priorities,
                ]}
              />
            </FieldGroup>

            <FieldGroup label={t('props.autoExtract')} hint={t('settings.categories.extractHint')}>
              {/* Three segments, since a switch cannot tell "no opinion" from
                  "keep packed". */}
              <Tabs
                variant="well"
                size="sm"
                label={t('props.autoExtract')}
                active={cat.extract === undefined ? INHERIT : cat.extract ? 'on' : 'off'}
                onSelect={(id) => setExtract(id === INHERIT ? undefined : id === 'on')}
                items={[
                  { id: INHERIT, label: t('props.inherit') },
                  { id: 'on', label: t('props.on') },
                  { id: 'off', label: t('props.off') },
                ]}
              />
            </FieldGroup>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t('settings.categories.speedLimit')} hint={t('settings.categories.speedLimitHint')}>
              <UnitNumberInput
                value={cat.speedLimit ?? 0}
                units={RATE_UNITS}
                onValue={(speedLimit) => onChange({ ...cat, speedLimit })}
              />
            </Field>

            {/* The server's list, which withholds "ask me", since nobody would
                be there to answer. */}
            {collisions.length > 0 && (
              <FieldGroup label={t('settings.categories.collision')} hint={t('settings.categories.collisionHint')}>
                <Tabs
                  variant="well"
                  size="sm"
                  label={t('settings.categories.collision')}
                  active={cat.collision?.trim() ? cat.collision : INHERIT}
                  onSelect={(id) => setCollision(id === INHERIT ? '' : id)}
                  items={[
                    { id: INHERIT, label: t('props.inherit') },
                    ...collisions.map((id) => ({
                      id,
                      label: COLLISION_LABEL[id] ? t(COLLISION_LABEL[id]) : id,
                    })),
                  ]}
                />
              </FieldGroup>
            )}
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <FieldGroup label={t('settings.categories.premiumOnly')} hint={t('settings.categories.premiumOnlyHint')}>
              {/* Three segments for the reason unpacking has them. */}
              <Tabs
                variant="well"
                size="sm"
                label={t('settings.categories.premiumOnly')}
                active={cat.premiumOnly === undefined ? INHERIT : cat.premiumOnly ? 'on' : 'off'}
                onSelect={(id) => setPremiumOnly(id === INHERIT ? undefined : id === 'on')}
                items={[
                  { id: INHERIT, label: t('props.inherit') },
                  { id: 'on', label: t('props.on') },
                  { id: 'off', label: t('props.off') },
                ]}
              />
            </FieldGroup>
          </div>

          {/* The media hook called once a package filed here is in place, by
              id. A select, since the list comes from another page. */}
          <FieldGroup label={t('settings.categories.notify')} hint={t('settings.categories.notifyHint')}>
            {hooks.length === 0 && !cat.notify ? (
              // A sentence instead of an empty select.
              <p className="text-xs text-carbon-textSub">{t('settings.categories.notifyEmpty')}</p>
            ) : (
              <NotifySelect
                value={cat.notify ?? ''}
                hooks={hooks}
                label={t('settings.categories.notify')}
                missingLabel={(id) => t('settings.categories.notifyMissing', { id })}
                noneLabel={t('settings.categories.notifyNone')}
                onChange={(next) => {
                  const patched = { ...cat };
                  // Absent rather than '', which ValidateMediaHooks would refuse.
                  if (next === '') delete patched.notify;
                  else patched.notify = next;
                  onChange(patched);
                }}
              />
            )}
          </FieldGroup>

          {/* Two segments rather than a switch, like the rows above: Inherit
              is "no opinion", not "off". */}
          <FieldGroup label={t('settings.categories.torrentFiles')} hint={t('settings.categories.torrentFilesHint')}>
            <Tabs
              variant="well"
              size="sm"
              label={t('settings.categories.torrentFiles')}
              active={cat.torrentFiles ? OWN : INHERIT}
              onSelect={(id) => setOwnTorrentFiles(id === OWN)}
              items={[
                { id: INHERIT, label: t('props.inherit') },
                { id: OWN, label: t('settings.categories.torrentFilesOwn') },
              ]}
            />
          </FieldGroup>
          {/* A refused pattern shows on the row, since the server files it
              under the category. */}
          {cat.torrentFiles && (
            <FileSelectionFields
              rules={cat.torrentFiles}
              onChange={(torrentFiles) => onChange({ ...cat, torrentFiles })}
            />
          )}
        </div>
      )}
    </li>
  );
}

/**
 * NotifySelect lists a hook id missing from `hooks` as a deleted entry, so a
 * category pointing at a removed address says so instead of showing a bare id.
 */
function NotifySelect({
  value,
  hooks,
  label,
  noneLabel,
  missingLabel,
  onChange,
}: {
  value: string;
  hooks: MediaHook[];
  label: string;
  noneLabel: string;
  missingLabel: (id: string) => string;
  onChange: (next: string) => void;
}) {
  const known = hooks.map((h) => ({ value: h.id, label: h.name || h.id }));
  const options =
    value !== '' && !hooks.some((h) => h.id === value)
      ? [{ value, label: missingLabel(value) }, ...known]
      : known;
  return (
    <Dropdown
      width="widest"
      label={label}
      value={value}
      onChange={onChange}
      options={[{ value: '', label: noneLabel }, ...options]}
    />
  );
}
