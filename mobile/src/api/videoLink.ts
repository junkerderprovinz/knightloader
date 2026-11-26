import { request } from './client';
import type { ServerConnection, Task } from './types';
import type { LinkChoice, LinkMenus } from './videoPick';

/**
 * What one video link offers, from a probe on the instance, or null for a
 * link the instance does not give yt-dlp, which it refuses with a 400. Any
 * other failure is thrown, so the caller can still add the link as it is.
 */
export async function fetchLinkMenus(conn: ServerConnection, url: string, base = '/api'): Promise<LinkMenus | null> {
  try {
    return await request<LinkMenus>(conn, base, `/ytdlp/link?url=${encodeURIComponent(url)}`);
  } catch (e) {
    if ((e as { status?: number }).status === 400) return null;
    throw e;
  }
}

/** Adds one video link with the rows and qualities picked for it. */
export async function addVideo(
  conn: ServerConnection,
  url: string,
  choice: LinkChoice,
  base = '/api',
  pkg?: string,
): Promise<Task[]> {
  return request<Task[]>(conn, base, '/links', {
    method: 'POST',
    body: JSON.stringify({ links: url, choice, package: pkg || undefined }),
  });
}
