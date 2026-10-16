import { useEffect, useState } from 'react';
import { ContextMenu, anchorBelow, anchorFromEvent, useContextMenu } from '../../../components/ContextMenu';
import {
  Button,
  Card,
  EmptyState,
  IconBadge,
  InfoBubble,
  Modal,
  SectionTitle,
  Toggle,
  ToggleRow,
} from '../../../components/ui';
import {
  connectVpn,
  disconnectVpn,
  fetchVpn,
  forgetVpn,
  setVpnRoutes,
  type VpnProvider,
  type VpnRoutes,
  type VpnState,
} from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { copyToClipboard } from '../../../lib/clipboard';
import { IconAdd, IconClose, IconCopy, IconGear, IconShieldOn, IconTrash } from '../../../lib/icons';
import { useToast } from '../../../lib/toast';
import { refusalText } from '../tx';
import { VpnMark, VpnStateBadge, failureOf, nameOf } from './VpnMark';
import { VpnPicker } from './VpnPicker';

// A connect settles within seconds, and a tunnel that drops shows within the
// slower beat.
const POLL_CONNECTING_MS = 1000;
const POLL_MS = 5000;

const ROUTES: { key: keyof VpnRoutes; label: TranslationKey; hint?: TranslationKey }[] = [
  { key: 'torrents', label: 'settings.vpn.route.torrents', hint: 'settings.vpn.route.torrentsHint' },
  { key: 'hosters', label: 'settings.vpn.route.hosters' },
  { key: 'ytdlp', label: 'settings.vpn.route.ytdlp' },
  { key: 'debrid', label: 'settings.vpn.route.debrid', hint: 'settings.vpn.route.debridHint' },
  { key: 'usenet', label: 'settings.vpn.route.usenet', hint: 'settings.vpn.route.usenetHint' },
];

/**
 * VpnCard holds the VPN providers that are set up, as the account cards hold
 * accounts: empty until one is added through Add, then a row each. Once one is
 * set up, a second card below holds the switches for which downloads go
 * through the one in use.
 */
