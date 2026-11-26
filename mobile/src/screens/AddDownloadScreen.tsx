import { useEffect, useState } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { ApiError, errorText } from '../api/client';
import {
  addLinksWithOptions,
  addOptionsSet,
  editRefusal,
  fetchCategories,
  fetchDownloadFolders,
  fetchPriorities,
  NO_ADD_OPTIONS,
  PRIORITY_KEYS,
  type AddOptions,
  type Category,
  type PriorityChoice,
} from '../api/taskEdit';
import type { Instance, ServerConnection } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { CardButton, GlimButton, GlimRow, GlimToggle, WellSelector } from '../components/glim';
import { Cross, Plus } from '../components/IconBadge';
import { EditBox, EditField, PickChips, categoryOptions } from '../components/TaskEdit';
import { InfoTip } from '../components/InfoTip';
import { Text, TextInput } from '../components/Text';
import { startWatch } from '../watch/watch';

export default function AddDownloadScreen({
  conn,
  peer,
  onDone,
}: {
  conn: ServerConnection;
  peer?: Instance;
  onDone: () => void;
}) {
  const { t } = useT();
  const { c, corners } = useAppearance();
  const base = peer ? `/api/instances/${encodeURIComponent(peer.name)}` : '/api';
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [options, setOptions] = useState<AddOptions>(NO_ADD_OPTIONS);

  const submit = async () => {
    const links = text
      .split('\n')
      .map((l) => l.trim())
      .filter(Boolean);
    if (links.length === 0) {
      setError(t('addDownload.errorEmpty'));
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await addLinksWithOptions(conn, links, options, base);
      void startWatch();
      onDone();
    } catch (err) {
      const message = editRefusal(t, err, options.dir.trim() !== '') ?? errorText(t, err);
      // An ApiError with no HTTP status is the app refusing before it asked.
      const answered = err instanceof ApiError && err.status > 0;
      setError(t(answered ? 'addDownload.errorServer' : 'addDownload.errorSend', { message }));
    } finally {
      setBusy(false);
    }
  };

  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      {/* How the field wants its links is an explanation, so it hangs off an
          (i) beside the title rather than standing under it as a paragraph
          (GlimStone rule 8). */}
      <View style={styles.titleRow}>
        <Text style={[styles.title, { color: c.text }]}>
          {peer ? t('addDownload.titlePeer', { name: peer.displayName ?? peer.name }) : t('addDownload.title')}
        </Text>
        <InfoTip text={t('addDownload.hint')} />
      </View>

      {/* Filled and borderless, as every field in the family is: a surface is
          told apart by its shade, never by a drawn line. */}
      <TextInput
        style={[styles.textArea, { backgroundColor: c.surface, color: c.text, ...corners.control }]}
        multiline
        placeholder={t('addDownload.placeholder')}
        placeholderTextColor={c.fieldPlaceholder}
        value={text}
        onChangeText={setText}
        autoCapitalize="none"
        autoCorrect={false}
        textAlignVertical="top"
      />

      <AddOptionsSection conn={conn} base={base} value={options} onChange={setOptions} />

      {error && <Text style={[styles.error, { color: c.statusFailSolid }]}>{error}</Text>}

      {/* The way back first and the one that goes ahead at the end, both the
          shared button with its glyph. Cancel is quiet: a page has one accent
          button, and it is the one that adds. */}
      <View style={styles.actions}>
        <GlimButton tone="quiet" grow label={t('addDownload.cancel')} icon={(ink) => <Cross color={ink} />} onPress={onDone} />
        <GlimButton
          hue={1}
          grow
          label={t('addDownload.button')}
          icon={(ink) => <Plus color={ink} />}
          busy={busy}
          onPress={submit}
        />
      </View>
    </View>
  );
}

/**
 * The batch's options, folded under the box as the web's add form keeps them
 * behind its Options badge. A box left empty leaves the choice to the
 * instance, and the lists it picks from are read each time it opens.
 */
