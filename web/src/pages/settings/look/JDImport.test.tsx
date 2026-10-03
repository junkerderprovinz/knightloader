// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { ApiError, applyJDImport, readJDImportFile, type JDImportPreview } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { en } from '../../../lib/locales/en';
import { JDImportCard } from './JDImport';

vi.mock('../../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../lib/api')>()),
  readJDImportFile: vi.fn(),
  applyJDImport: vi.fn(),
}));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.clearAllMocks();
});

const preview: JDImportPreview = {
  token: 'token',
  files: [],
  problems: [],
  items: [
    { id: 'passwords', group: 'settings', kind: 'passwords', name: '', count: 1, total: 1, ticked: true },
    {
      id: 'package:0',
      group: 'downloads',
      kind: 'package',
      name: 'Show S01',
      count: 1,
      ticked: true,
      notes: [{ code: 'linksFinished', params: { n: '1' }, text: 'english' }],
    },
  ],
};

async function openPreview() {
  vi.mocked(readJDImportFile).mockResolvedValue(preview);
  await act(async () =>
    root.render(
      <I18nProvider>
        <JDImportCard hue={0} />
      </I18nProvider>,
    ),
  );
  const input = host.querySelector<HTMLInputElement>('input[type=file]')!;
  Object.defineProperty(input, 'files', { value: [new File(['zip'], 'cfg.zip')] });
  await act(async () => input.dispatchEvent(new Event('change', { bubbles: true })));
}

async function takeOver() {
  const button = [...document.body.querySelectorAll('button')].find((b) => b.textContent?.startsWith('Take over'));
  await act(async () => button!.click());
}

const text = () => document.body.textContent ?? '';

it('does not call a preview the server no longer holds half an hour old', async () => {
  await openPreview();
  vi.mocked(applyJDImport).mockRejectedValue(new ApiError('gone', 'jdimport.unknown', undefined, 410));
  await takeOver();
  expect(text()).toContain(en['settings.jdimport.unknown']);
  expect(text()).not.toContain(en['settings.jdimport.expired']);
});
