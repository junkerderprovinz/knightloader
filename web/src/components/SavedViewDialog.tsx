// The three windows a saved view needs: name it, rename it, delete it.
//
// They live here rather than inside SavedViewChips.tsx so that the chip strip
// stays a row of controls instead of a file that is two thirds modal markup,
// the same split ListToolbar.tsx already makes between its own row and
// ConfirmRemove/TaskOptionsDialog.
//
// Naming and renaming are ONE window, not two. They ask the identical question
// of the identical box, and a second copy of it is a second place for the
// overwrite rule and the length limit to drift.
import { useState } from 'react';
import { useT } from '../lib/i18n';
import { Button, Field, Modal, TextInput } from './ui';
import { MAX_VIEW_NAME } from '../lib/savedViews';
import type { SavedView } from '../lib/listNarrowing';

/**
 * ViewNameDialog is the box a view's name is typed into.
 *
 * `taken` is the lower-cased names of the OTHER saved views, and what happens
 * when the typed name is one of them depends on which question is being asked.
 * Saving reuses a name on purpose, because that is how somebody updates a view
 * they have just adjusted, so it overwrites and the primary button says so
 * first. Renaming cannot: there is nothing to fold the old view into, so a
 * name already in use would simply make two chips with the same label and no
 * way to tell them apart. Either way the answer is on the button rather than in
 * an error that appears after the click.
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
  taken: ReadonlySet<string>;
  /** What a name that is already in use means here. */
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
          <Button disabled={blocked} onClick={() => onConfirm(clean)}>
            {overwrites ? t('views.overwriteConfirm') : t('views.saveConfirm')}
          </Button>
          <Button kind="ghost" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <span className="flex-1" />
          {refusal && <span className="text-sm text-statusFail">{refusal}</span>}
        </>
      }
    >
      {/* What a view is and, just as important, what it is not, lives behind
          the (i) on the caption rather than as a paragraph over the box: it is
          worth reading once and costs the window height for ever after. */}
      <Field label={t('views.nameLabel')} hint={t('views.saveHint')}>
        <TextInput
          autoFocus
          value={name}
          maxLength={MAX_VIEW_NAME}
          placeholder={t('views.namePlaceholder')}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            // Enter is what a one-field window is for, and it obeys the same
            // guard the button does rather than being a second way past it.
            // Escape is already the Modal's own, so it is not handled twice.
            if (e.key === 'Enter' && !blocked) onConfirm(clean);
          }}
        />
      </Field>
    </Modal>
  );
}

/**
 * ViewDeleteDialog confirms throwing a name away.
 *
 * No `mute` id: lib/dialogmute.ts's own rule is that a confirmation becomes
 * silenceable when the action behind it is reversible or merely tedious, and
 * deleting a view is neither frequent enough to be tedious nor undoable.
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
          <Button kind="danger" onClick={onConfirm}>
            {t('views.deleteConfirm')}
          </Button>
          <span className="flex-1" />
          <Button kind="ghost" onClick={onClose}>
            {t('common.cancel')}
          </Button>
        </>
      }
    >
      {/* The one sentence somebody needs before answering: this deletes a name,
          not any links. It is the window's whole content, so it is the window's
          own text rather than something hidden behind an (i) that would then be
          the only thing in here to read. */}
      <p className="text-sm text-carbon-textSub">{t('views.deleteBody')}</p>
    </Modal>
  );
}
