import { Card, FieldGroup, SectionTitle, ToggleRow } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { fetchOptions } from '../../../lib/api';
import { useResource } from '../../../lib/useResource';
import { useDraft } from '../context';
import { choices, useTx } from '../tx';

// What becomes of a link that is another copy of a file already in the list.

export function MirrorsCard({ hue }: { hue: number }) {
  const { tx } = useTx();
  const { cfg, patch } = useDraft();
  // An older server may leave the list undefined.
  const { data: options } = useResource(fetchOptions);
  const policies = options?.mirrorPolicies ?? [];

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{tx('settings.advanced.mirrorsTitle')}</SectionTitle>

      {/* FieldGroup, because a Field's label would pass a click on the
          caption to the first tab. Drawn only once the server answered, since
          dedupe.ParsePolicy silently folds an unknown value onto the default. */}
      {policies.length > 0 && (
        <FieldGroup
          layout="row"
          label={tx('settings.advanced.mirrorPolicy')}
          hint={tx('settings.advanced.mirrorPolicyHint')}
        >
          <Tabs
            variant="well"
            label={tx('settings.advanced.mirrorPolicy')}
            active={cfg.mirrorPolicy ?? ''}
            onSelect={(mirrorPolicy) => patch({ mirrorPolicy })}
            items={choices(tx, 'settings.advanced.mirror.', policies)}
          />
        </FieldGroup>
      )}

      {/* Not dimmed when the policy is off: it decides what becomes of a
          detection, and the bubble explains that. */}
      <ToggleRow
        hue={0}
        checked={cfg.keepMirrors ?? false}
        onChange={(keepMirrors) => patch({ keepMirrors })}
        label={tx('settings.advanced.keepMirrors')}
        hint={tx('settings.advanced.keepMirrorsHint')}
      />

      {/* Absent while nothing is kept, since there is nothing to release. */}
      {cfg.keepMirrors && (
        <ToggleRow
          hue={1}
          checked={cfg.mirrorFailover ?? false}
          onChange={(mirrorFailover) => patch({ mirrorFailover })}
          label={tx('settings.advanced.mirrorFailover')}
          hint={tx('settings.advanced.mirrorFailoverHint')}
        />
      )}
    </Card>
  );
}
