import { type ReactNode, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useT } from '../../lib/i18n';
import { Button, Card, InfoBubble, LinkBadge, SectionTitle, type ButtonVerdict } from '../../components/ui';
import {
  fetchDeploymentInfo,
  fetchHealth,
  fetchMediaTools,
  fetchUpdateCheck,
  fetchYtdlpLatest,
  type MediaToolsStatus,
  type UpdateCheck,
  type YtdlpLatest,
} from '../../lib/api';
import { GLIMSTONE_VERSION } from '../../lib/glimstoneVersion';
import { IconBook, IconBug, IconCheck, IconClose, IconDownloads, IconGithub, IconInfo, IconRetry } from '../../lib/icons';
import { COFFEE_BUTTON_SVG, MAIL_SVG } from '../../lib/appMarks';
import { IconBitcoin, IconPayPal } from '../../components/donateMarks';
import { BrandMark, ReadmeButton } from '../../components/ReadmeButton';
import { CoffeeDialog } from '../../components/CoffeeDialog';
import { CryptoDonateDialog } from '../../components/CryptoDonateDialog';
import { PaypalDialog } from '../../components/PaypalDialog';
import { PAYPAL_PAGE } from '../../lib/donate';
import { followExternal, openExternal, openMail, popupsWork } from '../../lib/external';
import { useToast } from '../../lib/toast';
import { openWhatsNew } from '../../lib/useWhatsNew';
import { saveDiagnostics } from './diagnostics/bundle';
import { PageBadge } from './ModuleToggle';

/**
 * A help topic answers a question people have ("how do I get past a hoster
 * limit") and points at the settings page that controls it. The topics are
 * written by hand against what the build ships rather than generated from the
 * feature registry, whose reasons explain a grey switch, not a feature.
 * check-docs-claims.mjs keeps the settings.help.* keys read here equal to the
 * ones in en.ts.
 */
function Topic({
  title,
  children,
  links,
  hue,
}: {
  title: string;
  children: ReactNode;
  links?: { to: string; label: string }[];
  hue?: number;
}) {
  const navigate = useNavigate();
  return (
      <Card hue={hue} className="flex flex-col gap-3">
        <SectionTitle>{title}</SectionTitle>
        <div className="flex flex-col gap-2 text-sm text-carbon-textSub">{children}</div>
        {links && links.length > 0 && (
          <div className="flex flex-wrap gap-2 pt-1">
            {links.map((l) => (
              <Button key={l.to} kind="secondary" onClick={() => navigate(l.to)}>
                {l.label}
              </Button>
            ))}
          </div>
        )}
      </Card>
  );
}

function Bullets({ items }: { items: string[] }) {
  return (
    <ul className="flex flex-col gap-1.5 ps-4">
      {items.map((it, i) => (
        <li key={i} className="list-disc marker:text-carbon-textMuted">
          {it}
        </li>
      ))}
    </ul>
  );
}

/**
 * Help draws the Info tile, the last one in Settings (GlimStone's "The Info
 * tile and the About card"): who made the app, every version it is made of and
 * the ways to report something, with the help topics under them.
 */
