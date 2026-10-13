// The ways a link reaches the collector, read from what the instance has set
// up: the module switches, the clipboard watch of this browser, the group's
// apps, the other instances and the accounts that import. internal/app's
// origins and docs/getting-links-in.md name the entrances. Two are left out:
// the move over from JDownloader, which happens once, and the API, which is an
// address and nothing that is set up.
import { useNavigate } from 'react-router-dom';
import type { Way } from '../../../components/pictures/WaysIn';
import { fetchAccounts, fetchConnect, type Instance } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import {
  IconAccounts,
  IconApp,
  IconBrowser,
  IconCopy,
  IconFleet,
  IconFolderOpen,
  IconGlobe,
  IconLink,
  IconNotifications,
  IconPaste,
  IconPhone,
  IconPin,
  IconUpload,
} from '../../../lib/icons';
import { useResource } from '../../../lib/useResource';
import { useFeatures } from '../context';
import type { Feature } from '../features';
import { requestJump } from '../jump';
import { moduleDetail, useTx } from '../tx';

/** Where a way is set up: a page of the app, or a row on a settings page. */
type Place = string | { page: string; title: TranslationKey; label?: TranslationKey };

const INTAKE: TranslationKey = 'settings.sectionLinkIntake';

const PLACES: Record<string, Place> = {
  paste: { page: 'downloads', title: 'settings.downloads.limitsTitle', label: 'settings.preParser' },
  file: '/collector',
  cnl: { page: 'collector', title: INTAKE, label: 'settings.module.cnl' },
  clipboard: { page: 'collector', title: INTAKE, label: 'intake.clipboardWatch' },
  watch: { page: 'collector', title: INTAKE, label: 'settings.module.watch' },
  crawler: { page: 'collector', title: 'settings.module.crawler' },
  feeds: { page: 'downloads', title: 'settings.module.feeds' },
  extension: { page: 'browsertools', title: 'settings.browsertools.extensionTitle' },
  bookmarklet: { page: 'browsertools', title: 'settings.browsertools.bookmarkletTitle' },
  phone: { page: 'browsertools', title: 'settings.browsertools.phoneTitle' },
  instances: { page: 'instances', title: 'settings.module.federation' },
  account: '/accounts',
  downloadclient: { page: 'access', title: 'settings.access.tokens.title', label: 'settings.module.downloadclient' },
};

const names = (list: { name: string }[]) => list.map((item) => item.name).join(', ');

/**
 * useWays lists the ways in with their state, and `open` goes to where one is
 * set up. `watch` is this browser's clipboard watch and `peers` the instances
 * added by address, both of which the card around the picture already holds.
 */
export function useWays(watch: boolean, peers: Instance[] | null): { ways: Way[]; open: (id: string) => void } {
  const { t } = useT();
  const { tx } = useTx();
  const navigate = useNavigate();
  const { features } = useFeatures();
  const { data: group } = useResource(fetchConnect);
  const { data: accounts } = useResource(fetchAccounts);

  const ways: Way[] = [
    { id: 'paste', name: t('collector.add'), glyph: IconPaste, state: 'on' },
    { id: 'file', name: t('picture.intake.file'), glyph: IconUpload, state: 'on' },
  ];

  const shipped = (id: string) => features.modules.find((m) => m.id === id && m.verdict === 'shipped');
  const switched = (m: Feature): Pick<Way, 'state' | 'detail'> => {
    if (m.enabled) return { state: 'on', detail: moduleDetail(tx, m) };
    return { state: m.switch === 'parked' && !m.parked ? 'unset' : 'off' };
  };
  const cnl = shipped('cnl');
  if (cnl) ways.push({ id: 'cnl', name: t('settings.module.cnl'), glyph: IconLink, ...switched(cnl) });
  ways.push({ id: 'clipboard', name: t('intake.clipboardWatch'), glyph: IconCopy, state: watch ? 'on' : 'off' });
  const folder = shipped('watch');
  if (folder) ways.push({ id: 'watch', name: t('settings.module.watch'), glyph: IconFolderOpen, ...switched(folder) });
  const crawler = shipped('crawler');
  if (crawler) ways.push({ id: 'crawler', name: t('settings.module.crawler'), glyph: IconGlobe, ...switched(crawler) });
  const feeds = shipped('feeds');
  if (feeds) ways.push({ id: 'feeds', name: t('settings.module.feeds'), glyph: IconNotifications, ...switched(feeds) });

  // A phone or a browser sends links once it has joined the group.
  const apps = group?.apps ?? [];
  const extensions = apps.filter((app) => app.deployment === 'extension');
  const phones = apps.filter((app) => app.deployment !== 'extension');
  ways.push(
    {
      id: 'extension',
      name: t('settings.browsertools.extensionTitle'),
      glyph: IconBrowser,
      state: extensions.length > 0 ? 'on' : 'unset',
      detail: names(extensions),
    },
    { id: 'bookmarklet', name: t('settings.browsertools.bookmarkletTitle'), glyph: IconPin, state: 'on' },
    {
      id: 'phone',
      name: t('settings.browsertools.phoneTitle'),
      glyph: IconPhone,
      state: phones.length > 0 ? 'on' : 'unset',
      detail: names(phones),
    },
  );

  const federation = shipped('federation');
  if (federation) {
    const others = [
      ...(group?.members ?? []).map((m) => ({ name: m.name || m.id })),
      ...(peers ?? []).filter((p) => !p.relayId).map((p) => ({ name: p.displayName ?? p.name })),
    ];
    ways.push({
      id: 'instances',
      name: t('settings.module.federation'),
      glyph: IconFleet,
      state: !federation.enabled ? 'off' : others.length > 0 ? 'on' : 'unset',
      detail: names(others),
    });
  }

  const importing = (accounts ?? []).filter((a) => a.canImport && a.import);
  ways.push({
    id: 'account',
    name: t('picture.intake.account'),
    glyph: IconAccounts,
    state: importing.length > 0 ? 'on' : (accounts ?? []).some((a) => a.canImport) ? 'off' : 'unset',
    detail: importing.map((a) => a.label || a.service).join(', '),
  });

  const client = shipped('downloadclient');
  if (client) {
    ways.push({ id: 'downloadclient', name: t('settings.module.downloadclient'), glyph: IconApp, ...switched(client) });
  }

  const open = (id: string) => {
    const place = PLACES[id];
    if (typeof place === 'string') {
      navigate(place);
      return;
    }
    requestJump(place);
    navigate(`/settings/${place.page}`);
  };
  return { ways, open };
}
