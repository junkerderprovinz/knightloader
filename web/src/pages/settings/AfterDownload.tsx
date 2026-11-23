import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { ArrowLoopMark } from '../../components/ArrowLoopMark';
import { Button, FIELD_TRIGGER, Modal, TextInput } from '../../components/ui';
import {
  ApiError,
  createAfterDownloadJob,
  fetchAfterDownload,
  fetchAfterDownloadDestinations,
  type AfterDownloadDestination,
  type AfterDownloadDirection,
  type AfterDownloadEngine,
  type AfterDownloadJob,
  type AfterDownloadMode,
  type AfterDownloadPick,
  type AfterDownloadResult,
  type AfterDownloadState,
  type Category,
} from '../../lib/api';
import { fmtDate } from '../../lib/format';
import {
  IconAdd,
  IconCheck,
  IconClose,
  IconCopy,
  IconDisc,
  IconExpand,
  IconFolder,
  IconGlobe,
  IconMove,
  IconNetwork,
  IconPause,
  IconStopMark,
  IconSync,
} from '../../lib/icons';
import { useT, type TranslationKey } from '../../lib/i18n';
import { SettingRow } from './controls';

/**
 * The After download station of a category: the ArrowLoop job started once a
 * package filed there has landed. The pick is part of the category, so it
 * saves with the page like every other field. A new job is the one thing
 * written elsewhere, in ArrowLoop, and only once its window is confirmed.
 */

const MODE: Record<AfterDownloadMode, { label: TranslationKey; icon: ReactNode }> = {
  sync: { label: 'settings.categories.afterDownloadModeSync', icon: <IconSync /> },
  mirror: { label: 'settings.categories.afterDownloadModeMirror', icon: <IconCopy /> },
  move: { label: 'settings.categories.afterDownloadModeMove', icon: <IconMove /> },
};

const DIRECTION: Record<AfterDownloadDirection, TranslationKey> = {
  both: 'settings.categories.afterDownloadDirBoth',
  leftToRight: 'settings.categories.afterDownloadDirLeftToRight',
  rightToLeft: 'settings.categories.afterDownloadDirRightToLeft',
};

const OUTCOME: Record<string, TranslationKey> = {
  done: 'settings.categories.afterDownloadDone',
  quiet: 'settings.categories.afterDownloadQuiet',
  left: 'settings.categories.afterDownloadLeft',
  failed: 'settings.categories.afterDownloadFailed',
};

const OUTCOME_DOT: Record<string, string> = {
  done: 'bg-statusOkSolid',
  quiet: 'bg-statusNeutralSolid',
  left: 'bg-statusWarnSolid',
  failed: 'bg-statusFailSolid',
};

/** useAfterDownload asks the server once the row opens, and again after a new job. */
function useAfterDownload(): [AfterDownloadState | null, () => void] {
  const [state, setState] = useState<AfterDownloadState | null>(null);
  const [round, setRound] = useState(0);
  useEffect(() => {
    let live = true;
    fetchAfterDownload().then(
      (s) => live && setState(s),
      () => live && setState({ fortress: false, engines: [], last: {} }),
    );
    return () => {
      live = false;
    };
  }, [round]);
  return [state, useCallback(() => setRound((r) => r + 1), [])];
}

