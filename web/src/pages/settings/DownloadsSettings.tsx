import { useEffect, useState } from 'react';
import { Card, Field, FieldGroup, NumberInput, SectionTitle, ToggleRow } from '../../components/ui';
import { Tabs } from '../../components/Tabs';
import { fetchOptions } from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useDraft } from './context';
import { ModuleToggle } from './ModuleToggle';
import { SettingPathInput } from './controls';
// Each card owns one subject in ./downloads and shares the draft through
// useDraft; the page passes the hues because it decides the order.
import { CollisionCard } from './downloads/Collision';
import { ChunksField, MaxConcurrentField, MaxPerHostField } from './downloads/Concurrency';
import { DiskSpaceCard } from './downloads/DiskSpace';
import { FeedsCard } from './downloads/Feeds';
import { FolderCheckCard } from './downloads/FolderCheck';
import { ReclaimCard } from './downloads/Reclaim';
import { SpeedLimitField } from './downloads/SpeedLimit';
import { StallCard } from './downloads/Stall';
import { VolumeCapCard } from './downloads/VolumeCap';

// Not Downloads, which is already the name of pages/Downloads.
export function DownloadsSettings() {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  // The resume modes come from the server.
  const [modes, setModes] = useState<string[]>([]);
  useEffect(() => {
    let live = true;
    void fetchOptions().then(
      (o) => {
        if (live) setModes(o.resumeModes ?? []);
      },
      () => {
        /* the strip stays out rather than offering a guess at the modes */
      },
    );
    return () => {
      live = false;
    };
  }, []);

  return (
    <div className="flex flex-col gap-10">
      <Card hue={0} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.downloads.locationTitle')}</SectionTitle>
        <Field
          label={t('settings.downloadDir')}
          hint={`${t('settings.downloadDirHint')} ${t('settings.pathVars')}`}
        >
          {/* The chooser browses the server and keeps a <jd:…> tail. */}
          <SettingPathInput
            field="downloadDir"
            value={cfg.downloadDir}
            placeholder="/downloads"
            onValue={(downloadDir) => patch({ downloadDir })}
          />
        </Field>
        <ToggleRow
          checked={cfg.subfolderByPackage}
          onChange={(v) => patch({ subfolderByPackage: v })}
          label={t('settings.subfolderByPackage')}
        />
        {/* The hint names no variables, because a template here would scatter
            the parts of a multi-volume archive across folders. */}
        <Field label={t('settings.downloads.workDir')} hint={t('settings.downloads.workDirHint')}>
          <SettingPathInput
            field="workDir"
            value={cfg.workDir}
            placeholder="/downloads/.incoming"
            title={t('settings.downloads.workDir')}
            onValue={(workDir) => patch({ workDir })}
          />
        </Field>
      </Card>

      <ReclaimCard hue={1} />

      <CollisionCard hue={2} />

      <Card hue={3} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.downloads.limitsTitle')}</SectionTitle>
        {/* Read together: two downloads on one host with eight connections
            each open sixteen. */}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <MaxConcurrentField value={cfg.maxConcurrent} onValue={(maxConcurrent) => patch({ maxConcurrent })} />
          <MaxPerHostField value={cfg.maxPerHost} onValue={(maxPerHost) => patch({ maxPerHost })} />
          <ChunksField value={cfg.chunks} onValue={(chunks) => patch({ chunks })} />
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <SpeedLimitField value={cfg.speedLimit} onValue={(speedLimit) => patch({ speedLimit })} />
          <Field label={t('settings.maxRetries')} hint={t('settings.maxRetriesHint')}>
            <NumberInput value={cfg.maxRetries} min={0} max={20} onValue={(v) => patch({ maxRetries: v })} />
          </Field>
        </div>
        {modes.length > 0 && (
          <FieldGroup layout="row" label={t('settings.resumeOnStart')} hint={t('settings.resumeOnStartHint')}>
            <Tabs
              variant="well"
              label={t('settings.resumeOnStart')}
              active={cfg.resumeOnStart}
              onSelect={(id) => patch({ resumeOnStart: id })}
              items={modes.map((m) => ({ id: m, label: t(`settings.resume.${m}` as TranslationKey) }))}
            />
          </FieldGroup>
        )}

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t('settings.keepFinishedDays')} hint={t('settings.keepFinishedDaysHint')}>
            <NumberInput
              value={cfg.keepFinishedDays}
              min={0}
              max={3650}
              onValue={(v) => patch({ keepFinishedDays: Math.max(0, v) })}
            />
          </Field>
          <Field label={t('settings.historyMax')} hint={t('settings.historyMaxHint')}>
            <NumberInput
              value={cfg.historyMax}
              min={0}
              max={1000000}
              onValue={(v) => patch({ historyMax: Math.max(0, v) })}
            />
          </Field>
        </div>

        <div className="flex flex-col gap-3">
          <ModuleToggle id="checksums" hue={1} hint={t('settings.verifyChecksums')} />
          <ToggleRow
            hue={2}
            checked={cfg.preParserEnabled}
            onChange={(v) => patch({ preParserEnabled: v })}
            label={t('settings.preParser')}
            hint={t('settings.preParserHint')}
          />
        </div>
      </Card>

      <StallCard hue={4} />
      <DiskSpaceCard hue={5} />

      <VolumeCapCard hue={6} />

      {/* setFeature's refusal points people here to add a feed. */}
      <FeedsCard hue={7} />

      <FolderCheckCard hue={8} />
    </div>
  );
}
