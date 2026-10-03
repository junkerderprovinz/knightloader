import { describe, expect, it } from 'vitest';

import { ApiError, refusal } from './api';
import type { TranslationKey } from './i18n';
import { taskRefusal } from './taskRefusal';

const t = (key: TranslationKey, vars?: Record<string, string | number>) => `${key} ${vars?.names ?? ''}`;

describe('task refusals', () => {
  it('names the finished files a restart cannot fetch again', async () => {
    const r = new Response(
      JSON.stringify({
        error: 'cannot be downloaded again because the .nzb is gone: show.mkv',
        code: 'nzbGone',
        params: { names: 'show.mkv' },
      }),
      { status: 409 },
    );
    expect(taskRefusal(await refusal(r), t)).toBe('task.restart.nzbGone show.mkv');
  });

  it('leaves a refusal without a known code to the server sentence', () => {
    expect(taskRefusal(new ApiError('no such task'), t)).toBeNull();
  });
});
