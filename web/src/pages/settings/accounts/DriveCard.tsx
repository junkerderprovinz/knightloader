import { useCallback, useState } from 'react';
import { Button, Card, Field, InfoBubble, NumberInput, SectionTitle } from '../../../components/ui';
import { appAddress } from '../../../lib/basePath';
import { copyToClipboard } from '../../../lib/clipboard';
import { useT } from '../../../lib/i18n';
import { useDraft, useFeatures } from '../context';
import { Reading } from '../health/Reading';
import { ModuleToggle } from '../ModuleToggle';

/** Mirrors settings.DebridDrive. lib/api.ts's Settings does not name it, as with `torrent`. */
interface DriveSettings {
  enabled: boolean;
  refreshMinutes: number;
}

// For an older server that sends no `debridDrive`; mirrors settings.defaultDebridDrive().
const DEFAULTS: DriveSettings = { enabled: false, refreshMinutes: 5 };

// A day, as settings.maxDriveRefresh caps it.
const REFRESH_MAX = 24 * 60;

function readDrive(cfg: unknown): DriveSettings {
  return { ...DEFAULTS, ...((cfg as { debridDrive?: Partial<DriveSettings> }).debridDrive ?? {}) };
}

/**
 * DriveCard switches the debrid drive, the read-only WebDAV share of what is on
 * the debrid accounts, and hands over what rclone needs to mount it. The switch
 * goes through the module registry, so it is the Modules page's switch too.
 */
export function DriveCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { features } = useFeatures();
  const drive = readDrive(cfg);
  const on = features.modules.find((m) => m.id === 'debriddrive')?.enabled ?? false;

  const write = useCallback(
    (fields: Partial<DriveSettings>) => {
      patch({ debridDrive: { ...readDrive(cfg), ...fields } } as unknown as Parameters<typeof patch>[0]);
    },
    [cfg, patch],
  );

  // Absolute, because it goes into rclone on another machine, and taken from
  // the address the reader used since the server cannot know it.
  const address = `${appAddress()}/dav/`;
  const snippet = [
    '[knightloader]',
    'type = webdav',
    `url = ${address}`,
    'vendor = other',
    `bearer_token = ${t('settings.accounts.driveTokenHere')}`,
  ].join('\n');

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.module.debriddrive')}</SectionTitle>

      <ModuleToggle id="debriddrive" hue={0} hint={t('settings.accounts.driveHint')} />

      <Field label={t('settings.accounts.driveRefresh')} hint={t('settings.accounts.driveRefreshHint')}>
        <div className="flex items-center gap-2">
          <NumberInput
            value={drive.refreshMinutes}
            min={1}
            max={REFRESH_MAX}
            onValue={(v) => write({ refreshMinutes: Math.min(REFRESH_MAX, Math.max(1, Math.round(v))) })}
          />
          <span className="glim-num shrink-0 text-xs text-carbon-textMuted">{t('settings.accounts.driveRefreshUnit')}</span>
        </div>
      </Field>

      <div className="flex flex-wrap items-end justify-between gap-3">
        <Reading label={t('settings.accounts.driveAddress')} hint={t('settings.accounts.driveAddressHint')} value={address} />
        <CopyButton text={address} />
      </div>

      <div className="flex flex-col gap-1.5">
        <span
          data-glim-label={t('settings.accounts.driveRclone')}
          className="flex items-center text-meta text-carbon-textMuted"
        >
          {t('settings.accounts.driveRclone')}
          <InfoBubble tip={t('settings.accounts.driveRcloneHint')} />
        </span>
        <pre
          dir="ltr"
          className="overflow-x-auto whitespace-pre rounded-[var(--radius-control)] bg-carbon-surface2 p-3 font-mono text-meta leading-relaxed text-carbon-textSub"
        >
          {snippet}
        </pre>
        <div>
          <CopyButton text={snippet} />
        </div>
      </div>

      {/* While the switch is off the address answers 404. */}
      {!on && <span className="text-meta text-carbon-textMuted">{t('settings.accounts.driveOffHint')}</span>}
    </Card>
  );
}

function CopyButton({ text }: { text: string }) {
  const { t } = useT();
  const [copied, setCopied] = useState(false);
  const [copies, setCopies] = useState(0);
  return (
    <Button
      kind="ghost"
      className="px-2.5 text-xs"
      confirm={copies}
      onClick={async () => {
        if (!(await copyToClipboard(text))) return;
        setCopied(true);
        setCopies((n) => n + 1);
        setTimeout(() => setCopied(false), 1800);
      }}
    >
      {copied ? t('common.copied') : t('common.copy')}
    </Button>
  );
}
