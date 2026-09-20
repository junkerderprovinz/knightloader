import { useEffect, useState } from 'react';
import { Card, Field, FieldGroup, NumberInput, SectionTitle, TextArea, TextInput, ToggleRow } from '../../components/ui';
import { PathInput } from '../../components/FolderPicker';
import { Tabs } from '../../components/Tabs';
import { fetchOptions } from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { isLeet } from '../../lib/leet';
import { useDraft } from './context';
// Each card owns one subject in ./downloads and shares the draft through
// useDraft; the page passes the hues because it decides the order.
// HeaderProfiles saves through its own routes instead.
import { CollisionCard } from './downloads/Collision';
import { CollectorCard } from './downloads/Collector';
import { DiskSpaceCard } from './downloads/DiskSpace';
import { FeedsCard } from './downloads/Feeds';
import { HeaderProfilesCard } from './downloads/HeaderProfiles';
import { FolderCheckCard } from './downloads/FolderCheck';
import { IdleActionCard } from './downloads/IdleAction';
import { HostRulesCard } from './downloads/HostRules';
import { MediaHooksCard } from './downloads/MediaHooks';
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
          <PathInput
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
        {/* A plain TextInput, because a template here would scatter the parts
            of a multi-volume archive across folders; sanitizeStaging drops one. */}
        <Field label={t('settings.downloads.workDir')} hint={t('settings.downloads.workDirHint')}>
          <TextInput
            value={cfg.workDir}
            placeholder="/downloads/.incoming"
            spellCheck={false}
            dir="ltr"
            onChange={(e) => patch({ workDir: e.target.value })}
          />
        </Field>
      </Card>

      <CollisionCard hue={1} />

      <Card hue={2} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.downloads.limitsTitle')}</SectionTitle>
        {/* Read together: two downloads on one host with eight connections
            each open sixteen. */}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Field label={t('settings.maxConcurrent')}>
            <NumberInput value={cfg.maxConcurrent} min={1} max={64} onValue={(v) => patch({ maxConcurrent: v })} />
          </Field>
          <Field label={t('settings.maxPerHost')}>
            <NumberInput value={cfg.maxPerHost} min={1} max={64} onValue={(v) => patch({ maxPerHost: v })} />
          </Field>
          {/* max is the engine's own bound. */}
          <Field label={t('settings.chunks')} hint={t('settings.chunksHint')}>
            <NumberInput value={cfg.chunks} min={0} max={16} onValue={(v) => patch({ chunks: v })} />
          </Field>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field label={t('settings.speedLimit')} hint={t('settings.speedHint')}>
            <span className="flex items-center gap-2">
              <span className="min-w-0 flex-1">
                <NumberInput
                  value={Math.round(cfg.speedLimit / 1024)}
                  min={0}
                  step={256}
                  onValue={(v) => patch({ speedLimit: Math.max(0, v) * 1024 })}
                />
              </span>
              {/* The 1337 easter egg (docs/easter-eggs.md). speedLimit is bytes
                  per second and the field shows KiB, so isLeet compares against
                  1337 KiB. The word stands beside the number rather than under
                  it, where it would read as a validation message. */}
              {isLeet(cfg.speedLimit) && (
                <span className="shrink-0 text-[11px] leading-none text-carbon-textMuted">
                  {t('settings.motion.storm')}
                </span>
              )}
            </span>
          </Field>
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
          <ToggleRow
            hue={1}
            checked={cfg.verifyChecksums}
            onChange={(v) => patch({ verifyChecksums: v })}
            label={t('settings.verifyChecksums')}
          />
          <ToggleRow
            hue={2}
            checked={cfg.preParserEnabled}
            onChange={(v) => patch({ preParserEnabled: v })}
            label={t('settings.preParser')}
            hint={t('settings.preParserHint')}
          />
        </div>
      </Card>

      <HostRulesCard hue={3} />

      {/* Its values live in the credential store, so the card saves itself. */}
      <HeaderProfilesCard hue={4} />

      <StallCard hue={5} />
      <DiskSpaceCard hue={6} />

      <VolumeCapCard hue={7} />

      <CollectorCard hue={8} />

      {/* The crawl settings are absent while crawling is off. */}
      <Card hue={9} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.crawl.title')}</SectionTitle>
        <ToggleRow hue={0} checked={cfg.crawl} onChange={(v) => patch({ crawl: v })} label={t('settings.crawl')} />

        {cfg.crawl && (
        <div className="flex flex-col gap-5">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {/* max is internal/crawler.MaxDepth. */}
            <Field label={t('settings.crawl.depth')} hint={t('settings.crawl.depthHint')}>
              <NumberInput value={cfg.crawlDepth} min={1} max={3} onValue={(v) => patch({ crawlDepth: v })} />
            </Field>
            {/* A depth of 1 is one page, so the page cap and the same-host rule
                only apply from 2. */}
            {cfg.crawlDepth >= 2 && (
            <Field label={t('settings.crawl.maxPages')} hint={t('settings.crawl.maxPagesHint')}>
              <NumberInput value={cfg.crawlMaxPages} min={1} max={200} onValue={(v) => patch({ crawlMaxPages: v })} />
            </Field>
            )}
          </div>

          {cfg.crawlDepth >= 2 && (
          <ToggleRow
            hue={1}
            checked={cfg.crawlSameHost}
            onChange={(v) => patch({ crawlSameHost: v })}
            label={t('settings.crawl.sameHost')}
            hint={t('settings.crawl.sameHostHint')}
          />
          )}

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label={t('settings.crawl.include')} hint={t('settings.crawl.includeHint')}>
              <TextArea
                rows={3}
                spellCheck={false}
                value={(cfg.crawlInclude ?? []).join('\n')}
                onChange={(e) => patch({ crawlInclude: e.target.value.split('\n').filter((p) => p.trim() !== '') })}
              />
            </Field>
            <Field label={t('settings.crawl.exclude')} hint={t('settings.crawl.excludeHint')}>
              <TextArea
                rows={3}
                spellCheck={false}
                value={(cfg.crawlExclude ?? []).join('\n')}
                onChange={(e) => patch({ crawlExclude: e.target.value.split('\n').filter((p) => p.trim() !== '') })}
              />
            </Field>
          </div>
        </div>
        )}
      </Card>

      {/* setFeature's refusal points people here to add a feed. */}
      <FeedsCard hue={10} />

      <IdleActionCard hue={11} />

      <FolderCheckCard hue={12} />

      <MediaHooksCard hue={13} />
    </div>
  );
}
