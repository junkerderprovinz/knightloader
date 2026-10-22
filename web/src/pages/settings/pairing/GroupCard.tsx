// GroupCard lists who is in the group: the devices that wait for approval,
// this instance with the stage it is in and its key, the other instances, the
// instances added by their address and every phone and browser extension, each
// with the button that takes it out. Beside the list the same group stands as
// a net, and a row and its node light up together.
import { useEffect, useState, type ReactNode } from 'react';
import { GroupNet, SELF_KEY, useNetHover, type NetNode } from '../../../components/GroupNet';
import { CheckingState, usePeerStats } from '../../../components/InstanceCard';
import { RemovalWindow, type Removal } from '../../../components/InstanceRemoval';
import { OkState, StatePill } from '../../../components/StatePill';
import { Button, Card, InfoBubble, SectionTitle } from '../../../components/ui';
import {
  approveDevice,
  fetchDeploymentInfo,
  fetchInstances,
  pairedBetweenCalls,
  type ConnectInfo,
  type GroupApp,
  type GroupMember,
  type Instance,
  type PendingDevice,
} from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { IconBrowser, IconCheck, IconFleet, IconPhone, IconTrash } from '../../../lib/icons';
import { useToast } from '../../../lib/toast';
import { refusalText } from './PhraseCard';
import { STAGE_PILL, clock, type PairStage } from './pairStage';

type T = ReturnType<typeof useT>['t'];

const APP_KIND: Record<string, TranslationKey> = {
  mobile: 'instances.kind.mobile',
  extension: 'instances.kind.extension',
};

/** Row is one member of the group: a glyph, its name, what it is, how it
 *  stands and, for one that can be taken out here, the button that does it.
 *  Rows share one height, so one without a button lines up with the rest. */
function Row({
  entry,
  hot,
  glyph,
  name,
  tag,
  state,
  action,
}: {
  /** The key the row shares with its node in the net. */
  entry: string;
  hot: boolean;
  glyph: ReactNode;
  name: string;
  tag?: string;
  state?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <li
      data-nk={entry}
      className={`flex min-h-11 flex-wrap items-center gap-x-2.5 gap-y-0.5 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-1.5 ${
        hot ? 'shadow-[inset_0_0_0_2px_var(--accent)]' : ''
      }`}
    >
      <span className="shrink-0 text-accentGlyph [&>svg]:h-4.5 [&>svg]:w-4.5">{glyph}</span>
      <span className="min-w-24 flex-1 truncate text-sm font-medium text-carbon-text">{name}</span>
      {tag && <span className="glim-eyebrow shrink-0 tracking-[.12em]">{tag}</span>}
      {state}
      {action}
    </li>
  );
}

function reached(on: boolean, t: T, route?: string): ReactNode {
  return on ? (
    <OkState parts={route ? [t('instances.connected'), route] : [t('instances.connected')]} />
  ) : (
    <StatePill label={t('instances.notConnected')} tone="fail" />
  );
}

/** PeerRow is an instance added by its address, asked live whether it answers. */
function PeerRow({
  p,
  hot,
  action,
  onReach,
  t,
}: {
  p: Instance;
  hot: boolean;
  action: ReactNode;
  /** Tells the card whether the instance answers, for its node in the net. */
  onReach: (name: string, online: boolean) => void;
  t: T;
}) {
  const stats = usePeerStats(`/api/instances/${encodeURIComponent(p.name)}`);
  const online = stats?.online ?? false;
  const answered = stats !== null;
  useEffect(() => {
    if (answered) onReach(p.name, online);
  }, [p.name, answered, online]);
  return (
    <Row
      entry={`peer:${p.name}`}
      hot={hot}
      glyph={<IconFleet />}
      name={p.displayName ?? p.name}
      tag={t('pairing.byAddress')}
      state={answered ? reached(online, t) : <CheckingState />}
      action={action}
    />
  );
}

/** PendingRow is a device that entered the words and waits for approval, with
 *  the key to compare it by and the two answers. */
function PendingRow({
  device,
  busy,
  onApprove,
  onRefuse,
  t,
}: {
  device: PendingDevice;
  busy: boolean;
  onApprove: () => void;
  onRefuse: () => void;
  t: T;
}) {
  const glyph = device.kind === 'mobile' ? <IconPhone /> : device.kind === 'extension' ? <IconBrowser /> : <IconFleet />;
  return (
    <li className="flex min-h-11 flex-wrap items-center gap-x-2.5 gap-y-1.5 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-1.5">
      <span className="shrink-0 text-accentGlyph [&>svg]:h-4.5 [&>svg]:w-4.5">{glyph}</span>
      <span className="flex min-w-24 flex-1 flex-col">
        <span className="truncate text-sm font-medium text-carbon-text">{device.name || device.id}</span>
        <span className="glim-num text-xs text-carbon-textMuted">{t('pairing.fingerprint', { fingerprint: device.fingerprint })}</span>
      </span>
      <span className="glim-eyebrow shrink-0 tracking-[.12em]">
        {APP_KIND[device.kind] ? t(APP_KIND[device.kind]) : t('pairing.pendingInstance')}
      </span>
      <span className="flex shrink-0 items-center gap-2">
        <Button kind="secondary" className="kl-raised-btn px-2.5 text-xs" disabled={busy} onClick={onRefuse}>
          {t('pairing.refuse')}
        </Button>
        <Button
          kind="primary"
          icon={<IconCheck width={14} height={14} />}
          className="px-2.5 text-xs"
          disabled={busy}
          onClick={onApprove}
        >
          {t('pairing.approve')}
        </Button>
      </span>
    </li>
  );
}