export function AfterDownloadRow({ cat, onChange }: { cat: Category; onChange: (next: Category) => void }) {
  const { t } = useT();
  const navigate = useNavigate();
  const [state, reload] = useAfterDownload();
  const [created, setCreated] = useState('');
  const [creating, setCreating] = useState<AfterDownloadEngine[] | null>(null);
  const pick = cat.afterDownload;

  const choose = (next: AfterDownloadPick | undefined) => {
    const patched = { ...cat };
    if (next) patched.afterDownload = next;
    else delete patched.afterDownload;
    onChange(patched);
  };

  let body: ReactNode;
  if (state && !state.fortress) {
    body = (
      <Empty>
        {t('settings.categories.afterDownloadNoFortress')}
        <Button kind="secondary" onClick={() => navigate('/instances')}>
          {t('settings.categories.afterDownloadToInstances')}
        </Button>
      </Empty>
    );
  } else if (state && state.engines.length === 0) {
    body = <Empty mark>{t('settings.categories.afterDownloadNoArrowLoop')}</Empty>;
  } else {
    const engines = state?.engines ?? [];
    const engine = engines.find((e) => e.id === pick?.member);
    const job = engine?.jobs.find((j) => j.name === pick?.job);
    body = (
      <>
        <Picker
          pick={pick}
          engines={engines}
          job={job}
          away={!!engine?.error}
          onPick={(next) => {
            setCreated('');
            choose(next);
          }}
          onCreate={() => setCreating(engines.filter((e) => !e.error))}
        />
        <Status pick={pick} job={job} created={created} last={state?.last[cat.id]} />
      </>
    );
  }

  return (
    <>
      <SettingRow
        control="long"
        label={t('settings.categories.afterDownload')}
        hint={t('settings.categories.afterDownloadHint')}
      >
        <span className="flex flex-col gap-2">{body}</span>
      </SettingRow>
      {creating && (
        <CreateJob
          cat={cat}
          engines={creating}
          onClose={() => setCreating(null)}
          onCreated={(engine, name) => {
            setCreating(null);
            setCreated(name);
            choose({ member: engine.id, device: engine.device, job: name });
            reload();
          }}
        />
      )}
    </>
  );
}

function Empty({ mark = false, children }: { mark?: boolean; children: ReactNode }) {
  return (
    <span className="flex items-start gap-3 rounded-[var(--radius-control)] bg-carbon-surface2 p-3 text-xs text-carbon-textSub">
      {mark && <Disc size="sm" />}
      <span className="flex flex-col items-start gap-2">{children}</span>
    </span>
  );
}

/** Disc is ArrowLoop's mark on a ring of its bronze. */
function Disc({ size = 'md' }: { size?: 'sm' | 'md' }) {
  const box = size === 'sm' ? 'h-7 w-7' : 'h-9 w-9';
  const mark = size === 'sm' ? 'h-5 w-5' : 'h-7 w-7';
  return (
    <span className={`grid ${box} shrink-0 place-items-center rounded-full bg-arrowloopSoft ring-1 ring-arrowloopLine`}>
      <ArrowLoopMark className={mark} />
    </span>
  );
}

/** Flow draws the direction as an arrow between two folders. */
function Flow({ direction }: { direction: AfterDownloadDirection }) {
  const { t } = useT();
  const arrow =
    direction === 'both' ? (
      <path d="M3 3.5h20M19 .5l4 3-4 3M23 8.5H3M7 5.5l-4 3 4 3" />
    ) : direction === 'rightToLeft' ? (
      <path d="M24 6H3M8 1.5 3 6l5 4.5" />
    ) : (
      <path d="M2 6h21M18 1.5 23 6l-5 4.5" />
    );
  return (
    <span role="img" aria-label={t(DIRECTION[direction])} className="inline-flex shrink-0 items-center gap-0.5 text-carbon-textSub">
      <IconFolder width={14} height={14} />
      <svg
        viewBox="0 0 26 12"
        className="h-3 w-6 text-arrowloop"
        fill="none"
        stroke="currentColor"
        strokeWidth={1.75}
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        {arrow}
      </svg>
      <IconFolder width={14} height={14} />
    </span>
  );
}

function ModeBadge({ job }: { job: AfterDownloadJob }) {
  const { t } = useT();
  const look = job.mode === 'move' && !job.paused ? 'bg-arrowloopSoft text-arrowloop ring-1 ring-arrowloopLine' : 'bg-carbon-surface3 text-carbon-text';
  return (
    <span className={`inline-flex h-5 shrink-0 items-center gap-1 rounded-[var(--radius-pill)] ps-1.5 pe-2 text-xs font-medium ${look}`}>
      <span className="h-3 w-3 [&>svg]:h-3 [&>svg]:w-3">{job.paused ? <IconPause /> : MODE[job.mode].icon}</span>
      {job.paused ? t('settings.categories.afterDownloadPaused') : t(MODE[job.mode].label)}
    </span>
  );
}

/** LastRun is the job's last run with its outcome, or only its dot where the
 *  line is short. */
