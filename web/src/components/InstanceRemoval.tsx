// Taking something out of the group asks first, in the same window wherever
// the button stands: on the Pairing page's member list and on an instance's
// card.
import { useState } from 'react';
import { refuseDevice, removeApp, removeInstance, removeMember } from '../lib/api';
import { useT } from '../lib/i18n';
import { IconClose, IconTrash } from '../lib/icons';
import { useToast } from '../lib/toast';
import { Button, Modal } from './ui';

/** What the removal window is about: a phone or extension of the group, an
 *  instance of the group, an instance added by its address, or a device that
 *  waits for approval and is about to be refused. */
export type Removal = { kind: 'app' | 'member' | 'peer' | 'refuse'; id: string; name: string };

export function RemovalWindow({
  removal,
  onClose,
  onRemoved,
}: {
  removal: Removal;
  onClose: () => void;
  /** Called once the server has taken it out, to load the lists again. */
  onRemoved: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const [shake, setShake] = useState(0);

  async function remove() {
    setBusy(true);
    try {
      if (removal.kind === 'app') {
        await removeApp(removal.id);
      } else if (removal.kind === 'refuse') {
        await refuseDevice(removal.id);
      } else if (removal.kind === 'member') {
        await removeMember(removal.id);
      } else {
        const res = await removeInstance(removal.id);
        if (!res.ok) throw new Error(await res.text());
      }
      onClose();
      onRemoved();
    } catch (e) {
      toast(e instanceof Error && e.message ? e.message : t('pairing.actionError'), 'fail');
      setShake((n) => n + 1);
    } finally {
      setBusy(false);
    }
  }

  const refuse = removal.kind === 'refuse';
  return (
    <Modal
      title={refuse ? t('pairing.refuseTitle', { name: removal.name }) : t('instances.removeTitle', { name: removal.name })}
      onClose={onClose}
      footer={
        <>
          <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          <Button
            kind="primary"
            labelled
            icon={<IconTrash />}
            title={refuse ? t('pairing.refuse') : t('instances.remove')}
            disabled={busy}
            shake={shake}
            onClick={() => void remove()}
          />
        </>
      }
    >
      <p className="text-sm text-carbon-textSub">
        {refuse
          ? t('pairing.refuseConfirm', { name: removal.name })
          : removal.kind === 'app'
            ? t('instances.removeAppConfirm', { name: removal.name })
            : removal.kind === 'member'
              ? t('instances.removeMemberConfirm', { name: removal.name })
              : t('instances.removePeerConfirm', { name: removal.name })}
      </p>
    </Modal>
  );
}
