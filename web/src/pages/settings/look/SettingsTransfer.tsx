import { useRef, useState } from 'react';
import { Button, Card, Modal, SectionTitle, ToggleRow } from '../../../components/ui';
import { IconDownloads, IconUpload } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import {
  ApiError,
  BACKUP_DOWNLOAD_URL,
  importSettings,
  settingsExportURL,
  uploadRestore,
  type SettingsExportDoc,
  type Settings,
} from '../../../lib/api';
import { useToast } from '../../../lib/toast';
import { diffRows, parseExport, type TransferRow } from '../../../lib/settingsTransfer';
import { fetchSettingsSchema } from '../features';
import { useDraft } from '../context';
import { SettingsImportPreview } from './SettingsImportPreview';

/**
 * Two ways out of this box and back into another one, on ONE card (jdp,
 * 2026-09-13: "Fuer was brauchen wir diese card und die sicherung card? das ist
 * redundant").
 *
 * They used to be two cards stacked on top of each other, and both of them said
 * "export" and "import" in the same words with the same two buttons, which is
 * precisely why they read as the same feature written twice. They are not the
 * same feature:
 *
 *   - the archive moves an INSTALL. Database, history, this box's own identity,
 *     the settings and their passwords, in one file, restored wholesale and in
 *     force at the next start.
 *   - "settings only" moves a CONFIGURATION. settings.json alone, offered key
 *     by key in a preview, and only what was ticked is written. It applies
 *     through app.PatchSettings, which runs every runtime effect a saved
 *     settings page already runs, so there is nothing to restart. That is this
 *     half's real advantage over its neighbour and it is worth saying on the
 *     card.
 *
 * Side by side in one card those two sentences are a comparison. In two cards
 * they were a repetition. Both mechanisms are untouched; what went is the card
 * frame that used to stand between them.
 *
 * TWO BUBBLES, AND NO MORE (jdp, same message: "zudem sind auf der card
 * uebertrieben viele infobubbles"). One on the title, for what is true of the
 * whole business: both files are written here, saved by the browser that asked
 * for them and go nowhere else, and NEITHER of them carries the debrid and
 * hoster logins, the header profiles, the captcha keys, the interface login
 * password or the user scripts. Those live sealed outside settings.json
 * (internal/accounts, internal/hosterauth, internal/apitoken, internal/auth,
 * internal/script) and backup.Build does not bundle any of them either.
 * Somebody moving boxes finds that out at three in the morning with an empty
 * queue unless the card says so first. One on the password switch, because that
 * is the single control here that writes a secret into a file in clear text.
 * What the four buttons do is written on the four buttons.
 *
 * The deployment fetch that used to gate the archive half lives one component
 * further up now: nothing on this card needs /api/deployment to have answered,
 * and a card that vanished because an unrelated request failed was the older
 * arrangement's accident rather than its intent. `onShutdown` is the one thing
 * that still crosses over - a restore the server answers with a restart is a
 * shutdown like any other, and the lifecycle card is where that is said.
 */