export function Help() {
  const { t } = useT();

  return (
    <div data-new="info-tile" className="flex flex-col gap-10">
      {/* The hues number the cards in draw order; renumber when adding one. */}
      <About hue={0} />
      <Versions hue={1} />
      <Report hue={2} />

      <Topic
        title={t('settings.help.intake.title')}
        hue={3}
        links={[{ to: '/settings/collector', label: t('settings.help.intake.link1') }]}
      >
        <p>{t('settings.help.intake.body')}</p>
        <Bullets
          items={[
            t('settings.help.intake.b1'),
            t('settings.help.intake.b2'),
            t('settings.help.intake.b3'),
            t('settings.help.intake.watchFiles'),
            t('settings.help.intake.nzb'),
          ]}
        />
      </Topic>

      <Topic
        title={t('settings.help.collector.title')}
        hue={4}
        links={[{ to: '/settings/collector', label: t('settings.help.collector.link') }]}
      >
        <p>{t('settings.help.collector.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.rules.title')}
        hue={5}
        links={[{ to: '/settings/rules', label: t('settings.help.rules.link') }]}
      >
        <p>{t('settings.help.rules.body')}</p>
      </Topic>

      <Topic title={t('settings.help.queue.title')} hue={6}>
        <p>{t('settings.help.queue.body')}</p>
        <Bullets items={[t('settings.help.queue.b1'), t('settings.help.queue.b2')]} />
      </Topic>

      <Topic
        title={t('settings.help.limits.title')}
        hue={7}
        links={[
          { to: '/settings/network', label: t('settings.help.limits.link1') },
          { to: '/accounts', label: t('settings.help.limits.link3') },
        ]}
      >
        <p>{t('settings.help.limits.body')}</p>
        <Bullets items={[t('settings.help.limits.b1'), t('settings.help.limits.b2'), t('settings.help.limits.b3')]} />
      </Topic>

      <Topic
        title={t('settings.help.captcha.title')}
        hue={8}
        links={[{ to: '/settings/captcha', label: t('settings.help.captcha.link') }]}
      >
        <p>{t('settings.help.captcha.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.after.title')}
        hue={9}
        links={[
          { to: '/settings/archives', label: t('settings.help.after.link1') },
          { to: '/settings/downloads', label: t('settings.help.after.link2') },
        ]}
      >
        <p>{t('settings.help.after.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.schedule.title')}
        hue={10}
        links={[{ to: '/settings/automation', label: t('settings.help.schedule.link') }]}
      >
        <p>{t('settings.help.schedule.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.instances.title')}
        hue={11}
        links={[{ to: '/instances', label: t('settings.help.instances.link') }]}
      >
        <p>{t('settings.help.instances.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.access.title')}
        hue={12}
        links={[
          { to: '/settings/access', label: t('settings.help.access.link1') },
          { to: '/settings/diagnostics', label: t('settings.help.access.link2') },
        ]}
      >
        <p>{t('settings.help.access.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.advanced.title')}
        hue={13}
        links={[{ to: '/settings/advanced', label: t('settings.help.advanced.link') }]}
      >
        <p>{t('settings.help.advanced.body')}</p>
      </Topic>
    </div>
  );
}

const REPO_URL = 'https://github.com/junkerderprovinz/knightloader';
const CONTACT_MAIL = 'hello@halleluja.design';
const GLIMSTONE_URL = 'https://github.com/junkerderprovinz/glimstone';
const YTDLP_URL = 'https://github.com/yt-dlp/yt-dlp';
const MANUAL_URL = 'https://junkerderprovinz.github.io/knightloader/';

/**
 * About says who made the app and on what terms, then offers the ways to give.
 * It is the first card of the Info tile. Its buttons are the README's
 * (GlimStone's "The App tab"), one line each, every one with its mark.
 */