function LastRun({ job, dot = false }: { job: AfterDownloadJob; dot?: boolean }) {
  const { t } = useT();
  if (!job.lastRun) {
    const never = t('settings.categories.afterDownloadNever');
    return (
      <span className="inline-flex shrink-0 items-center gap-1.5 text-xs text-carbon-textSub" title={dot ? never : undefined}>
        <i className="h-2 w-2 rounded-full ring-[1.5px] ring-statusNeutralSolid ring-inset" />
        {!dot && never}
      </span>
    );
  }
  const outcome = OUTCOME[job.lastRun.outcome];
  const when = fmtDate(job.lastRun.at);
  const said = outcome ? `${t(outcome)} · ${when}` : when;
  return (
    <span className="inline-flex shrink-0 items-center gap-1.5 text-xs text-carbon-textSub" title={dot ? said : undefined}>
      <i className={`h-2 w-2 rounded-full ${OUTCOME_DOT[job.lastRun.outcome] ?? 'bg-statusNeutralSolid'}`} />
      {!dot && said}
    </span>
  );
}

/**
 * Picker is a dropdown whose rows are cards, one per job, under a heading per
 * ArrowLoop instance. Its list is portalled to the body, since the row it
 * stands in is a label and would hand every click inside to its trigger.
 */
function Picker({
  pick,
  engines,
  job,
  away,
  onPick,
  onCreate,
}: {
  pick: AfterDownloadPick | undefined;
  engines: AfterDownloadEngine[];
  job: AfterDownloadJob | undefined;
  away: boolean;
  onPick: (next: AfterDownloadPick | undefined) => void;
  onCreate: () => void;
}) {
  const { t } = useT();
  const trigger = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [place, setPlace] = useState<{ left: number; top: number; width: number; maxHeight: number } | null>(null);

  useLayoutEffect(() => {
    if (!open || !trigger.current) return;
    const at = () => {
      const r = trigger.current!.getBoundingClientRect();
      const vw = document.documentElement.clientWidth;
      const width = Math.min(Math.max(r.width, 416), vw - 16);
      const left = Math.max(8, Math.min(r.right - width, vw - 8 - width));
      setPlace({ left, top: r.bottom + 6, width, maxHeight: Math.max(160, window.innerHeight - r.bottom - 16) });
    };
    at();
    window.addEventListener('resize', at);
    window.addEventListener('scroll', at, true);
    return () => {
      window.removeEventListener('resize', at);
      window.removeEventListener('scroll', at, true);
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const away = (e: PointerEvent) => {
      const target = e.target as Node;
      if (!list.current?.contains(target) && !trigger.current?.contains(target)) setOpen(false);
    };
    document.addEventListener('pointerdown', away);
    list.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.focus();
    return () => document.removeEventListener('pointerdown', away);
  }, [open]);

  const close = () => {
    setOpen(false);
    trigger.current?.focus();
  };
  const choose = (next: AfterDownloadPick | undefined) => {
    onPick(next);
    close();
  };
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      close();
      return;
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    e.preventDefault();
    const rows = [...(list.current?.querySelectorAll<HTMLElement>('[data-row]:not([disabled])') ?? [])];
    const at = rows.indexOf(document.activeElement as HTMLElement);
    rows[(at + (e.key === 'ArrowDown' ? 1 : -1) + rows.length) % rows.length]?.focus();
  };

  let face: ReactNode;
  if (!pick) {
    face = <span className="min-w-0 flex-1 truncate text-carbon-textSub">{t('settings.categories.afterDownloadNone')}</span>;
  } else if (job) {
    face = (
      <>
        <Disc size="sm" />
        <span className="min-w-20 flex-1 truncate font-medium">{job.name}</span>
        <Flow direction={job.direction} />
        <ModeBadge job={job} />
        <LastRun job={job} dot />
      </>
    );
  } else {
    face = (
      <>
        <Disc size="sm" />
        <span className="min-w-0 flex-1 truncate font-medium">
          {away ? pick.job : t('settings.categories.afterDownloadMissing', { job: pick.job })}
        </span>
        {away && (
          <span className="inline-flex shrink-0 items-center gap-1.5 text-xs text-statusWarn">
            <i className="h-2 w-2 rounded-full bg-statusWarnSolid" />
            {t('settings.categories.afterDownloadAway')}
          </span>
        )}
      </>
    );
  }

  return (
    <>
      <button
        ref={trigger}
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        title={pick?.device ? t('settings.categories.afterDownloadOn', { device: pick.device }) : undefined}
        className={`${FIELD_TRIGGER} flex w-full min-w-0 items-center gap-2 ps-1.5 pe-2.5 text-start text-sm`}
        onClick={() => setOpen((o) => !o)}
      >
        {face}
        <IconExpand width={16} height={16} className="shrink-0 text-carbon-textMuted" />
      </button>
      {open &&
        place &&
        createPortal(
          <div
            ref={list}
            role="listbox"
            aria-label={t('settings.categories.afterDownload')}
            onKeyDown={onKey}
            style={{ left: place.left, top: place.top, width: place.width, maxHeight: place.maxHeight }}
            className="glim-float fixed z-50 flex flex-col gap-1.5 overflow-y-auto rounded-[var(--radius-card)] bg-carbon-surface2 p-1.5"
          >
            <button
              type="button"
              role="option"
              data-row
              aria-selected={!pick}
              onClick={() => choose(undefined)}
              className="flex items-center gap-3 rounded-[var(--radius-control)] px-2.5 py-2 text-start text-sm hover:bg-carbon-surface3 focus-visible:bg-carbon-surface3 focus-visible:outline-none aria-selected:bg-accentSoft"
            >
              <span className="grid h-7 w-7 place-items-center rounded-full bg-carbon-surface3 text-carbon-textSub [&>svg]:h-4 [&>svg]:w-4">
                <IconStopMark />
              </span>
              <span className="flex-1 font-medium">{t('settings.categories.afterDownloadNone')}</span>
            </button>
            {engines.map((e) => (
              <EngineGroup key={e.id} engine={e} pick={pick} onPick={choose} />
            ))}
            {engines.some((e) => !e.error) && (
              <button
                type="button"
                data-row
                onClick={() => {
                  close();
                  onCreate();
                }}
                className="flex items-center gap-3 rounded-[var(--radius-control)] border-[1.5px] border-dashed border-arrowloopLine px-3 py-2.5 text-start text-arrowloop hover:bg-arrowloopSoft focus-visible:bg-arrowloopSoft focus-visible:outline-none"
              >
                <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full border-[1.5px] border-dashed border-arrowloopLine [&>svg]:h-4 [&>svg]:w-4">
                  <IconAdd />
                </span>
                <span className="flex flex-col">
                  <span className="text-sm font-semibold">{t('settings.categories.afterDownloadNew')}</span>
                  <span className="text-xs text-carbon-textMuted">{t('settings.categories.afterDownloadNewHint')}</span>
                </span>
              </button>
            )}
          </div>,
          document.body,
        )}
    </>
  );
}

