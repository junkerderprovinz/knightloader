import { useEffect } from 'react';
import { useT } from '../../lib/i18n';
import { closeWhatsNew, openWhatsNew, startWhatsNew, useWhatsNew, useWhatsNewOpen } from '../../lib/useWhatsNew';
import { Button } from '../ui';
import { NewDots } from './NewDots';
import { WhatsNewWindow, newsTitle } from './WhatsNewWindow';
import { IconWhatsNew } from '../../lib/icons';

/**
 * WhatsNew is everything an update shows, mounted once in app/Layout.tsx: the
 * dots on the pages and the window with a card per release, which opens by
 * itself the first time a new version runs.
 */
export function WhatsNew() {
  const news = useWhatsNew();
  const open = useWhatsNewOpen();

  useEffect(() => {
    // Without the version or the stored state there is nothing to compare, and
    // the next load asks again.
    startWhatsNew().catch(() => {});
  }, []);

  if (!news) return null;
  return (
    <>
      <NewDots news={news} />
      {open && <WhatsNewWindow news={news} onClose={closeWhatsNew} />}
    </>
  );
}

/**
 * WhatsNewEntry is the way from the event list to the window, with the number
 * still unseen. It is drawn only while the window lists a change. `onOpen`
 * lets the event list close itself first.
 */
export function WhatsNewEntry({ onOpen }: { onOpen: () => void }) {
  const { t } = useT();
  const news = useWhatsNew();
  if (!news || news.changes.length === 0) return null;
  const unseen = news.unseen.length;
  return (
    <Button
      icon={<IconWhatsNew />}
      kind="secondary"
      className="w-full"
      onClick={() => {
        onOpen();
        openWhatsNew();
      }}
    >
      {unseen > 0 && <span aria-hidden className="h-2 w-2 shrink-0 rounded-[var(--radius-pill)] bg-statusOkSolid" />}
      <span className="min-w-0 flex-1 truncate text-start">{newsTitle(t, news)}</span>
      {unseen > 0 && <span className="glim-num text-carbon-textSub">{unseen}</span>}
    </Button>
  );
}
