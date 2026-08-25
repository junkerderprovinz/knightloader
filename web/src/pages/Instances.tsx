import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  type DiscoveredInstance,
  type Instance,
  type Settings,
  addInstance,
  fetchDiscovered,
  fetchInstances,
  fetchSettings,
  redeemPairingCode,
  removeInstance,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { PageHeader, Card, Button, Field, IconBadge, TextInput, SectionTitle } from '../components/ui';
import { InstanceCard } from '../components/InstanceCard';
import { FirstTouchHint } from '../components/FirstTouchHint';
import { IconClipboard } from '../lib/icons';
import { useToast } from '../lib/toast';

// Reading the clipboard, unlike writing it (lib/clipboard.ts's copyToClipboard),
// has no reliable fallback for an insecure origin - execCommand('copy') works
// everywhere, but browsers refuse a script-driven execCommand('paste') outright
// as a real security boundary, not merely an availability quirk. Feature-detected
// once at module scope (the same check PasteFromClipboardButton.tsx already
// established for exactly this API), then used to DISABLE the paste badge
// below rather than hide it - a control that vanishes teaches nobody what the
// mode can do (settings/Look.tsx's UpdateCard/SystemCards established that
// pattern first). `disabled` carries the "why" in its own title instead of
// pretending the option was never there, which is the honest response to a
// real, unworkaroundable browser boundary - KnightLoader's most common real
// deployment is plain http://<lan-ip>, an insecure context, where this is
// always false.
const CLIPBOARD_READABLE = typeof navigator !== 'undefined' && !!navigator.clipboard?.readText;

