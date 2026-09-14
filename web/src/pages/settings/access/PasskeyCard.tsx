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
import { IconEdit, IconKey, IconPlus, IconTrash } from '../../../lib/icons';
import { useToast } from '../../../lib/toast';

/**
 * Passkeys: signing in with the key on a phone, a laptop or a security stick
 * instead of typing the password.
 *
 * THE CARD'S REAL JOB IS THE EXPLANATION, not the button. WebAuthn binds a
 * credential to a domain name, so a browser refuses the whole exchange on a
 * bare IP address and again on a certificate it does not trust - and
 * KnightLoader's ordinary installation is exactly that, reached at
 * http://[LAN IP]:8749. Most people opening this card cannot use the feature at
 * all, and the honest answer is no control plus the paragraph saying what is
 * wrong, rather than a button that hands them "NotAllowedError".
 *
 * THE PARAGRAPH IS THIS APP'S OWN COPY, TRANSLATED. The server does send a
 * sentence - it is a diagnostic, in English, for an API caller and the log -
 * and this card must never render it: it is not an error in a toast, it is the
 * text that explains a whole feature to somebody whose interface is running in
 * their language. Only the boolean crosses over. web/check-passkey-reason.mjs
 * fails the build if that ever stops being true.
 *
 * Two more rules the interface carries:
 *
 *   - the password stays. A passkey is an additional way IN, never the only
 *     one, so the removal dialog names the way that remains rather than only
 *     what it destroys;
 *   - a key belongs to ONE address. Register through a proxy and the key does
 *     not exist over the LAN IP, so every key is listed and the ones that
 *     cannot answer here say which address they belong to. Hiding them would
 *     make a key somebody deliberately created look lost, which is the most
 *     alarming thing this surface can imply.
 */
export function PasskeyCard({
  hue,
  /** Whether a password is set at all. Without one there is no login for a
   *  passkey to be a second way into, and the server refuses to register one. */
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
      // Non-fatal, and it lands on the same answer an unreachable server means
      // for this card: the feature is not available here.
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
      // The browser's own refusal lands here too - a cancelled prompt, a
      // timeout, an authenticator that declined - and its message is the useful
      // one, so it is not replaced with a sentence of ours.
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

        {/* THE refusal. No control and the paragraph that says what is wrong -
            the third case beside "dim it" and "leave it out", and the only one
            that owes prose. On a stock install this is what the card always
            shows, because the stock install is reached on an IP address. */}
        {passwordSet && status !== null && !addressOK && (
          <UnavailableNotice
            title={t('auth.passkey.unavailableTitle')}
            reason={t('auth.passkey.unavailableReason')}
          />
        )}

        {/* The browser's own limit is a second refusal of the same kind, so it
            gets the same treatment rather than a bare coloured line beside it:
            one shape, one answer. */}
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
                  {/* Two independent facts about one key, separated in the
                      MARKUP rather than inside either sentence. Whether the
                      second one appears is decided by the authenticator, so a
                      full stop on the first would dangle on every row where it
                      never comes - which is why neither of these keys ends in
                      one. */}
                  <div className="text-[11px] text-carbon-textMuted">
                    {p.usableHere
                      ? t('auth.passkey.usableHere')
                      : t('auth.passkey.otherAddress').replace('{host}', p.rpId)}
                    {!p.backedUp && <> · {t('auth.passkey.notSynced')}</>}
                    {/* The store keeps whole seconds, fmtDate takes what
                        `new Date()` takes - so the conversion happens once,
                        here, rather than the column being widened to
                        milliseconds for one caller's convenience. */}
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

        {/* THE SAME WORD AS THE SECOND FACTOR'S BUTTON, and the plus is what
            tells them apart (GlimStone 2.1.0). This one said "Add a passkey"
            while the card above said "Enable"; two verbs for one act - opening a
            guided sequence that ends with a capability armed - and a reader
            meeting both in this tab had to work out whether the difference meant
            anything. The word is `auth.twoFactor.enable`'s, per language, and
            web/check-enrolment-buttons.mjs holds the two keys to one value in
            all 42 catalogues so they cannot drift apart again.

            The glyph is the plus and not a key: a key is what the ROWS above
            wear, one per registered credential, and this button is the act of
            adding one to that list. */}
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
              <Button kind="ghost" disabled={busy} onClick={() => setRenaming(null)}>
                {t('common.cancel')}
              </Button>
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
              <Button kind="ghost" disabled={busy} onClick={() => setPendingRemove(null)}>
                {t('common.cancel')}
              </Button>
              <Button kind="primary" disabled={busy} onClick={() => void remove(pendingRemove)}>
                {t('auth.passkey.remove')}
              </Button>
            </>
          }
        >
          {/* Names the way that REMAINS, not only what it takes away. A
              confirmation that states only the destruction makes somebody stop
              and work out whether anything is left - and here the answer is
              always yes, because the password never goes anywhere. */}
          <p className="text-sm text-carbon-textSub">
            {t('auth.passkey.removeBody').replace('{name}', pendingRemove.name)}
          </p>
        </Modal>
      )}
    </>
  );
}
