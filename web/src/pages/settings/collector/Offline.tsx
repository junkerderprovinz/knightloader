import { Card, FieldGroup, SectionTitle } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { fetchOptions } from '../../../lib/api';
import { useResource } from '../../../lib/useResource';
import { useDraft } from '../context';
import { choices, useTx } from '../tx';

// What becomes of a link a check has already found gone, as its batch leaves
// the collector.

export function OfflineCard({ hue }: { hue: number }) {
  const { tx } = useTx();
  const { cfg, patch } = useDraft();
  // The server withholds "use-global", since a global default cannot defer to
  // itself.
  const { data: options } = useResource(fetchOptions);
  const policies = options?.confirmPolicies ?? [];

  // Only with a menu, since the card holds this one control.
  if (policies.length === 0) return null;

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{tx('settings.advanced.offlineTitle')}</SectionTitle>
      <FieldGroup
        layout="row"
        label={tx('settings.advanced.onOffline')}
        hint={tx('settings.advanced.onOfflineHint')}
      >
        <Tabs
          variant="well"
          label={tx('settings.advanced.onOffline')}
          active={cfg.onOffline ?? ''}
          onSelect={(onOffline) => patch({ onOffline })}
          items={choices(tx, 'settings.advanced.offline.', policies)}
        />
      </FieldGroup>
    </Card>
  );
}
