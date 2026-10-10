import { useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { ok, restartTasks, type HosterLogin, type Task } from '../lib/api';
import { adviceFor } from '../lib/failureAdvice';
import { openExternal } from '../lib/external';
import { fmtDate, fmtGB, fmtTotal } from '../lib/format';
import { useT, type TranslationKey } from '../lib/i18n';
import {
  IconAccounts,
  IconCaptcha,
  IconCheck,
  IconDiagnostics,
  IconExternalLink,
  IconEye,
  IconHelp,
  IconRetry,
  IconSliders,
} from '../lib/icons';
import { message } from '../lib/intake';
import { en } from '../lib/locales/en';
import type { Need } from '../lib/needs';
import { requestReveal } from '../lib/reveal';
import { explainFailure } from '../lib/taskError';
import { taskRefusal } from '../lib/taskRefusal';
import { useToast } from '../lib/toast';
import { folderName } from '../lib/useDiskSpace';
import { healthLabel } from '../lib/useHealthReport';
import type { NeedsFeed } from '../lib/useNeeds';
import { requestJump } from '../pages/settings/jump';
import { showCaptcha } from './CaptchaModal';
import { failureFallback, reasonKey } from './columns';
import { FailureAdvice } from './FailureAdvice';
import { Button, Card, SectionTitle } from './ui';

type Translate = ReturnType<typeof useT>['t'];

interface Act {
  label: string;
  icon: ReactNode;
  run: () => void;
  /** Keeps the glyph alone whatever the label setting says, for a name too long for a row. */
  glyphOnly?: boolean;
}

/** What one need shows: its two lines, where a click on it leads and what it offers. */
interface Row {
  name: string;
  sub: string;
  open: () => void;
  main: Act;
  side?: Act;
}

/** Why a login was refused, in the reader's words where this build knows the code. */
function loginDetail(t: Translate, login: HosterLogin): string {
  const key = `accounts.hoster.detail.${login.code ?? ''}` as TranslationKey;
  if (login.code && key in en) return t(key);
  return login.detail || t('health.remedy.accounts.invalid');
}

/** A package's name, or the file's own for a link outside any package. */
const titleOf = (tasks: Task[]): string => tasks[0].package || tasks[0].name || tasks[0].url;

/**
 * NeedsCard lists what waits for the person, one row each, with the step that
 * settles it on the row. It draws nothing while nothing waits.
 */
export function NeedsCard({ feed, hue }: { feed: NeedsFeed; hue?: number }) {
  const { t } = useT();
  const { toast } = useToast();
  const navigate = useNavigate();
  const [advised, setAdvised] = useState<Task | null>(null);

  if (feed.needs.length === 0) return null;

  const reveal = (task: Task) => {
    navigate('/downloads');
    requestReveal(task.id, () => toast(t('events.jumpGone'), 'fail'));
  };
  const retry = async (tasks: Task[]) => {
    try {
      await ok(await restartTasks(tasks.map((x) => x.id)));
    } catch (e) {
      toast(taskRefusal(e, t) ?? t('list.failed', { error: message(e) }), 'fail');
    }
  };
  const settings = (page: string, title?: TranslationKey) => () => {
    if (title) requestJump({ page, title });
    navigate(`/settings/${page}`);
  };
  const accounts = () => navigate('/accounts');
  const openAccounts: Act = { label: t('overview.needs.open'), icon: <IconAccounts />, run: accounts };

  function rowOf(need: Need): Row {
    switch (need.kind) {
      case 'captcha': {
        const solve = () => showCaptcha(need.challenge.id);
        return {
          name: t('overview.needs.captcha', { host: need.challenge.host || '?' }),
          sub: t('health.remedy.captcha.waiting'),
          open: solve,
          main: { label: t('overview.needs.solve'), icon: <IconCaptcha />, run: solve },
        };
      }
      case 'failed': {
        const first = need.tasks[0];
        const why = explainFailure(t, first, failureFallback(first, t))?.line ?? t('status.error');
        const reason = first.reason ? reasonKey[first.reason] : undefined;
        return {
          name: titleOf(need.tasks),
          sub: need.tasks.length > 1 ? `${t('overview.needs.links', { n: need.tasks.length })} · ${why}` : why,
          open: () => reveal(first),
          main: { label: t('common.retry'), icon: <IconRetry />, run: () => void retry(need.tasks) },
          side:
            reason && adviceFor(first.reason)
              ? { label: t('failure.open'), icon: <IconHelp />, run: () => setAdvised(first), glyphOnly: true }
              : undefined,
        };
      }
      case 'unpack': {
        const show = () => reveal(need.tasks[0]);
        return {
          name: titleOf(need.tasks),
          sub: t(need.password ? 'archive.needsPassword' : 'archive.failed'),
          open: show,
          main: { label: t('events.jump'), icon: <IconEye />, run: show },
        };
      }
      case 'account':
        return {
          name: t('overview.needs.accountFailing', { name: need.name }),
          sub: need.detail,
          open: accounts,
          main: openAccounts,
        };
      case 'login':
        return {
          name: t('overview.needs.loginRejected', { name: need.login.host }),
          sub: loginDetail(t, need.login),
          open: accounts,
          main: openAccounts,
        };
      case 'expiry': {
        const url = need.renewUrl;
        return {
          name: t('overview.needs.accountExpires', { name: need.name, date: fmtDate(need.expiry) }),
          sub: t('overview.needs.accountExpiresHint'),
          open: accounts,
          main: openAccounts,
          side: url
            ? { label: t('accounts.renew'), icon: <IconExternalLink />, run: () => openExternal(url) }
            : undefined,
        };
      }
      case 'disk': {
        const limits = settings('downloads', 'settings.downloads.diskTitle');
        return {
          name: t('overview.needs.diskLow', { folder: folderName(need.volume.dir) }),
          sub: t(need.stopped ? 'overview.needs.diskStopHint' : 'overview.needs.diskLowHint', {
            free: fmtTotal(need.volume.free),
          }),
          open: limits,
          main: { label: t('disk.limits'), icon: <IconSliders />, run: limits },
        };
      }
      case 'volumeCap': {
        const u = need.usage;
        const cap = settings('downloads', 'settings.volume.title');
        const used = t('settings.volume.usedOf', { used: fmtGB(u.used), cap: fmtGB(u.cap) });
        return {
          name: t('settings.volume.title'),
          sub: `${t('settings.volume.reached')} ${used} · ${t('settings.volume.resetsOn', { date: fmtDate(u.periodEnd) })}`,
          open: cap,
          main: { label: t('overview.needs.open'), icon: <IconSliders />, run: cap },
        };
      }
      case 'health': {
        const health = settings('health');
        const part = healthLabel(t, 'health.part.', need.part.id);
        return {
          name: `${part} · ${healthLabel(t, 'health.state.', need.part.state)}`,
          sub: healthLabel(t, 'health.remedy.', need.part.remedy ?? ''),
          open: health,
          main: { label: t('settings.nav.health'), icon: <IconDiagnostics />, run: health },
        };
      }
    }
  }

  const setAside = (need: Need) => {
    feed.markDone(need);
    toast(t('overview.needs.doneToast'), 'ok', 'action-done', {
      label: t('remove.undo'),
      run: () => feed.restore(need.covers),
    });
  };

  return (
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle>{t('overview.needs.title')}</SectionTitle>
      {/* The rows scroll here rather than the card, which would cut its badge. */}
      <div
        data-new="overview-needs"
        className="-mx-2 flex max-h-[28rem] flex-col divide-y divide-carbon-border/60 overflow-y-auto"
      >
        {feed.needs.map((need) => {
          const row = rowOf(need);
          return (
            <div
              key={need.key}
              data-need={need.kind}
              className="relative flex flex-wrap items-center gap-x-3.5 gap-y-2 rounded-[var(--radius-control)] px-2 py-3
                hover:bg-carbon-hover md:flex-nowrap"
            >
              <span
                aria-hidden
                className={`h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${
                  need.tone === 'fail' ? 'bg-statusFailSolid' : 'bg-statusWarnSolid'
                }`}
              />
              <div className="min-w-0 flex-1 basis-40">
                {/* The name is the row's link: its ::after covers the row, and
                    the buttons stand above it. */}
                <button
                  type="button"
                  dir="auto"
                  onClick={row.open}
                  className="block max-w-full truncate text-start text-sm font-semibold text-carbon-text
                    after:absolute after:inset-0 after:rounded-[var(--radius-control)] after:content-['']
                    focus-visible:outline-none focus-visible:after:outline-2 focus-visible:after:-outline-offset-2
                    focus-visible:after:outline-accent"
                >
                  {row.name}
                </button>
                <p dir="auto" className="mt-0.5 break-words text-[13px] text-carbon-textMuted">
                  {row.sub}
                </p>
              </div>
              <div className="relative flex w-full shrink-0 items-center justify-end gap-2 ps-[22px] md:w-auto md:ps-0">
                {row.side && (
                  <Button
                    kind="ghost"
                    labelled={!row.side.glyphOnly}
                    icon={row.side.icon}
                    title={row.side.label}
                    onClick={row.side.run}
                  />
                )}
                {need.covers.length > 0 && (
                  <Button
                    kind="ghost"
                    labelled
                    icon={<IconCheck />}
                    title={t('overview.needs.done')}
                    onClick={() => setAside(need)}
                  />
                )}
                <Button kind="secondary" className="max-md:flex-1" icon={row.main.icon} onClick={row.main.run}>
                  {row.main.label}
                </Button>
              </div>
            </div>
          );
        })}
      </div>
      {advised?.reason && reasonKey[advised.reason] && (
        <FailureAdvice
          task={advised}
          base="/api"
          reasonLabel={reasonKey[advised.reason]}
          onClose={() => setAdvised(null)}
        />
      )}
    </Card>
  );
}