export function About({ hue }: { hue: number }) {
  const { t } = useT();
  // One window at a time, and each mounts only while open, so nothing from
  // BMAC or PayPal loads before somebody asks for it.
  const [giving, setGiving] = useState<'coffee' | 'paypal' | 'crypto' | null>(null);
  const stopGiving = () => setGiving(null);
  // The window's wallet button logs in through a popup. Where the webview
  // cannot open one, PayPal's own donation page does the whole job.
  const givePaypal = async () => {
    if (await popupsWork()) setGiving('paypal');
    else openExternal(PAYPAL_PAGE);
  };
  return (
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle>{t('settings.about.title')}</SectionTitle>
      <p className="text-sm text-carbon-textSub">{t('settings.about.body')}</p>
      <p className="text-sm text-carbon-textSub">{t('settings.about.coffee')}</p>
      {/* The ways to give share one row, a blank line below their sentence: the
          hosted payments first, the wallet last. Each opens its own window in
          the app. Buy Me a Coffee wears its own artwork, whose lettering
          carries the words, so its name is the accessible one. */}
      <div className="glim-readme-btn-rows glim-about-give">
        <ReadmeButton
          brand="coffee"
          parts={[{ name: t('settings.about.coffeeButton'), onClick: () => setGiving('coffee') }]}
          art={<BrandMark svg={COFFEE_BUTTON_SVG} />}
        />
        <ReadmeButton
          brand="paypal"
          parts={[{ name: t('settings.about.paypal'), onClick: () => void givePaypal() }]}
          mark={<IconPayPal />}
          markClass="glim-paypal-mark"
        />
        {/* The bare letterform, which a mark class can paint; the coin tiles
            in the window keep the disc. */}
        <ReadmeButton
          brand="bitcoin"
          parts={[{ name: t('settings.about.crypto'), onClick: () => setGiving('crypto') }]}
          mark={<IconBitcoin />}
          markClass="glim-bitcoin-mark"
        />
      </div>
      {giving === 'coffee' && <CoffeeDialog onClose={stopGiving} />}
      {giving === 'paypal' && <PaypalDialog onClose={stopGiving} />}
      {giving === 'crypto' && <CryptoDonateDialog onClose={stopGiving} />}
    </Card>
  );
}

/**
 * releaseTag derives the release tag from a version string the way GlimStone's
 * AboutCard does: it drops semver build metadata and adds a leading 'v' only
 * where the stamp lacks one, since release builds are stamped with it.
 */
export function releaseTag(version: string): string {
  const bare = version.split('+')[0].trim();
  if (!bare) return '';
  return bare.startsWith('v') ? bare : `v${bare}`;
}

/** Only a published release earns a link; a preview or branch stamp has no page. */
const RELEASE_TAG = /^v\d+\.\d+\.\d+$/;

/** releasePage is the release page behind one of this house's versions, or '' for a stamp without one. */
function releasePage(repo: string, version: string): string {
  const tag = releaseTag(version);
  return RELEASE_TAG.test(tag) ? `${repo}/releases/tag/${encodeURIComponent(tag)}` : '';
}

/**
 * ytdlpPage is the release behind a yt-dlp version. Its stable releases are
 * tagged with their date; a nightly's longer stamp is published elsewhere.
 */
function ytdlpPage(version: string): string {
  return /^\d{4}\.\d{2}\.\d{2}$/.test(version) ? `${YTDLP_URL}/releases/tag/${version}` : '';
}

/**
 * VersionNumber shows a version without its leading 'v', linked to its release
 * page in a new tab where it has one. The link uses the row's ink, which lifts
 * on hover, the same in every colour mode.
 */
function VersionNumber({ version, page }: { version: string; page: string }) {
  if (!page) return <>{version}</>;
  return (
    <a
      href={page}
      target="_blank"
      rel="noreferrer noopener"
      onClick={followExternal}
      className="no-underline hover:text-carbon-text"
    >
      {version.replace(/^v/, '')}
    </a>
  );
}

/**
 * One thing the app is made of: its name, what it is, and its number at the
 * end. `aside` is a badge in front of the number, such as the way to a newer
 * release.
 */
function VersionRow({
  name,
  sub,
  aside,
  children,
}: {
  name: string;
  sub?: ReactNode;
  aside?: ReactNode;
  children: ReactNode;
}) {
  return (
    <li className="flex items-center justify-between gap-4">
      <span className="flex min-w-0 flex-col">
        <span className="text-sm text-carbon-text">{name}</span>
        {sub && <span className="flex items-center text-subline text-carbon-textMuted">{sub}</span>}
      </span>
      <span className="flex shrink-0 items-center gap-3">
        {aside}
        <span dir="ltr" className="glim-num text-sm text-carbon-textSub">
          {children}
        </span>
      </span>
    </li>
  );
}

/** What the update check found, by component. `app` is null when it could not run. */
interface Checked {
  app: UpdateCheck | null;
  ytdlp: YtdlpLatest | null;
}

