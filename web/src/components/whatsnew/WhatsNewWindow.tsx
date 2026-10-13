import { useNavigate } from 'react-router-dom';
import { useT, type TranslationKey } from '../../lib/i18n';
import { IconCheck, IconClose, IconEye, IconForward } from '../../lib/icons';
import type { NewsTab, WhatsNew } from '../../lib/useWhatsNew';
import { releaseNotesPage, releaseOf, routeOf, sentenceOf, type Change } from '../../lib/whatsNew';
import { label } from '../../pages/settings/tx';
import { Tabs } from '../Tabs';
import { Button, LinkBadge, Modal } from '../ui';
import { ReleaseNotes } from './ReleaseNotes';

const RAIL: Record<string, TranslationKey> = {
  '/': 'nav.overview',
  '/downloads': 'nav.downloads',
  '/collector': 'nav.collector',
  '/instances': 'nav.instances',
  '/accounts': 'nav.accounts',
  '/settings': 'nav.settings',
};

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

/** pageName is what the navigation calls the page at a route. */
function pageName(t: Translate, route: string): string {
  const settings = /^\/settings\/(.+)$/.exec(route);
  if (settings) return `${t('nav.settings')} · ${label(t, 'settings.nav.', settings[1])}`;
  return route in RAIL ? t(RAIL[route]) : route;
}

/** byPage groups the changes by the page "Go there" opens, in the order they are listed. */
function byPage(changes: Change[]): { route: string; changes: Change[] }[] {
  const pages: { route: string; changes: Change[] }[] = [];
  for (const change of changes) {
    const route = routeOf(change.to[0]);
    let page = pages.find((p) => p.route === route);
    if (!page) pages.push((page = { route, changes: [] }));
    page.changes.push(change);
  }
  return pages;
}

/**
 * WhatsNewWindow is what an update brought: the release notes of the running
 * version, and the list of what the dots mark with the way to each change.
 * `notes` is the text this build carries for the version, '' when it has none.
 */
export function WhatsNewWindow({
  news,
  notes,
  tab,
  onTab,
  onClose,
}: {
  news: WhatsNew;
  notes: string;
  tab: NewsTab;
  onTab: (tab: NewsTab) => void;
  onClose: () => void;
}) {
  const { t } = useT();
  const navigate = useNavigate();
  const title = t('whatsnew.title', { version: releaseOf(news.version) || news.version });
  const listed = news.changes.length > 0;
  const all = news.changes.map((c) => c.id);
  const left = news.unseen.length;

  function go(route: string) {
    onClose();
    navigate(route);
  }

  return (
    <Modal
      title={title}
      height="capped"
      // Release notes run to long lines and lists, which a question window's width breaks up.
      wide
      onClose={onClose}
      footer={
        <>
          <LinkBadge href={releaseNotesPage(news.version)} title={t('whatsnew.github')} />
          <span className="flex-1" />
          <Button
            kind="primary"
            labelled
            icon={<IconClose width={16} height={16} />}
            title={t('common.close')}
            onClick={onClose}
          />
        </>
      }
    >
      {listed && (
        <Tabs
          variant="well"
          label={title}
          active={tab}
          onSelect={(id) => onTab(id as NewsTab)}
          items={[
            { id: 'notes', label: t('settings.look.updatesReleaseNotes') },
            { id: 'changes', label: t('whatsnew.changes'), badge: news.unseen.length || undefined },
          ]}
        />
      )}

      {tab === 'changes' && listed ? (
        <>
          {/* One button, since hiding the dots and marking everything as seen
              are the same act. Its number is what a press acts on. */}
          <div className="flex justify-end">
            <Button
              kind="secondary"
              icon={left > 0 ? <IconCheck /> : <IconEye />}
              data-new="news-toggle"
              onClick={() => news.markSeen(all, left > 0)}
            >
              {left > 0 ? t('whatsnew.markAll', { n: left }) : t('whatsnew.showAll', { n: all.length })}
            </Button>
          </div>
          {byPage(news.changes).map((page) => (
            <section key={page.route} className="flex flex-col gap-2">
              <div className="flex items-center gap-3">
                <h3 className="glim-eyebrow min-w-0 flex-1 truncate">{pageName(t, page.route)}</h3>
                <Button kind="ghost" icon={<IconForward />} onClick={() => go(page.route)}>
                  {t('whatsnew.go')}
                </Button>
              </div>
              <ul className="flex flex-col gap-1.5">
                {page.changes.map((change) => {
                  const seen = news.seen.has(change.id);
                  return (
                    <li
                      key={change.id}
                      data-seen={seen}
                      className={`glim-well flex items-start gap-3 px-3 py-2.5 text-sm text-carbon-text ${seen ? 'opacity-50' : ''}`}
                    >
                      <span
                        aria-hidden
                        className={`mt-1.5 h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${
                          seen ? 'bg-carbon-textMuted' : 'bg-statusOkSolid'
                        }`}
                      />
                      <span className="min-w-0 flex-1">{sentenceOf(t, change)}</span>
                    </li>
                  );
                })}
              </ul>
            </section>
          ))}
        </>
      ) : notes ? (
        <ReleaseNotes text={notes} />
      ) : (
        <p className="text-sm text-carbon-textSub">{t('whatsnew.notesMissing')}</p>
      )}
    </Modal>
  );
}