export function VpnCard({ hue, routesHue }: { hue: number; routesHue: number }) {
  const { t } = useT();
  const { toast } = useToast();
  const [vpn, setVpn] = useState<VpnState | null>(null);
  const [retry, setRetry] = useState(0);
  const [picking, setPicking] = useState<{ editing?: string } | null>(null);
  const [removing, setRemoving] = useState<VpnProvider | null>(null);

  const connecting = vpn?.providers.some((p) => p.state === 'connecting') ?? false;
  useEffect(() => {
    let live = true;
    const load = async () => {
      try {
        const next = await fetchVpn();
        if (live) setVpn(next);
      } catch {
        // The next beat tries again; the card keeps what it showed.
      }
    };
    void load();
    const timer = window.setInterval(() => void load(), connecting ? POLL_CONNECTING_MS : POLL_MS);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, [connecting, retry]);

  async function act(call: () => Promise<VpnState>) {
    try {
      setVpn(await call());
    } catch (e) {
      toast(refusalText(t, e), 'fail');
      setRetry((n) => n + 1);
    }
  }

  const setUp = vpn?.providers.filter((p) => p.configured || p.id === vpn.active) ?? [];
  const active = vpn?.providers.find((p) => p.id === vpn.active);

  return (
    <>
      <Card hue={hue} data-new="vpn" className="flex flex-col gap-4">
        <SectionTitle
          hint={t('settings.vpn.cardHint')}
          right={
            <Button kind="secondary" icon={<IconAdd width={16} height={16} />} onClick={() => setPicking({})} disabled={!vpn}>
              {t('settings.vpn.add')}
            </Button>
          }
        >
          {t('settings.vpn.title')}
        </SectionTitle>

        {setUp.length === 0 ? (
          <EmptyState
            nested
            icon={<IconShieldOn width={26} height={26} />}
            title={t('settings.vpn.empty')}
            hint={t('settings.vpn.emptyHint', { button: t('settings.vpn.add') })}
          />
        ) : (
          <>
            <VpnTable
              rows={setUp}
              active={vpn?.active ?? ''}
              onUse={(p, on) => void act(() => (on ? connectVpn(p.id, {}) : disconnectVpn()))}
              onEdit={(p) => setPicking({ editing: p.id })}
              onRemove={setRemoving}
            />
            {active?.state === 'failed' && (
              <p role="status" className="text-sm text-statusFail">
                {failureOf(t, active)}
              </p>
            )}
          </>
        )}

        {picking && vpn && (
          <VpnPicker
            vpn={vpn}
            editing={picking.editing}
            hue={hue}
            onClose={() => setPicking(null)}
            onState={setVpn}
          />
        )}

        {removing && (
          <Modal
            title={t('accounts.remove')}
            hue={hue}
            onClose={() => setRemoving(null)}
            footer={
              <>
                <span className="flex-1" />
                <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={() => setRemoving(null)} />
                <Button
                  kind="ghost"
                  icon={<IconTrash width={16} height={16} />}
                  onClick={() => {
                    const id = removing.id;
                    setRemoving(null);
                    void act(() => forgetVpn(id));
                  }}
                >
                  {t('accounts.remove')}
                </Button>
              </>
            }
          >
            <p className="text-sm text-carbon-text">{t('settings.vpn.removeConfirm', { name: nameOf(t, removing) })}</p>
          </Modal>
        )}
      </Card>

      {vpn && setUp.length > 0 && (
        <Card hue={routesHue} data-new="vpn-routes" className="flex flex-col gap-3">
          <SectionTitle hint={t('settings.vpn.routesHint')}>{t('settings.vpn.routesTitle')}</SectionTitle>
          {ROUTES.map((r) => (
            <ToggleRow
              key={r.key}
              label={t(r.label)}
              hint={r.hint ? t(r.hint) : undefined}
              checked={vpn.routes[r.key]}
              onChange={(on) => void act(() => setVpnRoutes({ ...vpn.routes, [r.key]: on }))}
            />
          ))}
        </Card>
      )}
    </>
  );
}

/**
 * VpnTable lists the providers set up, in the shape of the account cards'
 * table: the switch puts torrents and the other kinds switched on through the
 * provider, the row opens its form, and the gear its menu.
 */
function VpnTable({
  rows,
  active,
  onUse,
  onEdit,
  onRemove,
}: {
  rows: VpnProvider[];
  active: string;
  onUse: (p: VpnProvider, on: boolean) => void;
  onEdit: (p: VpnProvider) => void;
  onRemove: (p: VpnProvider) => void;
}) {
  const { t } = useT();
  const menu = useContextMenu();
  const [menuKey, setMenuKey] = useState<string | null>(null);
  const open = rows.find((r) => r.id === menuKey);
  const label = t('settings.vpn.title');

  return (
    <div className="glim-well relative overflow-x-auto p-0">
      <table className="w-full min-w-[34rem] border-collapse text-sm" aria-label={label}>
        <thead>
          <tr className="text-start text-xs text-carbon-textMuted">
            <th className="w-12 px-4 py-3 text-start font-medium">{t('settings.vpn.col.inUse')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('settings.vpn.col.provider')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.status')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('settings.vpn.col.exitIp')}</th>
            <th className="px-2 py-3 text-start font-medium">
              <span data-new="vpn-ports" className="inline-flex items-center">
                {t('settings.vpn.col.port')}
                <InfoBubble tip={t('settings.vpn.col.portHint')} label={t('settings.vpn.col.port')} />
              </span>
            </th>
            <th className="w-10 px-2 py-3">
              <span className="sr-only">{t('accounts.rowActions')}</span>
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-carbon-border/40">
          {rows.map((p, i) => {
            const name = nameOf(t, p);
            const up = p.state === 'connected';
            return (
              <tr
                key={p.id}
                className="glim-row cursor-pointer transition-colors hover:bg-carbon-surface3"
                onClick={(e) => {
                  if (!(e.target as Element).closest('button, a, [role="switch"], [role="note"]')) onEdit(p);
                }}
                onContextMenu={(e) => {
                  e.preventDefault();
                  setMenuKey(p.id);
                  menu.openAt(anchorFromEvent(e));
                }}
              >
                <td className="px-4 py-3">
                  <Toggle
                    checked={p.id === active}
                    onChange={(on) => onUse(p, on)}
                    label={t('settings.vpn.use', { name })}
                    hideLabel
                  />
                </td>
                <td className="px-2 py-3 font-medium text-carbon-text">
                  <span className="inline-flex items-center gap-2">
                    <VpnMark id={p.id} name={name} size={18} />
                    {name}
                  </span>
                </td>
                <td className="px-2 py-3">
                  <VpnStateBadge provider={p} t={t} />
                </td>
                <td className="glim-num px-2 py-3 text-carbon-textSub" dir="ltr">
                  {up && p.exitIp ? p.exitIp : '-'}
                </td>
                <td className="glim-num px-2 py-3 text-carbon-textSub" dir="ltr">
                  {!up || !p.forwardedPort ? (
                    '-'
                  ) : p.portManual ? (
                    <span className="inline-flex items-center">
                      {t('settings.vpn.portToForward', { port: p.forwardedPort })}
                      <InfoBubble
                        tip={t('settings.vpn.portToForwardHint', { name })}
                        label={t('settings.vpn.portToForward', { port: p.forwardedPort })}
                      />
                    </span>
                  ) : (
                    <ReachedAt provider={p} hue={i} />
                  )}
                </td>
                <td className="px-2 py-3 text-end">
                  <span className="glim-row-actions inline-flex">
                    <IconBadge
                      hue={i}
                      icon={<IconGear width={16} height={16} />}
                      title={t('accounts.rowActions')}
                      aria-label={t('accounts.rowActions')}
                      aria-haspopup="menu"
                      aria-expanded={menuKey === p.id}
                      onClick={(e) => {
                        setMenuKey(p.id);
                        menu.openAt(anchorBelow(e.currentTarget));
                      }}
                    />
                  </span>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>

      {menu.anchor && open && (
        <ContextMenu
          anchor={menu.anchor}
          label={t('accounts.rowActions')}
          onClose={() => {
            menu.close();
            setMenuKey(null);
          }}
          groups={[
            { id: 'edit', items: [{ id: 'edit', label: t('accounts.edit'), onSelect: () => onEdit(open) }] },
            { id: 'remove', items: [{ id: 'remove', label: t('accounts.remove'), onSelect: () => onRemove(open) }] },
          ]}
        />
      )}
    </div>
  );
}

/**
 * ReachedAt is where peers reach the forwarded port: the address the gateway
 * takes it on where NAT-PMP says one, which need not be the exit address, and
 * the exit address otherwise.
 */
function ReachedAt({ provider: p, hue }: { provider: VpnProvider; hue: number }) {
  const { t } = useT();
  const [copies, setCopies] = useState(0);
  const [copied, setCopied] = useState(false);
  const host = p.forwardedAddress || p.exitIp;
  const where = host ? `${host}:${p.forwardedPort}` : String(p.forwardedPort);
  const word = copied ? t('common.copied') : t('common.copy');
  return (
    <span className="inline-flex items-center gap-2">
      {where}
      <IconBadge
        hue={hue}
        icon={<IconCopy width={14} height={14} />}
        title={word}
        aria-label={word}
        confirm={copies}
        onClick={async () => {
          if (await copyToClipboard(where)) {
            setCopied(true);
            setCopies((n) => n + 1);
            setTimeout(() => setCopied(false), 1800);
          }
        }}
      />
    </span>
  );
}
