import { useRef, useState } from 'react';
import { Button, HintBubble, Modal, PasswordInput, TextArea, TextInput } from '../../../components/ui';
import { ApiError, connectVpn, forgetVpn, type VpnField, type VpnProvider, type VpnState } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { IconClose, IconFolderOpen, IconTrash } from '../../../lib/icons';
import { useToast } from '../../../lib/toast';
import { SettingRow } from '../controls';
import { refusalText } from '../tx';
import { VpnBadges, VpnMark, failureOf, nameOf } from './VpnMark';

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

/**
 * ConnectDialog asks for what one provider needs and connects it. A stored
 * login is never sent back to the page, so its fields start empty and an empty
 * field keeps what is stored. Connecting a second provider ends the first,
 * which the window says before it happens.
 */
export function ConnectDialog({
  provider: p,
  current,
  hue,
  onClose,
  onState,
}: {
  provider: VpnProvider;
  /** The provider torrents go through now, if any. */
  current?: VpnProvider;
  hue: number;
  onClose: () => void;
  onState: (next: VpnState) => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [values, setValues] = useState<Values>({});
  const [missing, setMissing] = useState<VpnField | ''>('');
  const [busy, setBusy] = useState(false);
  const [shake, setShake] = useState(0);
  const name = nameOf(t, p);
  const filled = p.configured || p.fields.every((f) => f === 'location' || values[f]?.trim());

  async function run(call: () => Promise<VpnState>) {
    setBusy(true);
    try {
      onState(await call());
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

  return (
    <Modal
      title={name}
      hue={hue}
      onClose={onClose}
      footer={
        <>
          <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          <span className="flex-1" />
          {p.configured && (
            <Button
              kind="ghost"
              icon={<IconTrash width={16} height={16} />}
              hint={t('settings.vpn.removeHint', { name })}
              disabled={busy}
              onClick={() => void run(() => forgetVpn(p.id))}
            >
              {t('settings.vpn.remove')}
            </Button>
          )}
          <Button disabled={busy || !filled} shake={shake} onClick={() => void run(() => connectVpn(p.id, values))}>
            {t('settings.vpn.connect')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          <VpnMark id={p.id} name={name} size={36} />
          <VpnBadges provider={p} t={t} />
        </div>
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
                onValue={(v) => set('config', v)}
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
                    onChange={(v) => set(f, v)}
                    showLabel={t('common.showPassword')}
                    hideLabel={t('common.hidePassword')}
                  />
                ) : (
                  <TextInput
                    dir="ltr"
                    autoComplete="off"
                    spellCheck={false}
                    value={values[f] ?? ''}
                    onChange={(e) => set(f, e.target.value)}
                  />
                )}
              </SettingRow>
            )}
            {missing === f && <p className="text-xs text-statusFail">{t('settings.vpn.field.missing')}</p>}
          </div>
        ))}
      </div>
    </Modal>
  );
}

/** ConfigField takes a .conf file, picked or pasted. Its name stands above it, as for any text area. */
function ConfigField({ hint, value, onValue }: { hint: string; value: string; onValue: (v: string) => void }) {
  const { t } = useT();
  const file = useRef<HTMLInputElement>(null);
  const label = t('settings.vpn.field.config');
  return (
    <>
      <span data-glim-label={label} className="flex items-center gap-1.5 text-sm text-carbon-text">
        {label}
        <HintBubble hint={hint} />
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
      <div>
        <Button kind="secondary" icon={<IconFolderOpen width={16} height={16} />} onClick={() => file.current?.click()}>
          {t('settings.vpn.field.chooseFile')}
        </Button>
      </div>
    </>
  );
}
