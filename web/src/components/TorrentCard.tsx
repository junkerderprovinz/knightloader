import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { fetchTorrentOverview, type Settings, type TorrentOverview } from '../lib/api';
import { fmtBytes, fmtRate, fmtUptime } from '../lib/format';
import { useT } from '../lib/i18n';
import { Card, SectionTitle } from './ui';

// The seeding figures move every few seconds, and the engine reads a torrent
// about that often, so a faster poll would only redraw the same numbers.
const POLL_MS = 3000;

/** A ratio to two places, as the list's Ratio column prints it. */
const fmtRatio = (r: number): string => r.toFixed(2);

/** One figure of the card: its name over its value. */
function Figure({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="min-w-0">
      <div className="glim-eyebrow truncate">{label}</div>
      <div className="glim-num mt-1 truncate text-lg font-semibold text-carbon-text">{value}</div>
      {sub && <div className="glim-num truncate text-xs text-carbon-textMuted">{sub}</div>}
    </div>
  );
}

/**
 * TorrentCard is the overview's card for the built-in torrent client: how many
 * torrents leech and seed, both speeds, what came in and went out today and in
 * all, the ratio of the two and the three torrents sending most. While the
 * network interface torrents are tied to is missing, it says so first. Like the
 * other cards it is left out while it has nothing to show, and while the
 * torrent module is off. A click on it opens the torrents under Downloads.
 */
export function TorrentCard({ settings, hue }: { settings: Settings | null; hue?: number }) {
  const { t } = useT();
  const navigate = useNavigate();
  const off = settings?.modulesOff?.includes('torrents') ?? false;
  const [data, setData] = useState<TorrentOverview | null>(null);

  useEffect(() => {
    if (off) return;
    let live = true;
    const load = () =>
      void fetchTorrentOverview().then(
        (d) => live && setData(d),
        // A failed poll keeps the last figures; the next one tries again.
        () => undefined,
      );
    load();
    const timer = window.setInterval(load, POLL_MS);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, [off]);

  if (off || !data?.any) return null;
  const top = data.top ?? [];

  return (
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle hint={t('overview.torrents.hint')}>{t('overview.torrents.title')}</SectionTitle>
      {data.interfaceDown && data.interface && (
        <p className="text-sm text-statusWarn">{t('overview.torrents.interfaceDown', { name: data.interface })}</p>
      )}
      <button
        type="button"
        onClick={() => navigate('/downloads?card=torrents')}
        aria-label={t('overview.torrents.open')}
        className="grid grid-cols-1 gap-5 rounded-[var(--radius-control)] bg-carbon-surface2 p-4 text-start
          transition-colors hover:bg-carbon-surface3 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]"
      >
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <Figure label={t('status.leeching')} value={String(data.leeching)} />
          <Figure label={t('status.seeding')} value={String(data.seeding)} />
          <Figure label={t('overview.torrents.down')} value={fmtRate(data.downloadSpeed)} />
          <Figure label={t('overview.torrents.up')} value={fmtRate(data.uploadSpeed)} />
          <Figure
            label={t('overview.torrents.downloaded')}
            value={fmtBytes(data.downloadedToday)}
            sub={t('overview.torrents.inAll', { size: fmtBytes(data.downloaded) })}
          />
          <Figure
            label={t('overview.torrents.uploaded')}
            value={fmtBytes(data.uploadedToday)}
            sub={t('overview.torrents.inAll', { size: fmtBytes(data.uploaded) })}
          />
          <Figure label={t('columns.ratio')} value={fmtRatio(data.ratio)} />
        </div>

        <div className="min-w-0">
          <div className="glim-eyebrow">{t('overview.torrents.top')}</div>
          {top.length === 0 ? (
            <p className="mt-2 text-sm text-carbon-textMuted">{t('overview.torrents.noneUploading')}</p>
          ) : (
            <ol className="mt-2 flex flex-col gap-2">
              {top.map((x) => (
                <li key={x.id} className="min-w-0">
                  <div dir="ltr" className="truncate text-start text-sm text-carbon-text">
                    {x.name}
                  </div>
                  <div className="glim-num truncate text-xs text-carbon-textMuted">
                    {[
                      fmtRate(x.uploadSpeed),
                      `${t('columns.ratio')} ${fmtRatio(x.ratio)}`,
                      x.secondsLeft < 0
                        ? t('overview.torrents.noTarget')
                        : t('overview.torrents.left', { time: fmtUptime(x.secondsLeft) }),
                    ].join(' · ')}
                  </div>
                </li>
              ))}
            </ol>
          )}
        </div>
      </button>
    </Card>
  );
}
