import { useState } from 'react';
import { ToggleRow } from '../../components/ui';
import { useToast } from '../../lib/toast';
import { useFeatures } from './context';
import { label, moduleReason, useTx } from './tx';

/**
 * ModuleToggle is a module's switch on its own page. It goes through the
 * module registry rather than the draft, so it and the Modules page are one
 * switch, and it is left out when the server has no such module.
 */
export function ModuleToggle({ id, hue = 0 }: { id: string; hue?: number }) {
  const { tx } = useTx();
  const { features, toggle } = useFeatures();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);

  const m = features.modules.find((f) => f.id === id);
  if (!m || m.verdict !== 'shipped') return null;

  async function onSwitch(next: boolean) {
    setBusy(true);
    try {
      await toggle(id, next);
    } catch (e) {
      // The server refuses a switch it cannot honour and says why.
      toast(tx('settings.modules.switchFailed', { reason: String(e).replace(/^Error:\s*/, '') }), 'fail');
    } finally {
      setBusy(false);
    }
  }

  return (
    <ToggleRow
      hue={hue}
      label={label(tx, 'settings.module.', id)}
      hint={m.switch === 'none' ? moduleReason(tx, m) : tx('settings.modules.pageSwitchHint')}
      checked={m.enabled}
      disabled={busy || m.switch === 'none'}
      onChange={(next) => void onSwitch(next)}
    />
  );
}