function EngineGroup({
  engine,
  pick,
  onPick,
}: {
  engine: AfterDownloadEngine;
  pick: AfterDownloadPick | undefined;
  onPick: (next: AfterDownloadPick) => void;
}) {
  const { t } = useT();
  return (
    <div role="group" aria-label={t('settings.categories.afterDownloadOn', { device: engine.device })} className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2 px-1.5 pt-2 text-xs text-carbon-textSub">
        <ArrowLoopMark className="h-4 w-4" />
        <span>{t('settings.categories.afterDownloadOn', { device: engine.device })}</span>
        {engine.error && (
          <span className="ms-auto inline-flex items-center gap-1.5 text-statusWarn">
            <i className="h-1.5 w-1.5 rounded-full bg-statusWarnSolid" />
            {t('settings.categories.afterDownloadAway')}
          </span>
        )}
      </div>
      {engine.error ? (
        <p className="px-1.5 pb-1 text-xs text-carbon-textSub">
          {t('settings.categories.afterDownloadAwayJobs', { device: engine.device })}
        </p>
      ) : (
        engine.jobs.map((j) => {
          const selected = pick?.member === engine.id && pick.job === j.name;
          return (
            <button
              key={j.name}
              type="button"
              role="option"
              data-row
              aria-selected={selected}
              disabled={j.paused}
              onClick={() => onPick({ member: engine.id, device: engine.device, job: j.name })}
              className={`flex items-center gap-3 rounded-[var(--radius-control)] bg-carbon-surface px-3 py-2.5 text-start ring-inset transition-shadow
                ${selected ? 'ring-2 ring-accent' : 'ring-1 ring-carbon-border hover:ring-carbon-surface3 focus-visible:ring-carbon-surface3'}
                focus-visible:outline-none disabled:cursor-not-allowed disabled:[&>*]:opacity-50`}
            >
              <Disc />
              <span className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="flex min-w-0 items-baseline gap-2">
                  <span className="truncate text-sm font-semibold">{j.name}</span>
                  <span className="shrink-0 text-xs font-medium text-arrowloop">
                    {t('settings.categories.afterDownloadBadge', { device: engine.device })}
                  </span>
                </span>
                <span className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
                  <Flow direction={j.direction} />
                  <ModeBadge job={j} />
                  <LastRun job={j} />
                </span>
              </span>
              <span className={`grid h-5 w-5 shrink-0 place-items-center rounded-full bg-accent text-accentContrast [&>svg]:h-3 [&>svg]:w-3 ${selected ? '' : 'invisible'}`}>
                <IconCheck />
              </span>
            </button>
          );
        })
      )}
    </div>
  );
}

