import { useState, type ReactNode } from 'react';
import { Button, Modal } from './ui';
import { IconClose } from '../lib/icons';
import { useT, type TranslationKey } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { useClipboardWatch } from '../lib/useClipboardWatch';
import { listWatchers, stopWatcher, watcherId, type ClipboardWatcher } from '../lib/clipboardWatchers';

const KIND_LABEL: Record<ClipboardWatcher['kind'], TranslationKey> = {
  web: 'intake.watcherKind.web',
  extension: 'intake.watcherKind.extension',
  desktop: 'intake.watcherKind.desktop',
};

/**
 * useWatchSwitch is the clipboard watch's switch for the places that flip it.
 * Switching on first asks the group who else watches; when another device
 * does, `dialog` names it and offers to switch it off there or keep both.
 * Without an answer from the group the watch goes on as asked.
 */
export function useWatchSwitch(): { watch: boolean; flip: (on: boolean) => void; dialog: ReactNode } {
  const [watch, setWatch] = useClipboardWatch();
  const [others, setOthers] = useState<ClipboardWatcher[]>([]);

  async function flip(on: boolean) {
    if (!on) {
      setWatch(false);
      return;
    }
    let found: ClipboardWatcher[] = [];
    try {
      const me = watcherId();
      found = (await listWatchers()).filter((w) => w.id !== me);
    } catch {
      // Nobody to warn about is the better guess than refusing the switch.
    }
    if (found.length === 0) setWatch(true);
    else setOthers(found);
  }

  const dialog =
    others.length > 0 ? (
      <WatchElsewhereDialog
        others={others}
        onClose={() => setOthers([])}
        onDecided={() => {
          setOthers([]);
          setWatch(true);
        }}
      />
    ) : null;

  return { watch, flip: (on) => void flip(on), dialog };
}

function WatchElsewhereDialog({
  others,
  onClose,
  onDecided,
}: {
  others: ClipboardWatcher[];
  onClose: () => void;
  onDecided: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);

  async function stopThere() {
    setBusy(true);
    for (const w of others) {
      try {
        await stopWatcher(w.id);
      } catch {
        toast(t('intake.watchElsewhereStopFailed', { device: w.name }), 'fail');
      }
    }
    setBusy(false);
    onDecided();
  }

  return (
    <Modal
      title={t('intake.watchElsewhereTitle')}
      onClose={onClose}
      footer={
        <>
          <span className="flex-1" />
          <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} disabled={busy} onClick={onClose} />
          <Button kind="secondary" disabled={busy} onClick={onDecided}>
            {t('intake.watchElsewhereKeep')}
          </Button>
          <Button disabled={busy} onClick={() => void stopThere()}>
            {t('intake.watchElsewhereStop')}
          </Button>
        </>
      }
    >
      <ul className="flex flex-col gap-1.5">
        {others.map((w) => (
          <li key={w.id} className="text-sm text-carbon-text">
            {t('intake.watcherLine', { device: w.name, kind: t(KIND_LABEL[w.kind]), instance: w.instance ?? '' })}
          </li>
        ))}
      </ul>
      <p className="text-xs text-carbon-textMuted">{t('intake.watchElsewhereBody')}</p>
    </Modal>
  );
}
