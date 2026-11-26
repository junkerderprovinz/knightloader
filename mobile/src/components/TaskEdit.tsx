import { useEffect, useState, type ReactNode } from 'react';
import {
  Modal,
  ScrollView,
  StyleSheet,
  TouchableOpacity,
  View,
  useWindowDimensions,
  type KeyboardTypeOptions,
} from 'react-native';
import { errorText } from '../api/client';
import type { ServerConnection } from '../api/types';
import {
  editRefusal,
  fetchCategories,
  fetchDownloadFolders,
  movePackage,
  planEdit,
  renameNote,
  renamePackage,
  setCategory,
  setTaskOptions,
  startOf,
  tasksOf,
  type Category,
  type EditDraft,
  type EditTarget,
} from '../api/taskEdit';
import { useT } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { TYPE } from '../theme/tokens';
import { GlimButton, NotchCard, WellSelector } from './glim';
import { Check, Cross } from './IconBadge';
import { InfoTip } from './InfoTip';
import { NoArrival } from './Moving';
import { Text, TextInput } from './Text';

/** One box of a form: its name, an (i) for what it does, and the control. */
export function EditField({ label, info, children }: { label: string; info?: string; children: ReactNode }) {
  const { c } = useAppearance();
  return (
    <View style={styles.field}>
      <View style={styles.labelRow}>
        <Text style={[styles.label, { color: c.textMuted }]}>{label}</Text>
        {info ? <InfoTip text={info} /> : null}
      </View>
      {children}
    </View>
  );
}

/** A text box on the card's own field ground. */
export function EditBox({
  value,
  onChange,
  label,
  placeholder,
  keyboard,
  multiline,
}: {
  value: string;
  onChange: (v: string) => void;
  /** What a screen reader calls the box, which has its name above it. */
  label: string;
  placeholder?: string;
  keyboard?: KeyboardTypeOptions;
  multiline?: boolean;
}) {
  const { c, corners } = useAppearance();
  return (
    <TextInput
      style={[styles.input, { backgroundColor: c.surface2, color: c.text, ...corners.control }]}
      value={value}
      onChangeText={onChange}
      accessibilityLabel={label}
      placeholder={placeholder}
      placeholderTextColor={c.fieldPlaceholder}
      keyboardType={keyboard}
      multiline={multiline}
      autoCapitalize="none"
      autoCorrect={false}
    />
  );
}

/**
 * The values a box can take in one tap, such as the download folders under the
 * folder box. The one the box holds stands a step up, so the eye finds it.
 */
export function PickChips({ values, chosen, onPick }: { values: string[]; chosen: string; onPick: (v: string) => void }) {
  const { c, corners } = useAppearance();
  if (values.length === 0) return null;
  return (
    <View style={styles.chips}>
      {values.map((v) => (
        <TouchableOpacity
          key={v}
          onPress={() => onPick(v)}
          accessibilityRole="button"
          accessibilityState={{ selected: v === chosen }}
          style={[styles.chip, corners.pill, { backgroundColor: v === chosen ? c.surface3 : c.surface2 }]}
        >
          <Text style={[styles.chipText, { color: c.text }]} numberOfLines={1}>
            {v}
          </Text>
        </TouchableOpacity>
      ))}
    </View>
  );
}

/** categoryOptions is a category selector's segments, None first. */
export function categoryOptions(categories: Category[], none: string): { value: string; label: string }[] {
  return [{ value: '', label: none }, ...categories.map((x) => ({ value: x.id, label: x.name || x.id }))];
}

/**
 * The properties window of a link or a package, the phone's side of the web's
 * properties panel and its rename windows. A package's boxes act on every
 * link of it.
 *
 * Only what changed is sent, so a box a package's links disagree on opens
 * empty and leaves each link its own value. A press beside the window does not
 * close it, since that would throw away what was typed.
 */