/** updateCount is how many of the listed components have a newer release. */
function updateCount(found: Checked): number {
  return (found.app?.available ? 1 : 0) + (found.ytdlp?.checked && found.ytdlp.compare === 'newer' ? 1 : 0);
}

/**
 * Versions lists every version the app is made of, each read from where it
 * runs: the server's own stamp, the design language's copied file and the
 * media tools as the server found them. "Check for updates" asks GitHub about
 * KnightLoader and yt-dlp at once and shows the answer on itself, and a row
 * with a newer release says so and leads to it.
 */
function Versions({ hue }: { hue: number }) {
  const { t } = useT();
  const [version, setVersion] = useState('');
  const [tools, setTools] = useState<MediaToolsStatus | null>(null);
  const [desktop, setDesktop] = useState(false);
  const [found, setFound] = useState<Checked | null>(null);
  const [checking, setChecking] = useState(false);
  // The failure counter of the check button, so a repeated failure shakes it again.
  const [shake, setShake] = useState(0);

  useEffect(() => {
    fetchHealth()
      .then((h) => setVersion(h.version))
      .catch(() => {});
    fetchMediaTools()
      .then(setTools)
      .catch(() => {});
    fetchDeploymentInfo()
      .then((d) => setDesktop(d.deployment === 'desktop'))
      .catch(() => {});
  }, []);

  async function onCheck() {
    setChecking(true);
    // yt-dlp is asked about only where one runs, and its answer never fails the
    // check: the app's own release is what the button is named for.
    const [app, ytdlp] = await Promise.allSettled([
      fetchUpdateCheck(),
      tools?.ytdlp.found ? fetchYtdlpLatest() : Promise.resolve(null),
    ]);
    const next: Checked = {
      app: app.status === 'fulfilled' && app.value.checked ? app.value : null,
      ytdlp: ytdlp.status === 'fulfilled' ? ytdlp.value : null,
    };
    setFound(next);
    if (!next.app) setShake((n) => n + 1);
    setChecking(false);
  }

  const updates = found ? updateCount(found) : 0;
  // The button changes its word and its glyph with the answer, like any button
  // that tests something.
  const face: { label: string; icon: ReactNode; verdict?: ButtonVerdict } = !found
    ? { label: t('settings.look.updatesCheck'), icon: <IconRetry /> }
    : !found.app
      ? { label: t('settings.info.checkFailed'), icon: <IconClose />, verdict: 'fail' }
      : updates > 0
        ? { label: t('settings.info.updatesFound', { n: updates }), icon: <IconDownloads />, verdict: 'warn' }
        : { label: t('settings.info.upToDate'), icon: <IconCheck />, verdict: 'ok' };

  const app = found?.app;
  // A downloaded version outranks the offer of it: it only waits for a restart.
  const appNews = app?.ready
    ? t('settings.look.updatesReady', { version: app.ready })
    : app?.available
      ? t('settings.look.updatesAvailable', { version: app.latest ?? '' })
      : '';
  const ytdlp = tools?.ytdlp;
  const ytdlpNews =
    found?.ytdlp?.checked && found.ytdlp.compare === 'newer'
      ? t('settings.look.updatesAvailable', { version: found.ytdlp.tag ?? '' })
      : '';
  const missing = t('settings.diagnostics.toolsMissing');

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.info.versionHint')}>{t('settings.about.version')}</SectionTitle>
      <ul className="flex flex-col gap-3">
        <VersionRow
          name="KnightLoader"
          sub={
            appNews && (
              <>
                {appNews}
                {app?.available && !app.ready && !desktop && (
                  <InfoBubble tip={t('settings.look.updatesContainerHint')} />
                )}
              </>
            )
          }
          aside={app?.available && app.url && <LinkBadge href={app.url} title={t('settings.look.updatesReleaseNotes')} />}
        >
          {/* No stamp yet, or 'dev' for an untagged build: the working title stands in. */}
          {!version || version === 'dev' ? (
            t('nav.workingTitle')
          ) : (
            <VersionNumber version={version} page={releasePage(REPO_URL, version)} />
          )}
        </VersionRow>
        <VersionRow name="GlimStone" sub={t('settings.info.glimstoneSub')}>
          <VersionNumber version={GLIMSTONE_VERSION} page={releasePage(GLIMSTONE_URL, GLIMSTONE_VERSION)} />
        </VersionRow>
        {/* Program names need no translation. A newer yt-dlp is fetched on the
            Resolvers page, so its row leads there. */}
        <VersionRow
          name="yt-dlp"
          sub={ytdlpNews || t('settings.info.ytdlpSub')}
          aside={ytdlpNews && <PageBadge page="resolvers" title={t('settings.nav.resolvers')} />}
        >
          {ytdlp?.version ? <VersionNumber version={ytdlp.version} page={ytdlpPage(ytdlp.version)} /> : tools && missing}
        </VersionRow>
        <VersionRow name="ffmpeg" sub={t('settings.info.ffmpegSub')}>
          {tools && (tools.ffmpeg.version || missing)}
        </VersionRow>
      </ul>
      <div className="flex flex-wrap justify-end gap-3">
        {/* What this version brought: its release notes and the changes the
            dots mark. */}
        <Button kind="secondary" icon={<IconInfo />} onClick={() => openWhatsNew('notes')}>
          {t('settings.look.updatesReleaseNotes')}
        </Button>
        <Button
          kind="secondary"
          icon={checking ? <IconRetry /> : face.icon}
          verdict={checking ? undefined : face.verdict}
          shake={shake}
          disabled={checking}
          onClick={() => void onCheck()}
        >
          {checking ? t('settings.look.updatesChecking') : face.label}
        </Button>
        <span role="status" className="sr-only">
          {found && !checking ? face.label : ''}
        </span>
      </div>
    </Card>
  );
}

