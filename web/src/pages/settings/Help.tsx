import { useCallback, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { useT, type TranslationKey } from '../../lib/i18n';
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
 * Same PENDING-table arrangement as Diagnostics.tsx and Captcha.tsx: none of
 * this is in en.ts yet, so the lookup asks the real catalogue first and falls
 * back to the English here - see either file's own comment on the pattern.
 */
const PENDING = {
  'settings.help.intro':
    'What this build can do, organised by task rather than by settings page. Every section links to where it is configured.',

  'settings.help.intake.title': 'Adding downloads',
  'settings.help.intake.body':
    'Paste links into the Collector - one per line, or messy text: a scan finds links wherever they sit, inside a sentence, several to a line, or wrapped across two lines by a mail client.',
  'settings.help.intake.b1': 'Drop a link-container file (.dlc, .ccf, .rsdf) or a plain link list.',
  'settings.help.intake.b2':
    "Click'n'Load buttons on hoster and forum pages work unchanged - KnightLoader answers on 127.0.0.1:9666, the same port every other downloader uses.",
  'settings.help.intake.b3':
    'Paste a page URL instead of a file link and switch Crawl on to pull out every file it links to, instead of downloading the page itself.',
  'settings.help.intake.b4': 'A watch folder picks up .txt/.crawljob files dropped into it automatically.',
  'settings.help.intake.link1': 'Open Downloads settings',
  'settings.help.intake.link2': "Open Access settings (Click'n'Load)",

  'settings.help.collector.title': 'The collector, before anything downloads',
  'settings.help.collector.body':
    'New links land in the Collector first, not the queue: a staging area to check names, sizes and duplicate warnings before anything starts. Auto-confirm can skip that step, with an optional delay; once confirmed, a link either starts immediately or waits in Queued, depending on Auto-start.',
  'settings.help.collector.link': 'Open General settings',

  'settings.help.rules.title': 'Rules: packages, folders and what gets kept',
  'settings.help.rules.body':
    'The Packagizer renames a link, chooses its folder and sets its download options as it arrives, from conditions you write. The Link Filter decides whether a link is kept at all - and unlike a filter that just makes links disappear, a rejection always names the rule that made it and why.',
  'settings.help.rules.link': 'Open Rules',

  'settings.help.queue.title': 'Managing the queue',
  'settings.help.queue.body':
    'Pause, resume or reorder any link, alone or as a whole package. Stopping the queue is two different actions: halting it leaves anything already running untouched, so it finishes on its own, while stopping every transfer right now shows what would be lost first and only goes ahead once you confirm - a transfer that cannot resume where it left off is exactly what that warning is for.',
  'settings.help.queue.b1': 'A single link can override the global connection count or the extract-after-download switch.',
  'settings.help.queue.b2': 'A duplicate of a link already in the list is always refused, before it ever reaches the collector.',

  'settings.help.limits.title': 'Getting through hoster limits',
  'settings.help.limits.body':
    'Three independent ways to stop a free-user limit from being the ceiling:',
  'settings.help.limits.b1': 'Connections - spread downloads over more than one outbound path (a second line, a proxy, SOCKS) instead of always leaving by this machine’s own address.',
  'settings.help.limits.b2':
    'Reconnect - ask the router for a new public address, the only thing that actually lifts a limit keyed to the address itself (UPnP, an external program, a script, or replaying HTTP requests against the router’s admin page - existing JDownloader LiveHeader/curl scripts work unchanged).',
  'settings.help.limits.b3':
    'Accounts - store premium or debrid logins (Real-Debrid, AllDebrid, TorBox and others) so an eligible link is fetched at full speed instead of the free-user limit.',
  'settings.help.limits.link1': 'Open Connections',
  'settings.help.limits.link2': 'Open Reconnect',
  'settings.help.limits.link3': 'Open Accounts',

  'settings.help.captcha.title': 'Captcha',
  'settings.help.captcha.body':
    'When a hoster asks for a captcha, an automatic solver you have configured is tried first, in the order you set. Anything it cannot solve - or if none is configured - is put in front of you instead of failing silently.',
  'settings.help.captcha.link': 'Open Captcha settings',

  'settings.help.after.title': 'After the download',
  'settings.help.after.body':
    'Archives are extracted automatically: zip (including encrypted, both WinZip AES and the legacy ZipCrypto), rar with multi-volume sets, 7z, tar, and gzip/bzip2/xz/zstd whether or not they wrap a tar - pure Go, no external unrar or 7z binary involved. A list of passwords is tried in order for an encrypted archive. A finished file is checked against whatever checksum shipped with it: an .sfv listing, an md5/sha1/sha256sum file, or a CRC32 the release name itself carries.',
  'settings.help.after.link1': 'Open Archives settings',
  'settings.help.after.link2': 'Open Downloads settings',

  'settings.help.schedule.title': 'Running unattended',
  'settings.help.schedule.body':
    'A weekly timetable pauses or throttles the queue by the clock - a nightly window, correct across the daylight-saving change - the same idea as JDownloader’s Scheduler.',
  'settings.help.schedule.link': 'Open Schedule',

  'settings.help.instances.title': 'Running more than one instance',
  'settings.help.instances.body':
    'Add another KnightLoader as a peer and its queue shows up on this one’s dashboard too - self-hosted, no relay involved: this instance simply calls that instance’s own API, the same way a browser would.',
  'settings.help.instances.link': 'Open Instances',

  'settings.help.access.title': 'Access and troubleshooting',
  'settings.help.access.body':
    'A password locks the whole interface down to a session cookie. The Access page also lists the intake ports and access methods this build has, and why - so an unfamiliar open port has an answer instead of a guess. The Diagnostics page builds a file to attach to a bug report: version and build info, the current settings with every password removed, this process’s own recent log lines, and how many goroutines are running.',
  'settings.help.access.link1': 'Open Access settings',
  'settings.help.access.link2': 'Open Diagnostics',

  'settings.help.advanced.title': 'Everything else',
  'settings.help.advanced.body':
    'Every setting this build has can be read and changed by its raw name on the Advanced page, including a few - how a mirror of an already-downloaded file is treated, what happens when a download would land on a name already taken - that do not have a dedicated control anywhere else yet.',
  'settings.help.advanced.link': 'Open Advanced settings',
} as const;

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return useCallback(
    (key: PendingKey) => {
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      return translated ?? PENDING[key];
    },
    [t],
  );
}