export function AddOptionsSection({
  conn,
  base,
  value,
  onChange,
}: {
  conn: ServerConnection;
  base: string;
  value: AddOptions;
  onChange: (next: AddOptions) => void;
}) {
  const { t } = useT();
  const { c, accent, corners } = useAppearance();
  const [open, setOpen] = useState(false);
  const [folders, setFolders] = useState<string[]>([]);
  const [categories, setCategories] = useState<Category[]>([]);
  const [priorities, setPriorities] = useState<PriorityChoice[]>([]);

  useEffect(() => {
    if (!open) return;
    let alive = true;
    void fetchDownloadFolders(conn, base).then((f) => alive && setFolders(f));
    void fetchCategories(conn, base).then((list) => alive && setCategories(list));
    void fetchPriorities(conn, base).then((p) => alive && setPriorities(p));
    return () => {
      alive = false;
    };
  }, [open, conn, base]);

  const set = (patch: Partial<AddOptions>) => onChange({ ...value, ...patch });

  return (
    <View style={styles.options}>
      <CardButton
        style={[styles.optionsToggle, { backgroundColor: c.surface, ...corners.control }]}
        onPress={() => setOpen(!open)}
        accessibilityLabel={t('edit.options')}
      >
        <Text style={[styles.optionsTitle, { color: c.text }]}>{t('edit.options')}</Text>
        {/* The web's dot on its Options badge: something in there is set. */}
        {addOptionsSet(value) ? <View style={[styles.optionsSet, corners.pill, { backgroundColor: accent }]} /> : null}
        {/* The package list's fold, turned the same way. */}
        <Text style={[styles.chevron, { color: c.textSub }, open && styles.chevronOpen]}>›</Text>
      </CardButton>

      {open ? (
        <ScrollView style={styles.optionsBody} keyboardShouldPersistTaps="handled">
          <EditField label={t('edit.addPackage')}>
            <EditBox value={value.package} onChange={(p) => set({ package: p })} label={t('edit.addPackage')} />
          </EditField>

          <EditField label={t('edit.destination')} info={t('edit.folderHint')}>
            <EditBox value={value.dir} onChange={(dir) => set({ dir })} label={t('edit.destination')} />
            <PickChips values={folders} chosen={value.dir} onPick={(dir) => set({ dir })} />
          </EditField>

          {categories.length > 0 ? (
            <EditField label={t('edit.category')} info={t('edit.categoryHint')}>
              <WellSelector
                options={categoryOptions(categories, t('edit.categoryNone'))}
                value={value.category}
                onPick={(category) => set({ category })}
              />
            </EditField>
          ) : null}

          {priorities.length > 0 ? (
            <EditField label={t('edit.priority')} info={t('edit.priorityHint')}>
              <WellSelector
                options={priorities.map((p) => {
                  const key = PRIORITY_KEYS[p.id];
                  return { value: String(p.value), label: key ? t(key) : p.id };
                })}
                value={value.priority}
                onPick={(priority) => set({ priority: priority === value.priority ? '' : priority })}
              />
            </EditField>
          ) : null}

          <EditField label={t('edit.autoExtract')} info={t('edit.autoExtractHint')}>
            <WellSelector
              options={[
                { value: 'inherit' as const, label: t('edit.inherit') },
                { value: 'on' as const, label: t('edit.on') },
                { value: 'off' as const, label: t('edit.off') },
              ]}
              value={value.autoExtract}
              onPick={(autoExtract) => set({ autoExtract })}
            />
          </EditField>

          <EditField label={t('edit.comment')} info={t('edit.commentHint')}>
            <EditBox value={value.comment} onChange={(comment) => set({ comment })} label={t('edit.comment')} multiline />
          </EditField>

          <EditField label={t('edit.password')} info={t('edit.passwordHint')}>
            <EditBox value={value.password} onChange={(password) => set({ password })} label={t('edit.password')} />
          </EditField>
          <EditField label={t('edit.linkPassword')} info={t('edit.linkPasswordHint')}>
            <EditBox
              value={value.downloadPassword}
              onChange={(downloadPassword) => set({ downloadPassword })}
              label={t('edit.linkPassword')}
            />
          </EditField>

          <GlimRow
            label={t('edit.overrule')}
            info={t('edit.overruleHint')}
            control={<GlimToggle value={value.overrule} onChange={(overrule) => set({ overrule })} label={t('edit.overrule')} />}
          />
        </ScrollView>
      ) : null}
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens rather than
// baked in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  container: { flex: 1, padding: 24, paddingTop: 56 },
  titleRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 16 },
  title: { fontSize: TYPE.status, fontWeight: '600', flexShrink: 1 },
  textArea: {
    flex: 1,
    padding: 14,
    fontSize: TYPE.body,
  },
  error: { marginTop: 12, fontSize: TYPE.body },
  actions: { flexDirection: 'row', gap: 12, marginTop: 16 },
  options: { marginTop: 12 },
  optionsToggle: { flexDirection: 'row', alignItems: 'center', gap: 10, minHeight: 48, paddingHorizontal: 14 },
  optionsTitle: { flex: 1, fontSize: TYPE.body, fontWeight: '600' },
  optionsSet: { width: 8, height: 8 },
  chevron: { fontSize: 17, lineHeight: 20, width: 12, textAlign: 'center' },
  chevronOpen: { transform: [{ rotate: '90deg' }] },
  optionsBody: { maxHeight: 340, paddingHorizontal: 2 },
});
