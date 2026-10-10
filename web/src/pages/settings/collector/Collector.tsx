import { Card, Field, FieldGroup, NumberInput, SectionTitle, ToggleRow } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { fetchOptions } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useResource } from '../../../lib/useResource';
import { useDraft } from '../context';
import { SettingRow } from '../controls';
import { choices, useTx } from '../tx';

// The collector card: what happens to a batch on its way out of the collector.
// How long it waits, what happens to a link already in the list or already
// found dead, and where the confirmed batch lands in the queue.

/**
 * Labels for the confirm policies. A map rather than a built key because
 * 'exclude-and-remove' is '.excludeAndRemove' in the catalogue; an id without a
 * label shows as itself.
 */
export const CONFIRM_LABEL: Partial<Record<string, TranslationKey>> = {
  include: 'settings.downloads.confirm.include',
  exclude: 'settings.downloads.confirm.exclude',
  'exclude-and-remove': 'settings.downloads.confirm.excludeAndRemove',
  ask: 'settings.downloads.confirm.ask',
};

export function CollectorCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { tx } = useTx();
  const { cfg, patch } = useDraft();

  // The ids come from GET /api/options, which withholds confirm.UseGlobal
  // because a global default cannot defer to itself. Without an answer the
  // strips stay out rather than offering a guess at the policies.
  const { data: options } = useResource(fetchOptions);
  const policies = options?.confirmPolicies ?? [];

  const policyLabel = (id: string) => {
    const key = CONFIRM_LABEL[id];
    return key ? t(key) : id;
  };

  // The countdown's switch is `autoConfirm` in the link intake card above.
  const autoConfirming = cfg.autoConfirm;

  // An older server may not send the field.
  const delay = cfg.autoConfirmDelay ?? 0;

  // confirm.Parse folds an empty or unknown value onto exclude, the default.
  const dupes = cfg.onDupes || 'exclude';

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.downloads.collectorTitle')}</SectionTitle>

      {/* While no countdown runs the box gives way to a reading that points at
          the switch. */}
      {autoConfirming ? (
        <Field
          label={t('settings.downloads.autoConfirmDelay')}
          hint={t('settings.downloads.autoConfirmDelayHint')}
        >
          <NumberInput
            value={delay}
            min={0}
            max={86400}
            step={1}
            // The bounds of sanitizeConfirm. 0 confirms the moment a batch is
            // staged; it is not off.
            onValue={(v) => patch({ autoConfirmDelay: Math.max(0, Math.min(86400, Math.round(v) || 0)) })}
          />
        </Field>
      ) : (
        <FieldGroup
          label={t('settings.downloads.autoConfirmDelay')}
          hint={t('settings.downloads.autoConfirmDelayHint')}
        >
          <span className="text-sm text-carbon-textSub">{t('settings.downloads.autoConfirmOff')}</span>
        </FieldGroup>
      )}

      {policies.length > 0 && (
        <SettingRow label={t('settings.downloads.onDupes')} hint={t('settings.downloads.onDupesHint')}>
          <Tabs
            variant="well"
            size="sm"
            inline
            hug
            label={t('settings.downloads.onDupes')}
            active={dupes}
            onSelect={(onDupes) => patch({ onDupes })}
            items={policies.map((id) => ({ id, label: policyLabel(id) }))}
          />
        </SettingRow>
      )}

      {/* What becomes of a link a check has already found gone. */}
      {policies.length > 0 && (
        <SettingRow label={tx('settings.advanced.onOffline')} hint={tx('settings.advanced.onOfflineHint')}>
          <Tabs
            variant="well"
            size="sm"
            inline
            hug
            label={tx('settings.advanced.onOffline')}
            active={cfg.onOffline ?? ''}
            onSelect={(onOffline) => patch({ onOffline })}
            items={choices(tx, 'settings.advanced.offline.', policies)}
          />
        </SettingRow>
      )}

      {/* app.startTasks applies it however the batch was confirmed. */}
      <ToggleRow
        checked={cfg.addAtTop ?? false}
        onChange={(addAtTop) => patch({ addAtTop })}
        label={t('settings.downloads.addAtTop')}
        hint={t('settings.downloads.addAtTopHint')}
      />
    </Card>
  );
}
