import { useEffect, useState } from 'react';
import {
  Button,
  Card,
  Field,
  FieldGroup,
  IconBadge,
  NumberInput,
  SectionTitle,
  TextInput,
} from '../../components/ui';
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
import { fmtSpeed } from '../../lib/format';
import { IconArrowDown, IconArrowUp, IconPlus, IconTrash } from '../../lib/icons';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useDraft } from './context';

/**
 * The category table: named drawers, each one a folder plus four defaults, that
 * a download can be filed into by name instead of by answering the same five
 * questions for every batch.
 *
 * Four things here are decisions rather than layout.
 *
 * THE SPEED LIMIT IS THE ONE FIELD THAT STILL CHANGES NOTHING, and its hint
 * says so. The folder, the unpacking switch, the collision rule and the queue
 * position are all live: dirFor calls CategoryDir, extractWanted calls
 * ExtractFor, the dispatcher and the last move both call CollisionFor, and
 * packagize calls PriorityFor once, where the task is made.
 *
 * SpeedLimitFor still has no caller and cannot get one by wiring: internal/
 * throttle is ONE limiter for the whole app, split between the engine, JD and
 * yt-dlp by measured demand, and engine.Job has no rate field at all. Honouring
 * a per-drawer limit means a bandwidth scheduler, not a line. The field goes on
 * round-tripping so nobody's settings.json changes shape later.
 *
 * That sentence has to come OUT of its hint in the same commit that makes it
 * false, in en.ts, de.ts and the other 40 locale files, or the page starts
 * lying in the other direction. Three of these four sentences were removed that
 * way when their resolvers were wired; this is the one that is still true.
 *
 * A PACKAGIZER RULE IS WHAT FILES A TASK INTO A DRAWER, and it is still the
 * only writer of Task.Category. The rule editor can now set it: the grammar
 * describes a `category` action and ActionField renders a picker for it. Both
 * halves had to land together, because ActionField falls through to the
 * accept/reject control for a Kind it does not know, so the grammar line alone
 * would have put a reject switch on the Packagizer tab wired to Action.Reject.
 * A Go test reads this file and refuses to let the grammar entry ship without
 * that branch.
 *
 * What is still missing is a way to file a link by hand: no intake route
 * accepts a category, so there is no picker at add-links time and no facet in
 * the list.
 *
 * THE SERVER OWNS THE KEY. CategoryID derives an id from the name once, on save,
 * and never re-derives it, which is what makes renaming a category free. This
 * page therefore never writes a derived id into the draft; it only PREVIEWS one,
 * in the placeholder of the empty key box, so that what will be stored is
 * visible before saving. Changing an existing key is not a rename: it orphans
 * every download already filed under the old one.
 *
 * THE SAVE IS ALL-OR-NOTHING. ValidateCategories refuses the WHOLE settings
 * document, not the offending row - a repeated key, a row with neither key nor
 * name, an out-of-range priority, a negative speed limit, an unknown collision
 * word. The controls below are shaped so that only the first two are reachable
 * at all: the two strips offer exactly what the server accepts, and the
 * duplicate key is drawn on both rows before the save bar is pressed.
 *
 * ONE INTERLOCK IS MISSING ON PURPOSE, and the next wave owns it.
 * ValidateCategories also refuses the document when a Packagizer rule files
 * links into a category that is not in the table, and this page cannot say so:
 * the string catalogue has no line for "these rules file into this drawer" or
 * for "removing this row will have the save refused", and the locale files were
 * closed while this page was written. Nothing reachable can produce such a rule
 * yet either - RuleAction carries no category and the grammar has no category
 * action - so the only way to that refusal today is a hand-edited settings.json
 * or an imported rule set. The two strings, and the check that draws them,
 * belong in the same commit as the rule action itself.
 */

/**
 * Mirrors settings.CategoryID (internal/settings/settings_categories.go), which
 * is the ONLY place that decides what a key looks like.
 *
 * It is here to show and to warn, never to write: the preview goes in the empty
 * key box's placeholder and the fold is what the duplicate check compares. A
 * copy that wrote its answer into the draft would freeze a spelling the server
 * did not choose, and would go on freezing it after the Go rule changed.
 */
const LETTER_OR_DIGIT = /[\p{L}\p{N}]/u;

