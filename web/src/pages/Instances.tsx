// Instances puts everything about other KnightLoaders behind one entry, a tab
// each. Pairing comes first, because the Fleet tab shows the group it sets up.
// Pairing always shows, since the phone app and the browser extension join
// the same group; Fleet follows the Instances switch on the Modules page.
import { useEffect, useState } from 'react';
import { connectWS } from '../lib/api';
import { useT } from '../lib/i18n';
import { IconInstances, IconLink } from '../lib/icons';
import { useTabSlide } from '../lib/motion';
import { PageHeader } from '../components/ui';
import { Tabs } from '../components/Tabs';
import { fetchFeatures } from './settings/features';
import { Fleet } from './instances/Fleet';
import { Pairing } from './instances/Pairing';

export const INSTANCE_TABS = ['pairing', 'fleet'] as const;
export type InstanceTab = (typeof INSTANCE_TABS)[number];

function isTab(v: string): v is InstanceTab {
  return (INSTANCE_TABS as readonly string[]).includes(v);
}

/** The tab a fresh mount shows: the one the address names, else the first. */
function tabFromHash(): InstanceTab {
  const h = window.location.hash.replace(/^#/, '');
  return isTab(h) ? h : 'pairing';
}

export function Instances() {
  const { t } = useT();
  const [tab, setTab] = useState<InstanceTab>(tabFromHash);
  // Until the modules answer the Fleet tab is assumed on, the usual case,
  // so the strip does not appear a moment late.
  const [fleetOn, setFleetOn] = useState(true);

  useEffect(() => {
    const loadModules = () =>
      fetchFeatures()
        .then((f) => setFleetOn(!f.modules.some((m) => m.id === 'federation' && m.verdict === 'shipped' && !m.enabled)))
        .catch(() => {});
    loadModules();
    const close = connectWS(
      (type) => {
        if (type === 'settings') loadModules();
      },
      ['settings'],
    );
    // A hash typed into the address bar switches the tab.
    const onHash = () => {
      const h = window.location.hash.replace(/^#/, '');
      if (isTab(h)) setTab(h);
    };
    window.addEventListener('hashchange', onHash);
    return () => {
      close();
      window.removeEventListener('hashchange', onHash);
    };
  }, []);

  const visible = INSTANCE_TABS.filter((k) => k !== 'fleet' || fleetOn);
  // A tab the address names whose switch is off falls back to the first one.
  const active: InstanceTab = visible.includes(tab) ? tab : visible[0];
  const slide = useTabSlide(active, [...visible]);

  function choose(next: InstanceTab) {
    setTab(next);
    window.history.replaceState(null, '', `#${next}`);
  }

  // One literal key per tab, so every label is a checked TranslationKey.
  const tabLabel: Record<InstanceTab, string> = {
    pairing: t('pairing.title'),
    fleet: t('fleet.title'),
  };
  const tabIcon = { pairing: <IconLink />, fleet: <IconInstances /> };

  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('instances.title')} />

      {visible.length > 1 && (
        <Tabs
          fit
          labelled
          label={t('instances.title')}
          active={active}
          onSelect={(id) => {
            if (isTab(id)) choose(id);
          }}
          items={visible.map((k) => ({ id: k, label: tabLabel[k], icon: tabIcon[k] }))}
        />
      )}

      <div key={active} className={slide.className} style={slide.style}>
        {active === 'pairing' && <Pairing />}
        {active === 'fleet' && <Fleet onOpenPairing={() => choose('pairing')} />}
      </div>
    </div>
  );
}
