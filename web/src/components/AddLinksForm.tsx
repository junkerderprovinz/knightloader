// The add-links form (build-plan.md §8A): the paste box every page already
// had, plus a per-batch destination, priority, unpacking switch, comment and
// the two passwords - a hoster's own and an archive's, two different secrets
// asked by two different parties (see lib/api.ts's TaskOptionsPatch for why
// they are never one field) - and a persisted history of recently used
// destinations.
//
// Extracted out of Collector.tsx rather than left inline, because the
// Collector page also grows a facet sidebar and a stats strip in this same
// wave: one file, two unrelated reasons to edit it, is exactly the collision
// the lane system exists to avoid. Collector.tsx keeps owning `pkg` (it is
// also handed to the container-drop zone) and the toast wording; everything
// else about the form lives here.
//
// THE PRECEDENCE DECISION (§4 conflict 5, §8's Wave 8 amendment): a matching
// Packagizer rule wins over the priority, unpacking switch and comment below
// UNLESS "Overrule" is on - the destination always wins regardless, because a
// folder picked by hand here is not a property the form and a rule are
// contending over. See app.LinkBatchOptions's own comment for the mechanism.
import { useEffect, useState } from 'react';
import {
  addLinksWithOptions,
  priorityChoices,
  type PriorityChoice,
  type Task,
} from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { useUIState } from '../lib/uistate';
import { PathInput } from './FolderPicker';
import { PasteFromClipboardButton } from './PasteFromClipboardButton';
import { Tabs } from './Tabs';
import { Button, Card, Field, FieldGroup, IconBadge, SectionTitle, TextArea, TextInput, ToggleRow } from './ui';
import { IconCollector, IconFolder, IconPlus, IconSettings } from '../lib/icons';

// JD keeps 25; matched rather than picking a new number, because the point of
// this list is "the folder I used last week", and there is no reason this
// app's users would want to remember fewer of them than JD's do.
const DESTINATION_HISTORY_MAX = 25;
const DESTINATION_HISTORY_KEY = 'addLinks.recentDestinations';
const OPTIONS_OPEN_KEY = 'addLinks.optionsOpen';
const OVERRULE_KEY = 'addLinks.overrule';

