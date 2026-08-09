import { Card, Field, NumberInput, TextInput, Toggle } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { useDraft, useFeatures } from './context';
import { useTx } from './tx';

// Named DownloadsSettings and not Downloads: there is already a pages/Downloads
// page, and two components with one name in the same import graph is a mistake
// that compiles.
export function DownloadsSettings() {
  const { t } = useT();
  const { tx } = useTx();
  const { cfg, patch } = useDraft();
  const { features } = useFeatures();

  // The registry, not the settings value, decides whether the folder field is
  // live. Switching the folder-watch module off clears the folder and parks it;
  // leaving the field editable here would let somebody type a folder back in and
  // restart the watcher while the modules page still reads "off". The two would
  // then disagree, which is exactly what a kill switch may not do.
  //
  // `parked` and not just `!enabled`: an empty folder on a fresh install is also
  // "off", and locking the field for that reason would leave nowhere to type the
  // first folder — the switch cannot turn on what was never set up either, so
  // the two would deadlock and folder watch would be unreachable forever.
  const watch = features.modules.find((m) => m.id === 'watch');
  const watchOff = watch !== undefined && !watch.enabled && watch.parked;

  return (
    <div className="flex flex-col gap-6">
      <Card className="flex flex-col gap-5">
        {/* The three counts that decide how much is open at once, together
            because they are read together: two downloads on one host, each
            pulled over eight sockets, is sixteen connections to that host and
            neither number says so on its own. */}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Field label={t('settings.maxConcurrent')}>
            <NumberInput value={cfg.maxConcurrent} min={1} max={64} onValue={(v) => patch({ maxConcurrent: v })} />
          </Field>
          <Field label={t('settings.maxPerHost')}>
            <NumberInput value={cfg.maxPerHost} min={1} max={64} onValue={(v) => patch({ maxPerHost: v })} />
          </Field>
          {/* max is the engine's own bound, not a number picked here: a spinner
              that goes to 32 while every download opens 16 is a control that
              lies about what saving it did. */}
          <Field label={t('settings.chunks')} hint={t('settings.chunksHint')}>
            <NumberInput value={cfg.chunks} min={0} max={16} onValue={(v) => patch({ chunks: v })} />
          </Field>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field label={t('settings.speedLimit')} hint={t('settings.speedHint')}>
            <NumberInput
              value={Math.round(cfg.speedLimit / 1024)}
              min={0}
              step={256}
              onValue={(v) => patch({ speedLimit: Math.max(0, v) * 1024 })}
            />
          </Field>
          <Field label={t('settings.maxRetries')} hint={t('settings.maxRetriesHint')}>
            <NumberInput value={cfg.maxRetries} min={0} max={20} onValue={(v) => patch({ maxRetries: v })} />
          </Field>
        </div>
        <div className="flex flex-col gap-3">
          <Toggle checked={cfg.crawl} onChange={(v) => patch({ crawl: v })} label={t('settings.crawl')} />
          <Toggle
            checked={cfg.verifyChecksums}
            onChange={(v) => patch({ verifyChecksums: v })}
            label={t('settings.verifyChecksums')}
          />
        </div>
      </Card>

      <Card className="flex flex-col gap-5">
        {/* Disabled rather than hidden: a field that vanishes teaches nobody
            that the folder watch exists, and the module page is where it is
            switched — which the info bubble says. */}
        <div className={watchOff ? 'pointer-events-none opacity-40' : ''}>
          <Field
            label={t('settings.watchDir')}
            hint={
              watchOff
                ? `${t('settings.watchDirHint')} ${tx('settings.downloads.watchOff')}`
                : t('settings.watchDirHint')
            }
          >
            <TextInput
              dir="ltr"
              value={cfg.watchDir}
              placeholder="/watch"
              spellCheck={false}
              disabled={watchOff}
              onChange={(e) => patch({ watchDir: e.target.value })}
            />
          </Field>
        </div>
      </Card>
    </div>
  );
}
