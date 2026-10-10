import { useNavigate } from 'react-router-dom';
import type { DiskVolume, Settings } from '../lib/api';
import { useT } from '../lib/i18n';
import { fmtTotal } from '../lib/format';
import { fits, folderName, roleLabel, spaceHint, useDiskSpace } from '../lib/useDiskSpace';
import { Vessel, type VesselMark } from './pictures/Vessel';
import { Button, Card, InfoBubble, SectionTitle, useTooltip } from './ui';

// Disk space is reported per folder, not per disk: the platform calls give no
// volume identity, so two folders on one disk show the same space twice and
// the rows are never totalled. Free, used and total are never derived from
// each other, since free excludes the root reserve and honours quotas.

// Smaller than LabelBadge, which is control height and would set the row's.
// surface3, because the well behind it is surface2.
function Chip({ label, hint, tone }: { label: string; hint?: string; tone: 'neutral' | 'warn' | 'fail' }) {
  const ground =
    tone === 'fail'
      ? 'bg-statusFailBg text-statusFail'
      : tone === 'warn'
        ? 'bg-statusWarnBg text-statusWarn'
        : 'bg-carbon-surface3 text-carbon-textSub';
  return (
    <span
      dir="auto"
      className={`inline-flex min-w-0 shrink items-center rounded-[var(--radius-pill)] px-2 py-0.5 text-[11px] ${ground}`}
    >
      <span className="truncate">{label}</span>
      {hint && <InfoBubble tip={hint} label={label} />}
    </span>
  );
}

/**
 * floorMarks puts the low-space and stop floors where the fill would reach
 * them. A floor of 0 means unset and draws nothing.
 */
function floorMarks(v: DiskVolume, cfg: Settings | null, t: ReturnType<typeof useT>['t']): VesselMark[] {
  if (!cfg) return [];
  const marks: VesselMark[] = [];
  const add = (bytes: number, label: string, tone: VesselMark['tone']) => {
    if (bytes <= 0 || bytes >= v.total) return;
    marks.push({ at: (v.total - bytes) / v.total, label, tone });
  };
  add(cfg.diskLowSpace, t('disk.markLow'), 'warn');
  add(cfg.diskCriticalSpace, t('disk.markStop'), 'fail');
  return marks;
}

/** floorTone colours the liquid once the free space is under a floor that is set. */
function floorTone(v: DiskVolume, cfg: Settings | null): 'accent' | 'warn' | 'fail' {
  if (!cfg) return 'accent';
  if (cfg.diskCriticalSpace > 0 && v.free < cfg.diskCriticalSpace) return 'fail';
  if (cfg.diskLowSpace > 0 && v.free < cfg.diskLowSpace) return 'warn';
  return 'accent';
}

/**
 * DiskVolumeRow shows one folder's space, here and on the Downloads settings
 * card. `hint` is for a caller without a card title to carry the explanation.
 */
export function DiskVolumeRow({ v, cfg, hint }: { v: DiskVolume; cfg: Settings | null; hint?: string }) {
  const { t } = useT();
  const room = fits(v);
  // The row shows only the last path segment, and the size has no label.
  const pathTip = useTooltip<HTMLSpanElement>(v.dir);
  const sizeTip = useTooltip<HTMLSpanElement>(t('disk.size'));

  return (
    <div className="flex items-center gap-4 px-5 py-3">
      {/* The platform cannot always measure, and then there is no level to draw. */}
      {v.known && v.total > 0 && (
        <Vessel share={v.used / v.total} label={t('disk.used')} tone={floorTone(v, cfg)} marks={floorMarks(v, cfg, t)} />
      )}
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
          {/* dir="auto": a path can be in a right-to-left script. */}
          <span {...pathTip.triggerProps} dir="auto" className="min-w-0 truncate text-[14px] text-carbon-text">
            {folderName(v.dir)}
          </span>
          {pathTip.node}
          <span className="flex shrink-0 items-center text-[11px] text-carbon-textMuted">
            {roleLabel(t, v.role)}
            {hint && <InfoBubble tip={hint} label={t('disk.title')} />}
          </span>
          <span className="flex-1" />

          {/* Ordinary: the first write creates the folder. */}
          {!v.exists && <Chip tone="neutral" label={t('disk.missing')} />}

          {/* The report climbs to the nearest existing parent. If a mount did not
              come up, that is the volume root of a different disk, so the
              measured path is always shown. */}
          {v.measured !== v.dir && (
            <Chip
              tone={v.exists ? 'warn' : 'neutral'}
              label={t('disk.measured', { path: v.measured })}
              hint={v.exists ? t('disk.elsewhereHint') : t('disk.missingHint', { path: v.measured })}
            />
          )}

          {/* `fits` answers null when nothing could be measured. */}
          {room === false && <Chip tone="fail" label={t('disk.tight')} hint={t('disk.tightHint')} />}
        </div>

        {v.known ? (
          <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-[11px] text-carbon-textMuted">
            <span className="flex items-baseline gap-1.5">
              <span>{t('disk.free')}</span>
              <span className="glim-num text-carbon-text">{fmtTotal(v.free)}</span>
              <span>{t('strip.of')}</span>
              <span {...sizeTip.triggerProps} className="glim-num text-carbon-textSub">
                {fmtTotal(v.total)}
              </span>
              {sizeTip.node}
            </span>
            <span className="flex items-baseline gap-1.5">
              <span>{t('disk.used')}</span>
              <span className="glim-num text-carbon-textSub">{fmtTotal(v.used)}</span>
            </span>
            {/* Not subtracted from free: a running download already claimed its
                room. Shown at zero too, so the row does not reflow. */}
            <span className="flex items-baseline gap-1.5">
              <span>{t('disk.queued')}</span>
              <span className="glim-num text-carbon-text">{fmtTotal(v.queued)}</span>
              <span>{t('disk.tasks', { n: v.tasks })}</span>
              <InfoBubble tip={t('disk.queuedHint')} />
            </span>
          </div>
        ) : (
          // The platform cannot measure, so the zeros mean nothing and no level is drawn.
          <span className="flex items-center text-[11px] text-carbon-textMuted">
            {t('disk.unknown')}
            <InfoBubble tip={t('disk.unknownHint')} />
          </span>
        )}
      </div>
    </div>
  );
}

/**
 * DiskSpaceTile is the Overview card, drawn once a report with folders has
 * arrived. The page passes `settings` so the floors match what it shows.
 */
export function DiskSpaceTile({ settings, hue }: { settings: Settings | null; hue?: number }) {
  const { t } = useT();
  const navigate = useNavigate();
  const report = useDiskSpace();
  const volumes = report?.volumes ?? [];
  if (volumes.length === 0) return null;

  return (
    // A Card, because SectionTitle's badge positions against .glim-card. The
    // hue on it recolours the whole subtree.
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle hint={spaceHint(t, report)}>{t('disk.title')}</SectionTitle>
      <div className="glim-well divide-y divide-carbon-border/60 p-0">
        {volumes.map((v, i) => (
          // One folder can appear under two roles.
          <DiskVolumeRow key={`${v.role}|${v.dir}|${i}`} v={v} cfg={settings} />
        ))}
      </div>
      {/* The server cuts only unconfigured destinations owed the least. */}
      {report?.truncated && (
        <span className="flex items-center text-[11px] text-carbon-textMuted">
          {t('disk.truncated')}
          <InfoBubble tip={t('disk.truncatedHint')} />
        </span>
      )}

      <Button kind="secondary" className="self-start" onClick={() => navigate('/settings/downloads')}>
        {t('disk.limits')}
      </Button>
    </Card>
  );
}
