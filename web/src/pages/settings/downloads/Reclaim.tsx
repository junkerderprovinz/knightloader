import { Card, FieldGroup, SectionTitle } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { fetchOptions } from '../../../lib/api';
import { useResource } from '../../../lib/useResource';
import { useDraft } from '../context';
import { choices, useTx } from '../tx';

// When a file already on disk counts as the download, for the pass that settles
// tasks from finished files instead of fetching them again.

export function ReclaimCard({ hue }: { hue: number }) {
  const { tx } = useTx();
  const { cfg, patch } = useDraft();
  const { data: options } = useResource(fetchOptions);
  const modes = options?.reclaimTrustModes ?? [];

  // The tiers come strictest first. Nothing is drawn without the list, because
  // reclaim.ParseTrust folds an unknown value onto a looser tier.
  if (modes.length === 0) return null;

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{tx('settings.advanced.reclaimTitle')}</SectionTitle>
      <FieldGroup
        layout="row"
        label={tx('settings.advanced.reclaimTrust')}
        hint={tx('settings.advanced.reclaimTrustHint')}
      >
        <Tabs
          variant="well"
          label={tx('settings.advanced.reclaimTrust')}
          active={cfg.reclaimTrust ?? ''}
          onSelect={(reclaimTrust) => patch({ reclaimTrust })}
          items={choices(tx, 'settings.advanced.reclaim.', modes)}
        />
      </FieldGroup>
    </Card>
  );
}
