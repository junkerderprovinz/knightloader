import { Card, SectionTitle, ToggleRow } from '../../../components/ui';
import { Dropdown } from '../../../components/Dropdown';
import { fetchOptions } from '../../../lib/api';
import { useResource } from '../../../lib/useResource';
import { useDraft } from '../context';
import { SettingRow } from '../controls';
import { label, useTx } from '../tx';

// What becomes of a link that is another copy of a file already in the list.

export function MirrorsCard({ hue }: { hue: number }) {
  const { tx } = useTx();
  const { cfg, patch } = useDraft();
  // An older server may leave the list undefined.
  const { data: options } = useResource(fetchOptions);
  const policies = options?.mirrorPolicies ?? [];

  return (
    <Card hue={hue} data-new="mirror-policy" className="flex flex-col gap-5">
      <SectionTitle>{tx('settings.advanced.mirrorsTitle')}</SectionTitle>

      {/* Drawn only once the server answered, since dedupe.ParsePolicy
          silently folds an unknown value onto the default. */}
      {policies.length > 0 && (
        <SettingRow label={tx('settings.advanced.mirrorPolicy')} hint={tx('settings.advanced.mirrorPolicyHint')}>
          <Dropdown
            label={tx('settings.advanced.mirrorPolicy')}
            value={cfg.mirrorPolicy ?? null}
            onChange={(mirrorPolicy) => patch({ mirrorPolicy })}
            options={policies.map((id) => ({ value: id, label: label(tx, 'settings.advanced.mirror.', id) }))}
            width="widest"
          />
        </SettingRow>
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

      {/* Live under every policy, since the same URL always counts. */}
      <ToggleRow
        hue={2}
        checked={cfg.rejectDownloaded ?? true}
        onChange={(rejectDownloaded) => patch({ rejectDownloaded })}
        label={tx('settings.advanced.rejectDownloaded')}
        hint={tx('settings.advanced.rejectDownloadedHint')}
      />
    </Card>
  );
}
