import { useMemo } from 'react';
import { fetchAccountCatalogue, type Task } from './api';
import { useConnections } from './connections';
import { useT } from './i18n';
import { flowOf, type Flow } from './transferFlow';
import { useDiskSpace } from './useDiskSpace';
import { useResource } from './useResource';

/**
 * useTransferFlow reads the ways of the running downloads among `tasks`, with
 * the connection names of the instance behind `base`. `local` is false for a
 * peer, whose disks this machine cannot ask about; its folders then stand
 * unmeasured.
 */
export function useTransferFlow(tasks: readonly Task[], base: string, local = true): Flow {
  const { t } = useT();
  const connections = useConnections(base);
  // The catalogue is the same list in every build, so a peer's rows read from this one.
  const { data: services } = useResource(fetchAccountCatalogue);
  const disk = useDiskSpace(local);
  return useMemo(
    () => flowOf(tasks, { connections, services, disk: local ? disk : null }, t),
    [tasks, connections, services, disk, local, t],
  );
}