function WaitRow({ text, seconds }: { text: string; seconds?: number }) {
  return (
    <div className="flex items-center gap-1.5 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2.5 text-sm text-carbon-textSub">
      <span className="me-1.5 flex shrink-0 gap-1.5" aria-hidden="true">
        {[0, 1, 2].map((i) => (
          <span key={i} className="glim-live h-1.5 w-1.5 rounded-full bg-accentInk" style={{ animationDelay: `${i * 200}ms` }} />
        ))}
      </span>
      <span>{text}</span>
      {seconds !== undefined && <span className="glim-num">{clock(seconds)}</span>}
    </div>
  );
}

export function GroupCard({
  group,
  stage,
  joinedAgo,
  onRefresh,
  hue,
}: {
  group: ConnectInfo;
  stage: PairStage;
  /** Seconds since this instance entered its group, counted on between polls. */
  joinedAgo: number;
  /** Asks the server for the group again, after something was taken out. */
  onRefresh: () => void;
  hue?: number;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const net = useNetHover<HTMLDivElement>();
  const [approving, setApproving] = useState(false);
  // Instances added by address rather than by the phrase, listed with the
  // group so every connection can be taken out in one place.
  const [peers, setPeers] = useState<Instance[]>([]);
  const [answers, setAnswers] = useState<Record<string, boolean>>({});
  const [removing, setRemoving] = useState<Removal | null>(null);
  // Which build this instance is, for the badge on its node.
  const [ownKind, setOwnKind] = useState('');

  useEffect(() => {
    fetchInstances()
      .then((list) => setPeers(list.filter((p) => !p.relayId)))
      .catch(() => {});
  }, [group]);
  useEffect(() => {
    fetchDeploymentInfo()
      .then((d) => setOwnKind(d.kind || d.deployment))
      .catch(() => {});
  }, []);

  const onReach = (name: string, online: boolean) =>
    setAnswers((a) => (a[name] === online ? a : { ...a, [name]: online }));

  const removeButton = (r: Removal) => (
    <Button
      kind="secondary"
      icon={<IconTrash width={14} height={14} />}
      className="kl-raised-btn px-2.5 text-xs"
      aria-label={t('instances.removeTitle', { name: r.name })}
      onClick={() => setRemoving(r)}
    >
      {t('instances.remove')}
    </Button>
  );

  async function approve(id: string) {
    setApproving(true);
    try {
      await approveDevice(id);
      onRefresh();
    } catch (e) {
      toast(refusalText(t, e), 'fail');
    } finally {
      setApproving(false);
    }
  }

  const route = (m: GroupMember) => (m.direct ? t('pairing.direct') : t('pairing.viaRelay'));
  const kind = (a: GroupApp) => (APP_KIND[a.deployment] ? t(APP_KIND[a.deployment]) : undefined);

  // A device waiting for approval is listed once, under the approval, and is
  // no member of the net until it has one.
  const waiting = new Set(group.pending.map((p) => p.id));
  const members = group.members.filter((m) => !waiting.has(m.id));
  const apps = group.apps.filter((a) => !waiting.has(a.id));
  const away = group.away.filter((m) => !members.some((there) => there.id === m.id));
  const inactive = <StatePill label={t('instances.inactive')} tone="neutral" />;

  const nodes: NetNode[] = [
    ...members.map(
      (m): NetNode => ({
        key: `member:${m.id}`,
        deployment: m.deployment,
        name: m.name || m.id,
        sub: route(m),
        relay: !m.direct,
      }),
    ),
    ...away.map(
      (m): NetNode => ({
        key: `member:${m.id}`,
        deployment: m.deployment,
        name: m.name || m.id,
        sub: t('instances.inactive'),
        dim: true,
      }),
    ),
    ...peers.map(
      (p): NetNode => ({
        key: `peer:${p.name}`,
        // A peer added by its address is a server build, as on its card.
        deployment: 'server',
        name: p.displayName ?? p.name,
        sub: t('pairing.byAddress'),
        dim: answers[p.name] === false,
      }),
    ),
    ...apps.map(
      (a): NetNode => ({
        key: `app:${a.id}`,
        deployment: a.deployment,
        name: a.name || a.id,
        sub: kind(a) ?? '',
        dim: !a.connected && !pairedBetweenCalls(a),
        // A phone and an extension reach the group through the relay alone.
        relay: true,
      }),
    ),
  ];

  const pill = stage === 'unpaired' ? null : STAGE_PILL[stage];
  const own =
    pill &&
    (pill.tone === 'ok' ? <OkState parts={[t(pill.key)]} /> : <StatePill label={t(pill.key)} tone={pill.tone} />);

  const list = (
    <div className="flex min-w-0 flex-col gap-2">
      {group.awaiting && <WaitRow text={t('pairing.awaiting')} />}
      {group.pending.length > 0 && (
        <section className="flex flex-col gap-2" data-testid="pending">
          <span className="flex items-center gap-1.5 text-xs font-semibold text-carbon-textSub">
            {t('pairing.pendingTitle')}
            <InfoBubble tip={t('pairing.pendingTip')} />
          </span>
          <ul className="flex flex-col gap-2">
            {group.pending.map((p) => (
              <PendingRow
                key={p.id}
                device={p}
                busy={approving}
                t={t}
                onApprove={() => void approve(p.id)}
                onRefuse={() => setRemoving({ kind: 'refuse', id: p.id, name: p.name || p.id })}
              />
            ))}
          </ul>
        </section>
      )}
      <ul className="flex flex-col gap-2" data-testid="members" data-new="pairing-group">
        <Row
          entry={SELF_KEY}
          hot={net.hot === SELF_KEY}
          glyph={<IconFleet />}
          name={group.name}
          tag={t('instances.thisInstance')}
          state={own && <span data-testid="pair-state" className="inline-flex">{own}</span>}
        />
        {members.map((m) => (
          <Row
            key={m.id}
            entry={`member:${m.id}`}
            hot={net.hot === `member:${m.id}`}
            glyph={<IconFleet />}
            name={m.name || m.id}
            state={<OkState parts={[t('pairing.paired'), route(m)]} />}
            action={removeButton({ kind: 'member', id: m.id, name: m.name || m.id })}
          />
        ))}
        {away.map((m) => (
          <Row
            key={m.id}
            entry={`member:${m.id}`}
            hot={net.hot === `member:${m.id}`}
            glyph={<IconFleet />}
            name={m.name || m.id}
            state={inactive}
            action={removeButton({ kind: 'member', id: m.id, name: m.name || m.id })}
          />
        ))}
        {peers.map((p) => (
          <PeerRow
            key={p.name}
            p={p}
            hot={net.hot === `peer:${p.name}`}
            onReach={onReach}
            t={t}
            action={removeButton({ kind: 'peer', id: p.name, name: p.displayName ?? p.name })}
          />
        ))}
        {apps.map((a) => (
          <Row
            key={a.id}
            entry={`app:${a.id}`}
            hot={net.hot === `app:${a.id}`}
            glyph={a.deployment === 'extension' ? <IconBrowser /> : <IconPhone />}
            name={a.name || a.id}
            tag={kind(a)}
            state={
              pairedBetweenCalls(a) ? (
                <span className="inline-flex items-center gap-1.5">
                  <OkState parts={[t('instances.paired')]} />
                  <InfoBubble tip={t('instances.extensionPairedTip')} />
                </span>
              ) : a.connected ? (
                reached(true, t)
              ) : (
                inactive
              )
            }
            action={removeButton({ kind: 'app', id: a.id, name: a.name || a.id })}
          />
        ))}
      </ul>
      {group.fingerprint && (
        <p className="flex items-center gap-1.5 px-3 text-xs text-carbon-textMuted" data-testid="own-key">
          <span className="glim-num">{t('pairing.ownKey', { fingerprint: group.fingerprint })}</span>
          <InfoBubble tip={t('pairing.ownKeyTip')} />
        </p>
      )}
      {stage === 'new' ? (
        <WaitRow text={t('pairing.waitNext')} />
      ) : stage === 'searching' ? (
        <WaitRow text={t('pairing.searching')} seconds={joinedAgo} />
      ) : stage === 'gone' ? (
        <WaitRow text={t('pairing.searching')} />
      ) : null}
    </div>
  );

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('pairing.groupHint')}>{t('instances.title')}</SectionTitle>
      {nodes.length > 0 ? (
        <div {...net.scope} className="grid grid-cols-1 items-center gap-x-7 gap-y-3 min-[1101px]:grid-cols-[minmax(0,1fr)_minmax(0,24rem)]">
          {list}
          <GroupNet
            self={{ name: group.name, sub: t('instances.thisInstance'), deployment: ownKind }}
            nodes={nodes}
            hot={net.hot}
            onPick={net.pick}
            alt={t('pairing.netAlt')}
          />
        </div>
      ) : (
        list
      )}
      {removing && <RemovalWindow removal={removing} onClose={() => setRemoving(null)} onRemoved={onRefresh} />}
    </Card>
  );
}