/**
 * Report holds the ways to report something: the repository, a mail, the
 * manual and the diagnostics bundle as a file to attach to an issue. The
 * sentence sits directly above the buttons it asks for.
 */
function Report({ hue }: { hue: number }) {
  const { t } = useT();
  const { toast } = useToast();
  const mailto = `mailto:${CONTACT_MAIL}?subject=${encodeURIComponent(`KnightLoader ${t('settings.about.mailSubject')}`)}`;

  async function onBugReport() {
    try {
      await saveDiagnostics();
    } catch (e) {
      toast(t('settings.diagnostics.downloadFailed', { error: String(e).replace(/^Error:\s*/, '') }), 'fail');
    }
  }

  return (
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle hint={t('settings.info.helpHint')}>{t('settings.info.helpTitle')}</SectionTitle>
      <p className="text-sm text-carbon-textSub">{t('settings.about.report')}</p>
      <div className="glim-readme-btn-rows">
        {/* A link, so middle-click and copy-link work. */}
        <ReadmeButton
          brand="github"
          parts={[{ name: t('settings.about.github'), href: REPO_URL }]}
          mark={<IconGithub />}
          markClass="glim-github-mark"
        />
        {/* The one control without a vendor's mark: it reaches this app's
            authors, so it follows the user's accent, and its envelope opens
            under the pointer. A mail address opened in a new tab would leave
            an empty one behind, so it goes through openMail. */}
        <ReadmeButton
          brand="house"
          parts={[{ name: t('settings.about.mail'), onClick: () => openMail(mailto) }]}
          mark={<BrandMark svg={MAIL_SVG} />}
          markClass="glim-house-mark"
        />
        <ReadmeButton
          brand="github"
          parts={[{ name: t('settings.info.manual'), sub: t('settings.info.manualOpen'), href: MANUAL_URL }]}
          mark={<IconBook />}
        />
        {/* The file goes with an issue on GitHub, so it lights up beside it. */}
        <ReadmeButton
          brand="github"
          parts={[
            {
              name: t('settings.info.bugReport'),
              sub: t('settings.info.bugReportDownload'),
              onClick: () => void onBugReport(),
            },
          ]}
          mark={<IconBug />}
        />
      </div>
    </Card>
  );
}
