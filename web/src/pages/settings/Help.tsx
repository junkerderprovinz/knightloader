import { type ReactNode, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useT } from '../../lib/i18n';
import { Button, Card, SectionTitle } from '../../components/ui';
import { fetchHealth } from '../../lib/api';
import { GLIMSTONE_VERSION } from '../../lib/glimstoneVersion';
import { IconGithub, IconMail } from '../../lib/icons';
import { IconBitcoin, IconBuyMeACoffee, IconPayPal } from '../../components/donateMarks';
import { CoffeeDialog } from '../../components/CoffeeDialog';
import { CryptoDonateDialog } from '../../components/CryptoDonateDialog';

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
            t('settings.help.intake.b4'),
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
 * PayPal's hosted donation button, the address README.md's donate row links.
 * GlimStone's PayPal window needs a PayPal app's client id and a plan per
 * interval, which this project does not have, so the button stays a link.
 * Typed as string so an empty value stays a case this file handles.
 */
const PAYPAL: string = 'https://www.paypal.com/donate/?hosted_button_id=76FVV52TKXTUS';

/**
 * The shared class of the About card's controls. `glim-brand-btn` gives every
 * mark the same colouring; each call site adds its own `glim-brand-<name>`.
 */
const ABOUT_BTN =
  'glim-brand-btn inline-flex items-center justify-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2' +
  ' px-3.5 py-2 text-sm font-medium text-carbon-text transition duration-150 select-none' +
  ' hover:bg-carbon-surface3 motion-safe:active:scale-[.98]';

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
      className="text-carbon-textMuted no-underline hover:text-carbon-text"
    >
      {version.replace(/^v/, '')}
    </a>
  );
}

/**
 * About shows what this is, the ways to give and to report something, and the
 * version numbers with their release links. It lives on the Help page, where
 * people already go when something does not work.
 */
export function About({ hue }: { hue: number }) {
  const { t } = useT();
  const [version, setVersion] = useState('');
  const [coffeeOpen, setCoffeeOpen] = useState(false);
  const [cryptoOpen, setCryptoOpen] = useState(false);
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
      {/* Each sentence sits directly above the button it asks for. */}
      <p className="text-sm text-carbon-textSub">{t('settings.about.coffee')}</p>
      {/* The ways to give share one row: the hosted payments first, the
          wallet last. Coffee and crypto open a window in the app, so they are
          buttons rather than anchors. */}
      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          className={`${ABOUT_BTN} glim-brand-coffee`}
          onClick={() => setCoffeeOpen(true)}
        >
          <span className="glim-btn-glyph">
            <IconBuyMeACoffee />
          </span>
          {t('settings.about.coffeeButton')}
        </button>
        {PAYPAL !== '' && (
          <a
            href={PAYPAL}
            target="_blank"
            rel="noreferrer noopener"
            className={`${ABOUT_BTN} glim-brand-paypal`}
          >
            <span className="glim-btn-glyph">
              <IconPayPal />
            </span>
            {t('settings.about.paypal')}
          </a>
        )}
        {/* Its mark is the bare letterform in one colour, which a brand class
            can paint (check-brand-marks.mjs). */}
        <button
          type="button"
          className={`${ABOUT_BTN} glim-brand-bitcoin`}
          onClick={() => setCryptoOpen(true)}
        >
          <span className="glim-btn-glyph">
            <IconBitcoin />
          </span>
          {t('settings.about.crypto')}
        </button>
      </div>
      {coffeeOpen && <CoffeeDialog onClose={() => setCoffeeOpen(false)} />}
      {cryptoOpen && <CryptoDonateDialog onClose={() => setCryptoOpen(false)} />}
      {/* The extra space keeps the coffee button paired with its own sentence. */}
      <p className="mt-2 text-sm text-carbon-textSub">{t('settings.about.report')}</p>
      <div className="flex flex-wrap gap-2">
        {/* Anchors, so middle-click and copy-link work. */}
        <a
          href={REPO_URL}
          target="_blank"
          rel="noreferrer noopener"
          className={`${ABOUT_BTN} glim-brand-github`}
        >
          <span className="glim-btn-glyph">
            <IconGithub />
          </span>
          {t('settings.about.github')}
        </a>
        {/* The one control without a vendor's mark: `glim-brand-house` follows
            the user's accent. */}
        <a
          href={`mailto:${CONTACT_MAIL}?subject=${encodeURIComponent(`KnightLoader ${t('settings.about.mailSubject')}`)}`}
          className={`${ABOUT_BTN} glim-brand-house`}
        >
          <span className="glim-btn-glyph">
            <IconMail />
          </span>
          {t('settings.about.mail')}
        </a>
      </div>
      {/* The footer: both version numbers link to their release pages (see
          releaseTag), and the crest sits on this line because the card's
          title badge is positioned absolutely on its top edge. */}
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
