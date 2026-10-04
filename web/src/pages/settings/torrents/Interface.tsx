import { useEffect, useState } from 'react';
import { Dropdown } from '../../../components/Dropdown';
import { Field } from '../../../components/ui';
import { useT } from '../../../lib/i18n';

/** engine.NetInterface and its json tags. */
interface NetInterface {
  name: string;
  up: boolean;
  addrs: string[] | null;
}

// A VPN that comes up or drops while the page is open shows within this long.
const POLL_MS = 5000;

/**
 * InterfaceField ties the built-in torrent client to one network interface.
 * The choices are the system's interfaces as the server sees them, and a saved
 * one the system does not have stays a choice, so the field never shows a
 * value it cannot name. Under it a line says where torrents go out, or that
 * they are on hold.
 */
export function InterfaceField({ value, onValue }: { value: string; onValue: (name: string) => void }) {
  const { t } = useT();
  const [list, setList] = useState<NetInterface[] | null>(null);

  useEffect(() => {
    let live = true;
    const load = async () => {
      try {
        const r = await fetch('/api/torrents/interfaces');
        if (!r.ok) return;
        const body = (await r.json()) as { interfaces: NetInterface[] | null };
        if (live) setList(body.interfaces ?? []);
      } catch {
        // An older server without the route offers only what is saved.
      }
    };
    void load();
    const timer = window.setInterval(() => void load(), POLL_MS);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, []);

  const ifs = list ?? [];
  const current = ifs.find((i) => i.name === value);
  const options = [
    { value: '', label: t('settings.torrents.interfaceAny') },
    ...ifs.map((i) => ({
      value: i.name,
      label: !i.up
        ? t('settings.torrents.interfaceGone', { name: i.name })
        : i.addrs?.length
          ? `${i.name} · ${i.addrs.join(', ')}`
          : i.name,
    })),
  ];
  if (value && !current) options.push({ value, label: t('settings.torrents.interfaceGone', { name: value }) });

  return (
    <div className="flex flex-col gap-1.5">
      <Field label={t('settings.torrents.interface')} hint={t('settings.torrents.interfaceHint')}>
        <Dropdown label={t('settings.torrents.interface')} value={value} options={options} onChange={onValue} />
      </Field>
      {value &&
        list &&
        (current?.up ? (
          <p className="text-xs text-carbon-textSub">
            {t('settings.torrents.interfaceUp', { addrs: (current.addrs ?? []).join(', ') })}
          </p>
        ) : (
          <p className="text-xs text-statusWarn">{t('overview.torrents.interfaceDown', { name: value })}</p>
        ))}
    </div>
  );
}
