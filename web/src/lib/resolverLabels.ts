// What a backend id READS as, in one place because two surfaces now show it.
//
// It lived inside components/StatusPill.tsx as a module-private map, which was
// right for as long as the task row's own badge was the only reader. The volume
// curve's legend is the second, and a copy is exactly how "jd" comes out as
// JDownloader on a download row and as "jd" in the chart three inches below it -
// the same id, two names, on one screen.
//
// Not translated, and that is not an oversight: these are product names, and
// "Real-Debrid" is Real-Debrid in every language the app speaks.
export const RESOLVER_LABELS: Record<string, string> = {
  direct: 'Direct',
  torbox: 'TorBox',
  alldebrid: 'AllDebrid',
  realdebrid: 'Real-Debrid',
  ytdlp: 'yt-dlp',
  jd: 'JDownloader',
  http: 'HTTP',
};

/**
 * An id this build has never met reads as ITSELF rather than as a blank.
 *
 * A backend the server grew after this bundle was built is a name we do not
 * know yet, not a name that is missing - and a legend band with no word beside
 * it is a colour nobody can look up. Same fallback DownloadsSettings makes for
 * an idle action id it has no key for, for the same reason.
 */
export function resolverLabel(id: string): string {
  return RESOLVER_LABELS[id] ?? id;
}
