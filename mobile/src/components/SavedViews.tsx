import { useState, type ReactNode } from 'react';
import { Modal, Pressable, ScrollView, StyleSheet, TouchableOpacity, View } from 'react-native';
import { getRandomBytes } from 'expo-crypto';
import { isNarrowed, type Narrowing, type QuickFilterId } from '../api/narrowing';
import {
  MAX_VIEWS,
  MAX_VIEW_NAME,
  fromStored,
  matching,
  removeView,
  renameView,
  saveView,
  taken,
  toStored,
  type SavedView,
} from '../api/savedViews';
import { useT } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { TYPE } from '../theme/tokens';
import { contrastOn, valid } from '../theme/appearance';
import { ConfirmDialog } from './ConfirmDialog';
import { GlimButton, NotchCard } from './glim';
import IconBadge, { Cross, Pencil, Pin, Save, Trash } from './IconBadge';
import { InfoTip } from './InfoTip';
import { NoArrival } from './Moving';
import { Text, TextInput } from './Text';

type Open =
  | { kind: 'save' }
  | { kind: 'manage' }
  | { kind: 'rename'; view: SavedView }
  | { kind: 'delete'; view: SavedView };

const newId = () => Array.from(getRandomBytes(16), (b) => b.toString(16).padStart(2, '0')).join('');

/**
 * SavedViews is the web's saved-view chips under a list's search: a press
 * applies a view and a press on the lit one clears it, the pin saves what is
 * set right now under a name, and the pencil renames or deletes views. The
 * views are the web's own, so a view saved on either side shows on both.
 */
