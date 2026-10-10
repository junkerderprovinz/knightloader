import { Card, Field, NumberInput, SectionTitle } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { fetchOptions } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { useResource } from '../../../lib/useResource';
import { COLLISION_LABEL } from '../Archives';
import { useDraft } from '../context';
import { SettingRow } from '../controls';
import { choices, useTx } from '../tx';

// The card for a file that is already on the disk: what a download does when
// its name is taken in the destination folder, how far "keep both" may count,
// and when a file found there counts as the download itself, for the pass that
// settles tasks from finished files instead of fetching them again. The
// collision labels are the extraction page's, since both strips answer the
// same question; the id lists stay apart because the server sends each its own.

export function CollisionCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { tx } = useTx();
  const { cfg, patch } = useDraft();

  // The ids come from GET /api/options, which withholds collide.Ask because a
  // download has nobody to ask. Without an answer the strips stay out rather
  // than offering a guess: reclaim.ParseTrust folds an unknown value onto a
  // looser tier.
  const { data: options } = useResource(fetchOptions);
  const policies = options?.collisionPolicies ?? [];
  // The tiers come strictest first.
  const trustModes = options?.reclaimTrustModes ?? [];

  const policyLabel = (id: string) => {
    const key = COLLISION_LABEL[id];
    return key ? t(key) : id;
  };

  // collide.ParsePolicy folds an empty value onto rename, the default.
  const policy = cfg.collisionPolicy || 'rename';
  const renaming = policy === 'rename';

  // The Go field is omitempty and absent whenever it is 0.
  const attempts = cfg.collisionMaxAttempts ?? 0;

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{tx('settings.advanced.reclaimTitle')}</SectionTitle>

      {policies.length > 0 && (
        <SettingRow label={t('settings.downloads.collision')} hint={t('settings.downloads.collisionHint')}>
          <Tabs
            variant="well"
            size="sm"
            inline
            hug
            label={t('settings.downloads.collision')}
            active={policy}
            onSelect={(collisionPolicy) => patch({ collisionPolicy })}
            items={policies.map((id) => ({ id, label: policyLabel(id) }))}
          />
        </SettingRow>
      )}

      {/* Only "keep both" counts attempts, so the box is absent otherwise and
          comes back with its number unchanged. */}
      {renaming && (
        <Field label={t('settings.downloads.collisionAttempts')} hint={t('settings.downloads.collisionAttemptsHint')}>
          <NumberInput
            value={attempts}
            min={0}
            max={1000}
            step={1}
            // The bounds of sanitizeIntake. 0 means the package's own cap of
            // 1000, not "no limit".
            onValue={(v) => patch({ collisionMaxAttempts: Math.max(0, Math.min(1000, Math.round(v) || 0)) })}
          />
        </Field>
      )}

      {trustModes.length > 0 && (
        <SettingRow label={tx('settings.advanced.reclaimTrust')} hint={tx('settings.advanced.reclaimTrustHint')}>
          <Tabs
            variant="well"
            size="sm"
            inline
            hug
            label={tx('settings.advanced.reclaimTrust')}
            active={cfg.reclaimTrust ?? ''}
            onSelect={(reclaimTrust) => patch({ reclaimTrust })}
            items={choices(tx, 'settings.advanced.reclaim.', trustModes)}
          />
        </SettingRow>
      )}
    </Card>
  );
}
