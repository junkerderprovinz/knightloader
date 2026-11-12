// What the fortress on the Pairing page shows of the server's view, in
// GlimStone's shapes: the other apps with a row per place they run, and the
// permissions that involve KnightLoader in KnightLoader's own words.
import { type FortressApp, type FortressLogLine, type FortressPermission, type FortressStatusReport } from '@glimstone/core';
import type { ConnectInfo, FortressMember, FortressMemberStatus, FortressPermissionState } from '../../../lib/api';
import type { TranslationKey } from '../../../lib/i18n';

export const SELF = { id: 'knightloader', name: 'KnightLoader' };

type T = (key: TranslationKey, vars?: Record<string, string | number>) => string;

/** The actions KnightLoader has words for: the ones it calls at ArrowLoop,
 *  the ones a backup app calls here, and opening an app's pages. */
const ACTION_TITLES: Record<string, TranslationKey> = {
  'jobs.list': 'pairing.fortressAction.jobsList',
  'jobs.run': 'pairing.fortressAction.jobsRun',
  'jobs.targets': 'pairing.fortressAction.jobsTargets',
  'jobs.create': 'pairing.fortressAction.jobsCreate',
  'backup.export': 'pairing.fortressAction.backupExport',
  'backup.import': 'pairing.fortressAction.backupImport',
  'ui.open': 'pairing.fortressAction.uiOpen',
};

/** PRINT_LENGTH is how much of a member's id names it, as much as the bar
 *  above an opened tab shows. */
const PRINT_LENGTH = 8;

/** Every server instance opens: through the fortress where it offers its
 *  pages, else as its status window. A phone has nothing to open. */
export function fortressApps(members: FortressMember[], ago: (unix: number) => string): FortressApp[] {
  const apps = new Map<string, FortressApp>();
  for (const m of members) {
    if (m.app === SELF.id) continue;
    const app = apps.get(m.app) ?? { id: m.app, name: m.name || m.app, instances: [] };
    // Unix seconds, left out until the member was seen once.
    const seen = m.lastSeen ?? 0;
    app.instances.push({
      id: m.id,
      device: m.device,
      deviceKind: m.kind === 'mobile' ? 'phone' : 'server',
      form: m.deployment || undefined,
      version: m.version || undefined,
      presence: m.online ? 'connected' : 'away',
      relay: m.relay,
      lastSeen: !m.online && seen > 0 ? ago(seen) : undefined,
      open: m.kind === 'instance' ? {} : undefined,
      doubt: doubted(m, members) || undefined,
      print: m.id.slice(-PRINT_LENGTH),
    });
    apps.set(m.app, app);
  }
  return [...apps.values()].sort((a, b) => a.name.localeCompare(b.name));
}

/** doubted reports whether another member claims m's app on m's server or
 *  device, so that one of the two may be copying the other's sign-in. */
function doubted(m: FortressMember, members: FortressMember[]): boolean {
  return members.some(
    (o) =>
      o.id !== m.id &&
      o.app === m.app &&
      o.kind === m.kind &&
      ((!!m.host && o.host === m.host) || (!!m.device && o.device === m.device)),
  );
}

export function fortressPermissions(
  permissions: FortressPermissionState[],
  members: FortressMember[],
  t: T,
): FortressPermission[] {
  const name = (id: string) => (id === SELF.id ? SELF.name : (members.find((m) => m.app === id)?.name ?? id));
  return permissions.map((p) => {
    const words = ACTION_TITLES[p.action];
    return {
      id: `${p.from} ${p.to} ${p.action}`,
      from: p.from,
      to: [p.to],
      on: p.on,
      title: words ? t(words) : p.action,
      detail: t('pairing.fortressPermissionPair', { from: name(p.from), to: name(p.to) }),
    };
  });
}

/** The switch an id of fortressPermissions names. */
export function permissionOf(id: string, on: boolean): FortressPermissionState {
  const [from, to, action] = id.split(' ');
  return { from, to, action, on };
}

/** phraseCode is what the QR code beside the phrase carries, as the server's
 *  relay.PairingCode has it: on a relay of the group's own, the address the
 *  Android app dials rides on a second line, since the app knows only the
 *  project relay. */
export function phraseCode(words: string[], group: Pick<ConnectInfo, 'relayMode' | 'relayUrl' | 'projectRelayUrl'>): string {
  const phrase = words.join(' ');
  const dial = group.relayMode === 'own' ? dialAddress(group.relayUrl) : null;
  return dial && dial !== group.projectRelayUrl ? `${phrase}\n${dial}` : phrase;
}

const SOCKET_SCHEMES: Record<string, string> = { 'http:': 'ws:', 'https:': 'wss:', 'ws:': 'ws:', 'wss:': 'wss:' };

/** dialAddress turns a configured relay address into the socket the kit dials,
 *  as its relay.connectURL does: a path that does not end in /connect is where
 *  a proxy serves the instance, with the relay below it. */
function dialAddress(raw: string): string | null {
  let u: URL;
  try {
    u = new URL(raw.trim());
  } catch {
    return null;
  }
  const scheme = SOCKET_SCHEMES[u.protocol];
  if (!scheme || u.host === '') return null;
  u.protocol = scheme;
  const path = u.pathname.replace(/\/+$/, '');
  u.pathname = path.endsWith('/connect') ? path : `${path}/relay/connect`;
  return u.toString();
}

/** memberStatus puts a member's report and log into the shape GlimStone's
 *  status window takes, every time in the app's own format. */
export function memberStatus(
  s: FortressMemberStatus,
  when: (iso: string) => string,
): { report: FortressStatusReport; log: FortressLogLine[] } {
  return {
    report: {
      state: s.report.state,
      needsYou: (s.report.needsYou ?? []).map((n) => ({ severity: n.severity, text: n.text })),
      work: (s.report.work ?? []).map((w) => ({ title: w.title, progress: w.progress })),
      next: (s.report.next ?? []).map((n) => ({ title: n.title, at: when(n.at) })),
    },
    log: s.log.map((e) => ({ at: when(e.at), level: e.level, text: e.text })),
  };
}