/** trimTo(id, 64): the Go side cuts on a rune boundary, so this counts bytes. */
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
  // A run of anything that is not a letter or a digit becomes ONE dash, and only
  // once something has already been kept - which is how a leading dash never
  // appears and a trailing one is never emitted in the first place.
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
  // The cut can land on a dash, so trim once more afterwards, as the Go side does.
  return cutToBytes(out, CATEGORY_ID_MAX_BYTES).replace(/-+$/, '');
}

/**
 * The key this row will be stored under: its own if it has one, otherwise the
 * one the name will produce on save.
 *
 * A typed key is folded too. The server stores what CategoryID returns, so
 * "Serien" and "serien" are one drawer and not two - and two rows that fold
 * together are REFUSED at save rather than merged, which is why this is checked
 * before the save bar rather than reported by it.
 */
const keyOf = (c: Category): string => (c.id.trim() !== '' ? categoryID(c.id) : categoryID(c.name ?? ''));

// A server value is looked up rather than switched on, and a policy with no
// string of its own falls back to its raw id: a word a later build adds shows up
// in the strip under its own name instead of as a blank segment. The three ids
// are the archive page's ids and the same three words, so they share its strings
// - one idea, one string.
const COLLISION_LABEL: Partial<Record<string, TranslationKey>> = {
  overwrite: 'settings.archives.collision.overwrite',
  rename: 'settings.archives.collision.rename',
  skip: 'settings.archives.collision.skip',
};

/**
 * The segment that writes "no opinion".
 *
 * The leading space is the point: this id has to be one no server-sent id can
 * ever equal, and neither a collision policy nor a priority step can carry a
 * space. A plain "inherit" would be a word the server is free to start sending
 * one day, and the two would then be one segment meaning two things - which is
 * the very distinction this control exists to keep. It is a control value only:
 * what absent SENDS is an absent field, or the empty string for the collision
 * rule, and neither ever leaves this file wearing this id.
 */
const INHERIT = ' inherit';

// React keys for the rows.
//
// Not `id`: a fresh row's id is empty BY DESIGN, so two new rows would share the
// key "" and React would treat one as the other. Not the array index either:
// removing row 2 hands row 3's values to the element that was row 2, and a text
// box under the cursor would keep focus while its contents changed underneath.
let uidSeq = 0;
const nextUid = () => `cat-${(uidSeq += 1)}`;

/** A new row. The id is empty on purpose - see the file's own note. */
const emptyCategory = (): Category => ({ id: '' });

/**
 * The priority ladder, as tab items, highest first.
 *
 * From priorityChoices() - the shared, memoised fetch - and never a list written
 * out here: a ladder typed into a page is how the app once offered five steps
 * against the server's seven. The strip stays empty until it answers rather than
 * offering a guess, and the row summary then falls back to the bare number.
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
 * The menus and the ceiling come from GET /api/options.
 *
 * maxCategories is served for the same reason collisionPolicies is: a 64 typed
 * into this file is a second copy of settings.MaxCategories, and the copy is the
 * one that goes stale in silence. It matters because the sanitiser keeps the
 * first N rows IN ORDER and drops the rest without a word - so an Add button
 * that stayed enabled past the limit would let somebody write rows that vanish
 * on save.
 */
/**
 * The stored addresses a drawer can call once a package filed in it has
 * finished.
 *
 * FETCHED HERE AND NOT READ OFF THE DRAFT, although settings.mediaHooks carries
 * the same rows. The card that stores them (Downloads page) writes through its
 * own route rather than through the shared draft, because half of what it saves
 * is a sealed credential - so the draft this shell loaded at mount does not know
 * about an address stored a minute ago, and a picker built from it would leave
 * somebody unable to select the address they had just created until they
 * reloaded the page.
 *
 * An empty list is not an error: it is the state every install is in until
 * somebody stores one, and the row below says so in words rather than offering
 * an empty menu.
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
        /* No picker rather than a guessed one - see the note above. */
      },
    );
    return () => {
      live = false;
    };
  }, []);
  return hooks;
}

function useCategoryOptions(): ApiOptions | null {
  const [options, setOptions] = useState<ApiOptions | null>(null);
  useEffect(() => {
    let live = true;
    fetchOptions().then(
      (o) => live && setOptions(o),
      () => {
        /* nothing here is guessed: see the two call sites below */
      },
    );
    return () => {
      live = false;
    };
  }, []);
  return options;
}

