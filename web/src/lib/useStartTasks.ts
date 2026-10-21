import { useCallback } from 'react';
import { startTasks } from './api';
import { useT } from './i18n';
import { message } from './intake';
import { useToast } from './toast';

/**
 * useStartTasks starts collected links and says what the start did, from the
 * route's answer: a schedule holding the queue, a filter holding the links,
 * disabled links or nothing matching each get their own sentence, and a call
 * that failed says why. It resolves to whether any link left the collector,
 * which one held by a schedule has too.
 */
export function useStartTasks(): (ids: string[], base?: string) => Promise<boolean> {
  const { t } = useT();
  const { toast } = useToast();
  return useCallback(
    async (ids: string[], base = '/api') => {
      try {
        const r = await startTasks(ids, base);
        const disabled = r.disabled ?? 0;
        if (r.blocked) toast(t('collector.toastStartBlocked'), 'fail');
        else if (r.started === 0 && r.skipped > 0) toast(t('collector.toastStartHeld', { n: r.skipped }), 'fail');
        else if (r.started === 0 && disabled > 0) toast(t('collector.toastStartDisabled', { n: disabled }), 'fail');
        else if (r.started > 0 && disabled > 0) toast(t('collector.toastStartedSomeDisabled', { n: r.started, disabled }), 'info');
        else if (r.started > 0) toast(t('collector.toastStarted', { n: r.started }), 'info');
        return r.started > 0;
      } catch (e) {
        toast(t('list.failed', { error: message(e) }), 'fail');
        return false;
      }
    },
    [t, toast],
  );
}
