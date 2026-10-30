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

/** NewsGlyph is What's new's gift, with the green dot in front of it while a change is unseen. */
export function NewsGlyph({ unseen }: { unseen: boolean }) {
  return (
    <>
      {unseen && <span aria-hidden className="me-1.5 h-2 w-2 shrink-0 rounded-[var(--radius-pill)] bg-statusOkSolid" />}
      <IconWhatsNew />
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
  const title = newsTitle(t, news);
  return (
    <Button
      kind="secondary"
      className="w-full"
      icon={<NewsGlyph unseen={unseen > 0} />}
      label={unseen > 0 ? `${title} · ${unseen}` : title}
      onClick={() => {
        onOpen();
        openWhatsNew();
      }}
    >
      <span className="min-w-0 flex-1 truncate text-start">{title}</span>
      {unseen > 0 && <span className="glim-num text-carbon-textSub">{unseen}</span>}
    </Button>
  );
}