/** Status says how the last hand-over went, and what a job that moves means. */
function Status({
  pick,
  job,
  created,
  last,
}: {
  pick: AfterDownloadPick | undefined;
  job: AfterDownloadJob | undefined;
  created: string;
  last: AfterDownloadResult | undefined;
}) {
  const { t } = useT();
  if (!pick) return null;
  const device = pick.device ?? '';
  const lines: ReactNode[] = [];
  if (created && created === pick.job) {
    lines.push(
      <Line key="created" tone="arrowloop" chip={t('settings.categories.afterDownloadCreated')}>
        {t('settings.categories.afterDownloadCreatedText', { job: created, device })}
      </Line>,
    );
  } else if (last && last.job === pick.job) {
    const vars = { package: last.package, job: last.job, device, when: fmtDate(last.at) };
    const said = describe(last, vars, t);
    if (said) lines.push(<Line key="last" tone={said.tone} chip={said.chip}>{said.text}</Line>);
  }
  if (job?.mode === 'move') {
    lines.push(
      <Line key="moves" tone="arrowloop" chip={t(MODE.move.label)}>
        {t('settings.categories.afterDownloadMoves')}
      </Line>,
    );
  }
  return lines.length ? <span className="flex flex-col gap-1.5">{lines}</span> : null;
}

type Tone = 'ok' | 'info' | 'warn' | 'fail' | 'arrowloop';

const TONE: Record<Tone, string> = {
  ok: 'bg-statusOkBg text-statusOk',
  info: 'bg-statusInfoBg text-statusInfo',
  warn: 'bg-statusWarnBg text-statusWarn',
  fail: 'bg-statusFailBg text-statusFail',
  arrowloop: 'bg-arrowloopSoft text-arrowloop',
};

function Line({ tone, chip, children }: { tone: Tone; chip: string; children: ReactNode }) {
  return (
    <span className="flex items-start gap-2 text-xs text-carbon-textSub">
      <span className={`shrink-0 rounded-[var(--radius-pill)] px-2 py-0.5 font-medium ${TONE[tone]}`}>{chip}</span>
      <span className="pt-0.5">{children}</span>
    </span>
  );
}

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

function describe(
  last: AfterDownloadResult,
  vars: Record<string, string>,
  t: Translate,
): { tone: Tone; chip: string; text: string } | null {
  if (last.status === 'started') {
    return { tone: 'ok', chip: t('settings.categories.afterDownloadStarted'), text: t('settings.categories.afterDownloadStartedText', vars) };
  }
  if (last.status === 'queued') {
    return { tone: 'info', chip: t('settings.categories.afterDownloadQueued'), text: t('settings.categories.afterDownloadQueuedText', vars) };
  }
  switch (last.error) {
    case 'unreachable':
      return { tone: 'warn', chip: t('settings.categories.afterDownloadUnreachable'), text: t('settings.categories.afterDownloadUnreachableText', vars) };
    case 'permissionOff':
      return { tone: 'fail', chip: t('settings.categories.afterDownloadPermissionOff'), text: t('settings.categories.afterDownloadPermissionOffText', vars) };
    case 'noSuchJob':
      return { tone: 'fail', chip: t('settings.categories.afterDownloadNoSuchJob'), text: t('settings.categories.afterDownloadNoSuchJobText', vars) };
    case 'jobPaused':
      return { tone: 'warn', chip: t('settings.categories.afterDownloadPaused'), text: t('settings.categories.afterDownloadJobPausedText', vars) };
    case 'gone':
      return { tone: 'fail', chip: t('settings.categories.afterDownloadGone'), text: t('settings.categories.afterDownloadGoneText', vars) };
    case undefined:
    case '':
      return null;
    default:
      return { tone: 'fail', chip: t('settings.categories.afterDownloadNotStarted'), text: t('settings.categories.afterDownloadNotStartedText', vars) };
  }
}

