import { useEffect, useState } from 'react';
import { connectWS, fetchSettings, patchSettings } from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { Card, SectionTitle, ToggleRow } from './ui';

/**
 * FreeDownloadsCard holds the Premium only switch. The Accounts page draws it,
 * so the sidebar entry and the settings tab both show it, and it reads and
 * saves the setting itself because the sidebar entry has no settings draft. A
 * change saved anywhere else arrives with the settings broadcast.
 */
export function FreeDownloadsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { toast } = useToast();
  const [on, setOn] = useState<boolean | null>(null);

  useEffect(() => {
    let live = true;
    const load = () =>
      fetchSettings().then(
        (s) => {
          if (live) setOn(s.premiumOnly);
        },
        () => {
          /* no switch rather than a guess at its state */
        },
      );
    void load();
    const close = connectWS((type) => type === 'settings' && void load(), ['settings']);
    return () => {
      live = false;
      close();
    };
  }, []);

  if (on === null) return null;

  async function onChange(next: boolean) {
    setOn(next);
    try {
      setOn((await patchSettings({ premiumOnly: next })).premiumOnly);
    } catch (e) {
      setOn(!next);
      toast(t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail');
    }
  }

  return (
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle>{t('settings.accounts.freeTitle')}</SectionTitle>
      <ToggleRow
        label={t('settings.accounts.premiumOnly')}
        hint={t('settings.accounts.premiumOnlyHint')}
        checked={on}
        onChange={(v) => void onChange(v)}
      />
    </Card>
  );
}
