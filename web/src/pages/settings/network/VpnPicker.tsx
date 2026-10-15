import { useRef, useState } from 'react';
import { BrandPicker, PickerSearch, brandMatches, type Brand } from '../../../components/BrandPicker';
import { Button, HintBubble, Modal, PasswordInput, TextArea, TextInput } from '../../../components/ui';
import { ApiError, connectVpn, type VpnField, type VpnProvider, type VpnState } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { IconBack, IconClose, IconFolderOpen } from '../../../lib/icons';
import { useToast } from '../../../lib/toast';
import { VPN_APP_ONLY } from '../../../lib/vpnProviders';
import { SettingRow } from '../controls';
import { refusalText } from '../tx';
import { VpnMark, failureOf, nameOf, vpnPaint } from './VpnMark';

const FIELDS: Record<VpnField, { label: TranslationKey; hint?: TranslationKey; secret?: boolean }> = {
  config: { label: 'settings.vpn.field.config', hint: 'settings.vpn.field.configHint' },
  privateKey: { label: 'settings.vpn.field.privateKey', hint: 'settings.vpn.field.privateKeyHint', secret: true },
  address: { label: 'settings.vpn.field.address', hint: 'settings.vpn.field.addressHint' },
  accountNumber: {
    label: 'settings.vpn.field.accountNumber',
    hint: 'settings.vpn.field.accountNumberHint',
    secret: true,
  },
  token: { label: 'settings.vpn.field.token', hint: 'settings.vpn.field.tokenHint', secret: true },
  username: { label: 'settings.vpn.field.username', hint: 'settings.vpn.field.usernameHint' },
  password: { label: 'settings.vpn.field.password', secret: true },
  location: { label: 'settings.vpn.field.location', hint: 'settings.vpn.field.locationHint' },
};

type Values = Partial<Record<VpnField, string>>;

/** What a provider's own configuration needs said before it is pasted. */
const NOTES: Partial<Record<string, TranslationKey>> = {
  kaspersky: 'settings.vpn.note.kaspersky',
  anonine: 'settings.vpn.note.anonine',
};

/**
 * VpnPicker is the window a VPN is added in, the way an account is: every
 * provider a tile, the form of the one picked in the same window. Opened on
 * a provider already set up, it is that provider's form alone. A stored login
 * is never sent back to the page, so its fields start empty and an empty field
 * keeps what is stored.
 */