function Topic({
  title,
  children,
  links,
}: {
  title: string;
  children: ReactNode;
  links?: { to: string; label: string }[];
}) {
  return (
    <>
      <SectionTitle>{title}</SectionTitle>
      <Card className="flex flex-col gap-3">
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
    </>
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
  const cx = useCx();

  return (
    <div className="flex flex-col gap-6">
      <p className="text-sm text-carbon-textSub">{cx('settings.help.intro')}</p>

      <Topic
        title={cx('settings.help.intake.title')}
        links={[
          { to: '/settings/downloads', label: cx('settings.help.intake.link1') },
          { to: '/settings/access', label: cx('settings.help.intake.link2') },
        ]}
      >
        <p>{cx('settings.help.intake.body')}</p>
        <Bullets
          items={[
            cx('settings.help.intake.b1'),
            cx('settings.help.intake.b2'),
            cx('settings.help.intake.b3'),
            cx('settings.help.intake.b4'),
          ]}
        />
      </Topic>

      <Topic
        title={cx('settings.help.collector.title')}
        links={[{ to: '/settings/general', label: cx('settings.help.collector.link') }]}
      >
        <p>{cx('settings.help.collector.body')}</p>
      </Topic>

      <Topic title={cx('settings.help.rules.title')} links={[{ to: '/settings/rules', label: cx('settings.help.rules.link') }]}>
        <p>{cx('settings.help.rules.body')}</p>
      </Topic>

      <Topic title={cx('settings.help.queue.title')}>
        <p>{cx('settings.help.queue.body')}</p>
        <Bullets items={[cx('settings.help.queue.b1'), cx('settings.help.queue.b2')]} />
      </Topic>

      <Topic
        title={cx('settings.help.limits.title')}
        links={[
          { to: '/settings/connections', label: cx('settings.help.limits.link1') },
          { to: '/settings/reconnect', label: cx('settings.help.limits.link2') },
          // /accounts, not /settings/accounts: registry.tsx's PAGES map still
          // leaves "accounts" deliberately absent (its own comment there), so
          // that tab renders the empty-state placeholder. The real, working
          // page has always been the main nav's own /accounts (router.tsx).
          { to: '/accounts', label: cx('settings.help.limits.link3') },
        ]}
      >
        <p>{cx('settings.help.limits.body')}</p>
        <Bullets items={[cx('settings.help.limits.b1'), cx('settings.help.limits.b2'), cx('settings.help.limits.b3')]} />
      </Topic>

      <Topic
        title={cx('settings.help.captcha.title')}
        links={[{ to: '/settings/captcha', label: cx('settings.help.captcha.link') }]}
      >
        <p>{cx('settings.help.captcha.body')}</p>
      </Topic>

      <Topic
        title={cx('settings.help.after.title')}
        links={[
          { to: '/settings/archives', label: cx('settings.help.after.link1') },
          { to: '/settings/downloads', label: cx('settings.help.after.link2') },
        ]}
      >
        <p>{cx('settings.help.after.body')}</p>
      </Topic>

      <Topic
        title={cx('settings.help.schedule.title')}
        links={[{ to: '/settings/schedule', label: cx('settings.help.schedule.link') }]}
      >
        <p>{cx('settings.help.schedule.body')}</p>
      </Topic>

      <Topic
        title={cx('settings.help.instances.title')}
        links={[{ to: '/instances', label: cx('settings.help.instances.link') }]}
      >
        <p>{cx('settings.help.instances.body')}</p>
      </Topic>

      <Topic
        title={cx('settings.help.access.title')}
        links={[
          { to: '/settings/access', label: cx('settings.help.access.link1') },
          { to: '/settings/diagnostics', label: cx('settings.help.access.link2') },
        ]}
      >
        <p>{cx('settings.help.access.body')}</p>
      </Topic>

      <Topic
        title={cx('settings.help.advanced.title')}
        links={[{ to: '/settings/advanced', label: cx('settings.help.advanced.link') }]}
      >
        <p>{cx('settings.help.advanced.body')}</p>
      </Topic>
    </div>
  );
}
