// How a backend id reads, shared by the task row's badge and the volume
// chart's legend. Product names, so not translated.
export const RESOLVER_LABELS: Record<string, string> = {
  direct: 'Direct',
  torbox: 'TorBox',
  alldebrid: 'AllDebrid',
  realdebrid: 'Real-Debrid',
  ytdlp: 'yt-dlp',
  jd: 'JDownloader',
  http: 'HTTP',
};

/** resolverLabel names a backend, or returns the id itself when it is unknown,
 *  so a legend entry is never blank. */
export function resolverLabel(id: string): string {
  return RESOLVER_LABELS[id] ?? id;
}