const KIND_ICON: Record<string, ReactNode> = {
  share: <IconNetwork />,
  disk: <IconDisc />,
  remote: <IconGlobe />,
};

const CREATE_ERROR: Record<string, TranslationKey> = {
  'afterDownload.nameTaken': 'settings.categories.afterDownloadErrNameTaken',
  'afterDownload.noSuchTarget': 'settings.categories.afterDownloadErrNoSuchTarget',
  'afterDownload.sourceNotMounted': 'settings.categories.afterDownloadErrSourceNotMounted',
  'afterDownload.noFolder': 'settings.categories.afterDownloadErrNoFolder',
};

/**
 * CreateJob asks for a name, a target ArrowLoop already knows and a mode. The
 * source is the category's own folder; nothing here takes an address, so a
 * login never passes through KnightLoader.
 */
function CreateJob({
  cat,
  engines,
  onClose,
  onCreated,
}: {
  cat: Category;
  engines: AfterDownloadEngine[];
  onClose: () => void;
  onCreated: (engine: AfterDownloadEngine, name: string) => void;
}) {
  const { t } = useT();
  const [member, setMember] = useState(engines[0]?.id ?? '');
  const [name, setName] = useState('');
  const [target, setTarget] = useState('');
  const [mode, setMode] = useState<AfterDownloadMode>('mirror');
  const [targets, setTargets] = useState<AfterDownloadDestination[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const engine = engines.find((e) => e.id === member) ?? engines[0];
  const device = engine?.device ?? '';
  const folder = (cat.dir ?? '').split('<')[0].trim().replace(/(.)[\/]+$/, '$1');

  useEffect(() => {
    let live = true;
    setTargets(null);
    setTarget('');
    fetchAfterDownloadDestinations(member).then(
      (list) => live && setTargets(list),
      () => live && setTargets([]),
    );
    return () => {
      live = false;
    };
  }, [member]);

  const ready = name.trim() !== '' && target !== '' && folder !== '' && !busy;
  const create = async () => {
    if (!ready || !engine) return;
    setBusy(true);
    setError('');
    try {
      const job = await createAfterDownloadJob({ category: cat.id, member, name: name.trim(), target, mode });
      onCreated(engine, job);
    } catch (e) {
      const code = e instanceof ApiError ? e.code ?? '' : '';
      const key = CREATE_ERROR[code];
      setError(key ? t(key, { job: name.trim(), device }) : t('settings.categories.afterDownloadErrOther', { error: String(e instanceof Error ? e.message : e) }));
      setBusy(false);
    }
  };

  const picked = targets?.find((d) => d.id === target);
  const radio = (on: boolean) =>
    `rounded-[var(--radius-control)] ring-inset transition-colors ${on ? 'bg-arrowloopSoft ring-[1.5px] ring-arrowloop' : 'bg-carbon-surface2 ring-1 ring-transparent hover:ring-carbon-surface3'}`;

  return (
    <Modal
      title={t('settings.categories.afterDownloadNew')}
      hint={t('settings.categories.afterDownloadNewHint')}
      onClose={onClose}
      footer={
        <>
          <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          <Button kind="primary" labelled icon={<IconAdd />} title={t('settings.categories.afterDownloadNewCreate')} disabled={!ready} onClick={() => void create()} />
        </>
      }
    >
      <div className="flex flex-col gap-4 text-sm">
        {engines.length > 1 && (
          <div role="radiogroup" aria-label="ArrowLoop" className="grid grid-cols-2 gap-1.5">
            {engines.map((e) => (
              <button key={e.id} type="button" role="radio" aria-checked={e.id === member} onClick={() => setMember(e.id)} className={`${radio(e.id === member)} flex items-center gap-2 px-3 py-2`}>
                <ArrowLoopMark className="h-5 w-5" />
                <span className="truncate">{e.device}</span>
              </button>
            ))}
          </div>
        )}

        <label className="flex flex-col gap-1.5">
          <span className="font-medium">{t('settings.categories.afterDownloadNewName')}</span>
          <TextInput
            value={name}
            autoFocus
            placeholder={t('settings.categories.afterDownloadNewNamePlaceholder')}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void create();
            }}
          />
        </label>

        <div className="flex flex-col gap-1.5">
          <span className="font-medium">{t('settings.categories.afterDownloadNewRoute')}</span>
          <div className="grid grid-cols-[1fr_auto_1fr] items-stretch gap-2">
            <span className="flex min-w-0 flex-col rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-xs text-carbon-textMuted">
              {t('settings.categories.afterDownloadNewSource')}
              <span className="truncate text-sm text-carbon-textSub" dir="ltr">{folder}</span>
            </span>
            <span className="grid place-items-center">
              <Flow direction={mode === 'sync' ? 'both' : 'leftToRight'} />
            </span>
            <span className="flex min-w-0 flex-col rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-xs text-carbon-textMuted">
              {t('settings.categories.afterDownloadNewTarget')}
              <span className="truncate text-sm text-carbon-text">
                {picked ? `${picked.name} / ${name.trim() || '…'}` : t('settings.categories.afterDownloadNewTargetNone')}
              </span>
            </span>
          </div>
          {folder === '' && <p className="text-xs text-statusWarn">{t('settings.categories.afterDownloadErrNoFolder')}</p>}
          <p className="text-xs text-carbon-textMuted">{t('settings.categories.afterDownloadNewIntoFolder')}</p>
        </div>

        <div className="flex flex-col gap-1.5">
          <span className="font-medium">{t('settings.categories.afterDownloadNewTarget')}</span>
          {targets && targets.length === 0 ? (
            <p className="text-xs text-carbon-textSub">{t('settings.categories.afterDownloadNewNoTargets', { device })}</p>
          ) : (
            <div role="radiogroup" aria-label={t('settings.categories.afterDownloadNewTarget')} className="flex flex-col gap-1">
              {(targets ?? []).map((d) => (
                <button key={d.id} type="button" role="radio" aria-checked={d.id === target} onClick={() => setTarget(d.id)} className={`${radio(d.id === target)} flex items-center gap-2.5 px-3 py-2 text-start`}>
                  <span className="text-carbon-textSub [&>svg]:h-4 [&>svg]:w-4">{KIND_ICON[d.kind] ?? <IconFolder />}</span>
                  <span className="flex min-w-0 flex-1 flex-col">
                    {d.name}
                    <span className="truncate text-xs text-carbon-textMuted">{d.where}</span>
                  </span>
                </button>
              ))}
            </div>
          )}
          <p className="text-xs text-carbon-textMuted">{t('settings.categories.afterDownloadNewTargetsHint', { device })}</p>
        </div>

        <div className="flex flex-col gap-1.5">
          <span className="font-medium">{t('settings.categories.afterDownloadNewMode')}</span>
          <div role="radiogroup" aria-label={t('settings.categories.afterDownloadNewMode')} className="grid grid-cols-3 gap-1.5">
            {(['mirror', 'sync', 'move'] as const).map((m) => (
              <button key={m} type="button" role="radio" aria-checked={m === mode} onClick={() => setMode(m)} className={`${radio(m === mode)} flex flex-col items-center gap-1 px-2 py-2 text-center text-xs ${m === mode ? 'text-arrowloop' : ''}`}>
                <span className="[&>svg]:h-[18px] [&>svg]:w-[18px]">{MODE[m].icon}</span>
                {t(MODE[m].label)}
                <span className="text-[11px] text-carbon-textMuted">
                  {t(m === 'mirror' ? 'settings.categories.afterDownloadNewMirrorHint' : m === 'sync' ? 'settings.categories.afterDownloadNewSyncHint' : 'settings.categories.afterDownloadNewMoveHint')}
                </span>
              </button>
            ))}
          </div>
        </div>

        {error && <p className="text-xs text-statusFail">{error}</p>}
      </div>
    </Modal>
  );
}
