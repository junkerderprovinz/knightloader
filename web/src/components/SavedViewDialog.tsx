// The windows for naming, renaming and deleting a saved view.
import { useState } from 'react';
import { useT } from '../lib/i18n';
import { Button, Field, Modal, TextInput } from './ui';
import { MAX_VIEW_NAME } from '../lib/savedViews';
import type { SavedView } from '../lib/listNarrowing';

/**
 * ViewNameDialog names a new view or renames one.
 */
export function ViewNameDialog({
  title,
  initial,
  taken,
  duplicates,
  refusal,
  onConfirm,
  onClose,
}: {
  title: string;
  initial: string;
  /** The lower-cased names of the other saved views. */
  taken: ReadonlySet<string>;
  /**
   * Saving over a taken name updates that view, and the button says so;
   * renaming onto one would leave two chips with the same label.
   */
  duplicates: 'overwrite' | 'reject';
  /** What the store said when the last attempt was turned down, if it was. */
  refusal?: string;
  onConfirm: (name: string) => void;
  onClose: () => void;
}) {
  const { t } = useT();
  const [name, setName] = useState(initial);
  const clean = name.trim();
  const collides = clean !== '' && taken.has(clean.toLowerCase());
  const overwrites = collides && duplicates === 'overwrite';
  const blocked = clean === '' || (collides && duplicates === 'reject');

  return (
    <Modal
      title={title}
      onClose={onClose}
      footer={
        <>
          {/* The forward button ends the row, so the refusal goes first. */}
          {refusal && <span className="min-w-0 text-sm text-statusFail">{refusal}</span>}
          <span className="flex-1" />
          <Button kind="ghost" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button disabled={blocked} onClick={() => onConfirm(clean)}>
            {overwrites ? t('views.overwriteConfirm') : t('views.saveConfirm')}
          </Button>
        </>
      }
    >
      <Field label={t('views.nameLabel')} hint={t('views.saveHint')}>
        <TextInput
          autoFocus
          value={name}
          maxLength={MAX_VIEW_NAME}
          placeholder={t('views.namePlaceholder')}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !blocked) onConfirm(clean);
          }}
        />
      </Field>
    </Modal>
  );
}

/**
 * ViewDeleteDialog confirms deleting a view. It cannot be muted, since the
 * delete is neither undoable nor frequent enough to be tedious.
 */
export function ViewDeleteDialog({
  view,
  onConfirm,
  onClose,
}: {
  view: SavedView;
  onConfirm: () => void;
  onClose: () => void;
}) {
  const { t } = useT();
  return (
    <Modal
      title={t('views.deleteTitle', { name: view.name })}
      onClose={onClose}
      footer={
        <>
          <span className="flex-1" />
          <Button kind="ghost" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          {/* Neutral rather than red; the body text carries the warning. */}
          <Button kind="secondary" onClick={onConfirm}>
            {t('views.deleteConfirm')}
          </Button>
        </>
      }
    >
      <p className="text-sm text-carbon-textSub">{t('views.deleteBody')}</p>
    </Modal>
  );
}
