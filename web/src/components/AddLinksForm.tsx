// The add-links form: the paste box plus per-batch destination, priority,
// unpacking, comment, the archive and hoster passwords (two different
// secrets, see TaskOptionsPatch) and a history of recent destinations.
//
// A matching Packagizer rule wins over priority, unpacking and comment unless
// "Overrule" is on; a destination picked here always wins (see
// app.LinkBatchOptions).
import { useEffect, useState, type CSSProperties } from 'react';
import { hueVars } from '../lib/appearance';
import {
  addLinksWithOptions,
  priorityChoices,
  type PriorityChoice,
  type Task,
} from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { useUIState } from '../lib/uistate';
import { PathInput } from './FolderPicker';
import { PasteFromClipboardButton } from './PasteFromClipboardButton';
import { LinkIntakeButtons } from './LinkIntakeButtons';
import { Tabs } from './Tabs';
import { Button, Card, Field, FieldGroup, IconBadge, SectionTitle, TextArea, TextInput, ToggleRow } from './ui';
import { IconCollector, IconFolder, IconPlus, IconSettings } from '../lib/icons';

// The same number JD keeps.
const DESTINATION_HISTORY_MAX = 25;
const DESTINATION_HISTORY_KEY = 'addLinks.recentDestinations';
const OPTIONS_OPEN_KEY = 'addLinks.optionsOpen';
const OVERRULE_KEY = 'addLinks.overrule';

// usePriorityTabs mirrors TaskList.tsx's usePriorities.
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
        // The strip stays empty rather than guessing.
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
  footer,
}: {
  pkg: string;
  onPkgChange: (v: string) => void;
  /** `created` is what the server staged; `submittedCount` is how many distinct
   *  URL lines the box held, for the "already known" toast. */
  onStaged: (created: Task[], submittedCount: number) => void;
  /** Opens FileDrop's file picker. */
  onChooseFile: () => void;
  /** Hands dropped files to FileDrop, since this box is the only drop target. */
  onFilesDropped: (files: File[]) => void;
  /** FileDrop's output, rendered inside this card beside the drop target. */
  footer?: React.ReactNode;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const priorities = usePriorityTabs();

  const [links, setLinks] = useState('');
  const [dragOver, setDragOver] = useState(false);
  const [busy, setBusy] = useState(false);
  // The add badge's key, bumped on failure so .glim-shake replays on a repeat.
  const [shake, setShake] = useState(0);

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
      // Comment and passwords belong to this batch; destination, priority,
      // unpacking and Overrule carry over to the next one.
      setComment('');
      setPassword('');
      setDownloadPassword('');
      if (dir.trim()) setRecent(pushRecent(recent, dir.trim()));
      onStaged(created, submittedCount);
    } catch (e) {
      toast(t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail', 'action-failed');
      setShake((n) => n + 1);
    } finally {
      setBusy(false);
    }
  }

  function onDrop(e: React.DragEvent) {
    e.preventDefault();
    setDragOver(false);
    // Files go to FileDrop; text is appended to the box.
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
      {/* flex-1 on the card itself so the row's cards match in height; the
          parent's items-stretch only reaches this wrapper. No overflow-hidden,
          which would clip SectionTitle's pill over the top edge. .glim-hue
          needs the hueVars style beside it. */}
      <div className="glim-card glim-hue flex flex-1 flex-col p-0" style={hueVars(0) as CSSProperties}>
        <div className="px-4 pt-3">
          <SectionTitle>{t('collector.addTitle')}</SectionTitle>
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
              <span className="flex items-center gap-2 text-sm font-medium text-accentInk">
                <IconCollector width={18} height={18} />
                {t('collector.add')}
              </span>
            </div>
          )}
        </div>
        {/* With labels on, this row is wider than the card, so it wraps, and the
            trailing actions wrap as one group pushed over by ms-auto. A flex-1
            spacer would drop everything after it to a line of its own. */}
        <div className="flex flex-wrap items-center gap-3 px-4 pb-4">
          <IconBadge
            labelled
            icon={<IconSettings width={16} height={16} />}
            hue={0}
            title={t('collector.options')}
            aria-label={t('collector.options')}
            aria-expanded={optionsOpen}
            onClick={() => setOptionsOpen(!optionsOpen)}
          />
          {/* Modes, so they sit with Options rather than with the actions. */}
          <LinkIntakeButtons />
          {/* Wraps inside itself too, since on a narrow card these three alone
              are wider than the card. */}
          <div className="ms-auto flex flex-wrap items-center justify-end gap-3">
            <PasteFromClipboardButton pkg={pkg} />
            <IconBadge
              labelled
              icon={<IconFolder width={16} height={16} />}
              hue={1}
              title={t('container.choose')}
              aria-label={t('container.choose')}
              onClick={onChooseFile}
            />
            <IconBadge
              key={shake}
              labelled
              icon={<IconPlus width={16} height={16} />}
              hue={2}
              title={t('collector.add')}
              aria-label={t('collector.add')}
              className={`bg-accent text-accentContrast hover:brightness-110${shake ? ' glim-shake' : ''}`}
              onClick={() => void onAdd()}
              disabled={!links.trim() || busy}
            />
          </div>
        </div>
        {footer && <div className="flex flex-col gap-1.5 px-4 pb-4">{footer}</div>}
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
              <span className="glim-eyebrow text-carbon-textSub">
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
        </Card>
      )}
    </div>
  );
}
