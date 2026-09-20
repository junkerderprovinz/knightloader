import { useEffect, useState } from 'react';
import { Card, Field, FieldGroup, NumberInput, SectionTitle, ToggleRow } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { fetchOptions } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useDraft } from '../context';

// The collector card: what happens to a batch on its way out of the collector.
// How long it waits, what happens to a link already in the list, and where the
// confirmed batch lands in the queue.

/**
 * Labels for the confirm policies. A map rather than a built key because
 * 'exclude-and-remove' is '.excludeAndRemove' in the catalogue; an id without a
 * label shows as itself.
 */
const CONFIRM_LABEL: Partial<Record<string, TranslationKey>> = {
  include: 'settings.downloads.confirm.include',
  exclude: 'settings.downloads.confirm.exclude',
  'exclude-and-remove': 'settings.downloads.confirm.excludeAndRemove',
  ask: 'settings.downloads.confirm.ask',
};

export function CollectorCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  // The ids come from GET /api/options, which withholds confirm.UseGlobal
  // because a global default cannot defer to itself.
  const [policies, setPolicies] = useState<string[]>([]);
  useEffect(() => {
    let live = true;
    void fetchOptions().then(
      (o) => {
        if (live) setPolicies(o.confirmPolicies ?? []);
      },
      () => {
        /* the strip stays out rather than offering a guess at the policies */
      },
    );
    return () => {
      live = false;
    };
  }, []);

  const policyLabel = (id: string) => {
    const key = CONFIRM_LABEL[id];
    return key ? t(key) : id;
  };

  // The countdown's switch is `autoConfirm` in the Linkeingang card on the
  // General page.
  const autoConfirming = cfg.autoConfirm;

  // An older server may not send the field.
  const delay = cfg.autoConfirmDelay ?? 0;

  // confirm.Parse folds an empty or unknown value onto exclude, the default.
  const dupes = cfg.onDupes || 'exclude';

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.downloads.collectorTitle')}</SectionTitle>

      {/* While no countdown runs the box gives way to a reading, which keeps
          the pointer to the switch on the other page. */}
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

      {/* FieldGroup, because a Field's label would pass a click on the
          caption to the first tab. */}
      {policies.length > 0 && (
        <FieldGroup layout="row" label={t('settings.downloads.onDupes')} hint={t('settings.downloads.onDupesHint')}>
          <Tabs
            variant="well"
            label={t('settings.downloads.onDupes')}
            active={dupes}
            onSelect={(onDupes) => patch({ onDupes })}
            items={policies.map((id) => ({ id, label: policyLabel(id) }))}
          />
        </FieldGroup>
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
