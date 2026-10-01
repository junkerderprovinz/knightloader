import { useCallback } from 'react';
import { startTasks } from './api';
import { useT } from './i18n';
import { message } from './intake';
import { useToast } from './toast';

/**
 * useStartTasks starts collected links and says what the start did, from the
 * route's answer: a schedule holding the queue, a filter holding the links,
 * disabled links or nothing matching each get their own sentence, and a call
 * that failed says why.
 */
export function useStartTasks(): (ids: string[], base?: string) => Promise<void> {
  const { t } = useT();
  const { toast } = useToast();
  return useCallback(
    async (ids: string[], base = '/api') => {
      try {
        const r = await startTasks(ids, base);
        if (r.blocked) return toast(t('collector.toastStartBlocked'), 'fail');
        if (r.started === 0 && r.skipped > 0) return toast(t('collector.toastStartHeld', { n: r.skipped }), 'fail');
        const disabled = r.disabled ?? 0;
        if (r.started === 0 && disabled > 0) return toast(t('collector.toastStartDisabled', { n: disabled }), 'fail');
        if (r.started === 0) return;
        if (disabled > 0) return toast(t('collector.toastStartedSomeDisabled', { n: r.started, disabled }), 'info');
        toast(t('collector.toastStarted', { n: r.started }), 'info');
      } catch (e) {
        toast(t('list.failed', { error: message(e) }), 'fail');
      }
    },
    [t, toast],
  );
}
