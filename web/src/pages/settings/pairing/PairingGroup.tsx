// PairingGroup is the two cards about the group itself: the phrase, and who
// came with it. Both read the same stage, so it is worked out once here. In a
// fortress the pairing lives under the fortress phrase, so the fortress's card
// is the one place for a phrase and the group's card says where it went.
import { useEffect, useState } from 'react';
import type { ConnectInfo, FortressState } from '../../../lib/api';
import { GroupCard } from './GroupCard';
import { MovedCard } from './MovedCard';
import { PhraseCard } from './PhraseCard';
import { pairStage } from './pairStage';

/** useJoinedAgo counts on from the seconds the server last reported, so the
 *  search timer runs and the minute passes between two reloads. */
function useJoinedAgo(group: ConnectInfo): number {
  const [base, setBase] = useState({ ago: group.joinedAgo, at: Date.now() });
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const at = Date.now();
    setBase({ ago: group.joinedAgo, at });
    setNow(at);
  }, [group]);
  const waiting = group.active && group.members.length === 0 && !group.memberSeen;
  useEffect(() => {
    if (!waiting) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [waiting]);
  return base.ago + Math.max(0, Math.floor((now - base.at) / 1000));
}

export function PairingGroup({
  group,
  fortress,
  onGroup,
  onRefresh,
  hues,
}: {
  group: ConnectInfo;
  fortress?: FortressState | null;
  onGroup: (g: ConnectInfo) => void;
  /** Asks the server for the group again. */
  onRefresh: () => void;
  hues?: [number, number];
}) {
  const [createdHere, setCreatedHere] = useState(false);
  const joinedAgo = useJoinedAgo(group);
  const moved = !!fortress?.joined;
  // The old phrase is forgotten once everybody followed, and the pairing goes
  // on in the fortress all the same.
  const stage = pairStage(moved ? { ...group, active: true } : group, joinedAgo, createdHere);
  return (
    <>
      {moved ? (
        <MovedCard left={fortress.left} hue={hues?.[0]} />
      ) : (
        <PhraseCard
          group={group}
          stage={stage}
          onCreatedHere={setCreatedHere}
          onGroup={onGroup}
          onRefresh={onRefresh}
          hue={hues?.[0]}
        />
      )}
      <GroupCard group={group} stage={stage} joinedAgo={joinedAgo} onRefresh={onRefresh} hue={hues?.[1]} />
    </>
  );
}