/**
 * usePriorityTabs is TaskList.tsx's usePriorities, kept as its own small copy
 * rather than imported: the two components sit in different lanes this wave,
 * and fifteen lines duplicated is cheaper than a shared export that makes
 * this file a second writer on TaskList.tsx's own.
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

function pushRecent(list: string[], value: string): string[] {
  const next = [value, ...list.filter((d) => d !== value)];
  return next.slice(0, DESTINATION_HISTORY_MAX);
}

export function AddLinksForm({
  pkg,
  onPkgChange,
  onStaged,
  onChooseFile,
  onFilesDropped,
}: {
  pkg: string;
  onPkgChange: (v: string) => void;
  /** created is exactly what the server staged; submittedCount is how many
   *  distinct URL-shaped lines the box held, for the "N already known" toast. */
  onStaged: (created: Task[], submittedCount: number) => void;
  /** Opens FileDrop's own file picker (jdp: "Dropzone mit Dateiwählen button
   *  neben dem Zum-Sammler-Button") - this form owns none of that logic, it
   *  only renders the trigger beside its own "Add to collector" button. */
  onChooseFile: () => void;
  /** Hands a dropped FILE list to FileDrop's own handling (jdp, 2026-08-24:
   *  "können wir diesen text und card nicht entfernen" - FileDrop no longer
   *  keeps a visible drop target of its own, so this box's own drop target
   *  is now the one place both text AND files can land). */
  onFilesDropped: (files: File[]) => void;
}) {
  const { t } = useT();
  const priorities = usePriorityTabs();

  const [links, setLinks] = useState('');
  const [dragOver, setDragOver] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const [optionsOpen, setOptionsOpen] = useUIState(OPTIONS_OPEN_KEY, false);
  const [recent, setRecent] = useUIState<string[]>(DESTINATION_HISTORY_KEY, []);
  const [overrule, setOverrule] = useUIState(OVERRULE_KEY, false);

  const [dir, setDir] = useState('');
  const [priority, setPriority] = useState<string | null>(null);
  const [autoExtract, setAutoExtract] = useState<'inherit' | 'on' | 'off'>('inherit');
  const [comment, setComment] = useState('');
  const [password, setPassword] = useState('');
  const [downloadPassword, setDownloadPassword] = useState('');

  async function onAdd() {
    if (!links.trim() || busy) return;
    const submittedCount = new Set(
      links
        .split(/[\r\n]+/)
        .map((l) => l.trim())
        .filter((l) => /^https?:\/\//i.test(l)),
    ).size;

    setBusy(true);
    setError('');
    try {
      const created = await addLinksWithOptions(links, {
        package: pkg,
        dir: dir.trim() || undefined,
        password: password.trim() || undefined,
        downloadPassword: downloadPassword.trim() || undefined,
        comment: comment.trim() || undefined,
        priority: priority === null ? undefined : Number(priority),
        autoExtract: autoExtract === 'inherit' ? undefined : autoExtract === 'on',
        overrule: overrule || undefined,
      });
      setLinks('');
      // A comment and a password are specific to what was just pasted, and a
      // password left sitting typed into a box is the wrong default; the
      // destination, priority, unpacking switch and Overrule survive because
      // several batches for the same project in one sitting is the common
      // case and re-picking them every time is the annoying one.
      setComment('');
      setPassword('');
      setDownloadPassword('');
      if (dir.trim()) setRecent(pushRecent(recent, dir.trim()));
      onStaged(created, submittedCount);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  function onDrop(e: React.DragEvent) {
    e.preventDefault();
    setDragOver(false);
    // Files first: a torrent/container file dropped here goes to FileDrop's
    // own handling (jdp: "können wir diesen text und card nicht entfernen" -
    // this box is now the one drop target for both). Text falls through to
    // the plain-link path below; the whole-window drop zone (build-plan.md
    // §8B) is a separate, broader listener elsewhere.
    const files = [...e.dataTransfer.files];
    if (files.length) {
      onFilesDropped(files);
      return;
    }
    const text = e.dataTransfer.getData('text');
    if (text) setLinks((l) => (l ? `${l}\n${text}` : text));
  }

  return (
    <div className="flex h-full flex-col gap-3">
      {/* flex-1 here, not just on the row wrapper in Collector.tsx: a flex
          row's own `items-stretch` only stretches its direct children's
          BOX, not whatever content sits inside them - without this, the
          taller of the three top-row cards (jdp, 2026-08-24: "alle drei
          card sollen immer gleich hoch sein") left the other two visibly
          shorter, with invisible empty space below them instead of a
          taller card. flex-1 on the visible card itself, in an h-full
          flex-col parent, is what actually grows the card. */}
      <div className="glim-card flex flex-1 flex-col p-0 overflow-hidden">
        <div className="px-4 pt-3">
          <SectionTitle hue={0}>{t('collector.addTitle')}</SectionTitle>
        </div>
        <div
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={onDrop}
          className={`relative m-3 flex-1 rounded-[var(--radius-control)] transition-colors ${
            dragOver ? 'bg-accentSoft shadow-[0_0_0_2px_var(--focus-ring)]' : 'bg-carbon-surface2'
          }`}
        >
          <textarea
            dir="ltr"
            placeholder={t('collector.placeholder')}
            rows={4}
            value={links}
            onChange={(e) => setLinks(e.target.value)}
            onKeyDown={(e) => {
              if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') void onAdd();
            }}
            className="h-full min-h-[6rem] w-full resize-y rounded-[var(--radius-control)] bg-transparent px-4 py-3 text-sm text-carbon-text placeholder:text-carbon-textMuted outline-none"
          />
          {dragOver && (
            <div className="pointer-events-none absolute inset-0 grid place-items-center rounded-[var(--radius-control)]">
              <span className="flex items-center gap-2 text-sm font-medium text-accent">
                <IconCollector width={18} height={18} />
                {t('collector.add')}
              </span>
            </div>
          )}
        </div>
        <div className="flex items-center gap-3 px-4 pb-4">
          {/* Square glyph badges, not text buttons (jdp, 2026-08-24: "Optionen
              soll ein quadratisches badge mit zahnrad sein, ordner hinzufügen
              ein quadratisches badge mit ordner symbol und zum sammler
              hinzufügen ein quadratisches badge mit plus"). "Paket (optional)"
              moved into the Optionen panel below instead of sitting here
              always-visible - it is exactly the kind of per-batch detail the
              rest of that panel already groups. */}
          <IconBadge
            icon={<IconSettings width={16} height={16} />}
            aria-label={t('collector.options')}
            aria-expanded={optionsOpen}
            onClick={() => setOptionsOpen(!optionsOpen)}
          />
          <span className="flex-1" />
          <PasteFromClipboardButton pkg={pkg} />
          {/* Opens FileDrop's picker (jdp: "Dropzone mit Dateiwählen button
              neben dem Zum-Sammler-Button") - the file-intake trigger sits
              beside the link-intake one instead of in its own row below. */}
          <IconBadge icon={<IconFolder width={16} height={16} />} aria-label={t('container.choose')} onClick={onChooseFile} />
          <IconBadge
            icon={<IconPlus width={16} height={16} />}
            aria-label={t('collector.add')}
            className="bg-accent text-accentContrast hover:brightness-110"
            onClick={() => void onAdd()}
            disabled={!links.trim() || busy}
          />
        </div>
      </div>

      {optionsOpen && (
        <Card className="flex flex-col gap-4">
          <Field label={t('collector.package')}>
            <TextInput value={pkg} onChange={(e) => onPkgChange(e.target.value)} className="max-w-xs" />
          </Field>
          <Field
            label={t('collector.destination')}
            hint={`${t('settings.downloadDirHint')} ${t('settings.pathVars')}`}
          >
            <PathInput
              value={dir}
              placeholder="/downloads"
              title={t('collector.destination')}
              onValue={setDir}
            />
          </Field>
          {recent.length > 0 && (
            <div className="flex flex-col gap-1.5">
              <span className="glim-eyebrow text-xs text-carbon-textSub">
                {t('collector.destinationRecent')}
              </span>
              <div dir="ltr" className="flex flex-wrap gap-1.5">
                {recent.map((d) => (
                  <Button
                    key={d}
                    type="button"
                    kind="ghost"
                    className="max-w-[220px] truncate px-2 py-1 text-xs"
                    title={d}
                    onClick={() => setDir(d)}
                  >
                    {d}
                  </Button>
                ))}
              </div>
            </div>
          )}

          <FieldGroup label={t('props.priority')} hint={t('props.priorityHint')}>
            <Tabs
              size="sm"
              label={t('props.priority')}
              active={priority}
              onSelect={setPriority}
              items={priorities.map((p) => ({ id: p.id, label: p.label }))}
            />
          </FieldGroup>

          <FieldGroup label={t('props.autoExtract')} hint={t('props.autoExtractHint')}>
            <Tabs
              size="sm"
              label={t('props.autoExtract')}
              active={autoExtract}
              onSelect={(id) => setAutoExtract(id as 'inherit' | 'on' | 'off')}
              items={[
                { id: 'inherit', label: t('props.inherit') },
                { id: 'on', label: t('props.on') },
                { id: 'off', label: t('props.off') },
              ]}
            />
          </FieldGroup>

          <Field label={t('props.comment')} hint={t('props.commentHint')}>
            <TextArea rows={2} value={comment} onChange={(e) => setComment(e.target.value)} />
          </Field>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label={t('task.password')} hint={t('collector.archivePasswordHint')}>
              <TextInput value={password} onChange={(e) => setPassword(e.target.value)} />
            </Field>
            <Field label={t('collector.linkPassword')} hint={t('collector.linkPasswordHint')}>
              <TextInput value={downloadPassword} onChange={(e) => setDownloadPassword(e.target.value)} />
            </Field>
          </div>

          <ToggleRow
            checked={overrule}
            onChange={setOverrule}
            label={t('collector.overrule')}
            hint={t('collector.overruleHint')}
          />

          {error && <p className="text-sm text-statusFail">{error}</p>}
        </Card>
      )}
    </div>
  );
}
