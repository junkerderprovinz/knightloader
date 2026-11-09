import { useEffect, useState } from 'react';
import { BrandPicker, type Brand } from '../../components/BrandPicker';
import { Button, Modal } from '../../components/ui';
import { IconAdd, IconClose } from '../../lib/icons';
import { useT } from '../../lib/i18n';
import {
  fetchEventTargets,
  fetchPlaceholders,
  newChannel,
  nextId,
  type Channel,
  type ChannelKind,
  type EventTargetStatus,
  type Placeholder,
} from '../../lib/eventtargets';
import { FALLBACK_TRIGGERS, fetchScriptTriggers } from '../../lib/scripts';
import { TARGET_BRANDS } from '../../lib/targetBrands';
import { useDraft } from './context';
import { KINDS, useKindName } from './eventtargets/kinds';
import { TargetMark } from './eventtargets/TargetMark';
import { TargetRow } from './eventtargets/TargetRow';
import { ModuleCard } from './ModuleToggle';

/**
 * EventTargetsCard lists the services this instance tells when something
 * happens, each a channel of one kind with its own events. It has a name of
 * its own so it is not taken for the in-browser notifications.
 *
 * Add asks first what the channel is. A new channel starts switched off, so
 * the server takes it into the settings before anything is filled in; it
 * checks a channel in full only once it is switched on. Secrets come back as
 * stars and are merged back on save only while the channel keeps its
 * destination.
 */

/** useHealth fetches the health table keyed by channel id, again whenever the stored channels change. */
function useHealth(stored: string): Record<string, EventTargetStatus> {
  const [health, setHealth] = useState<Record<string, EventTargetStatus>>({});
  useEffect(() => {
    let alive = true;
    void fetchEventTargets().then(
      (list) => {
        if (!alive) return;
        const byID: Record<string, EventTargetStatus> = {};
        for (const s of list) byID[s.id] = s;
        setHealth(byID);
      },
      () => {
        /* No status is drawn when nothing answered. */
      },
    );
    return () => {
      alive = false;
    };
  }, [stored]);
  return health;
}

/** useVocabulary loads the trigger and placeholder lists from the server. */
function useVocabulary(): { triggers: string[]; placeholders: Placeholder[] } {
  const [triggers, setTriggers] = useState<string[]>(FALLBACK_TRIGGERS);
  const [placeholders, setPlaceholders] = useState<Placeholder[]>([]);
  useEffect(() => {
    let alive = true;
    void fetchScriptTriggers().then((list) => {
      if (alive) setTriggers(list);
    });
    void fetchPlaceholders().then((list) => {
      if (alive) setPlaceholders(list);
    });
    return () => {
      alive = false;
    };
  }, []);
  return { triggers, placeholders };
}

export function EventTargetsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const kindName = useKindName();
  const { cfg, saved, patch } = useDraft();

  const rows = cfg.notifyChannels ?? [];
  const health = useHealth(JSON.stringify(saved.notifyChannels ?? []));
  const { triggers, placeholders } = useVocabulary();

  const [openRow, setOpenRow] = useState('');
  const [picking, setPicking] = useState(false);

  // An emptied list goes out as [], never undefined, which the shell's diff
  // would send as a key without a value.
  const write = (next: Channel[]) => patch({ notifyChannels: next });

  const add = (kind: string) => {
    const c = newChannel(nextId(rows), kind as ChannelKind, kindName(kind as ChannelKind));
    setPicking(false);
    write([...rows, c]);
    setOpenRow(c.id);
  };

  const brands: Brand[] = KINDS.map(({ kind, host, icon }) => {
    if (kind === 'discord') {
      const b = TARGET_BRANDS.find((x) => x.id === 'discord')!;
      return { id: kind, name: b.name, mark: <TargetMark brand="discord" size={25} lit />, paint: b.logo };
    }
    const mark = icon && (
      <span className="glim-house-mark flex h-full w-full items-center justify-center">{icon}</span>
    );
    return host ? { id: kind, name: kindName(kind), host } : { id: kind, name: kindName(kind), mark, paint: { tile: '#000000', ink: '#ffffff' } };
  });

  return (
    // Switched off, the channels are parked on the server and the card folds,
    // so Add cannot put a channel in their place.
    <ModuleCard
      id="eventtargets"
      hue={hue}
      className="flex flex-col gap-4"
      titleHint={t('settings.eventTargets.titleHint')}
      titleRight={
        <Button kind="secondary" icon={<IconAdd width={16} height={16} />} data-new="discord-target" onClick={() => setPicking(true)}>
          {t('settings.eventTargets.add')}
        </Button>
      }
    >
      <div>
        {rows.length === 0 ? (
          <p className="py-6 text-center text-sm text-carbon-textSub">
            {t('settings.eventTargets.empty')}
            <span className="mt-1 block text-meta text-carbon-textMuted">{t('settings.eventTargets.emptyHint')}</span>
          </p>
        ) : (
          <ul className="flex flex-col">
            {rows.map((row, i) => (
              <TargetRow
                key={row.id}
                row={row}
                index={i}
                last={i === rows.length - 1}
                triggers={triggers}
                placeholders={placeholders}
                status={health[row.id]}
                open={openRow === row.id}
                onToggle={() => setOpenRow(openRow === row.id ? '' : row.id)}
                onChange={(next) => write(rows.map((r) => (r.id === row.id ? next : r)))}
                onRemove={() => write(rows.filter((r) => r.id !== row.id))}
              />
            ))}
          </ul>
        )}
      </div>
      {picking && (
        <Modal
          title={t('settings.eventTargets.add')}
          hint={t('settings.eventTargets.pickHint')}
          wide
          onClose={() => setPicking(false)}
          footer={
            <>
              <span className="flex-1" />
              <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={() => setPicking(false)} />
            </>
          }
        >
          <BrandPicker brands={brands} none="" onPick={add} />
        </Modal>
      )}
    </ModuleCard>
  );
}
