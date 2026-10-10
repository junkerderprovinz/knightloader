// RelayCard picks how the instances of a group reach each other: the project
// relay, an own relay, or none. The route picker, what the relay sees and
// whether it is connected stand in one line. Under it each route gets a few
// fact lines on one side and its picture on the other, and an own relay the
// two places it can come from. Every control saves on its own, as the rest of
// the settings do.
import { useEffect, useRef, useState } from 'react';
import { StatePill } from '../../../components/StatePill';
import { Button, Card, IconBadge, InfoBubble, Modal, SectionTitle, TextInput, ToggleRow } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { saveRelayConfig, type ConnectInfo, type RelayConfig, type RelayMode } from '../../../lib/api';
import { copyToClipboard } from '../../../lib/clipboard';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { IconCheckDrawn, IconClipboard, IconClose, IconEye, IconRetry } from '../../../lib/icons';
import { useToast } from '../../../lib/toast';
import { FactGlyph, RouteDiagram, RouteGlyph, type RouteKind } from './pairingArt';
import { Fact, SetRow, Subcard } from './parts';

type T = ReturnType<typeof useT>['t'];

const MODES: RelayMode[] = ['project', 'own', 'off'];

// An own relay without TLS still works on a LAN, but its relay key then
// travels in the clear, so the card says so.
const PLAINTEXT_RELAY = /^(ws|http):\/\//i;

/**
 * The command that starts ParleyPort in plain mode, for a reverse proxy in
 * front. Only its domain mode keeps certificates, so this one needs no volume.
 */
export const RELAY_RUN_COMMAND =
  'docker run -d --name parleyport -p 8760:8760 --restart unless-stopped junkerderprovinz/parleyport:latest';

const COPY: Record<
  RelayMode,
  { name: TranslationKey; sentence: TranslationKey; need?: TranslationKey; sees: TranslationKey; alt: TranslationKey }
> = {
  project: {
    name: 'relay.project',
    sentence: 'relay.projectSentence',
    need: 'relay.projectNeed',
    sees: 'relay.projectSees',
    alt: 'relay.projectAlt',
  },
  // No `need`: the two sub-cards under the picture say what an own relay
  // takes, in more detail than one line could.
  own: {
    name: 'relay.own',
    sentence: 'relay.ownSentence',
    sees: 'relay.ownSees',
    alt: 'relay.ownAlt',
  },
  off: {
    name: 'relay.off',
    sentence: 'relay.offSentence',
    need: 'relay.offNeed',
    sees: 'relay.offSees',
    alt: 'relay.offAlt',
  },
};

function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

function AddressAfter({ address, t }: { address: string; t: T }) {
  return (
    <p className="mt-auto pt-0.5 text-xs text-carbon-textMuted">
      {t('relay.addressAfter')}
      <span dir="ltr" className="block break-all font-mono text-carbon-textSub">
        {address}
      </span>
    </p>
  );
}

function CommandLine({ t }: { t: T }) {
  const [copies, setCopies] = useState(0);
  return (
    <div className="flex items-start gap-2">
      <code
        className="glim-num min-w-0 flex-1 overflow-x-auto rounded-[var(--radius-control)] bg-carbon-surface px-3 py-2 text-xs leading-relaxed text-carbon-text"
        dir="ltr"
      >
        {RELAY_RUN_COMMAND}
      </code>
      <IconBadge
        labelled
        className="kl-raised-btn"
        icon={copies > 0 ? <IconCheckDrawn width={16} height={16} /> : <IconClipboard width={16} height={16} />}
        title={t('common.copy')}
        aria-label={t('common.copy')}
        confirm={copies}
        onClick={async () => {
          if (await copyToClipboard(RELAY_RUN_COMMAND)) setCopies((n) => n + 1);
        }}
      />
    </div>
  );
}

