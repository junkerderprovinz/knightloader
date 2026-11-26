import { useState } from 'react';
import { ActivityIndicator, ScrollView, StyleSheet, View } from 'react-native';
import { getDocumentAsync } from 'expo-document-picker';
import { File } from 'expo-file-system';
import {
  MAX_FILE_BYTES,
  commitTorrent,
  outcomeText,
  sendFile,
  tooBig,
  type Outcome,
  type TorrentTree,
} from '../api/fileIntake';
import { fmtBytes } from '../api/stats';
import type { ServerConnection } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { NUM, TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { GlimButton, GlimToggle } from './glim';
import { Check, Cross, Folder, Plus } from './IconBadge';
import { InfoTip } from './InfoTip';
import { Text } from './Text';

/** A torrent of several files waiting for its selection; selected runs like tree.files. */
interface Pending {
  file: string;
  tree: TorrentTree;
  selected: boolean[];
}

/**
 * The Add links screen's way in for a file: a .torrent, a link container or an
 * .nzb picked on the phone goes where the web UI's file drop sends it, and a
 * torrent of several files shows them for a choice first. `pkg` names the
 * package the links land in.
 */
export function AddFileSection({
  conn,
  base,
  pkg = '',
  onAdded,
}: {
  conn: ServerConnection;
  base: string;
  pkg?: string;
  /** Called once something reached the instance. */
  onAdded?: () => void;
}) {
  const { t } = useT();
  const { c, corners } = useAppearance();
  const [busy, setBusy] = useState(false);
  const [results, setResults] = useState<Outcome[]>([]);
  const [pending, setPending] = useState<Pending | null>(null);

  const report = (out: Outcome[]) => {
    setResults(out);
    if (out.some((o) => o.kind !== 'failed')) onAdded?.();
  };

  const choose = async () => {
    if (busy || pending) return;
    // Any type: Android knows no type for a .dlc, a .ccf or an .rsdf, and a
    // file manager hands a .torrent over as whatever it guessed.
    const picked = await getDocumentAsync({ type: '*/*', multiple: true, copyToCacheDirectory: true });
    if (picked.canceled || !picked.assets?.length) return;
    setBusy(true);
    setResults([]);
    const out: Outcome[] = [];
    let review: Pending | null = null;
    for (const asset of picked.assets) {
      const copy = new File(asset.uri);
      if (review) {
        out.push({ file: asset.name, kind: 'failed', reason: { key: 'addFile.onlyOne' } });
      } else {
        const r = await sendPicked(asset.name, asset.size, copy);
        if ('tree' in r) review = { file: asset.name, tree: r.tree, selected: r.tree.files.map((f) => f.selected) };
        else out.push(r);
      }
      try {
        copy.delete();
      } catch {
        // The picker's copy sits in the cache, which Android clears anyway.
      }
    }
    setPending(review);
    report(out);
    setBusy(false);
  };

  const sendPicked = async (name: string, size: number | undefined, copy: File): Promise<Outcome | { tree: TorrentTree }> => {
    const big = tooBig(size);
    if (big) return { file: name, kind: 'failed', reason: big };
    let data: string;
    try {
      data = await copy.base64();
    } catch {
      return { file: name, kind: 'failed', reason: { key: 'addFile.unreadable' } };
    }
    // The picker does not always know the size beforehand.
    const late = tooBig(Math.floor((data.length * 3) / 4));
    if (late) return { file: name, kind: 'failed', reason: late };
    return sendFile(conn, base, name, data, pkg);
  };

  const confirm = async () => {
    if (!pending) return;
    setBusy(true);
    const out = await commitTorrent(conn, base, pending.file, pending.tree, pending.selected, pkg);
    setPending(null);
    report([...results, out]);
    setBusy(false);
  };

  const flip = (i: number) => {
    if (!pending) return;
    const selected = [...pending.selected];
    selected[i] = !selected[i];
    setPending({ ...pending, selected });
  };

  const tone = { ok: c.statusOkText, muted: c.textMuted, warn: c.statusWarnText, fail: c.statusFailSolid };
  const count = pending ? pending.selected.filter(Boolean).length : 0;
  const size = pending ? pending.tree.files.reduce((sum, f, i) => (pending.selected[i] ? sum + f.size : sum), 0) : 0;

  return (
    <View style={styles.box}>
      <View style={styles.chooseRow}>
        <GlimButton
          tone="quiet"
          label={t('addFile.choose')}
          icon={(ink) => <Folder color={ink} />}
          disabled={!!pending}
          busy={busy && !pending}
          onPress={() => void choose()}
        />
        <InfoTip text={t('addFile.hint', { max: fmtBytes(MAX_FILE_BYTES) })} />
      </View>

      {busy && !pending && <Text style={[styles.line, { color: c.textMuted }]}>{t('addFile.uploading')}</Text>}

      {pending && (
        <View style={[styles.review, { backgroundColor: c.surface, ...corners.card }]}>
          <View style={styles.head}>
            <View style={styles.headText}>
              <Text style={[styles.name, { color: c.text }]} numberOfLines={1}>
                {pending.tree.name}
              </Text>
              <Text style={[styles.line, { color: c.textSub }]}>
                {t('addFile.treeSummary', { n: count, total: pending.tree.files.length, size: fmtBytes(size) })}
              </Text>
            </View>
            {pending.tree.private && (
              <Text style={[styles.badge, { color: c.textSub, backgroundColor: c.surface2, ...corners.pill }]}>
                {t('addFile.treePrivate')}
              </Text>
            )}
          </View>
          <View style={styles.actions}>
            <GlimButton
              tone="quiet"
              label={t('addFile.selectAll')}
              icon={(ink) => <Check color={ink} />}
              onPress={() => setPending({ ...pending, selected: pending.tree.files.map(() => true) })}
            />
            <GlimButton
              tone="quiet"
              label={t('addFile.selectNone')}
              icon={(ink) => <Cross color={ink} />}
              onPress={() => setPending({ ...pending, selected: pending.tree.files.map(() => false) })}
            />
          </View>
          <ScrollView style={styles.files} nestedScrollEnabled>
            {pending.tree.files.map((f, i) => (
              <View key={f.path} style={styles.file}>
                <View style={styles.fileText}>
                  <Text style={[styles.path, { color: pending.selected[i] ? c.textSub : c.textMuted }]} numberOfLines={1}>
                    {f.path}
                  </Text>
                  <Text style={[styles.size, { color: c.textMuted }]}>{fmtBytes(f.size)}</Text>
                </View>
                <GlimToggle
                  value={pending.selected[i]}
                  hue={i}
                  label={t(pending.selected[i] ? 'torrent.skipFile' : 'torrent.fetchFile')}
                  onChange={() => flip(i)}
                />
              </View>
            ))}
          </ScrollView>
          <View style={styles.actions}>
            <GlimButton
              tone="quiet"
              grow
              label={t('addDownload.cancel')}
              icon={(ink) => <Cross color={ink} />}
              disabled={busy}
              onPress={() => setPending(null)}
            />
            <GlimButton
              hue={1}
              grow
              label={busy ? t('addFile.adding') : t('addFile.treeAdd')}
              icon={(ink) => <Plus color={ink} />}
              busy={busy}
              disabled={count === 0}
              onPress={() => void confirm()}
            />
          </View>
        </View>
      )}

      {results.map((o, i) => {
        const line = outcomeText(t, o);
        return (
          <Text key={`${o.file}|${i}`} style={[styles.line, { color: tone[line.tone] }]}>
            {line.text}
          </Text>
        );
      })}
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens, because a
// stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  box: { marginTop: 12, gap: 8 },
  chooseRow: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  line: { fontSize: TYPE.subline },
  review: { padding: 14, gap: 10 },
  head: { flexDirection: 'row', alignItems: 'flex-start', gap: 10 },
  headText: { flex: 1, minWidth: 0, gap: 2 },
  name: { fontSize: TYPE.body, fontWeight: '600' },
  badge: { fontSize: TYPE.meta, paddingHorizontal: 8, paddingVertical: 2, overflow: 'hidden' },
  actions: { flexDirection: 'row', gap: 10 },
  files: { maxHeight: 240 },
  file: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 6 },
  fileText: { flex: 1, minWidth: 0, gap: 2 },
  path: { fontSize: TYPE.subline },
  size: { fontSize: TYPE.meta, ...NUM },
});
