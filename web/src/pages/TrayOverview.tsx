import { useCallback, useEffect, useMemo, useState } from 'react';
import logoUrl from '../assets/logo.svg';
import { Counters } from '../components/Counters';
import { ProgressBar } from '../components/ProgressBar';
import { StatusPill, rowState } from '../components/StatusPill';
import { Button } from '../components/ui';
import { type QueueState, connectWS, fetchCaptchas, fetchQueue, setQueue } from '../lib/api';
import { showMain, useEdgeResize } from '../lib/desktop';
import { fmtRate, pct } from '../lib/format';
import { useT } from '../lib/i18n';
import { IconCaptcha, IconPause, IconPlay } from '../lib/icons';
import { useTasks } from '../lib/useTasks';

/**
 * How many running downloads the window lists before it only counts the rest,
 * and how many of the others, newest first, follow them. The lists scroll
 * inside the window at any size.
 */
const SHOWN = 20;
const RECENT = 20;

/**
 * The small window at the desktop app's tray icon: the queue's switch, the
 * speed and the counts of the Overview, the captchas waiting, what is
 * downloading right now and the latest of the rest. The window is only hidden between clicks, so it asks
 * again whenever it gains the focus and otherwise follows the live stream.
 */
export function TrayOverview() {
  const { t } = useT();
  useEdgeResize();
  const tasks = useTasks('');
  const [queue, setQueueState] = useState<QueueState | null>(null);
  const [captchas, setCaptchas] = useState<Set<string>>(new Set());

  const refresh = useCallback(() => {
    void fetchQueue()
      .then(setQueueState)
      .catch(() => {});
    void fetchCaptchas()
      .then((list) => setCaptchas(new Set(list.map((c) => c.id))))
      .catch(() => {});
  }, []);

  useEffect(() => {
    refresh();
    window.addEventListener('focus', refresh);
    // The main window keeps the language and the look in the browser, and a
    // reload takes a change over the way a fresh start would.
    const follow = () => window.location.reload();
    window.addEventListener('storage', follow);
    // Not a viewer: the captcha prompt is in the main window.
    const close = connectWS(
      (type, data) => {
        if (type === 'queue') setQueueState(data as QueueState);
        else if (type === 'captcha') setCaptchas((prev) => new Set(prev).add(data.id));
        else if (type === 'captchaResolved')
          setCaptchas((prev) => {
            const next = new Set(prev);
            next.delete(data.id);
            return next;
          });
      },
      ['queue', 'captcha', 'captchaResolved'],
    );
    return () => {
      window.removeEventListener('focus', refresh);
      window.removeEventListener('storage', follow);
      close();
    };
  }, [refresh]);

  const list = useMemo(() => Object.values(tasks), [tasks]);
  const counts = useMemo(() => {
    let running = 0,
      queued = 0,
      done = 0,
      error = 0,
      speed = 0;
    for (const x of list) {
      if (x.status === 'running' || x.status === 'extracting') running++;
      else if (x.status === 'queued') queued++;
      else if (x.status === 'done') done++;
      else if (x.status === 'error') error++;
      if (x.status === 'running') speed += x.speed;
    }
    return { running, queued, done, error, speed };
  }, [list]);
  const running = useMemo(
    () => list.filter((x) => x.status === 'running' || x.status === 'extracting').sort((a, b) => b.speed - a.speed),
    [list],
  );

  const recent = useMemo(
    () =>
      list
        .filter((x) => x.status !== 'collected' && x.status !== 'running' && x.status !== 'extracting')
        .sort((a, b) => (a.createdAt > b.createdAt ? -1 : 1))
        .slice(0, RECENT),
    [list],
  );

  const halted = queue?.halted ?? false;
  const toggle = () => {
    void setQueue({ halted: !halted })
      .then(setQueueState)
      .catch(() => {});
  };
  const open = () => void showMain().catch(() => {});

  return (
    <div className="flex h-screen flex-col gap-4 overflow-hidden bg-carbon-background p-4 text-carbon-text">
      <header className="flex items-center gap-3">
        <img src={logoUrl} alt="" className="h-9 w-auto shrink-0" />
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="text-sm font-semibold">KnightLoader</span>
          <span className={`truncate text-xs ${halted ? 'text-statusWarn' : 'text-carbon-textSub'}`}>
            {halted ? t('task.waiting.halted') : running.length > 0 ? t('status.running') : t('tray.idle')}
          </span>
        </div>
      </header>

      <div>
        <div className="glim-eyebrow">{t('overview.totalSpeed')}</div>
        <div className="glim-num mt-1 text-[28px] font-semibold leading-none tracking-tight">
          {fmtRate(counts.speed)}
        </div>
        <div className="mt-3">
          <Counters counts={counts} />
        </div>
      </div>

      {captchas.size > 0 && (
        <Button kind="secondary" icon={<IconCaptcha />} onClick={open}>
          {t('tray.captchas', { n: captchas.size })}
        </Button>
      )}

      <section className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto">
        {running.length > 0 && (
          <ul className="glim-well flex flex-col divide-y divide-carbon-border/60 p-0">
            {running.slice(0, SHOWN).map((x) => (
              <li key={x.id} className="flex flex-col gap-1.5 px-3 py-2">
                <div className="flex items-baseline gap-2">
                  <span className="min-w-0 flex-1 truncate text-sm">{x.name || x.url}</span>
                  <span className="glim-num shrink-0 text-xs text-carbon-textSub">{fmtRate(x.speed)}</span>
                </div>
                <ProgressBar
                  percent={pct(x.loaded, x.size, false)}
                  active
                  indeterminate={x.status === 'running' && x.size <= 0}
                  moving
                />
              </li>
            ))}
            {running.length > SHOWN && (
              <li className="px-3 py-2 text-xs text-carbon-textMuted">{t('tray.more', { n: running.length - SHOWN })}</li>
            )}
          </ul>
        )}
        {recent.length > 0 && (
          <div className="flex flex-col gap-2">
            <h2 className="glim-eyebrow">{t('overview.recent')}</h2>
            <ul className="glim-well flex flex-col divide-y divide-carbon-border/60 p-0">
              {recent.map((x) => (
                <li key={x.id} className="flex items-center gap-2 px-3 py-2">
                  <span className="min-w-0 flex-1 truncate text-sm">{x.name || x.url}</span>
                  <StatusPill status={rowState(x)} />
                </li>
              ))}
            </ul>
          </div>
        )}
      </section>

      <footer className="flex flex-col gap-2">
        <Button kind="secondary" className="w-full" icon={halted ? <IconPlay /> : <IconPause />} onClick={toggle}>
          {halted ? t('queue.start') : t('queue.stop')}
        </Button>
        <Button className="w-full" onClick={open}>
          {t('tray.show')}
        </Button>
      </footer>
    </div>
  );
}