export function TaskEdit({
  conn,
  base,
  target,
  packages,
  onClose,
  onSaved,
}: {
  conn: ServerConnection;
  base: string;
  target: EditTarget | null;
  /** Every package of the list, which a link can be moved into. */
  packages: string[];
  onClose: () => void;
  /** After anything was saved, so the list can pull it at once. */
  onSaved: () => void;
}) {
  const { t } = useT();
  const { c } = useAppearance();
  const { motion } = useMotion();
  // The boxes scroll between the name above them and the buttons below, which
  // stay in view with the keyboard up.
  const { height } = useWindowDimensions();
  const [start, setStart] = useState<EditDraft | null>(null);
  const [draft, setDraft] = useState<EditDraft | null>(null);
  const [folders, setFolders] = useState<string[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    const s = target ? startOf(target) : null;
    setStart(s);
    setDraft(s);
    setError('');
    if (!target) return;
    let alive = true;
    void fetchDownloadFolders(conn, base).then((f) => alive && setFolders(f));
    void fetchCategories(conn, base).then((list) => alive && setCategories(list));
    return () => {
      alive = false;
    };
  }, [target, conn, base]);

  const set = (patch: Partial<EditDraft>) => setDraft((d) => (d ? { ...d, ...patch } : d));

  const save = async () => {
    if (!target || !start || !draft) return;
    const plan = planEdit(target, start, draft);
    if ('fault' in plan) {
      setError(t(plan.fault));
      return;
    }
    const ids = tasksOf(target).map((x) => x.id);
    setBusy(true);
    setError('');
    // What has gone through so far, so a save that stops halfway and is
    // pressed again sends only the rest.
    let now = start;
    let step = '';
    try {
      step = 'package';
      if (plan.renamePackage !== undefined) await renamePackage(conn, ids, plan.renamePackage, base);
      if (plan.move !== undefined) await movePackage(conn, ids, plan.move, base);
      now = { ...now, name: target.kind === 'package' ? draft.name : now.name, pkg: draft.pkg };
      step = 'options';
      if (Object.keys(plan.options).length > 0) await setTaskOptions(conn, ids, plan.options, base);
      now = { ...now, name: draft.name, dir: draft.dir, comment: draft.comment, chunks: draft.chunks };
      setDraft((d) => (d ? { ...d, password: '', downloadPassword: '' } : d));
      step = 'category';
      if (plan.category !== undefined) await setCategory(conn, ids, plan.category, base);
    } catch (e) {
      setStart(now);
      setError(editRefusal(t, e, step === 'options' && plan.options.dir !== undefined) ?? errorText(t, e));
      setBusy(false);
      onSaved();
      return;
    }
    setBusy(false);
    onSaved();
    onClose();
  };

  if (!target || !start || !draft) return null;
  const link = target.kind === 'link' ? target.task : null;
  const note = link ? renameNote(link) : null;
  const mixed = (k: EditDraft['mixed'][number]) => (start.mixed.includes(k) ? t('edit.mixed') : undefined);
  const hidden = t('edit.passwordHidden');
  const others = packages.filter((p) => p !== '' && p !== (link?.package ?? '')).slice(0, 12);

  return (
    <Modal visible transparent animationType={motion === 'off' ? 'none' : 'fade'} onRequestClose={onClose}>
      <NoArrival>
        <View style={[styles.scrim, { backgroundColor: c.scrim }]}>
          <View style={styles.window} accessibilityViewIsModal>
            <NotchCard title={t('edit.title')} style={styles.flush}>
              <Text style={[styles.subject, { color: c.text }]} numberOfLines={2}>
                {target.kind === 'link' ? target.task.name || target.task.url : target.name || t('packages.loose')}
              </Text>
              <ScrollView style={[styles.body, { maxHeight: height * 0.55 }]} keyboardShouldPersistTaps="handled">
                {link ? (
                  <EditField label={t('edit.name')} info={t('edit.nameHint')}>
                    {note?.blocked ? null : (
                      <EditBox value={draft.name} onChange={(name) => set({ name })} label={t('edit.name')} />
                    )}
                    {note ? <Text style={[styles.note, { color: c.textMuted }]}>{t(note.key)}</Text> : null}
                  </EditField>
                ) : (
                  <EditField label={t('edit.packageName')} info={t('edit.packageNameHint')}>
                    <EditBox value={draft.name} onChange={(name) => set({ name })} label={t('edit.packageName')} />
                  </EditField>
                )}

                {link ? (
                  <EditField label={t('edit.package')} info={t('edit.packageHint')}>
                    <EditBox value={draft.pkg} onChange={(pkg) => set({ pkg })} label={t('edit.package')} />
                    <PickChips values={others} chosen={draft.pkg} onPick={(pkg) => set({ pkg })} />
                  </EditField>
                ) : null}

                {categories.length > 0 ? (
                  <EditField label={t('edit.category')}>
                    <WellSelector
                      options={categoryOptions(categories, t('edit.categoryNone'))}
                      value={draft.category}
                      onPick={(category) => set({ category })}
                    />
                  </EditField>
                ) : null}

                <EditField label={t('edit.folder')} info={t('edit.folderHint')}>
                  <EditBox value={draft.dir} onChange={(dir) => set({ dir })} label={t('edit.folder')} placeholder={mixed('dir')} />
                  <PickChips values={folders} chosen={draft.dir} onPick={(dir) => set({ dir })} />
                </EditField>

                <EditField label={t('edit.password')} info={hidden}>
                  <EditBox value={draft.password} onChange={(password) => set({ password })} label={t('edit.password')} />
                </EditField>
                <EditField label={t('edit.linkPassword')} info={`${t('edit.linkPasswordHint')} ${hidden}`}>
                  <EditBox
                    value={draft.downloadPassword}
                    onChange={(downloadPassword) => set({ downloadPassword })}
                    label={t('edit.linkPassword')}
                  />
                </EditField>

                <EditField label={t('edit.comment')} info={t('edit.commentHint')}>
                  <EditBox
                    value={draft.comment}
                    onChange={(comment) => set({ comment })}
                    label={t('edit.comment')}
                    placeholder={mixed('comment')}
                    multiline
                  />
                </EditField>

                <EditField label={t('edit.chunks')} info={t('edit.chunksHint')}>
                  <EditBox
                    value={draft.chunks}
                    onChange={(chunks) => set({ chunks })}
                    label={t('edit.chunks')}
                    placeholder={mixed('chunks')}
                    keyboard="number-pad"
                  />
                </EditField>
              </ScrollView>

              {error ? <Text style={[styles.error, { color: c.statusFailSolid }]}>{error}</Text> : null}
              <View style={styles.actions}>
                <GlimButton tone="quiet" grow label={t('edit.cancel')} icon={(ink) => <Cross color={ink} />} onPress={onClose} />
                <GlimButton grow label={t('edit.save')} icon={(ink) => <Check color={ink} />} busy={busy} onPress={save} />
              </View>
            </NotchCard>
          </View>
        </View>
      </NoArrival>
    </Modal>
  );
}

const styles = StyleSheet.create({
  scrim: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: 16 },
  window: { width: '100%', maxWidth: 480 },
  flush: { marginTop: 0 },
  subject: { fontSize: TYPE.body, fontWeight: '600', marginTop: 4 },
  body: { marginTop: 4 },
  field: { marginTop: 14, gap: 6 },
  labelRow: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  label: { fontSize: TYPE.subline, flexShrink: 1 },
  input: { paddingHorizontal: 14, paddingVertical: 10, fontSize: TYPE.body },
  note: { fontSize: TYPE.subline },
  chips: { flexDirection: 'row', flexWrap: 'wrap', gap: 6 },
  chip: { paddingVertical: 6, paddingHorizontal: 10, maxWidth: '100%' },
  chipText: { fontSize: TYPE.meta },
  error: { marginTop: 12, fontSize: TYPE.subline },
  actions: { flexDirection: 'row', gap: 8, marginTop: 16 },
});