export function SavedViews({
  views,
  narrowing,
  allowed,
  keepFacets,
  onApply,
  onViews,
}: {
  views: SavedView[];
  narrowing: Narrowing;
  allowed: readonly QuickFilterId[];
  keepFacets: boolean;
  onApply: (n: Narrowing) => void;
  /** Stores the list's views; a rejection says why it did not take. */
  onViews: (next: SavedView[]) => Promise<void>;
}) {
  const { t } = useT();
  const { c, accent, accentContrast, accentInk, corners, hueAt } = useAppearance();
  const { motion } = useMotion();
  const [open, setOpen] = useState<Open | null>(null);
  const [name, setName] = useState('');
  const [refusal, setRefusal] = useState('');
  const [busy, setBusy] = useState(false);

  const lit = matching(views, narrowing, allowed, keepFacets);
  const anything = isNarrowed(narrowing);
  if (views.length === 0 && !anything) return null;

  const show = (next: Open | null, initial = '') => {
    setRefusal('');
    setName(initial);
    setOpen(next);
  };

  const store = async (next: SavedView[]) => {
    setBusy(true);
    setRefusal('');
    try {
      await onViews(next);
      return true;
    } catch (e) {
      setRefusal(e instanceof Error ? e.message : String(e));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const confirmSave = async () => {
    const r = saveView(views, name, toStored(narrowing, allowed), newId);
    if ('refused' in r) {
      // The window stays open and says why.
      setRefusal(r.refused === 'full' ? t('views.full', { n: MAX_VIEWS }) : t('views.tooBig', { filter: t('filter.label') }));
      return;
    }
    if (await store(r.views)) show(null);
  };

  const confirmRename = async (view: SavedView) => {
    if (await store(renameView(views, view.id, name))) show({ kind: 'manage' });
  };

  const confirmDelete = async (view: SavedView) => {
    if (await store(removeView(views, view.id))) show(views.length > 1 ? { kind: 'manage' } : null);
  };

  // The windows share the confirmation's frame: a card over the scrim, the way
  // out first and the step forward at the end of the row.
  const frame = (title: string, body: ReactNode) => (
    <Modal visible transparent animationType={motion === 'off' ? 'none' : 'fade'} onRequestClose={() => show(null)}>
      <NoArrival>
        <Pressable style={[styles.scrim, { backgroundColor: c.scrim }]} onPress={() => show(null)}>
          <Pressable style={styles.window} onPress={() => {}} accessibilityViewIsModal>
            <NotchCard title={title} style={styles.flush}>
              {body}
            </NotchCard>
          </Pressable>
        </Pressable>
      </NoArrival>
    </Modal>
  );

  const nameWindow = (title: string, duplicates: 'overwrite' | 'reject', onConfirm: () => void, except?: string) => {
    const clean = name.trim();
    const collides = taken(views, name, except);
    const blocked = clean === '' || (collides && duplicates === 'reject');
    return frame(
      title,
      <>
        <View style={styles.labelLine}>
          <Text style={[styles.label, { color: c.textSub }]}>{t('views.nameLabel')}</Text>
          <InfoTip text={t('views.saveHint')} />
        </View>
        <TextInput
          style={[styles.field, { backgroundColor: c.surface2, color: c.text, ...corners.control }]}
          value={name}
          onChangeText={setName}
          maxLength={MAX_VIEW_NAME}
          placeholder={t('views.namePlaceholder')}
          placeholderTextColor={c.fieldPlaceholder}
          accessibilityLabel={t('views.nameLabel')}
          autoFocus
          returnKeyType="done"
          onSubmitEditing={() => !blocked && onConfirm()}
        />
        {refusal !== '' && <Text style={[styles.refusal, { color: c.statusFailSolid }]}>{refusal}</Text>}
        <View style={styles.actions}>
          <GlimButton tone="quiet" grow label={t('settings.cancel')} icon={(ink) => <Cross color={ink} />} onPress={() => show(null)} />
          <GlimButton
            grow
            label={collides && duplicates === 'overwrite' ? t('views.overwriteConfirm') : t('views.saveConfirm')}
            icon={(ink) => <Save color={ink} />}
            disabled={blocked}
            busy={busy}
            onPress={onConfirm}
          />
        </View>
      </>,
    );
  };

  return (
    <View style={styles.row}>
      {views.map((v, i) => {
        const on = lit?.id === v.id;
        const fill = hueAt(i) ?? accent;
        const ink = on ? (valid(fill) ? contrastOn(fill) : accentContrast) : c.textSub;
        return (
          <TouchableOpacity
            key={v.id}
            onPress={() => onApply(on ? fromStored(null, allowed, keepFacets) : fromStored(v.state, allowed, keepFacets))}
            accessibilityRole="checkbox"
            accessibilityState={{ checked: on }}
            accessibilityLabel={`${t('views.label')}: ${v.name}`}
            style={[styles.chip, corners.pill, { backgroundColor: on ? fill : c.surface2 }]}
          >
            <Text style={[styles.chipText, { color: ink }]} numberOfLines={1}>
              {v.name}
            </Text>
          </TouchableOpacity>
        );
      })}
      {anything && (
        <IconBadge icon={<Pin color={accentInk} />} onPress={() => show({ kind: 'save' })} accessibilityLabel={t('views.save')} />
      )}
      {views.length > 0 && (
        <IconBadge icon={<Pencil color={accentInk} />} onPress={() => show({ kind: 'manage' })} accessibilityLabel={t('views.manage')} />
      )}

      {open?.kind === 'save' && nameWindow(t('views.saveTitle'), 'overwrite', () => void confirmSave())}

      {open?.kind === 'manage' &&
        frame(
          t('views.manage'),
          <>
            <ScrollView style={styles.list} nestedScrollEnabled>
              {views.map((v) => (
                <View key={v.id} style={styles.manageRow}>
                  <Text style={[styles.manageName, { color: c.text }]} numberOfLines={1}>
                    {v.name}
                  </Text>
                  <IconBadge
                    icon={<Pencil color={c.textSub} />}
                    onPress={() => show({ kind: 'rename', view: v }, v.name)}
                    accessibilityLabel={`${t('views.rename')}: ${v.name}`}
                  />
                  <IconBadge
                    icon={<Trash color={c.textSub} />}
                    onPress={() => show({ kind: 'delete', view: v })}
                    accessibilityLabel={`${t('views.delete')}: ${v.name}`}
                  />
                </View>
              ))}
            </ScrollView>
            {refusal !== '' && <Text style={[styles.refusal, { color: c.statusFailSolid }]}>{refusal}</Text>}
            <View style={styles.actions}>
              <GlimButton tone="quiet" grow label={t('common.close')} icon={(ink) => <Cross color={ink} />} onPress={() => show(null)} />
            </View>
          </>,
        )}

      {open?.kind === 'rename' &&
        nameWindow(t('views.renameTitle'), 'reject', () => void confirmRename(open.view), open.view.id)}

      {open?.kind === 'delete' && (
        <ConfirmDialog
          visible
          title={t('views.deleteTitle', { name: open.view.name })}
          message={refusal || t('views.deleteBody')}
          cancelLabel={t('settings.cancel')}
          confirmLabel={t('views.deleteConfirm')}
          confirmIcon={(ink) => <Trash color={ink} />}
          onCancel={() => show({ kind: 'manage' })}
          onConfirm={() => void confirmDelete(open.view)}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  row: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: 6 },
  chip: { paddingHorizontal: 12, height: 32, justifyContent: 'center', maxWidth: 224 },
  chipText: { fontSize: TYPE.meta },
  // No backgroundColor here: it is `c.scrim`, applied at render time, so it
  // follows the theme.
  scrim: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24 },
  window: { width: '100%', maxWidth: 420 },
  flush: { marginTop: 0 },
  labelLine: { flexDirection: 'row', alignItems: 'center', gap: 6, marginTop: 4 },
  label: { fontSize: TYPE.subline },
  field: { height: 40, paddingHorizontal: 12, fontSize: TYPE.body, marginTop: 8 },
  refusal: { marginTop: 10, fontSize: TYPE.subline },
  actions: { flexDirection: 'row', gap: 8, marginTop: 20 },
  list: { maxHeight: 320, marginTop: 4 },
  manageRow: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingVertical: 4 },
  manageName: { flex: 1, minWidth: 0, fontSize: TYPE.body },
});