export function VpnPicker({
  vpn,
  editing,
  hue,
  onClose,
  onState,
}: {
  vpn: VpnState;
  /** The provider to edit, instead of the list to add one from. */
  editing?: string;
  hue: number;
  onClose: () => void;
  onState: (next: VpnState) => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [picked, setPicked] = useState(editing ?? '');
  const [query, setQuery] = useState('');
  const [values, setValues] = useState<Values>({});
  const [missing, setMissing] = useState<VpnField | ''>('');
  const [busy, setBusy] = useState(false);
  const [shake, setShake] = useState(0);
  const p = vpn.providers.find((x) => x.id === picked);
  const current = vpn.providers.find((x) => x.id === vpn.active);

  const brands: Brand[] = vpn.providers.map((x) => {
    const name = nameOf(t, x);
    return {
      id: x.id,
      name,
      mark: <VpnMark id={x.id} name={name} size={25} lit />,
      paint: vpnPaint(x.id, name),
      sub: x.configured ? t('settings.vpn.state.saved') : undefined,
      held: x.configured,
    };
  });
  const appOnly: Brand[] = VPN_APP_ONLY.map((x) => ({
    id: x.id,
    name: x.name,
    mark: <VpnMark id={x.id} name={x.name} size={25} />,
    sub: t('settings.vpn.appOnlyTitle'),
    unavailable: t('settings.vpn.appOnlyHint'),
  }));
  const shown = brands.filter((b) => brandMatches(b, query));
  const shownAppOnly = appOnly.filter((b) => brandMatches(b, query));

  async function connect() {
    if (!p) return;
    setBusy(true);
    try {
      onState(await connectVpn(p.id, values));
      onClose();
    } catch (e) {
      const field = e instanceof ApiError ? e.field : undefined;
      if (field && p.fields.includes(field as VpnField)) setMissing(field as VpnField);
      else toast(refusalText(t, e), 'fail');
      setShake((n) => n + 1);
    } finally {
      setBusy(false);
    }
  }

  const set = (f: VpnField, v: string) => {
    setValues((prev) => ({ ...prev, [f]: v }));
    if (missing === f) setMissing('');
  };

  const name = p ? nameOf(t, p) : '';
  const filled = !!p && (p.configured || p.fields.every((f) => f === 'location' || values[f]?.trim()));

  return (
    <Modal
      title={p ? name : t('settings.vpn.pick')}
      hint={p ? undefined : t('settings.vpn.pickHint')}
      hue={hue}
      wide
      height={p ? 'content' : 'capped'}
      holdHeight={!p && query !== ''}
      right={!p && <PickerSearch value={query} onChange={setQuery} placeholder={t('search.placeholder')} />}
      onClose={onClose}
      footer={
        p ? (
          <>
            {/* A new VPN's form leads back to the list it was picked from; an
                edit has no list behind it and offers the way out. */}
            {editing ? (
              <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
            ) : (
              <Button
                kind="secondary"
                labelled
                icon={<IconBack className="rtl:-scale-x-100" />}
                title={t('onboarding.back')}
                onClick={() => {
                  setPicked('');
                  setQuery('');
                  setValues({});
                  setMissing('');
                }}
              />
            )}
            <span className="flex-1" />
            <Button disabled={busy || !filled} shake={shake} onClick={() => void connect()}>
              {t('settings.vpn.connect')}
            </Button>
          </>
        ) : (
          <>
            <span className="flex-1" />
            <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          </>
        )
      }
    >
      {!p ? (
        <div className="flex flex-col gap-4">
          <BrandPicker brands={shown} none={t('settings.vpn.noneFound')} onPick={setPicked} />
          {shownAppOnly.length > 0 && (
            <>
              <p className="text-meta text-carbon-textSub">{t('settings.vpn.appOnlyTitle')}</p>
              <BrandPicker brands={shownAppOnly} none="" onPick={() => {}} />
            </>
          )}
        </div>
      ) : (
        <ConnectForm
          provider={p}
          current={current}
          values={values}
          missing={missing}
          onValue={set}
        />
      )}
    </Modal>
  );
}

function ConnectForm({
  provider: p,
  current,
  values,
  missing,
  onValue,
}: {
  provider: VpnProvider;
  current?: VpnProvider;
  values: Values;
  missing: VpnField | '';
  onValue: (f: VpnField, v: string) => void;
}) {
  const { t } = useT();
  const name = nameOf(t, p);
  const note = NOTES[p.id];
  return (
    <div data-new="picker-form" className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <VpnMark id={p.id} name={name} size={36} />
        <p className="text-sm text-carbon-textSub">
          {t(p.portForwarding ? 'settings.vpn.portForwardingHint' : 'settings.vpn.noPortForwardingHint', { name })}
        </p>
      </div>
      {note && <p className="text-sm text-carbon-textSub">{t(note)}</p>}
      {p.state === 'failed' && (
        <p role="status" className="text-sm text-statusFail">
          {failureOf(t, p)}
        </p>
      )}
      {current && current.id !== p.id && (
        <p className="rounded-[var(--radius-card)] bg-statusWarnBgSoft px-3 py-2.5 text-sm text-carbon-text">
          {t('settings.vpn.replaces', { name, current: nameOf(t, current) })}
        </p>
      )}
      {p.configured && <p className="text-meta text-carbon-textSub">{t('settings.vpn.keepSaved')}</p>}
      {p.fields.map((f) => (
        <div key={f} className="flex flex-col gap-1.5">
          {f === 'config' ? (
            <ConfigField
              hint={t(p.id === 'custom' ? 'settings.vpn.field.configHintOwn' : 'settings.vpn.field.configHint', { name })}
              value={values.config ?? ''}
              onValue={(v) => onValue('config', v)}
            />
          ) : (
            <SettingRow
              control="long"
              label={t(FIELDS[f].label)}
              hint={FIELDS[f].hint ? t(FIELDS[f].hint, { name }) : undefined}
            >
              {FIELDS[f].secret ? (
                <PasswordInput
                  autoComplete="off"
                  value={values[f] ?? ''}
                  onChange={(v) => onValue(f, v)}
                  showLabel={t('common.showPassword')}
                  hideLabel={t('common.hidePassword')}
                />
              ) : (
                <TextInput
                  dir="ltr"
                  autoComplete="off"
                  spellCheck={false}
                  value={values[f] ?? ''}
                  onChange={(e) => onValue(f, e.target.value)}
                />
              )}
            </SettingRow>
          )}
          {missing === f && <p className="text-xs text-statusFail">{t('settings.vpn.field.missing')}</p>}
        </div>
      ))}
    </div>
  );
}

/** ConfigField takes a .conf file, picked or pasted. Its name stands above it, as for any text area. */
function ConfigField({ hint, value, onValue }: { hint: string; value: string; onValue: (v: string) => void }) {
  const { t } = useT();
  const file = useRef<HTMLInputElement>(null);
  const label = t('settings.vpn.field.config');
  return (
    <>
      <span className="flex flex-wrap items-center justify-between gap-2">
        <span data-glim-label={label} className="flex items-center gap-1.5 text-sm text-carbon-text">
          {label}
          <HintBubble hint={hint} />
        </span>
        <Button kind="secondary" icon={<IconFolderOpen width={16} height={16} />} onClick={() => file.current?.click()}>
          {t('settings.vpn.field.chooseFile')}
        </Button>
      </span>
      <TextArea
        dir="ltr"
        rows={6}
        spellCheck={false}
        aria-label={label}
        value={value}
        onChange={(e) => onValue(e.target.value)}
        className="font-mono text-meta"
      />
      <input
        ref={file}
        type="file"
        accept=".conf,text/plain"
        className="hidden"
        onChange={(e) => {
          const f = e.target.files?.[0];
          // Cleared so picking the same file again fires a change event.
          e.target.value = '';
          if (f) void f.text().then(onValue);
        }}
      />
    </>
  );
}
