import { useEffect, useState } from 'react';
import { type Settings, fetchSettings, saveSettings } from '../lib/api';
import { useT } from '../lib/i18n';
import { PageHeader, Card, Button, Field, NumberInput, Toggle } from '../components/ui';

export function SettingsPage() {
  const { t } = useT();
  const [cfg, setCfg] = useState<Settings | null>(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    fetchSettings().then(setCfg);
  }, []);

  async function onSave() {
    if (!cfg) return;
    const applied = await saveSettings(cfg);
    setCfg(applied);
    setSaved(true);
    setTimeout(() => setSaved(false), 1800);
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('settings.title')} subtitle={t('settings.subtitle')} />

      {cfg && (
        <Card className="flex flex-col gap-5 max-w-2xl">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <Field label={t('settings.maxConcurrent')}>
              <NumberInput
                value={cfg.maxConcurrent}
                min={1}
                max={64}
                onValue={(v) => setCfg({ ...cfg, maxConcurrent: v })}
              />
            </Field>
            <Field label={t('settings.maxPerHost')}>
              <NumberInput value={cfg.maxPerHost} min={1} max={64} onValue={(v) => setCfg({ ...cfg, maxPerHost: v })} />
            </Field>
            <Field label={t('settings.speedLimit')} hint={t('settings.speedHint')}>
              <NumberInput
                value={Math.round(cfg.speedLimit / 1024)}
                min={0}
                step={256}
                onValue={(v) => setCfg({ ...cfg, speedLimit: Math.max(0, v) * 1024 })}
              />
            </Field>
          </div>
          <div className="flex flex-col gap-3">
            <Toggle
              checked={cfg.extract}
              onChange={(v) => setCfg({ ...cfg, extract: v })}
              label={t('settings.extract')}
            />
            <Toggle
              checked={cfg.deleteArchive}
              onChange={(v) => setCfg({ ...cfg, deleteArchive: v })}
              label={t('settings.deleteArchive')}
            />
            <Toggle
              checked={cfg.autoStart}
              onChange={(v) => setCfg({ ...cfg, autoStart: v })}
              label={t('settings.autoStart')}
            />
          </div>
          <div className="flex items-center gap-3">
            <Button onClick={onSave}>{t('settings.save')}</Button>
            {saved && <span className="text-statusOk text-sm">{t('settings.saved')}</span>}
          </div>
        </Card>
      )}
    </div>
  );
}