export function Instances() {
  const { t } = useT();
  const { toast } = useToast();
  const [peers, setPeers] = useState<Instance[]>([]);
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [err, setErr] = useState('');
  const [code, setCode] = useState('');
  const [pairing, setPairing] = useState(false);
  const [pairErr, setPairErr] = useState('');
  const [pairOk, setPairOk] = useState('');
  // The configured name (settings/Access.tsx's own IdentityCard), so this
  // instance shows up on its own card the same way a peer does - not the
  // generic "this instance" placeholder (jdp: "unter instanz soll diese
  // instanz mit dem eingestellten namen erscheinen nicht mit 'diese
  // instanz'"). Falls back to the placeholder for the common case of never
  // having named it.
  const [ownName, setOwnName] = useState('');
  // Instances announcing themselves on this network (internal/discovery).
  // Polled rather than pushed: an instance appears when it boots and drops out
  // when it stops announcing, so a page left open should follow that without
  // needing a reload.
  const [found, setFound] = useState<DiscoveredInstance[]>([]);
  const navigate = useNavigate();

  const load = () => fetchInstances().then(setPeers);
  const loadFound = () => fetchDiscovered().then(setFound).catch(() => {});
  useEffect(() => {
    load();
    loadFound();
    fetchSettings()
      .then((s: Settings) => setOwnName(s.instanceName))
      .catch(() => {});
    const iv = setInterval(loadFound, 5000);
    return () => clearInterval(iv);
  }, []);

  // One click instead of typing an address. Deliberately the SAME add the
  // form below runs - discovery supplies the address, it does not grant any
  // trust of its own, and a peer with a password still needs a pairing code
  // to exchange credentials (see internal/api/peertokens.go).
  async function onAddFound(f: DiscoveredInstance) {
    setErr('');
    try {
      const r = await addInstance(f.name, f.url);
      // "Refused us" and "could not be reached" have completely different
      // fixes, so they get different sentences. See addInstance's own doc.
      if (r.needsPairing) setErr(t('instances.needsPairing'));
      else if (!r.online) setErr(t('instances.offlineWarning'));
      await load();
      await loadFound();
    } catch (e: any) {
      setErr(String(e?.message ?? e));
    }
  }

  async function onAdd() {
    setErr('');
    try {
      const r = await addInstance(name.trim(), url.trim());
      if (r.needsPairing) setErr(t('instances.needsPairing'));
      else if (!r.online) setErr(t('instances.offlineWarning'));
      setName('');
      setUrl('');
      await load();
    } catch (e: any) {
      setErr(String(e?.message ?? e));
    }
  }

  // Redeem a code from the OTHER instance's own Access tab (settings/Access.tsx's
  // own "Generate pairing code" card, backed by POST /api/instances/pairing-code)
  // instead of typing that instance's name and address here by hand - one call
  // registers both directions (internal/api/routes_pairing.go's own doc comment
  // on why the redeem handler completes the other side before adding it locally).
  async function onPair() {
    setPairErr('');
    setPairOk('');
    setPairing(true);
    try {
      const r = await redeemPairingCode(code.trim());
      // Two independent halves, reported separately because they fail
      // separately: `online` is this instance reaching the peer, `reachedBack`
      // is the peer reaching this one. Both are now measured - until issue #28
      // the second was stored on the redeemer's word and never tried, so an
      // asymmetric pairing reported a clean success with one half dead.
      //
      // Saying so matters more here than on the manual-add path above: that
      // one only ever claimed one direction, while this one connects both and
      // would otherwise imply both work.
      //
      // THREE cases, not two warnings concatenated. Stacking them produced a
      // message that contradicted itself: the offline warning says the peer
      // did not answer, and pairOneWay's closing clause says this instance can
      // still reach it. Both true only if you read one of them as being about
      // a different moment, which nobody does.
      const warn = !r.online && !r.reachedBack
        ? t('instances.pairNeitherWay')
        : !r.online
          ? t('instances.offlineWarning')
          : !r.reachedBack
            ? t('instances.pairOneWay')
            : '';
      // Said out loud, because it changes what the peer IS: a relay peer is
      // live only while the relay sees it, and it is never written down. A
      // pairing that quietly took a different road than the address suggested
      // is worth one sentence.
      const how = r.viaRelay ? ` ${t('instances.pairViaRelay')}` : '';
      setPairOk(t('instances.pairSuccess', { name: r.name }) + how + (warn ? ` ${warn}` : ''));
      setCode('');
      await load();
    } catch (e: any) {
      setPairErr(String(e?.message ?? e));
    } finally {
      setPairing(false);
    }
  }

  async function onRemove(n: string) {
    await removeInstance(n);
    await load();
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Subtitle removed (jdp, 2026-08-24: "text entfernen: Alle
          KnightLoader von einer Oberfläche aus sehen und steuern.") - the
          title alone already says what this page is. */}
      <PageHeader title={t('instances.title')} />

      <FirstTouchHint id="instances" />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <InstanceCard name={ownName || t('instances.thisInstance')} url={location.host} base="/api" />
        {peers.map((p, i) => (
          <InstanceCard
            key={p.name}
            name={p.displayName ?? p.name}
            url={p.url}
            relayId={p.relayId}
            base={`/api/instances/${encodeURIComponent(p.name)}`}
            onOpen={() => navigate(`/downloads?instance=${encodeURIComponent(p.name)}`)}
            // No remove badge for a relay peer, because there is nothing to
            // remove. A relay peer is synthesised per request from whoever is
            // currently connected to the relay (federation.Manager.reachable)
            // and is never in the stored list, so Remove deleted nothing, the
            // route still answered 204, and the reload put the identical card
            // straight back - a button that reproducibly did nothing, silently.
            // A relay peer goes away by disconnecting it or clearing the relay
            // config, not from here.
            onRemove={p.relayId ? undefined : () => onRemove(p.name)}
            hue={i}
          />
        ))}
      </div>

      {found.length > 0 && (
        <Card className="flex flex-col gap-3">
          <SectionTitle hint={t('instances.foundHint')}>{t('instances.foundTitle')}</SectionTitle>
          {found.map((f) => (
            <div key={f.id} className="flex flex-wrap items-center gap-3">
              <span className="min-w-0 flex-1">
                <span className="text-sm text-carbon-text">{f.name}</span>
                <span className="ml-2 text-xs text-carbon-textMuted" dir="ltr">
                  {f.url}
                </span>
              </span>
              {f.known ? (
                <span className="text-xs text-carbon-textMuted">{t('instances.foundKnown')}</span>
              ) : (
                <Button kind="secondary" className="px-2.5 text-xs" onClick={() => void onAddFound(f)}>
                  {t('instances.foundAdd')}
                </Button>
              )}
            </div>
          ))}
        </Card>
      )}

      <Card className="flex flex-col gap-4">
        <SectionTitle>{t('instances.add')}</SectionTitle>
        <div className="grid grid-cols-1 sm:grid-cols-[1fr_2fr_auto] gap-3 items-end">
          <Field label={t('instances.name')}>
            <TextInput placeholder="cellar" value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t('instances.url')}>
            <TextInput placeholder="http://host:8749" value={url} onChange={(e) => setUrl(e.target.value)} />
          </Field>
          <Button kind="secondary" onClick={onAdd} disabled={!name.trim() || !url.trim()}>
            {t('instances.addButton')}
          </Button>
        </div>
        {err && <div className="text-statusFail text-sm">{err}</div>}
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle hint={t('instances.pairHint')}>{t('instances.pairTitle')}</SectionTitle>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          {/* Same TextInput the Name field above uses (jdp, 2026-08-25:
              "das eingabefeld für den Pairing-code ... soll exakt gleich
              hoch und formatiert sein wie ... das Eingabefeld für den
              Namen") - a hand-built well (a div padded around a bare
              <input>) LOOKS like it should match TextInput's own
              px-3/py-2/text-sm, but a native <input>'s own box-model
              quirks are exactly why that component exists instead of every
              caller re-deriving the same three classes; dir="ltr" and a
              monospace class are just props now, not a reason to opt out
              of it. h-9 on all three controls below (the input, the paste
              badge, the button) is the second half of the same request -
              IconBadge's own square footprint is a fixed h-8 everywhere
              else it is used, which reads a few px short beside a
              text-sm/py-2 input and button once the three sit in one row. */}
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <TextInput
              dir="ltr"
              placeholder={t('instances.pairPlaceholder')}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              className="glim-num h-9 min-w-0 flex-1"
            />
            <IconBadge
              icon={<IconClipboard width={14} height={14} />}
              title={CLIPBOARD_READABLE ? t('instances.pairPaste') : t('instances.pairPasteUnavailable')}
              aria-label={CLIPBOARD_READABLE ? t('instances.pairPaste') : t('instances.pairPasteUnavailable')}
              disabled={!CLIPBOARD_READABLE}
              className="h-9 w-9 shrink-0"
              onClick={async () => {
                try {
                  const text = (await navigator.clipboard.readText()).trim();
                  if (text) setCode(text);
                } catch (e) {
                  toast(t('list.failed', { error: String(e) }), 'fail');
                }
              }}
            />
          </div>
          <Button kind="secondary" className="h-9" onClick={() => void onPair()} disabled={!code.trim() || pairing}>
            {t('instances.pairButton')}
          </Button>
        </div>
        {pairErr && <div className="text-statusFail text-sm">{pairErr}</div>}
        {pairOk && <div className="text-statusOk text-sm">{pairOk}</div>}
      </Card>
    </div>
  );
}
