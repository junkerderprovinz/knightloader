// Where an instance stands in its group, read from GET /api/connect. Kept
// apart from the card so the rules can be tested without drawing it.
import type { ConnectInfo } from '../../../lib/api';

/** How long a new member may take to show up before the card suggests why
 *  nobody has. The relay puts a member through within seconds. */
export const ALONE_AFTER_S = 60;

/** "gone" is a group whose members were there once and are unreachable now. */
export type PairStage = 'unpaired' | 'new' | 'searching' | 'alone' | 'gone' | 'paired';

/**
 * pairStage reads the stage from the group, the seconds since this instance
 * entered it and whether this page generated its phrase. Only an instance or
 * a phone there now counts as paired.
 */
export function pairStage(
  g: Pick<ConnectInfo, 'active' | 'members' | 'apps' | 'memberSeen'>,
  joinedAgo: number,
  createdHere: boolean,
): PairStage {
  if (!g.active) return 'unpaired';
  if (g.members.length > 0 || g.apps.some((a) => a.connected)) return 'paired';
  if (g.memberSeen) return 'gone';
  if (joinedAgo >= ALONE_AFTER_S) return 'alone';
  return createdHere ? 'new' : 'searching';
}

/** What this instance's row says in each stage once there is a group. A stage
 *  that still waits for somebody takes the tone with the live dot. */
export const STAGE_PILL = {
  new: { key: 'pairing.stateNew', tone: 'run' },
  searching: { key: 'pairing.stateSearching', tone: 'run' },
  gone: { key: 'pairing.stateSearching', tone: 'run' },
  alone: { key: 'pairing.stateAlone', tone: 'warn' },
  paired: { key: 'pairing.paired', tone: 'ok' },
} as const satisfies Record<Exclude<PairStage, 'unpaired'>, { key: string; tone: string }>;

/** clock writes seconds as minutes and seconds, 75 as "1:15". */
export function clock(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
}
