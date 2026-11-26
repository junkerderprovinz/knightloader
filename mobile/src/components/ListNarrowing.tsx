import { useState } from 'react';
import { Pressable, StyleSheet, TouchableOpacity, View } from 'react-native';
import {
  facetOptions,
  isNarrowed,
  offeredFilters,
  toggled,
  NOTHING_NARROWED,
  type FacetKind,
  type FileType,
  type Listed,
  type Narrowing,
  type QuickFilterId,
} from '../api/narrowing';
import type { TranslationKey } from '../i18n/en';
import { useT } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { NUM, TYPE } from '../theme/tokens';
import { contrastOn, valid } from '../theme/appearance';
import IconBadge, { Cross, Filter, Search } from './IconBadge';
import { InfoTip } from './InfoTip';
import { Text, TextInput } from './Text';

const TYPE_WORDS: Record<FileType, TranslationKey> = {
  archive: 'collector.facets.type.archive',
  video: 'collector.facets.type.video',
  audio: 'collector.facets.type.audio',
  image: 'collector.facets.type.image',
  document: 'collector.facets.type.document',
  other: 'collector.facets.type.other',
};

const FACET_TITLES: Record<FacetKind, TranslationKey> = {
  host: 'search.host',
  fileType: 'collector.facets.fileType',
  package: 'search.package',
};

/**
 * ListNarrowing is the search and the filters over one list, the web's search
 * field, the quick filters under a list's Status head and, in the collector,
 * the host, file type and package facets. The field is always there, and the
 * filter badge opens the chips below it, filled while any of them is on. Each
 * chip carries how many rows it matches. Everything runs on the rows the
 * screen already has.
 */
export function ListNarrowing({
  tasks,
  shown,
  narrowing,
  onChange,
  filters,
  facets = [],
}: {
  /** Every row of the list, before anything narrows it. */
  tasks: Listed[];
  /** How many rows are left. */
  shown: number;
  narrowing: Narrowing;
  onChange: (n: Narrowing) => void;
  filters: QuickFilterId[];
  facets?: FacetKind[];
}) {
  const { t } = useT();
  const { c, corners, accentContrast, accentInk } = useAppearance();
  const [open, setOpen] = useState(false);
  const chipsOn = narrowing.filters.size > 0 || facets.some((k) => narrowing.facets[k].size > 0);
  const offered = offeredFilters(filters, tasks, narrowing.filters);

  const facetWord = (kind: FacetKind, value: string) => {
    if (kind === 'fileType') return t(TYPE_WORDS[value as FileType] ?? 'collector.facets.type.other');
    if (kind === 'host') return value || t('collector.facets.unknownHost');
    return value || t('packages.loose');
  };

  return (
    <View style={styles.wrap}>
      <View style={styles.bar}>
        <View style={[styles.field, { backgroundColor: c.surface, ...corners.control }]}>
          <Search color={c.textMuted} />
          <TextInput
            style={[styles.input, { color: c.text }]}
            value={narrowing.search}
            onChangeText={(search) => onChange({ ...narrowing, search })}
            placeholder={t('search.placeholder')}
            placeholderTextColor={c.fieldPlaceholder}
            autoCapitalize="none"
            autoCorrect={false}
            returnKeyType="search"
            accessibilityLabel={t('search.placeholder')}
          />
          {narrowing.search !== '' ? (
            <Pressable
              onPress={() => onChange({ ...narrowing, search: '' })}
              accessibilityRole="button"
              accessibilityLabel={t('search.clear')}
              hitSlop={8}
            >
              <Cross color={c.textMuted} />
            </Pressable>
          ) : (
            <InfoTip text={t('search.syntax')} />
          )}
        </View>
        <IconBadge
          icon={<Filter color={chipsOn ? accentContrast : accentInk} />}
          accent={chipsOn}
          onPress={() => setOpen((o) => !o)}
          accessibilityLabel={t('filter.label')}
        />
      </View>

      {open && (
        <View style={[styles.panel, { backgroundColor: c.surface, ...corners.card }]}>
          <View style={styles.panelHead}>
            <Text style={[styles.label, { color: c.textMuted }]}>{t('filter.label')}</Text>
            <View style={styles.grow} />
            {chipsOn && (
              <Pressable
                onPress={() => onChange({ ...NOTHING_NARROWED, search: narrowing.search })}
                accessibilityRole="button"
                hitSlop={8}
              >
                <Text style={[styles.label, { color: c.textSub }]}>{t('filter.clear')}</Text>
              </Pressable>
            )}
          </View>
          <View style={styles.chips}>
            {offered.map(({ id, n }, i) => (
              <Chip
                key={id}
                label={t(`filter.${id}` as TranslationKey)}
                n={n}
                on={narrowing.filters.has(id)}
                hue={i}
                onPress={() => onChange({ ...narrowing, filters: toggled(narrowing.filters, id) })}
              />
            ))}
          </View>
          {facets.map((kind) => (
            <View key={kind} style={styles.facet}>
              <Text style={[styles.label, { color: c.textMuted }]}>{t(FACET_TITLES[kind])}</Text>
              <View style={styles.chips}>
                {facetOptions(tasks, kind, narrowing.facets[kind]).map(({ value, n }, i) => (
                  <Chip
                    key={value}
                    label={facetWord(kind, value)}
                    n={n}
                    on={narrowing.facets[kind].has(value)}
                    hue={i}
                    onPress={() =>
                      onChange({
                        ...narrowing,
                        facets: { ...narrowing.facets, [kind]: toggled(narrowing.facets[kind], value) },
                      })
                    }
                  />
                ))}
              </View>
            </View>
          ))}
        </View>
      )}

      {/* How much is left, and the way back to everything, only while
          something narrows the list. */}
      {isNarrowed(narrowing) && (
        <View style={styles.shownLine}>
          <Text style={[styles.shown, NUM, { color: c.textMuted }]}>
            {t('search.shown', { n: shown, total: tasks.length })}
          </Text>
          <Pressable onPress={() => onChange(NOTHING_NARROWED)} accessibilityRole="button" hitSlop={8}>
            <Text style={[styles.shown, { color: c.textSub }]}>{t('filter.clear')}</Text>
          </Pressable>
        </View>
      )}
    </View>
  );
}

