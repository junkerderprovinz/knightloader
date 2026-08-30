import { type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { useT } from '../../lib/i18n';
import { Card, SectionTitle } from '../../components/ui';

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
    </div>
  );
}
