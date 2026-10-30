import { useId, type CSSProperties } from 'react';
import { useNavigate } from 'react-router-dom';
import { hueVars } from '../../lib/appearance';
import { useT, type TranslationKey } from '../../lib/i18n';
import { IconCheck, IconClose, IconEye, IconForward } from '../../lib/icons';
import type { WhatsNew } from '../../lib/useWhatsNew';
import {
  bundledNotes,
  newer,
  placed,
  releaseNotesPage,
  releaseOf,
  routeOf,
  sentenceOf,
  type Change,
} from '../../lib/whatsNew';
import { label } from '../../pages/settings/tx';
import { Button, LinkBadge, Modal, SectionTitle } from '../ui';
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

/**
 * byPage groups a release's changes by the page "Go there" opens, in the order
 * they are listed. Those no page shows come first.
 */
function byPage(changes: Change[]): { route: string | null; changes: Change[] }[] {
  const pages: { route: string | null; changes: Change[] }[] = [];
  for (const change of changes) {
    const route = placed(change) ? routeOf(change.to[0]) : null;
    const page = pages.find((p) => p.route === route);
    if (page) page.changes.push(change);
    else if (route === null) pages.unshift({ route, changes: [change] });
    else pages.push({ route, changes: [change] });
  }
  return pages;
}

/**
 * newsTitle names the window: what is new since the release the instance ran
 * before an update, or in the running one.
 */
export function newsTitle(t: Translate, news: WhatsNew): string {
  const running = releaseOf(news.version);
  return news.from && running && newer(running, news.from)
    ? t('whatsnew.titleSince', { version: news.from })
    : t('whatsnew.title', { version: running || news.version });
}

/** ReleaseCard is one release on a sub-card of its own: its number on the badge, its notes and its changes. */
function ReleaseCard({
  news,
  version,
  hue,
  onGo,
}: {
  news: WhatsNew;
  version: string;
  hue: number;
  onGo: (route: string) => void;
}) {
  const { t } = useT();
  const id = useId();
  const notes = bundledNotes(version);
  const changes = news.changes.filter((c) => c.since === version);

  return (
    <section
      data-release={version}
      aria-labelledby={id}
      className="glim-subcard glim-hue relative flex flex-col gap-4 p-4 sm:p-5"
      style={hueVars(hue) as CSSProperties}
    >
      <SectionTitle id={id} hue={hue}>
        <span dir="ltr" className="glim-num">
          {version}
        </span>
      </SectionTitle>
      {notes ? <ReleaseNotes text={notes} /> : <p className="text-sm text-carbon-textSub">{t('whatsnew.notesMissing')}</p>}
      {byPage(changes).map((page) => (
        <div key={page.route ?? ''} className="flex flex-col gap-1.5">
          <h3 className="glim-eyebrow">{page.route === null ? t('whatsnew.background') : pageName(t, page.route)}</h3>
          <ul className="flex flex-col gap-1.5">
            {page.changes.map((change) => {
              const seen = news.seen.has(change.id);
              return (
                <li
                  key={change.id}
                  data-seen={seen}
                  className={`flex flex-wrap items-center gap-x-3 gap-y-2 rounded-[var(--radius-control)] bg-carbon-surface px-3 py-2.5 text-sm text-carbon-text ${seen ? 'opacity-50' : ''}`}
                >
                  <span
                    aria-hidden
                    className={`h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${seen ? 'bg-carbon-textMuted' : 'bg-statusOkSolid'}`}
                  />
                  <span className="min-w-0 flex-[1_1_9rem]">{sentenceOf(t, change)}</span>
                  {placed(change) && (
                    <Button kind="secondary" icon={<IconForward />} className="ms-auto" onClick={() => onGo(routeOf(change.to[0]))}>
                      {t('whatsnew.go')}
                    </Button>
                  )}
                </li>
              );
            })}
          </ul>
        </div>
      ))}
    </section>
  );
}

/**
 * WhatsNewWindow is what the updates brought, one card per release since the
 * one the instance ran before. A change no page shows counts as seen once the
 * window that listed it closes, since it has no dot to click away.
 */
export function WhatsNewWindow({ news, onClose }: { news: WhatsNew; onClose: () => void }) {
  const { t } = useT();
  const navigate = useNavigate();
  const listed = news.changes;
  const left = listed.filter((c) => !news.seen.has(c.id));

  function close() {
    const read = left.filter((c) => !placed(c)).map((c) => c.id);
    if (read.length > 0) news.markSeen(read);
    onClose();
  }

  function go(route: string) {
    close();
    navigate(route);
  }

  return (
    <Modal
      title={newsTitle(t, news)}
      height="capped"
      // Release notes run to long lines and lists, which a question window's width breaks up.
      wide
      onClose={close}
      footer={
        <>
          <LinkBadge href={releaseNotesPage(news.version)} title={t('whatsnew.github')} />
          <span className="flex-1" />
          <Button kind="primary" labelled icon={<IconClose width={16} height={16} />} title={t('common.close')} onClick={close} />
        </>
      }
    >
      {listed.length > 0 && (
        // One button, since hiding the dots and marking everything as seen
        // are the same act. Its number is what a press acts on.
        <div className="flex justify-end">
          <Button
            kind="secondary"
            icon={left.length > 0 ? <IconCheck /> : <IconEye />}
            data-new="news-toggle"
            onClick={() =>
              news.markSeen(
                listed.map((c) => c.id),
                left.length > 0,
              )
            }
          >
            {left.length > 0 ? t('whatsnew.markAll', { n: left.length }) : t('whatsnew.showAll', { n: listed.length })}
          </Button>
        </div>
      )}
      {/* The top padding holds the half of the first badge that stands above its card. */}
      <div className="flex flex-col gap-6 pt-3">
        {news.releases.length > 0 ? (
          news.releases.map((version, i) => <ReleaseCard key={version} news={news} version={version} hue={i} onGo={go} />)
        ) : (
          <p className="text-sm text-carbon-textSub">{t('whatsnew.notesMissing')}</p>
        )}
      </div>
    </Modal>
  );
}