export function RelayCard({
  group,
  relay,
  onRelay,
  onRefresh,
  hue,
}: {
  group: ConnectInfo;
  relay: RelayConfig;
  /** Called with what the server stored, so the page reloads the group. */
  onRelay: (r: RelayConfig) => void;
  /** Asks the server again whether the relay is reachable. */
  onRefresh?: () => void;
  hue?: number;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [mode, setMode] = useState<RelayMode>(relay.mode);
  const [url, setUrl] = useState(relay.relayUrl);
  const [urlError, setUrlError] = useState<string | null>(null);
  const [seesOpen, setSeesOpen] = useState(false);
  const editing = useRef(false);
  const typed = useRef(0);
  const timer = useRef<number | null>(null);

  // The page reloads the relay every few seconds; that must not undo what is
  // being typed or picked right now.
  useEffect(() => {
    if (!editing.current) setUrl(relay.relayUrl);
  }, [relay.relayUrl]);
  useEffect(() => {
    setMode(relay.mode);
  }, [relay.mode]);
  useEffect(
    () => () => {
      if (timer.current !== null) window.clearTimeout(timer.current);
    },
    [],
  );

  async function save(patch: { mode?: RelayMode; url?: string; serve?: boolean }): Promise<boolean> {
    try {
      onRelay(await saveRelayConfig(patch.url ?? url, undefined, patch.serve, patch.mode));
      return true;
    } catch (e) {
      const msg = e instanceof Error && e.message ? e.message : t('relay.saveError');
      if (patch.url !== undefined) setUrlError(msg);
      else toast(msg, 'fail');
      return false;
    }
  }

  // A failed save leaves the server on its old mode, and the tabs go back to
  // it, or the address typed next would be stored for a relay nobody uses.
  async function pick(next: RelayMode) {
    setMode(next);
    if (!(await save({ mode: next }))) setMode(relay.mode);
  }

  function typeUrl(v: string) {
    editing.current = true;
    const n = ++typed.current;
    setUrl(v);
    setUrlError(null);
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      void save({ url: v.trim() }).then(() => {
        // Still typing: the stored address must not replace the newer text.
        if (typed.current === n) editing.current = false;
      });
    }, 800);
  }

  const copy = COPY[mode];
  // The relay key comes from the twelve words, so the relay is only dialled
  // inside a group; before that there is nothing to be connected.
  const state =
    mode === 'off' ? (
      <StatePill label={t('relay.off')} tone="neutral" />
    ) : !group.active ? (
      <StatePill label={t('relay.noGroup')} tone="neutral" tip={t('relay.noGroupLine')} />
    ) : relay.connected ? (
      <StatePill label={t('instances.connected')} tone="ok" />
    ) : (
      <StatePill label={t('instances.notConnected')} tone="warn" />
    );
  const unreachable = group.active && mode !== 'off' && !relay.connected;

  // An instance that serves the relay itself is the relay in the picture.
  const route: RouteKind = mode === 'own' ? (relay.serve ? 'instance' : 'own') : mode;
  const [before, after] = t('pairing.relayCheckProject').split('{host}');

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={`${t('relay.lead')} ${t('relay.hint')}`}>{t('relay.title')}</SectionTitle>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Tabs
          variant="well"
          inline
          labelled
          label={t('relay.title')}
          active={mode}
          onSelect={(id) => void pick(id as RelayMode)}
          items={MODES.map((m) => ({ id: m, label: t(COPY[m].name), icon: <RouteGlyph kind={m} /> }))}
        />
        {/* Where the two do not fit side by side, the state goes under the
            button, whose words stay on one line. */}
        <span className="ms-auto flex flex-wrap items-center justify-end gap-2.5">
          <Button kind="secondary" className="whitespace-nowrap" icon={<IconEye />} onClick={() => setSeesOpen(true)}>
            {t('relay.seesTitle')}
          </Button>
          <span data-testid="relay-state" className="inline-flex">
            {state}
          </span>
        </span>
      </div>

      <div className="grid grid-cols-1 items-start gap-6 min-[861px]:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-2.5">
          <Fact glyph={<FactGlyph kind="route" />}>
            {mode === 'project' ? t(copy.sentence, { host: hostOf(group.projectRelayUrl) }) : t(copy.sentence)}
          </Fact>
          {copy.need && (
            <Fact glyph={<FactGlyph kind="need" />} label={t('relay.needLabel')}>
              {t(copy.need)}
            </Fact>
          )}
          {mode === 'own' && (
            <>
              <SetRow label={t('relay.addressLabel')} hint={t('relay.addressTip')} htmlFor="relay-address">
                <div className="w-[18rem] max-w-full">
                  <TextInput
                    id="relay-address"
                    type="url"
                    value={url}
                    onChange={(e) => typeUrl(e.target.value)}
                    spellCheck={false}
                    autoComplete="off"
                    placeholder="https://relay.example.org"
                    dir="ltr"
                    className="font-mono"
                  />
                </div>
              </SetRow>
              {urlError && <p className="text-sm text-statusFail">{urlError}</p>}
              {!urlError && PLAINTEXT_RELAY.test(url.trim()) && (
                <p className="rounded-[var(--radius-control)] bg-statusWarnBgSoft px-3 py-2.5 text-subline leading-relaxed text-carbon-text">
                  {t('relay.plaintextWarning')}
                </p>
              )}
            </>
          )}
          <Fact glyph={<FactGlyph kind="lock" />}>
            {t('relay.e2e')} <InfoBubble tip={t('relay.e2eTip')} />
          </Fact>
          {unreachable && (
            <Subcard title={t('pairing.relayCheckOpen')} hint={t('pairing.relayCheckTip')}>
              <p className="text-subline text-carbon-textMuted">{t('pairing.relayCheckLead')}</p>
              <ul className="flex list-disc flex-col gap-1 ps-5 text-subline text-carbon-textSub">
                <li>
                  {mode === 'own' ? (
                    t('pairing.relayCheckOwn')
                  ) : (
                    <>
                      {before}
                      <span dir="ltr" className="font-mono text-[0.92em] text-carbon-text">
                        {hostOf(group.projectRelayUrl)}
                      </span>
                      {after}
                    </>
                  )}
                </li>
                <li>{t('pairing.relayCheckFilter')}</li>
              </ul>
              {onRefresh && (
                <div className="flex justify-end">
                  <Button kind="secondary" className="kl-raised-btn" icon={<IconRetry />} onClick={onRefresh}>
                    {t('pairing.checkAgain')}
                  </Button>
                </div>
              )}
            </Subcard>
          )}
        </div>
        <RouteDiagram
          kind={route}
          alt={t(copy.alt)}
          labels={{
            yourNetwork: t('relay.diagramYourNetwork'),
            otherNetwork: t('relay.diagramOtherNetwork'),
            relay: t('relay.diagramRelay'),
            relayName: t('relay.containerName'),
            selfHosted: t('relay.own'),
          }}
        />
      </div>

      {mode === 'own' && (
        <div className="flex flex-col gap-3" data-testid="own-relay">
          <span className="glim-eyebrow">{t('relay.sourcesTitle')}</span>
          <div className="grid grid-cols-1 gap-3 min-[861px]:grid-cols-2">
            <Subcard title={t('relay.containerName')}>
              <p className="text-subline text-carbon-textMuted">{t('relay.containerSub')}</p>
              <Fact glyph={<FactGlyph kind="search" />}>{t('relay.containerFind', { name: t('relay.containerName') })}</Fact>
              <CommandLine t={t} />
              <Fact glyph={<FactGlyph kind="shield" />}>{t('relay.containerCert')}</Fact>
              <AddressAfter address="https://relay.example.org" t={t} />
            </Subcard>
            <Subcard title={t('relay.instanceName')}>
              <p className="text-subline text-carbon-textMuted">{t('relay.instanceSub')}</p>
              <div className="rounded-[var(--radius-control)] bg-carbon-surface px-3 py-2">
                <ToggleRow
                  label={t('relay.serve')}
                  hint={t('relay.serveHint')}
                  checked={relay.serve}
                  onChange={(v) => void save({ serve: v })}
                />
                {relay.serve && (
                  <p className="mt-1 text-xs text-carbon-textMuted">{t('relay.serveClients', { count: relay.serveClients })}</p>
                )}
              </div>
              <Fact glyph={<FactGlyph kind="shield" />}>
                {t('relay.instanceCert')} <InfoBubble tip={t('relay.instanceCertTip')} />
              </Fact>
              <AddressAfter address="https://knightloader.example.org" t={t} />
            </Subcard>
          </div>
        </div>
      )}

      {seesOpen && (
        <Modal
          title={t('relay.seesTitle')}
          onClose={() => setSeesOpen(false)}
          footer={<Button kind="secondary" labelled icon={<IconClose />} title={t('common.close')} onClick={() => setSeesOpen(false)} />}
        >
          <Fact glyph={<FactGlyph kind="sees" />} label={t('relay.seesLabel')}>
            {t(copy.sees)}
          </Fact>
          {/* What it sees and what it never does stand together, so the one
              honest limit, the routing metadata, sits next to everything it
              does not cost. */}
          {mode !== 'off' && (
            <div className="flex flex-col gap-2">
              <Fact glyph={<FactGlyph kind="hidden" />} label={t('relay.notSeesLabel')} />
              <ul className="flex list-disc flex-col gap-1 ps-7 text-sm text-carbon-textSub">
                <li>{t('relay.notSeesContent')}</li>
                <li>{t('relay.notSeesFiles')}</li>
              </ul>
            </div>
          )}
        </Modal>
      )}
    </Card>
  );
}