export function SettingsTransfer({ hue, onShutdown }: { hue: number; onShutdown: () => void }) {
  const { t } = useT();
  const { toast } = useToast();
  const draft = useDraft();

  // ---- the whole install: one archive, restored wholesale ----
  const archiveInput = useRef<HTMLInputElement>(null);
  const [pendingFile, setPendingFile] = useState<File | null>(null);
  const [restoring, setRestoring] = useState(false);
  const [restoreStatus, setRestoreStatus] = useState('');
  // Keyed onto the confirm button so a second identical refusal builds a fresh
  // DOM node and the shake replays instead of playing once ever.
  const [restoreShake, setRestoreShake] = useState(0);

  // ---- settings only: settings.json, merged key by key ----
  const fileInput = useRef<HTMLInputElement>(null);

  /**
   * OFF when the card opens, and that is the owner's decision rather than a
   * default somebody picked while typing (jdp, 2026-09-08).
   *
   * The failure it avoids cannot be taken back: the export lands in a sync
   * folder, a mail attachment or a chat, and the router password went with it.
   * The failure it accepts is a proxy that dials with no password after an
   * import, which is visible, named in the import's own answer and fixable in
   * one field. Do not flip this to true because "it matches the backup" - the
   * backup is a file people keep, this is a file people send.
   */
  const [withSecrets, setWithSecrets] = useState(false);

  const [pending, setPending] = useState<{ doc: SettingsExportDoc; rows: TransferRow[] } | null>(null);
  const [busy, setBusy] = useState(false);
  const [previewError, setPreviewError] = useState('');
  /** What the card says after the dialog has closed: how much was taken over,
   *  which passwords still have to be typed in, and whether any rule that came
   *  over will never fire. */
  const [notice, setNotice] = useState<string[]>([]);
  const [failure, setFailure] = useState('');

  async function confirmRestore() {
    if (!pendingFile) return;
    setRestoring(true);
    try {
      const res = await uploadRestore(pendingFile);
      setRestoreStatus(res.status);
      if (res.restarting) onShutdown();
      setPendingFile(null);
    } catch (e) {
      // The reason goes into the toast and nowhere else - a sentence left on
      // the page never clears itself, so an hour-old failure reads exactly as
      // current as a fresh one. The window stays OPEN on failure and the button
      // that was pressed shakes in it: closing here would spend the confirm
      // click the user already gave, for a failure that was not their mistake,
      // and would unmount the one element meant to be seen shaking.
      toast(t('settings.system.restoreFailed', { error: String(e).replace(/^Error:\s*/, '') }), 'fail');
      setRestoreShake((n) => n + 1);
    } finally {
      setRestoring(false);
    }
  }

  async function choose(file: File) {
    setNotice([]);
    setFailure('');
    setPreviewError('');
    try {
      const doc = parseExport(await file.text());
      // The type table comes from the server rather than from anything compiled
      // into this bundle, so a key the running build has and this page has never
      // heard of is still offered. It is also what decides which keys are
      // unknown, by exactly the rule the import route uses.
      const schema = await fetchSettingsSchema();
      const stored = draft.cfg as unknown as Record<string, unknown>;
      setPending({ doc, rows: diffRows(doc, stored, schema.kinds) });
    } catch (e) {
      setFailure(t('settings.transfer.parseFailed', { reason: reasonOf(e) }));
    }
  }

  async function apply(keys: string[]) {
    if (!pending) return;
    setBusy(true);
    setPreviewError('');
    try {
      const res = await importSettings(pending.doc, keys);
      // Folded into BOTH copies the settings shell holds, and this is the single
      // sharpest trap in the feature: the shell keeps a draft plus the `saved`
      // baseline it was seeded from, and its autosave sends the DIFFERENCE
      // between them. An import writing through its own route makes the server
      // diverge from both, so the next unrelated edit anywhere in the settings -
      // an accent colour, a speed limit - would send the pre-import copy of
      // every imported key straight back over it, silently, with a green toast.
      // patchNow already solves exactly this for its own writes; this is the
      // same fold without the request.
      draft.reseed(res.settings as Settings, res.applied);

      const lines = [t('settings.transfer.applied', { n: res.applied.length })];
      if (res.incomplete.length > 0) {
        lines.push(
          t('settings.transfer.incomplete', {
            fields: res.incomplete.map((code) => t(INCOMPLETE_LABEL[code] ?? 'settings.transfer.noPassword')).join(', '),
          }),
        );
      }
      if (res.ruleProblems > 0) lines.push(t('settings.transfer.ruleProblems', { n: res.ruleProblems }));
      setNotice(lines);
      setPending(null);
    } catch (e) {
      // The server's own words, always. Every refusal it produces already names
      // which check failed, and wrapping that in "invalid file" is how somebody
      // re-uploads the same broken document - the argument routes_backup.go
      // already makes for the archive. The one refusal with a typed half is the
      // version guard, because "upgrade the server first" is an instruction and
      // an instruction has to be readable in the reader's own language.
      if (e instanceof ApiError && e.code === 'transfer.tooNew') {
        setPreviewError(
          t('settings.transfer.tooNew', {
            version: String(e.params?.version ?? ''),
            running: String(e.params?.running ?? ''),
          }),
        );
      } else {
        setPreviewError(t('settings.transfer.applyFailed', { error: reasonOf(e) }));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.transfer.cardHint')}>{t('settings.transfer.cardTitle')}</SectionTitle>

      {/* The whole install. Deliberately NOT wrapped in a well of its own: two
          identical sunken boxes one above the other is the shape this merge
          exists to get rid of, and the difference between the halves is meant
          to be read in the two sentences rather than seen in two frames. */}
      <div className="flex flex-col gap-2">
        <span className="text-sm font-medium text-carbon-text">{t('settings.transfer.archiveLabel')}</span>
        <p className="text-xs leading-relaxed text-carbon-textSub">{t('settings.transfer.archiveText')}</p>
        <input
          ref={archiveInput}
          type="file"
          accept="application/zip,.zip"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0];
            // Cleared straight away, or picking the same file twice in a row
            // raises no change event and a second restore attempt after a failed
            // one silently does nothing - the same reason the settings input
            // below and Rules.tsx's import input both do this.
            e.target.value = '';
            if (f) setPendingFile(f);
          }}
        />
        <div className="flex flex-wrap items-center gap-3">
          <Button
            hue={hue}
            kind="secondary"
            icon={<IconDownloads width={16} height={16} />}
            onClick={() => {
              window.location.href = BACKUP_DOWNLOAD_URL;
            }}
          >
            {t('settings.system.backupButton')}
          </Button>
          {/* The one that brings something in ends the row (GlimStone 1.14.0),
              and it carries no status colour: replacing an install is the
              weightiest thing on this card, which is a reason for the confirm
              window it opens rather than for a red button. */}
          <Button
            hue={hue}
            kind="secondary"
            icon={<IconUpload width={16} height={16} />}
            onClick={() => archiveInput.current?.click()}
            disabled={restoring}
          >
            {t('settings.system.restoreButton')}
          </Button>
        </div>
        {restoreStatus && (
          <span className="text-sm text-statusOk">{t('settings.system.restoreStaged', { status: restoreStatus })}</span>
        )}
      </div>

      {/* Settings only, under it and on the same card. The hairline is the whole
          of the separation: enough to say "a second thing", not enough to make
          it a second card again. */}
      <div className="flex flex-col gap-2 border-t border-carbon-border/60 pt-4">
        <span className="text-sm font-medium text-carbon-text">{t('settings.transfer.settingsLabel')}</span>
        <p className="text-xs leading-relaxed text-carbon-textSub">{t('settings.transfer.settingsText')}</p>

        {/* The second and last bubble on this card, on the one control that
            writes a secret into a file in clear text. No hue of its own: the
            switch inherits the card's position, the way a lone switch with
            nothing beside it to distinguish should. */}
        <ToggleRow
          label={t('settings.transfer.withSecrets')}
          hint={t('settings.transfer.withSecretsHint')}
          checked={withSecrets}
          onChange={setWithSecrets}
        />

        <input
          ref={fileInput}
          type="file"
          accept="application/json,.json"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0];
            e.target.value = '';
            if (f) void choose(f);
          }}
        />

        <div className="flex flex-wrap items-center gap-3">
          <Button
            hue={hue}
            kind="secondary"
            icon={<IconDownloads width={16} height={16} />}
            onClick={() => {
              // Opened rather than fetched, exactly as the backup download
              // above is: the browser owns the save dialog, and a Blob built
              // here would put the whole document through this client for no
              // gain.
              window.location.href = settingsExportURL(withSecrets);
            }}
          >
            {t('settings.transfer.export')}
          </Button>
          <Button
            hue={hue}
            kind="secondary"
            icon={<IconUpload width={16} height={16} />}
            onClick={() => fileInput.current?.click()}
            disabled={busy}
          >
            {t('settings.transfer.import')}
          </Button>
        </div>

        {failure && <span className="text-sm text-statusFail">{failure}</span>}
        {notice.map((line, i) => (
          <span key={i} className={i === 0 ? 'text-sm text-statusOk' : 'text-sm text-statusWarn'}>
            {line}
          </span>
        ))}
      </div>

      {pending && (
        <SettingsImportPreview
          doc={pending.doc}
          rows={pending.rows}
          busy={busy}
          error={previewError}
          onApply={(keys) => void apply(keys)}
          onClose={() => {
            if (busy) return;
            setPending(null);
            setPreviewError('');
          }}
        />
      )}

      {pendingFile && (
        <Modal
          title={t('settings.system.restoreConfirmTitle')}
          onClose={() => (restoring ? undefined : setPendingFile(null))}
          footer={
            <>
              <span className="flex-1" />
              <Button kind="ghost" onClick={() => setPendingFile(null)} disabled={restoring}>
                {t('settings.system.confirmCancel')}
              </Button>
              <Button
                key={restoreShake}
                className={restoreShake > 0 ? 'glim-shake' : ''}
                kind="ghost"
                onClick={() => void confirmRestore()}
                disabled={restoring}
              >
                {restoring ? t('settings.system.restoring') : t('settings.system.confirmProceed')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">{t('settings.system.restoreConfirmBody', { name: pendingFile.name })}</p>
        </Modal>
      )}
    </Card>
  );
}

/**
 * The three codes the server answers in, mapped to the words for them.
 *
 * Codes rather than sentences on the wire, because the server has no idea which
 * of forty-two languages the reader is looking at and the same message also goes
 * to its log - the identical argument writeValidationError makes for the
 * reconnect refusals. A code with no entry here falls back to the generic
 * "arrives without its password", which is untranslated in no language and
 * still true.
 */
const INCOMPLETE_LABEL: Record<string, 'settings.transfer.incompleteReconnect' | 'settings.transfer.incompleteConnections' | 'settings.transfer.incompleteArchives'> = {
  'reconnect.password': 'settings.transfer.incompleteReconnect',
  'connections.password': 'settings.transfer.incompleteConnections',
  archivePasswords: 'settings.transfer.incompleteArchives',
};

/** The sentence inside a failure, without the class name a stringified Error
 *  drags in front of it. */
function reasonOf(e: unknown): string {
  return String(e).replace(/^(Error|ApiError|SyntaxError):\s*/, '');
}
