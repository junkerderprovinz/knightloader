import { useRef, useState } from 'react';
import { Button, Card, InfoBubble, SectionTitle, ToggleRow } from '../../../components/ui';
import { IconDownloads, IconUpload } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import { ApiError, importSettings, settingsExportURL, type SettingsExportDoc, type Settings } from '../../../lib/api';
import { diffRows, parseExport, type TransferRow } from '../../../lib/settingsTransfer';
import { fetchSettingsSchema } from '../features';
import { useDraft } from '../context';
import { SettingsImportPreview } from './SettingsImportPreview';

/**
 * Nur die Einstellungen: settings.json in one file, and a merging import back.
 *
 * It sits directly beside Backup & Restore because the two answer neighbouring
 * questions and somebody will otherwise use the wrong one. The archive moves an
 * INSTALL - database, history, this box's own identity - and applies at the next
 * start-up, wholesale. This moves a CONFIGURATION, takes only what was ticked,
 * and applies live: settings apply through app.PatchSettings, which runs every
 * runtime effect a saved settings page already runs, so there is nothing to
 * restart. That is this feature's real advantage over its sibling and it is
 * worth saying on the card.
 *
 * What neither of them carries is the thing worth saying loudest, which is why
 * settings.transfer.notTravelling has a bubble of its own: the debrid and hoster
 * logins, the header profiles, the captcha keys, the interface login password
 * and the user scripts all live sealed OUTSIDE settings.json (internal/accounts,
 * internal/hosterauth, internal/apitoken, internal/auth, internal/script), and
 * backup.Build does not bundle any of them either. Somebody moving boxes finds
 * that out at three in the morning with an empty queue unless the card says so
 * first.
 */
export function SettingsTransfer({ hue }: { hue: number }) {
  const { t } = useT();
  const draft = useDraft();
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
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle hint={t('settings.transfer.hint')}>{t('settings.transfer.title')}</SectionTitle>

      <ToggleRow
        hue={0}
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
          // Cleared straight away, or picking the same file twice in a row
          // raises no change event and a second attempt after a failed one
          // silently does nothing - the same reason the backup card's own input
          // and Rules.tsx's import input both do this.
          e.target.value = '';
          if (f) void choose(f);
        }}
      />

      {/* One bubble per button rather than one sentence in the title badge:
          these are two different actions, the badge sits over on the left, and
          Rules.tsx already argues the same case at its own import/export pair. */}
      <div className="flex flex-wrap items-center gap-2">
        <Button
          hue={hue}
          kind="secondary"
          icon={<IconDownloads width={16} height={16} />}
          onClick={() => {
            // Opened rather than fetched, exactly as the backup download is: the
            // browser owns the save dialog, and a Blob built here would put the
            // whole document through this client for no gain.
            window.location.href = settingsExportURL(withSecrets);
          }}
        >
          {t('settings.transfer.export')}
        </Button>
        <InfoBubble tip={t('settings.transfer.exportHint')} />
        <Button
          hue={hue}
          kind="secondary"
          icon={<IconUpload width={16} height={16} />}
          onClick={() => fileInput.current?.click()}
          disabled={busy}
        >
          {t('settings.transfer.import')}
        </Button>
        <InfoBubble tip={t('settings.transfer.importHint')} />
      </div>

      {/* The quiet second bubble: what is in neither file. It has no button of
          its own because it is not an action, and it is not loose prose on the
          card because every explanation in this app lives behind an (i). */}
      <span className="flex items-center gap-1.5 text-[11px] text-carbon-textMuted">
        {t('settings.transfer.notTravellingLabel')}
        <InfoBubble tip={t('settings.transfer.notTravelling')} />
      </span>

      {failure && <span className="text-sm text-statusFail">{failure}</span>}
      {notice.map((line, i) => (
        <span key={i} className={i === 0 ? 'text-sm text-statusOk' : 'text-sm text-statusWarn'}>
          {line}
        </span>
      ))}

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
