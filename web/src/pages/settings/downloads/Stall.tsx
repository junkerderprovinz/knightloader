import { useState } from 'react';
import { Card, Field, NumberInput, SectionTitle, ToggleRow } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { useDraft } from '../context';

// Copies of the bounds in internal/settings/settings_stall.go, which no route
// serves.
const MIN_TIMEOUT = 60;
const MAX_TIMEOUT = 86400;
const MAX_RESTARTS = 20;

// What the switch writes when it is turned on with no earlier number to go
// back to: the server's DefaultStallTimeout.
const FIRST_TIMEOUT = 120;

/**
 * StallCard sets the mark for a download that is running at 0 B/s, the new
 * connections that follow it and the optional restart after that.
 */
export function StallCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  // A timeout of 0 is off, so the switch remembers the number it replaced.
  const enabled = cfg.stallTimeout > 0;
  const [lastTimeout, setLastTimeout] = useState(FIRST_TIMEOUT);

  // The value is taken when switching off, since the draft can still be empty
  // on the first render.
  const setEnabled = (on: boolean) => {
    if (on) {
      // sanitizeStall raises 1..59 to 60.
      patch({ stallTimeout: Math.max(MIN_TIMEOUT, lastTimeout) });
      return;
    }
    setLastTimeout(cfg.stallTimeout);
    // The switches below keep their values while the mark is off.
    patch({ stallTimeout: 0 });
  };

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.stall.title')}</SectionTitle>

      <ToggleRow
        hue={0}
        checked={enabled}
        onChange={setEnabled}
        label={t('settings.stall.enabled')}
        hint={t('settings.stall.enabledHint')}
      />

      {/* Absent while the mark is off, since nothing reads these then. */}
      {enabled && (
      <div className="flex flex-col gap-5">
        {/* min keeps the stepper out of 1..59, which sanitizeStall rewrites to
            60. onValue does not clamp, or 600 could not be typed. */}
        <Field label={t('settings.stall.timeout')} hint={t('settings.stall.timeoutHint')}>
          <NumberInput
            value={cfg.stallTimeout}
            min={MIN_TIMEOUT}
            max={MAX_TIMEOUT}
            step={60}
            onValue={(v) => patch({ stallTimeout: v })}
          />
        </Field>

        {/* Only what the built-in engine fetches gets new connections
            (app.markStallsLocked). */}
        <ToggleRow
          hue={1}
          checked={cfg.stallReconnect}
          onChange={(v) => patch({ stallReconnect: v })}
          label={t('settings.stall.reconnect')}
          hint={t('settings.stall.reconnectHint')}
        />

        {/* A switch of its own, since a restart throws away fetched bytes.
            The server exempts torrents (app.stallRestartDueLocked). */}
        <ToggleRow
          hue={2}
          checked={cfg.stallRestart}
          onChange={(v) => patch({ stallRestart: v })}
          label={t('settings.stall.restart')}
          hint={t('settings.stall.restartHint')}
        />

        {/* The server reads 0 as DefaultStallRestarts (3), so 0 stays
            reachable. Above 20 comes back as 20. */}
        {cfg.stallRestart && (
        <Field label={t('settings.stall.maxRestarts')} hint={t('settings.stall.maxRestartsHint')}>
          <NumberInput
            value={cfg.stallMaxRestarts}
            min={0}
            max={MAX_RESTARTS}
            step={1}
            onValue={(v) => patch({ stallMaxRestarts: v })}
          />
        </Field>
        )}
      </div>
      )}
    </Card>
  );
}
