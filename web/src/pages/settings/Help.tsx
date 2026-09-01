import { type ReactNode, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useT } from '../../lib/i18n';
import { Card, SectionTitle } from '../../components/ui';
import { fetchHealth } from '../../lib/api';

/**
 * The help page: what this build actually does, organised by the question a
 * person has ("how do I get past a hoster limit", "where do encrypted zips
 * get their password"), each pointing at the settings page that controls it.
 *
 * Content-driven in the sense that build-plan.md's 10C brief means it -
 * grounded in what this build actually ships (internal/api/routes_features.go's
 * own registry, and the packages it names, is the ground truth this was
 * checked against) rather than a static copy of marketing prose that drifts
 * the day a feature changes shape. It is not, however, generated from that
 * registry at runtime: a registry row's Reason is one English sentence aimed
 * at "why is this switch grey", not the paragraph a person opening a help
 * page actually wants, so the two stay separate documents that happen to
 * agree today rather than one page pretending to be both.
 *
 * All 47 settings.help.* keys this page reads already live in en.ts (and are
 * typed TranslationKey, checked against every other locale) - read straight
 * through useT() below, same as any other page.
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
  return (
      <Card hue={hue} className="flex flex-col gap-3">
        <SectionTitle>{title}</SectionTitle>
        <div className="flex flex-col gap-2 text-sm text-carbon-textSub">{children}</div>
        {links && links.length > 0 && (
          <div className="flex flex-wrap gap-x-4 gap-y-1 pt-1">
            {links.map((l) => (
              <Link
                key={l.to}
                to={l.to}
                className="text-xs text-carbon-textMuted underline-offset-2 hover:text-carbon-text hover:underline"
              >
                {l.label} →
              </Link>
            ))}
          </div>
        )}
      </Card>
  );
}

function Bullets({ items }: { items: string[] }) {
  return (
    <ul className="flex flex-col gap-1.5 pl-4">
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
        links={[
          { to: '/settings/downloads', label: t('settings.help.intake.link1') },
          { to: '/settings/access', label: t('settings.help.intake.link2') },
        ]}
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
        links={[{ to: '/settings/general', label: t('settings.help.collector.link') }]}
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
          { to: '/settings/connections', label: t('settings.help.limits.link1') },
          { to: '/settings/reconnect', label: t('settings.help.limits.link2') },
          // /accounts, not /settings/accounts: registry.tsx's PAGES map still
          // leaves "accounts" deliberately absent (its own comment there), so
          // that tab renders the empty-state placeholder. The real, working
          // page has always been the main nav's own /accounts (router.tsx).
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
        links={[{ to: '/settings/schedule', label: t('settings.help.schedule.link') }]}
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

      <About hue={11} />
    </div>
  );
}

const REPO_URL = 'https://github.com/junkerderprovinz/knightloader';
const CONTACT_MAIL = 'hello@knightloader.app';
const GLIMSTONE_URL = 'https://github.com/junkerderprovinz/glimstone';

/** A version number that goes where a version number should go. New tab: leaving
 *  Settings to read a changelog is not what anybody meant by clicking a number.
 *  Underlined on hover only - a permanent line under every number turns a quiet
 *  fact into two links shouting at each other. */
function VersionLink({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a href={href} target="_blank" rel="noreferrer noopener" className="text-accentInk hover:underline">
      {children}
    </a>
  );
}

/**
 * Which GlimStone this app is built against. Kept in step by hand, because the
 * design language is a document plus a stylesheet rather than a package - the
 * same constant lives in the extension's options.js and the app's
 * SettingsScreen, and the three are expected to agree.
 */
const GLIMSTONE_VERSION = '1.6.0';

/**
 * The About card (jdp, 2026-08-31: "in der App und der Erweiterung und im KL
 * soll eine neue Card rein ... darin sollen die versionsnummern stehen und ein
 * text ... Die vversionsnummer sollen dann nicht nochmal unter den card im
 * hintergrund angeziegt werden").
 *
 * It replaces the fixed, centred version line that used to sit at the bottom of
 * every settings tab. That line was GlimStone's own answer until now, and its
 * weakness only shows once you ask what somebody does NEXT with a version
 * number: they report something. Page chrome has nowhere to put that, a card
 * does, and the two buttons turn "report it" from a search into a click.
 *
 * On the Help page rather than a page of its own, because this is already the
 * destination for "something is not working and I do not know where to look",
 * and one more entry in the settings rail for two sentences and two links would
 * be a tile nobody visits on purpose. hue 11 continues this page's own run.
 */
function About({ hue }: { hue: number }) {
  const { t } = useT();
  const [version, setVersion] = useState('');
  useEffect(() => {
    fetchHealth()
      .then((h) => setVersion(h.version))
      .catch(() => {});
  }, []);
  return (
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle>{t('settings.about.title')}</SectionTitle>
      <p className="text-sm text-carbon-textSub">{t('settings.about.body')}</p>
      {/* Both numbers are LINKS to their own release page (jdp, 2026-08-31:
          "Die Versionsnummer (auch von Glimstone) soll immer auf deren release
          auf github zeigen ... Das soll immmer und überall gelten"). A version
          answers "which build is this"; the question straight after it is
          always "and what changed". GlimStone 1.6.0 makes it the family rule.

          Built from the version, never a hand-kept list of links. A dev build
          has no release to point at, so it stays plain text rather than
          offering a link into a 404. */}
      <p className="glim-num text-xs text-carbon-textMuted">
        {t('settings.about.version')}{' '}
        {version && version !== 'dev' ? (
          <VersionLink href={`${REPO_URL}/releases/tag/v${version}`}>{version}</VersionLink>
        ) : (
          t('nav.workingTitle')
        )}
        {' · GlimStone '}
        <VersionLink href={`${GLIMSTONE_URL}/releases/tag/v${GLIMSTONE_VERSION}`}>{GLIMSTONE_VERSION}</VersionLink>
      </p>
      <div className="flex flex-wrap gap-2">
        {/* Anchors dressed as buttons rather than buttons that navigate: one
            opens a site and one hands off to a mail client, and both want the
            browser's own middle-click, copy-link and open-in-new-tab. */}
        <a
          href={REPO_URL}
          target="_blank"
          rel="noreferrer noopener"
          className="inline-flex items-center justify-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 px-3.5 py-2 text-sm font-medium text-carbon-text transition duration-150 select-none hover:bg-carbon-surface3 motion-safe:active:scale-[.98]"
        >
          {t('settings.about.github')}
        </a>
        <a
          href={`mailto:${CONTACT_MAIL}?subject=${encodeURIComponent(`KnightLoader ${t('settings.about.mailSubject')}`)}`}
          className="inline-flex items-center justify-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 px-3.5 py-2 text-sm font-medium text-carbon-text transition duration-150 select-none hover:bg-carbon-surface3 motion-safe:active:scale-[.98]"
        >
          {t('settings.about.mail')}
        </a>
      </div>
    </Card>
  );
}
