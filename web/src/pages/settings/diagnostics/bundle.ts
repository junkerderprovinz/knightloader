import { fetchDiagnostics } from '../../../lib/api';

/** fileStamp is a sortable UTC stamp for the file name. */
function fileStamp(): string {
  return new Date().toISOString().replace(/[-:]/g, '').replace(/\.\d+Z$/, 'Z');
}

function saveJSON(doc: unknown, filename: string): void {
  const blob = new Blob([JSON.stringify(doc, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

/**
 * saveDiagnostics saves the diagnostics bundle to a file for a bug report. It
 * fetches a fresh one, since the log lines and goroutine count keep moving.
 */
export async function saveDiagnostics(): Promise<void> {
  saveJSON(await fetchDiagnostics(), `knightloader-diagnostics-${fileStamp()}.json`);
}
