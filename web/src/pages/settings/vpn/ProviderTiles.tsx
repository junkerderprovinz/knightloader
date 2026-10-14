import { Card, NamedHint, SectionTitle, useTooltip } from '../../../components/ui';
import type { VpnProvider, VpnState } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { IconPortForward } from '../../../lib/icons';
import { STATE_WORD, VpnMark, nameOf } from './VpnMark';

/**
 * ProviderTiles offers every provider as a tile in the shape of the picker's
 * brand tiles. A provider in use or set up keeps its state on the tile's
 * second line, and one that can forward a port carries the port mark.
 */
export function ProviderTiles({ hue, vpn, onPick }: { hue: number; vpn: VpnState; onPick: (id: string) => void }) {
  const { t } = useT();
  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.vpn.providersHint')}>{t('settings.vpn.providersTitle')}</SectionTitle>
      <ul className="glim-provider-pick kl-vpn-pick">
        {vpn.providers.map((p) => (
          <ProviderTile key={p.id} provider={p} active={p.id === vpn.active} onPick={() => onPick(p.id)} />
        ))}
      </ul>
    </Card>
  );
}

function ProviderTile({ provider: p, active, onPick }: { provider: VpnProvider; active: boolean; onPick: () => void }) {
  const { t } = useT();
  const name = nameOf(t, p);
  const held = p.state !== 'idle' || p.configured;
  let sub = t(STATE_WORD[p.state]);
  if (p.state === 'connected' && p.exitIp) sub = `${sub} · ${p.exitIp}`;
  else if (p.state === 'idle' && p.configured) sub = t('settings.vpn.state.saved');
  else if (p.state === 'idle' && p.id === 'custom') sub = t('settings.vpn.customSub');
  return (
    <li className={`glim-readme-btn-unit group glim-tile-house ${held ? 'glim-readme-btn-unit--note' : ''}`}>
      <button
        type="button"
        onClick={onPick}
        aria-label={`${name} ${sub}`}
        aria-current={active || undefined}
        className="glim-readme-btn glim-brand-tile"
      >
        <span className="glim-readme-btn-mark" aria-hidden>
          <VpnMark id={p.id} name={name} size={25} />
        </span>
        <span className="glim-readme-btn-text">
          <span className="glim-readme-btn-name">{name}</span>
          <span className="glim-readme-btn-sub">{sub}</span>
        </span>
      </button>
      <span className="glim-readme-btn-sheen" aria-hidden />
      {/* A sibling of the button, as ReadmeButton's (i) is, so hovering it
          explains and pressing it opens nothing. */}
      {p.portForwarding && (
        <span className="glim-readme-btn-hint text-carbon-textSub">
          <PortMark name={name} />
        </span>
      )}
    </li>
  );
}

function PortMark({ name }: { name: string }) {
  const { t } = useT();
  const label = t('settings.vpn.portForwarding');
  const tip = useTooltip<HTMLSpanElement>(
    <NamedHint name={label} hint={t('settings.vpn.portForwardingHint', { name })} />,
  );
  return (
    <>
      <span {...tip.triggerProps} aria-label={label} className="flex cursor-help">
        <IconPortForward width={16} height={16} />
      </span>
      {tip.node}
    </>
  );
}
