import { type ReactNode, useEffect, useState } from 'react';
import { useT } from '../../lib/i18n';
import { Button, Card, HintBubble, SectionTitle } from '../../components/ui';
import { fetchHealth } from '../../lib/api';
import { GLIMSTONE_VERSION } from '../../lib/glimstoneVersion';
import { IconDiagnostics, IconGithub, IconInfo } from '../../lib/icons';
import { COFFEE_BUTTON_SVG, MAIL_SVG, MANUAL_SVG } from '../../lib/appMarks';
import { IconBitcoin, IconPayPal } from '../../components/donateMarks';
import { BrandMark, ReadmeButton } from '../../components/ReadmeButton';
import { CoffeeDialog } from '../../components/CoffeeDialog';
import { CryptoDonateDialog } from '../../components/CryptoDonateDialog';
import { PaypalDialog } from '../../components/PaypalDialog';
import { PAYPAL_PAGE } from '../../lib/donate';
import { followExternal, openExternal, openMail, popupsWork } from '../../lib/external';
import { useToast } from '../../lib/toast';
import { openWhatsNew, useWhatsNew } from '../../lib/useWhatsNew';
import { saveDiagnostics } from './diagnostics/bundle';
import { PageBadge } from './ModuleToggle';

/**
 * Help draws the Info tile, the last one in Settings: every version the app is
 * made of, the ways to report something and who made the app, in the order of
 * BombVault's Info tile. What the app can do is told by the manual, which the
 * Help card opens.
 */
export function Help() {
  return (
    <div data-new="info-tile" className="flex flex-col gap-10">
      {/* The hues number the cards in draw order; renumber when adding one. */}
      <Versions hue={0} />
      <Report hue={1} />
      <About hue={2} />
    </div>
  );
}

const REPO_URL = 'https://github.com/junkerderprovinz/knightloader';
const CONTACT_MAIL = 'hello@halleluja.design';
const GLIMSTONE_URL = 'https://github.com/junkerderprovinz/glimstone';
const MANUAL_URL = 'https://junkerderprovinz.github.io/knightloader/';

/**
 * About says who made the app and on what terms, then offers the ways to give.
 * It is the last card of the Info tile. Its buttons are the README's
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

/** One thing the app is made of: its name with what it is behind an (i), and its number at the end. */
function VersionRow({ name, sub, children }: { name: string; sub?: string; children: ReactNode }) {
  return (
    <li className="flex items-center justify-between gap-4">
      <span className="flex min-w-0 items-center gap-1.5">
        <span className="text-sm text-carbon-text">{name}</span>
        <HintBubble hint={sub} />
      </span>
      <span dir="ltr" className="glim-num shrink-0 text-sm text-carbon-textSub">
        {children}
      </span>
    </li>
  );
}

/**
 * Versions lists the versions the app is made of: the server's own stamp and
 * the design language's copied file. The programs it starts, and looking for
 * newer versions, are on the General page, which the badge leads to.
 */
function Versions({ hue }: { hue: number }) {
  const { t } = useT();
  const [version, setVersion] = useState('');

  useEffect(() => {
    fetchHealth()
      .then((h) => setVersion(h.version))
      .catch(() => {});
  }, []);

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle
        hint={t('settings.info.versionHint')}
        right={<PageBadge page="look" title={t('settings.nav.look')} />}
      >
        {t('settings.about.version')}
      </SectionTitle>
      <ul className="flex flex-col gap-3">
        <VersionRow name="KnightLoader">
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
      </ul>
      <div className="flex flex-wrap justify-end gap-3">
        <NewsButton />
      </div>
    </Card>
  );
}

/**
 * NewsButton opens the one window of what this version brought, its release
 * notes and the changes the dots mark, on the changes while any is unseen. Its
 * figure is the number still unseen, and once all are seen how many there
 * are. A version that marked nothing has the release notes alone.
 */
function NewsButton() {
  const { t } = useT();
  const news = useWhatsNew();
  const total = news?.changes.length ?? 0;
  const unseen = news?.unseen.length ?? 0;
  return (
    <Button
      kind="secondary"
      icon={<IconInfo />}
      data-new="news-button"
      onClick={() => openWhatsNew(unseen > 0 ? 'changes' : 'notes')}
    >
      {total > 0 ? t('whatsnew.newCount', { n: unseen || total }) : t('settings.look.updatesReleaseNotes')}
    </Button>
  );
}

/**
 * Report holds the ways to report something: the repository, a mail, the
 * manual and the diagnostics bundle as a file to attach to an issue. The
 * sentence sits directly above the buttons it asks for. The two glyphs are
 * drawn tight, so they fill the mark's box as the envelope and the book do,
 * which come cropped to their ink.
 */
function Report({ hue }: { hue: number }) {
  const { t } = useT();
  const { toast } = useToast();
  const mailto = `mailto:${CONTACT_MAIL}?subject=${encodeURIComponent(`KnightLoader ${t('settings.about.mailSubject')}`)}`;

  async function onBugReport() {
    try {
      await saveDiagnostics();
    } catch (e) {
      toast(t('settings.diagnostics.downloadFailed', { error: String(e).replace(/^(Error|ApiError):\s*/, '') }), 'fail');
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
          mark={<IconGithub tight />}
          markClass="glim-github-mark"
        />
        {/* Without a vendor's mark: it reaches this app's authors, so it
            follows the user's accent, and its envelope opens under the
            pointer. A mail address opened in a new tab would leave an empty
            one behind, so it goes through openMail. */}
        <ReadmeButton
          brand="house"
          parts={[{ name: t('settings.about.mail'), onClick: () => openMail(mailto) }]}
          mark={<BrandMark svg={MAIL_SVG} />}
          markClass="glim-house-mark"
        />
        {/* The manual wears the README's docs button, its book and its
            yellow, and the book opens as the envelope does. */}
        <ReadmeButton
          brand="docs"
          data-new="manual-button"
          parts={[{ name: t('settings.info.manual'), sub: t('settings.info.manualOpen'), href: MANUAL_URL }]}
          mark={<BrandMark svg={MANUAL_SVG} />}
          markClass="glim-docs-mark"
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
          mark={<IconDiagnostics tight />}
        />
      </div>
    </Card>
  );
}