export function Categories() {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const options = useCategoryOptions();
  const priorities = usePriorityTabs();
  const hooks = useMediaHooks();

  const [openRow, setOpenRow] = useState(-1);
  // A row nobody has named yet lives HERE and not in the shared draft. See
  // writePending below for why, and HostRules.tsx for the same arrangement made
  // for the same reason on the page next door.
  const [pending, setPending] = useState<Category | null>(null);

  // Through a fallback: the server always sends the key (no omitempty, on
  // purpose), but an older one predates the field entirely and this page must
  // draw an empty table rather than throw and take the whole settings shell
  // down with it.
  const cats = cfg.categories ?? [];

  const [uids, setUids] = useState<string[]>(() => cats.map(() => nextUid()));
  // The draft can change length without this page doing it: the save bar reloads
  // the document and the advanced key table can replace it wholesale. Adjusted
  // during render rather than in an effect, so the keys and the rows are never
  // out of step in a painted frame.
  if (uids.length !== cats.length) {
    setUids(cats.map((_, i) => uids[i] ?? nextUid()));
  }

  // The whole page writes through this one call. Never mutate a row and never
  // rebuild cfg: the draft is a subset of a document the server owns, and one
  // save carries this page's edits together with every other page's.
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
    // The open editor follows the row it belongs to. Left alone, it would go on
    // writing into whatever row now sits at that index - the order is not
    // cosmetic here, it is the order a picker offers and the order the sanitiser
    // keeps a duplicate from.
    if (openRow === index) setOpenRow(to);
    else if (openRow === to) setOpenRow(index);
  };

  const removeAt = (index: number) => {
    // Safe for downloads already filed here: the dead key is deliberately KEPT
    // on the task, those downloads behave like untagged ones, and they come back
    // the day a category with that key exists again.
    write(
      cats.filter((_, i) => i !== index),
      uids.filter((_, i) => i !== index),
    );
    setOpenRow(-1);
  };

  const add = () => {
    // A second press while one unnamed row is still waiting would stack a
    // second one that also cannot be stored. Open the one already there.
    if (pending) {
      setOpenRow(cats.length);
      return;
    }
    setPending(emptyCategory());
    setOpenRow(cats.length);
  };

  /**
   * The waiting row's own writer, and the moment it stops waiting.
   *
   * A category with neither an id nor a name is refused by the server for the
   * whole document ("nothing could ever be filed in it"), and this shell saves
   * 600ms after every edit. So a brand-new empty row written straight into the
   * shared draft turns a press of Add into a red refusal about a row nobody has
   * finished typing - and it takes every OTHER page's pending edit down with
   * it, because one document is saved for all of them.
   *
   * It joins the real list at exactly the moment it gains an identity, which is
   * the same moment the server would accept it. Promoting on that condition and
   * not on blur is deliberate: blur is a gesture somebody can skip.
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

  // Both sides folded the way CategoryID folds them, and BOTH rows marked: the
  // server refuses the whole document over a repeat rather than merging the two,
  // so the answer has to be on screen before the save bar is pressed.
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

  // Unknown until the options answer, and an unknown ceiling must not block the
  // one button that gets anybody out of the empty state. An older server that
  // does not serve the number is the same case.
  const max = options?.maxCategories ?? 0;
  const full = max > 0 && cats.length >= max;

  return (
    <div className="flex flex-col gap-10">
      <Card hue={0} className="flex flex-col gap-4">
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

        {/* A fact about the table, in the shape Connections.tsx's StateLine
            uses. Not an explanation - those live behind the (i) on the title -
            and drawn only at the ceiling, where the disabled button on its own
            would read as a fault. */}
        {full && <p className="text-xs text-carbon-textMuted">{t('settings.categories.full', { max })}</p>}

        {cats.length === 0 && !pending ? (
          // Inside the card, not instead of it: Add is the way out of this state,
          // and swapping the card for an EmptyState would take it off the page.
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
            {/* Always last, and deliberately not movable: it has no place in
                the order until it has a name, and the order is what a picker
                offers. */}
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
    </div>
  );
}

