import { useEffect, useState } from 'react';
import { fetchPieces, type PieceMap, type Task } from '../../lib/api';
import { useT } from '../../lib/i18n';
import { PieceBand, pieceWords } from '../pictures/PieceBand';
import { Card, SectionTitle } from '../ui';

const POLL_MS = 1000;

/**
 * usePieces asks which parts of the task's file are here, every second while
 * they change and once when the transfer pauses, so the band keeps its last
 * state. A torrent is not asked about: its pieces stay inside its client.
 */
function usePieces(task: Task, base: string): PieceMap | null {
  const [held, setHeld] = useState<{ id: string; map: PieceMap | null } | null>(null);
  const live = task.status === 'running' || !!task.repair;
  const mode = task.resolver === 'torrent' ? 'none' : live ? 'live' : task.status === 'paused' ? 'once' : 'none';
  const id = task.id;

  useEffect(() => {
    if (mode === 'none') return;
    let wanted = true;
    const ask = () =>
      void fetchPieces(id, base).then(
        (map) => wanted && setHeld({ id, map }),
        // A peer on an older build has no such route.
        () => wanted && setHeld({ id, map: null }),
      );
    ask();
    const timer = mode === 'live' ? window.setInterval(ask, POLL_MS) : 0;
    return () => {
      wanted = false;
      window.clearInterval(timer);
    };
  }, [id, base, mode]);

  return mode !== 'none' && held?.id === id ? held.map : null;
}

/**
 * PiecesCard shows the file as a band of its pieces while there is something
 * to say about them: an HTTP download with its connections, a Usenet file
 * with its articles, or a file its par2 check found damaged.
 */
export function PiecesCard({ task, base, hue }: { task: Task; base: string; hue?: number }) {
  const { t } = useT();
  const read = usePieces(task, base);
  if (!read || read.total <= 0) return null;
  // A reading can be a moment older than the status, and nothing writes into a file that is not running.
  const map = task.status === 'running' ? read : { ...read, heads: [] };
  const words = pieceWords(map, t);
  if (!words) return null;

  return (
    <Card hue={hue} data-new="piece-map" className="flex flex-col gap-3">
      <SectionTitle hint={t('detail.piecesHint')}>{t('detail.pieces')}</SectionTitle>
      <PieceBand map={map} label={words} mending={task.repair?.stage === 'repairing'} />
      <p aria-hidden className="glim-num text-meta text-carbon-textMuted">
        {words}
      </p>
    </Card>
  );
}
