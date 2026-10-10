// "Add links" as a page's floating action, for the pages that show downloads
// without holding the add form themselves: the overview and the download list.
// It opens the collector's own form in a window, so adding is found in the
// same corner on those pages and works as it does in the collector.
import { useMemo, useRef, useState, type ReactNode } from 'react';
import { useT } from '../lib/i18n';
import { IconClose, IconPlus } from '../lib/icons';
import { useTasks } from '../lib/useTasks';
import { AddLinksForm, useStagedReport } from './AddLinksForm';
import { FileDrop, newestContainerLink, type FileDropHandle } from './FileDrop';
import { PageAction } from './PageActions';
import { Button, Modal } from './ui';

/**
 * useAddLinks is the floating action and the window it opens. The action goes
 * into the page's PageActions and the window beside the page's other windows:
 * the actions' slot lets the pointer through to the page under it, which a
 * window drawn inside it would do as well.
 */
export function useAddLinks(): { action: ReactNode; dialog: ReactNode } {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  return {
    action: <PageAction primary icon={<IconPlus />} label={t('collector.add')} onClick={() => setOpen(true)} />,
    dialog: open ? <AddLinksDialog onClose={() => setOpen(false)} /> : null,
  };
}

function AddLinksDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT();
  const tasks = useTasks('');
  const [pkg, setPkg] = useState('');
  const fileDrop = useRef<FileDropHandle>(null);
  const report = useStagedReport();
  const landedAt = useMemo(() => newestContainerLink(tasks), [tasks]);

  return (
    <Modal
      title={t('collector.add')}
      height="capped"
      onClose={onClose}
      footer={
        <>
          <span className="flex-1" />
          <Button kind="secondary" labelled icon={<IconClose />} title={t('common.close')} onClick={onClose} />
        </>
      }
    >
      <AddLinksForm
        bare
        pkg={pkg}
        onPkgChange={setPkg}
        onStaged={(created, submittedCount) => {
          report(created, submittedCount);
          // A file's outcome is read in the window, so only a sent batch of
          // links closes it.
          if (created.length) onClose();
        }}
        onChooseFile={() => fileDrop.current?.openPicker()}
        onFilesDropped={(files) => fileDrop.current?.handleFiles(files)}
        footer={<FileDrop ref={fileDrop} pkg={pkg} landedAt={landedAt} />}
      />
    </Modal>
  );
}
