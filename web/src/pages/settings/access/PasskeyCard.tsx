import { useCallback, useEffect, useState } from 'react';
import {
  Button,
  Card,
  Field,
  IconBadge,
  IconTile,
  Modal,
  SectionTitle,
  TextInput,
  UnavailableNotice,
} from '../../../components/ui';
import {
  fetchPasskeys,
  passkeysInBrowser,
  registerPasskey,
  removePasskey,
  renamePasskey,
  type PasskeyStatus,
  type PasskeyView,
} from '../../../lib/api';
import { fmtDate } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import { IconClose, IconEdit, IconKey, IconPlus, IconTrash } from '../../../lib/icons';
import { useToast } from '../../../lib/toast';

/**
 * PasskeyCard manages signing in with a passkey instead of the password.
 * WebAuthn needs a domain name and a trusted certificate, which a stock install
 * reached by LAN IP lacks, so the card often shows only the translated reason.
 * The server's English sentence is never shown (web/check-passkey-reason.mjs).
 * A key belongs to one address, so keys registered elsewhere are listed with
 * the address they answer on.
 */
export function PasskeyCard({
  hue,
  /** Whether a password is set; without one the server refuses to register a passkey. */
  passwordSet,
}: {
  hue: number;
  passwordSet: boolean;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [status, setStatus] = useState<PasskeyStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState('');
  const [renaming, setRenaming] = useState<PasskeyView | null>(null);
  const [renameTo, setRenameTo] = useState('');
  const [pendingRemove, setPendingRemove] = useState<PasskeyView | null>(null);
  const [shake, setShake] = useState(0);

  const reload = useCallback(async () => {
    try {
      setStatus(await fetchPasskeys());
    } catch {
      // Treated like an unreachable server: the feature is unavailable here.
      setStatus(null);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  function fail(e: unknown) {
    toast(String(e).replace(/^Error:\s*/, ''), 'fail');
    setShake((n) => n + 1);
  }

  const browserOK = passkeysInBrowser();
  const addressOK = status?.supported === true;
  const keys = status?.passkeys ?? [];

  async function add() {
    setBusy(true);
    try {
      await registerPasskey(name.trim() || t('auth.passkey.defaultName'));
      setName('');
      setAdding(false);
      await reload();
      toast(t('auth.passkey.added'), 'ok');
    } catch (e) {
      // The browser's own refusal lands here too, and its message is the useful one.
      fail(e);
    } finally {
      setBusy(false);
    }
  }

  async function saveName() {
    if (!renaming) return;
    setBusy(true);
    try {
      await renamePasskey(renaming.id, renameTo.trim());
      setRenaming(null);
      await reload();
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  }

  async function remove(p: PasskeyView) {
    setBusy(true);
    try {
      await removePasskey(p.id);
      setPendingRemove(null);
      await reload();
      toast(t('auth.passkey.removed'), 'ok');
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <Card hue={hue} className="flex flex-col gap-5">
        <SectionTitle hint={t('auth.passkey.hint')}>{t('auth.passkey.title')}</SectionTitle>

        <div className="flex items-center gap-2">
          <span
            className={`inline-block h-2 w-2 rounded-[var(--radius-pill)] ${
              keys.length > 0 ? 'bg-statusOkSolid' : 'bg-carbon-textMuted'
            }`}
          />
          <span className="text-sm text-carbon-text">
            {keys.length > 0
              ? t('auth.passkey.registered').replace('{n}', String(keys.length))
              : t('auth.passkey.none')}
          </span>
        </div>

        {!passwordSet && <p className="text-sm text-carbon-textSub">{t('auth.passkey.needsPassword')}</p>}

        {/* What a stock install reached by IP address shows. */}
        {passwordSet && status !== null && !addressOK && (
          <UnavailableNotice
            title={t('auth.passkey.unavailableTitle')}
            reason={t('auth.passkey.unavailableReason')}
          />
        )}

        {passwordSet && addressOK && !browserOK && (
          <UnavailableNotice
            title={t('auth.passkey.noBrowserTitle')}
            reason={t('auth.passkey.noBrowserReason')}
          />
        )}

        {/* Every key, including the ones bound to another address. */}
        {keys.length > 0 && (
          <div className="flex flex-col divide-y divide-carbon-border/40">
            {keys.map((p) => (
              <div key={p.id} className="flex items-center gap-3 py-2.5 first:pt-0 last:pb-0">
                <IconTile icon={<IconKey width={16} height={16} />} hue={hue} />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm text-carbon-text">{p.name}</div>
                  {/* The two facts are joined in markup, and neither string
                      ends in a full stop, since the second one is optional. */}
                  <div className="text-[11px] text-carbon-textMuted">
                    {p.usableHere
                      ? t('auth.passkey.usableHere')
                      : t('auth.passkey.otherAddress').replace('{host}', p.rpId)}
                    {!p.backedUp && <> · {t('auth.passkey.notSynced')}</>}
                    {/* The store keeps whole seconds. */}
                    {p.lastUsedAt > 0 && (
                      <>
                        {' · '}
                        {t('auth.passkey.lastUsed').replace(
                          '{when}',
                          fmtDate(new Date(p.lastUsedAt * 1000).toISOString()),
                        )}
                      </>
                    )}
                  </div>
                </div>
                <IconBadge
                  labelled
                  hue={hue}
                  icon={<IconEdit width={16} height={16} />}
                  disabled={busy}
                  title={t('auth.passkey.rename')}
                  aria-label={t('auth.passkey.rename')}
                  onClick={() => {
                    setRenaming(p);
                    setRenameTo(p.name);
                  }}
                  className="shrink-0"
                />
                <IconBadge
                  labelled
                  hue={hue}
                  icon={<IconTrash width={16} height={16} />}
                  disabled={busy}
                  title={t('auth.passkey.remove')}
                  aria-label={t('auth.passkey.remove')}
                  onClick={() => setPendingRemove(p)}
                  className="shrink-0"
                />
              </div>
            ))}
          </div>
        )}

        {/* The same word as the second factor's button, told apart by the
            plus; web/check-enrolment-buttons.mjs keeps the two keys equal. */}
        {passwordSet && addressOK && browserOK && !adding && (
          <div>
            <Button
              key={shake}
              className={shake > 0 ? 'glim-shake' : ''}
              kind="secondary"
              hue={hue}
              icon={<IconPlus width={16} height={16} />}
              disabled={busy}
              onClick={() => setAdding(true)}
            >
              {t('auth.passkey.add')}
            </Button>
          </div>
        )}

        {adding && (
          <div className="flex flex-col gap-3">
            <Field label={t('auth.passkey.nameLabel')} hint={t('auth.passkey.nameHint')}>
              <TextInput
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t('auth.passkey.namePlaceholder')}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !busy) void add();
                }}
              />
            </Field>
            <div className="flex items-center gap-3">
              <span className="flex-1" />
              <Button
                kind="ghost"
                disabled={busy}
                onClick={() => {
                  setAdding(false);
                  setName('');
                }}
              >
                {t('common.cancel')}
              </Button>
              <Button
                key={shake}
                className={shake > 0 ? 'glim-shake' : ''}
                kind="primary"
                disabled={busy}
                onClick={() => void add()}
              >
                {t('auth.passkey.create')}
              </Button>
            </div>
          </div>
        )}
      </Card>

      {renaming && (
        <Modal
          title={t('auth.passkey.rename')}
          onClose={() => (busy ? undefined : setRenaming(null))}
          footer={
            <>
              <span className="flex-1" />
              <Button
                kind="ghost"
                labelled
                icon={<IconClose />}
                title={t('common.cancel')}
                disabled={busy}
                onClick={() => setRenaming(null)}
              />
              <Button
                kind="primary"
                disabled={busy || renameTo.trim() === ''}
                onClick={() => void saveName()}
              >
                {t('auth.passkey.saveName')}
              </Button>
            </>
          }
        >
          <Field label={t('auth.passkey.nameLabel')}>
            <TextInput
              autoFocus
              value={renameTo}
              onChange={(e) => setRenameTo(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && renameTo.trim() !== '' && !busy) void saveName();
              }}
            />
          </Field>
        </Modal>
      )}

      {pendingRemove && (
        <Modal
          title={t('auth.passkey.removeTitle')}
          onClose={() => (busy ? undefined : setPendingRemove(null))}
          footer={
            <>
              <span className="flex-1" />
              <Button
                kind="ghost"
                labelled
                icon={<IconClose />}
                title={t('common.cancel')}
                disabled={busy}
                onClick={() => setPendingRemove(null)}
              />
              <Button kind="primary" disabled={busy} onClick={() => void remove(pendingRemove)}>
                {t('auth.passkey.remove')}
              </Button>
            </>
          }
        >
          {/* Names the password as the way in that remains. */}
          <p className="text-sm text-carbon-textSub">
            {t('auth.passkey.removeBody').replace('{name}', pendingRemove.name)}
          </p>
        </Modal>
      )}
    </>
  );
}
