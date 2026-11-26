import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';
import { errorText } from '../api/client';
import { SaveStopped } from '../api/saveCopy';
import { saveTask, saveTorrentFile, type SaveProgress } from '../api/saveFile';
import { fmtBytes } from '../api/stats';
import type { ServerConnection, Task, TorrentFileView } from '../api/types';
import { useT } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { NUM, TYPE } from '../theme/tokens';
import { GlimButton } from './glim';
import { Cross } from './IconBadge';
import { Arrive } from './Moving';
import { Text } from './Text';

type State =
  | { kind: 'idle' }
  | { kind: 'saving'; name: string; size: number; done: number }
  | { kind: 'saved'; name: string }
  | { kind: 'failed'; message: string };

// Long enough to read the line, short enough that it does not stay on top of
// the list.
const SAVED_MS = 6000;

/**
 * useSaveToPhone is what a row's Save badge does, and the card above the list
 * that follows the copy: how far it has got, a way to stop it, and how it
 * ended. One save at a time, so a press while a copy runs is let go.
 */
export function useSaveToPhone(
  conn: ServerConnection,
  base: string,
): {
  save: (task: Task) => void;
  saveFile: (task: Task, files: TorrentFileView[], index: number) => void;
  card: ReactNode;
} {
  const { t } = useT();
  const [state, setState] = useState<State>({ kind: 'idle' });
  const stop = useRef<AbortController | null>(null);

  useEffect(() => {
    if (state.kind !== 'saved') return;
    const timer = setTimeout(() => setState({ kind: 'idle' }), SAVED_MS);
    return () => clearTimeout(timer);
  }, [state]);

  // A screen that goes away takes its copy with it, half a file included.
  useEffect(() => () => stop.current?.abort(), []);

  const run = useCallback(
    (copy: (signal: AbortSignal, onProgress: (p: SaveProgress) => void) => Promise<boolean>) => {
      if (stop.current) return;
      const abort = new AbortController();
      stop.current = abort;
      let name = '';
      setState({ kind: 'idle' });
      copy(abort.signal, (p) => {
        name = p.name;
        setState({ kind: 'saving', ...p });
      })
        .then((saved) => setState(saved ? { kind: 'saved', name } : { kind: 'idle' }))
        .catch((e: unknown) =>
          setState(e instanceof SaveStopped ? { kind: 'idle' } : { kind: 'failed', message: errorText(t, e) }),
        )
        .finally(() => {
          stop.current = null;
        });
    },
    [t],
  );
  const save = useCallback(
    (task: Task) => run((signal, onProgress) => saveTask(conn, task, t('task.save'), signal, onProgress, base)),
    [run, conn, base, t],
  );
  const saveFile = useCallback(
    (task: Task, files: TorrentFileView[], index: number) =>
      run((signal, onProgress) => saveTorrentFile(conn, task, files, index, t('task.save'), signal, onProgress, base)),
    [run, conn, base, t],
  );

  return { save, saveFile, card: <SaveCard state={state} onStop={() => stop.current?.abort()} /> };
}

function SaveCard({ state, onStop }: { state: State; onStop: () => void }) {
  const { t } = useT();
  const { c, accent, corners } = useAppearance();
  if (state.kind === 'idle') return null;
  if (state.kind === 'failed') {
    return (
      <Text style={[styles.line, { color: c.statusFailSolid }]} numberOfLines={3}>
        {t('task.saveFailed', { reason: state.message })}
      </Text>
    );
  }
  if (state.kind === 'saved') {
    return (
      <Text style={[styles.line, { color: c.statusOkText }]} numberOfLines={2}>
        {t('task.saved', { name: state.name })}
      </Text>
    );
  }
  const pct = state.size > 0 ? Math.min(100, Math.floor((state.done / state.size) * 100)) : 0;
  return (
    <Arrive style={[styles.card, { backgroundColor: c.surface, ...corners.card }]}>
      <View style={styles.body}>
        <Text style={[styles.title, { color: c.text }]} numberOfLines={2}>
          {t('task.saving', { name: state.name })}
        </Text>
        <View style={[styles.track, { backgroundColor: c.surface2, ...corners.pill }]}>
          <View style={[styles.fill, { width: `${pct}%`, backgroundColor: accent }]} />
        </View>
        <Text style={[styles.meta, { color: c.textMuted }]}>
          {`${fmtBytes(state.done)} / ${fmtBytes(state.size)} · ${pct}%`}
        </Text>
      </View>
      <GlimButton tone="quiet" label={t('task.saveStop')} icon={(ink) => <Cross color={ink} />} onPress={onStop} />
    </Arrive>
  );
}

/**
 * SaveDown: put the file on this phone. An arrow onto a tray, drawn from
 * filled shapes as the badge's other glyphs are.
 */
export function SaveDown({ color, size = 16 }: { color: string; size?: number }) {
  // Stem, head and tray stand about 12 units tall in a 13-unit grid.
  const u = size / 13;
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ width: 2.8 * u, height: 5 * u, backgroundColor: color, borderRadius: u }} />
      <View
        style={{
          width: 0,
          height: 0,
          marginTop: -0.5 * u,
          borderStyle: 'solid',
          borderLeftWidth: 4.5 * u,
          borderRightWidth: 4.5 * u,
          borderTopWidth: 4 * u,
          borderLeftColor: 'transparent',
          borderRightColor: 'transparent',
          borderTopColor: color,
        }}
      />
      <View style={{ width: 11 * u, height: 2.4 * u, marginTop: 1.2 * u, backgroundColor: color, borderRadius: 1.2 * u }} />
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens, because a
// stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  card: { flexDirection: 'row', alignItems: 'center', gap: 12, padding: 14, marginBottom: 12 },
  body: { flex: 1, minWidth: 0 },
  title: { fontSize: TYPE.body, fontWeight: '500' },
  track: { height: 4, marginTop: 8, overflow: 'hidden' },
  fill: { height: '100%' },
  meta: { fontSize: TYPE.meta, marginTop: 6, ...NUM },
  line: { marginBottom: 8, fontSize: TYPE.subline },
});