/**
 * One filter, the web's many-select tab: the name and its count, filled
 * while it is on, in its place's colour under the rainbow as a well's chosen
 * segment is.
 */
function Chip({ label, n, on, hue, onPress }: { label: string; n: number; on: boolean; hue: number; onPress: () => void }) {
  const { c, accent, accentContrast, corners, hueAt } = useAppearance();
  const fill = hueAt(hue) ?? accent;
  const ink = on ? (valid(fill) ? contrastOn(fill) : accentContrast) : c.textSub;
  return (
    <TouchableOpacity
      onPress={onPress}
      accessibilityRole="checkbox"
      accessibilityState={{ checked: on }}
      accessibilityLabel={`${label}, ${n}`}
      style={[styles.chip, corners.pill, { backgroundColor: on ? fill : c.surface2 }]}
    >
      <Text style={[styles.chipText, { color: ink }]} numberOfLines={1}>
        {label}
      </Text>
      <Text style={[styles.chipText, NUM, { color: ink, opacity: 0.75 }]}>{n}</Text>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: 8, marginBottom: 10 },
  bar: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  field: { flex: 1, minWidth: 0, height: 40, flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 12 },
  input: { flex: 1, minWidth: 0, fontSize: TYPE.body, paddingVertical: 0 },
  panel: { padding: 12, gap: 10 },
  panelHead: { flexDirection: 'row', alignItems: 'center' },
  grow: { flex: 1 },
  label: { fontSize: TYPE.subline },
  chips: { flexDirection: 'row', flexWrap: 'wrap', gap: 6 },
  facet: { gap: 6 },
  chip: { flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 12, height: 32, maxWidth: '100%' },
  chipText: { fontSize: TYPE.meta, flexShrink: 1 },
  shownLine: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 12 },
  shown: { fontSize: TYPE.meta },
});
