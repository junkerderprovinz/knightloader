import { type ReactNode, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useT } from '../../lib/i18n';
import { Button, Card, SectionTitle } from '../../components/ui';
import { fetchHealth } from '../../lib/api';
import { GLIMSTONE_VERSION } from '../../lib/glimstoneVersion';
import { IconGithub } from '../../lib/icons';
import { COFFEE_BUTTON_SVG, MAIL_SVG } from '../../lib/appMarks';
import { IconBitcoin, IconPayPal } from '../../components/donateMarks';
import { BrandMark, ReadmeButton } from '../../components/ReadmeButton';
import { CoffeeDialog } from '../../components/CoffeeDialog';
import { CryptoDonateDialog } from '../../components/CryptoDonateDialog';
import { PaypalDialog } from '../../components/PaypalDialog';
import { PAYPAL_PAGE } from '../../lib/donate';
import { followExternal, openExternal, openMail, popupsWork } from '../../lib/external';

/**
 * The help page answers the questions people have ("how do I get past a hoster
 * limit") and points each at the settings page that controls it. It is written
 * by hand against what the build ships rather than generated from the feature
 * registry, whose reasons explain a grey switch, not a feature.
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

export function Help() {
  const { t } = useT();

  return (
    <div className="flex flex-col gap-10">
      <Topic
        title={t('settings.help.intake.title')}
        hue={0}
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
        hue={1}
        links={[{ to: '/settings/collector', label: t('settings.help.collector.link') }]}
      >
        <p>{t('settings.help.collector.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.rules.title')}
        hue={2}
        links={[{ to: '/settings/rules', label: t('settings.help.rules.link') }]}
      >
        <p>{t('settings.help.rules.body')}</p>
      </Topic>

      <Topic title={t('settings.help.queue.title')} hue={3}>
        <p>{t('settings.help.queue.body')}</p>
        <Bullets items={[t('settings.help.queue.b1'), t('settings.help.queue.b2')]} />
      </Topic>

      <Topic
        title={t('settings.help.limits.title')}
        hue={4}
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
        hue={5}
        links={[{ to: '/settings/captcha', label: t('settings.help.captcha.link') }]}
      >
        <p>{t('settings.help.captcha.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.after.title')}
        hue={6}
        links={[
          { to: '/settings/archives', label: t('settings.help.after.link1') },
          { to: '/settings/downloads', label: t('settings.help.after.link2') },
        ]}
      >
        <p>{t('settings.help.after.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.schedule.title')}
        hue={7}
        links={[{ to: '/settings/automation', label: t('settings.help.schedule.link') }]}
      >
        <p>{t('settings.help.schedule.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.instances.title')}
        hue={8}
        links={[{ to: '/instances', label: t('settings.help.instances.link') }]}
      >
        <p>{t('settings.help.instances.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.access.title')}
        hue={9}
        links={[
          { to: '/settings/access', label: t('settings.help.access.link1') },
          { to: '/settings/diagnostics', label: t('settings.help.access.link2') },
        ]}
      >
        <p>{t('settings.help.access.body')}</p>
      </Topic>

      <Topic
        title={t('settings.help.advanced.title')}
        hue={10}
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

/**
 * VersionNumber shows a version without its leading 'v', linked to its release
 * page in a new tab when it is a released tag and plain otherwise. The link
 * uses muted ink that lifts on hover, the same in every colour mode.
 */
function VersionNumber({
  version,
  repo,
  unreleased,
}: {
  version: string;
  repo: string;
  unreleased: string;
}) {
  // No stamp yet, or 'dev' for an untagged build: the working title stands in.
  if (!version || version === 'dev') return <>{unreleased}</>;
  const tag = releaseTag(version);
  // A stamp without a release, such as a preview image, is shown as written.
  if (!RELEASE_TAG.test(tag)) return <>{version}</>;
  return (
    <a
      href={`${repo}/releases/tag/${encodeURIComponent(tag)}`}
      target="_blank"
      rel="noreferrer noopener"
      onClick={followExternal}
      className="text-carbon-textMuted no-underline hover:text-carbon-text"
    >
      {version.replace(/^v/, '')}
    </a>
  );
}

/**
 * About shows what this is, the ways to give and to report something, and the
 * version numbers with their release links. It is the last card on the General
 * tab, where a version and a contact are looked for. Its buttons are the
 * README's (GlimStone's "The About card"), one line each, every one with its
 * mark.
 */
export function About({ hue }: { hue: number }) {
  const { t } = useT();
  const [version, setVersion] = useState('');
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
  const mailto = `mailto:${CONTACT_MAIL}?subject=${encodeURIComponent(`KnightLoader ${t('settings.about.mailSubject')}`)}`;
  useEffect(() => {
    fetchHealth()
      .then((h) => {
        setVersion(h.version);
      })
      .catch(() => {});
  }, []);
  return (
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle>{t('settings.about.title')}</SectionTitle>
      <p className="text-sm text-carbon-textSub">{t('settings.about.body')}</p>
      {/* Each sentence sits directly above the buttons it asks for. */}
      <p className="text-sm text-carbon-textSub">{t('settings.about.coffee')}</p>
      {/* The ways to give share one row, a blank line apart from the sentences
          on both sides: the hosted payments first, the wallet last. Each opens
          its own window in the app. Buy Me a Coffee wears its own artwork,
          whose lettering carries the words, so its name is the accessible
          one. */}
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
      </div>
      {/* The footer: both version numbers link to their release pages (see
          releaseTag). */}
      <p className="glim-num text-xs text-carbon-textMuted">
        {t('settings.about.version')}{' '}
        <VersionNumber version={version} repo={REPO_URL} unreleased={t('nav.workingTitle')} />
        {' · GlimStone '}
        <VersionNumber
          version={GLIMSTONE_VERSION}
          repo={GLIMSTONE_URL}
          unreleased={t('nav.workingTitle')}
        />
      </p>
    </Card>
  );
}