/**
 * What this drawer actually sets, in one line under its name, so a table of
 * twenty can be read without opening each one.
 *
 * Only what has an opinion appears. A field left empty is not "off", it is the
 * category declining to answer, and printing "Priority: none" for it would say
 * the opposite of what the row does.
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
  // fmtSpeed answers '' at 0, which is exactly the "no opinion" case this line
  // must stay silent about.
  if (cat.speedLimit) parts.push(fmtSpeed(cat.speedLimit));
  const collision = cat.collision?.trim();
  if (collision) {
    const key = COLLISION_LABEL[collision];
    parts.push(key ? t(key) : collision);
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
   * An absent value is not the same value as a zero or a false one, which is
   * the whole reason the Go fields are pointers: 0 is the MIDDLE priority and
   * false is a drawer that deliberately keeps archives packed. So "no opinion"
   * deletes the key rather than writing undefined into it - the two serialise
   * the same, but only one of them is still absent to anything that reads the
   * object before it is sent.
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

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="group grid grid-cols-[1fr_auto] items-center gap-3 py-2.5">
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          className="flex min-w-0 items-center gap-3 text-left"
        >
          <span className="glim-num w-5 shrink-0 text-xs text-carbon-textMuted">{index + 1}</span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm text-carbon-text">
              {/* A row that has neither a name nor a key yet stands under the
                  name of the thing it is missing, drawn in the muted ink a
                  placeholder is drawn in. It is the row somebody has just added
                  and is already editing, not a row in a bad state. */}
              {title || <span className="text-carbon-textMuted">{t('settings.categories.name')}</span>}
            </span>
            <span className="block truncate text-[11px] text-carbon-textMuted">
              {summarise(t, cat, priorities)}
            </span>
          </span>
        </button>
        <div className="flex items-center gap-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          <IconBadge
            icon={<IconArrowUp width={14} height={14} />}
            hue={index}
            title={t('settings.rules.moveUp')}
            aria-label={t('settings.rules.moveUp')}
            disabled={index === 0}
            onClick={() => onMove(-1)}
          />
          <IconBadge
            icon={<IconArrowDown width={14} height={14} />}
            hue={index}
            title={t('settings.rules.moveDown')}
            aria-label={t('settings.rules.moveDown')}
            disabled={last}
            onClick={() => onMove(1)}
          />
          <IconBadge
            kind="danger"
            icon={<IconTrash width={14} height={14} />}
            hue={index}
            title={t('settings.categories.remove')}
            aria-label={t('settings.categories.remove')}
            onClick={onRemove}
          />
        </div>
      </div>

      {/* Drawn on the row whether or not it is open, because a save refused over
          a repeated key is refused for the WHOLE document, including edits made
          on other pages in the same draft - so it has to be findable in a list of
          twenty without opening each one. */}
      {duplicate && !open && (
        <p className="pb-2.5 ps-8 text-xs text-statusWarn">{t('settings.categories.duplicate')}</p>
      )}

      {open && (
        <div className="glim-well mb-3 flex flex-col gap-4 p-4">
          {duplicate && <p className="text-xs text-statusWarn">{t('settings.categories.duplicate')}</p>}

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t('settings.categories.name')} hint={t('settings.categories.nameHint')}>
              <TextInput value={cat.name ?? ''} onChange={(e) => onChange({ ...cat, name: e.target.value })} />
            </Field>
            {/* The placeholder is the preview: an empty box on a new row is
                CORRECT, and this is where what will be stored becomes visible
                without this page ever writing it into the draft. dir="ltr" for
                the same reason a path is: a key is read left to right whatever
                the interface language does. */}
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

          {/* The shared chooser and not a bare box: it browses the SERVER, which
              is the only machine that knows what is mounted where, and picking a
              folder replaces only the fixed part so a <jd:...> tail survives.
              `title` because the dialog would otherwise be headed "Download
              folder", which is the one folder this field is NOT. */}
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

          {/* FieldGroup and not Field for both strips: a Field is a <label>, and
              a label hands a click on its caption to the first control inside it
              - so reading the word "Priority" would set a priority. */}
          <div className="grid gap-4 sm:grid-cols-2">
            <FieldGroup label={t('props.priority')} hint={t('settings.categories.priorityHint')}>
              <div className="overflow-x-auto">
                {/* Eight segments and not seven: the ladder the server serves,
                    plus the one that means no opinion. The range is the queue's
                    own -3..3 and the strip offers nothing outside it, because
                    ValidateCategories REFUSES an out-of-range priority before
                    the clamp is ever reached. */}
                <Tabs
                  variant="well"
                  size="sm"
                  className="w-fit"
                  label={t('props.priority')}
                  active={cat.priority === undefined ? INHERIT : String(cat.priority)}
                  onSelect={(id) => setPriority(id === INHERIT ? undefined : Number(id))}
                  items={[
                    // Its OWN word and not props.inherit, which the two strips
                    // below still use: this field's hint argues at length about
                    // the difference between "no opinion" and "default", naming
                    // both in quotes, and a segment reading "Inherit" made the
                    // explanation point at a word that was not on the screen.
                    // Extract and collision carry no such sentence, so "Inherit"
                    // still matches what their own hints say.
                    { id: INHERIT, label: t('settings.categories.priorityNone') },
                    ...priorities,
                  ]}
                />
              </div>
            </FieldGroup>

            <FieldGroup label={t('props.autoExtract')} hint={t('settings.categories.extractHint')}>
              <div className="overflow-x-auto">
                {/* Three segments and never a toggle: a two-state switch cannot
                    tell "no opinion" from "deliberately packed", and the second
                    one has to survive a global setting that says unpack. */}
                <Tabs
                  variant="well"
                  size="sm"
                  className="w-fit"
                  label={t('props.autoExtract')}
                  active={cat.extract === undefined ? INHERIT : cat.extract ? 'on' : 'off'}
                  onSelect={(id) => setExtract(id === INHERIT ? undefined : id === 'on')}
                  items={[
                    { id: INHERIT, label: t('props.inherit') },
                    { id: 'on', label: t('props.on') },
                    { id: 'off', label: t('props.off') },
                  ]}
                />
              </div>
            </FieldGroup>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            {/* Bytes per second in the document, KiB on screen, the same
                arithmetic the instance-wide limit uses. No upper bound, because
                the Go side has none; 0 is not "off" here, it is this category
                declining to answer, and the label says so. */}
            <Field label={t('settings.categories.speedLimit')} hint={t('settings.categories.speedLimitHint')}>
              <NumberInput
                value={Math.round((cat.speedLimit ?? 0) / 1024)}
                min={0}
                step={256}
                onValue={(v) => onChange({ ...cat, speedLimit: Math.max(0, v) * 1024 })}
              />
            </Field>

            {/* Built from the list the server sends, never from one written out
                here: it already withholds "ask me" (nobody would be there to
                answer, and the download would sit in the queue for ever), and
                normalizeCategoryCollision folds a word it does not know to
                empty rather than to a policy - so a strip built from that list
                can never trip the refusal. */}
            {collisions.length > 0 && (
              <FieldGroup label={t('settings.categories.collision')} hint={t('settings.categories.collisionHint')}>
                <div className="overflow-x-auto">
                  <Tabs
                    variant="well"
                    size="sm"
                    className="w-fit"
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
                </div>
              </FieldGroup>
            )}
          </div>

          {/* The one field on this row that reaches outside the download
              folder: which stored address is called once a package filed here
              has finished AND its files have been moved into place. It is a
              REFERENCE by id, like everything else on a drawer, so an address
              edited on the Downloads page reaches every drawer pointing at it.

              A <select> and not a tab strip: the list is as long as somebody's
              media servers, it is the only control on this row whose entries
              come from another page, and a strip of five would wrap. */}
          <FieldGroup label={t('settings.categories.notify')} hint={t('settings.categories.notifyHint')}>
            {hooks.length === 0 && !cat.notify ? (
              // A fact and a way out, not an empty menu. An empty <select>
              // would look like a control that is broken rather than one with
              // nothing to offer yet.
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
                  // Absent and never '': an empty string is a real value the
                  // server would have to weigh, and ValidateMediaHooks would
                  // refuse a drawer for calling an address named "".
                  if (next === '') delete patched.notify;
                  else patched.notify = next;
                  onChange(patched);
                }}
              />
            )}
          </FieldGroup>
        </div>
      )}
    </li>
  );
}

/**
 * The drawer's own address picker.
 *
 * The one thing it has to get right is an id THIS TABLE NO LONGER HOLDS: a
 * drawer written before an address was deleted, or a settings.json carried over
 * from another instance. A <select> whose value is not among its options renders
 * the FIRST option, so leaving that id out would show the drawer as calling some
 * other address and then rewrite it to that address the next time anything on
 * the form changed, without anybody touching this box. The same trap
 * RuleEditor's category picker documents, solved the same way: the dead id is
 * offered, marked as deleted.
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
    <select
      // aria-label and not a wrapping <label>: FieldGroup is a plain div for
      // exactly this reason, and naming the control twice would announce the
      // caption twice.
      aria-label={label}
      value={value}
      dir="ltr"
      onChange={(e) => onChange(e.target.value)}
      className="glim-select w-fit appearance-none rounded-[var(--radius-control)] bg-carbon-surface2 px-2.5 py-2 pe-6
        text-sm text-carbon-text outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
    >
      {[{ value: '', label: noneLabel }, ...options].map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}
