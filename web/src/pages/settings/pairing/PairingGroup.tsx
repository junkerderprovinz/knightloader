// PairingGroup is the two cards about the group itself: the phrase, and who
// came with it. Both read the same stage, so it is worked out once here.
import { useEffect, useState } from 'react';
import type { ConnectInfo } from '../../../lib/api';
import { GroupCard } from './GroupCard';
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
  onGroup,
  onRefresh,
  hues,
}: {
  group: ConnectInfo;
  onGroup: (g: ConnectInfo) => void;
  /** Asks the server for the group again. */
  onRefresh: () => void;
  hues?: [number, number];
}) {
  const [createdHere, setCreatedHere] = useState(false);
  const joinedAgo = useJoinedAgo(group);
  const stage = pairStage(group, joinedAgo, createdHere);
  return (
    <>
      <PhraseCard
        group={group}
        stage={stage}
        onCreatedHere={setCreatedHere}
        onGroup={onGroup}
        onRefresh={onRefresh}
        hue={hues?.[0]}
      />
      <GroupCard group={group} stage={stage} joinedAgo={joinedAgo} onRefresh={onRefresh} hue={hues?.[1]} />
    </>
  );
}
