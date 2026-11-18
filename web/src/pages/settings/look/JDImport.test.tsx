// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import {
  ApiError,
  applyJDImport,
  fetchDeploymentInfo,
  readJDImportFile,
  readJDImportPath,
  type DeploymentInfo,
  type JDImportPreview,
} from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { en } from '../../../lib/locales/en';
import { JDImportCard } from './JDImport';

vi.mock('../../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../lib/api')>()),
  readJDImportFile: vi.fn(),
  readJDImportPath: vi.fn(),
  applyJDImport: vi.fn(),
  fetchDeploymentInfo: vi.fn(),
}));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  vi.mocked(fetchDeploymentInfo).mockRejectedValue(new Error('offline'));
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
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

it('counts one password, one link and one left-out link in the singular', async () => {
  await openPreview();
  expect(text()).toContain('1 archive password');
  expect(text()).not.toContain('1 archive passwords');
  expect(text()).toContain('1 link');
  expect(text()).not.toContain('1 links');
  expect(text()).toContain('1 finished link is left out.');
});

it('says one link is waiting after the take-over', async () => {
  await openPreview();
  vi.mocked(applyJDImport).mockResolvedValue({ imported: ['package:0'], failed: [], links: 1, held: 0, filterStops: false });
  await takeOver();
  expect(text()).toContain('1 link is waiting in the link collector.');
});

it('says how many links went to the rejected links apart from those waiting', async () => {
  await openPreview();
  vi.mocked(applyJDImport).mockResolvedValue({ imported: ['package:0'], failed: [], links: 1, held: 2, filterStops: false });
  await takeOver();
  expect(text()).toContain('1 link is waiting in the link collector.');
  expect(text()).toContain('2 links rejected');
});

it('does not call a preview the server no longer holds half an hour old', async () => {
  await openPreview();
  vi.mocked(applyJDImport).mockRejectedValue(new ApiError('gone', 'jdimport.unknown', undefined, 410));
  await takeOver();
  expect(text()).toContain(en['settings.jdimport.unknown']);
  expect(text()).not.toContain(en['settings.jdimport.expired']);
});

it('shakes the upload button, not the path button, when an uploaded zip is refused', async () => {
  vi.mocked(readJDImportFile).mockRejectedValue(new ApiError('no zip', 'jdimport.notZip', undefined, 400));
  await act(async () =>
    root.render(
      <I18nProvider>
        <JDImportCard hue={0} />
      </I18nProvider>,
    ),
  );
  const input = host.querySelector<HTMLInputElement>('input[type=file]')!;
  Object.defineProperty(input, 'files', { value: [new File(['zip'], 'cfg.txt')] });
  await act(async () => input.dispatchEvent(new Event('change', { bubbles: true })));
  const shaking = (label: string) =>
    [...host.querySelectorAll('button')]
      .find((b) => b.textContent === label)!
      .className.split(' ')
      .some((c) => c.endsWith('-shake'));
  expect(text()).toContain(en['settings.jdimport.notZip']);
  expect(shaking(en['settings.jdimport.uploadButton'])).toBe(true);
  expect(shaking(en['settings.jdimport.folderButton'])).toBe(false);
});

function runsIn(kind: string) {
  vi.mocked(fetchDeploymentInfo).mockResolvedValue({ deployment: 'container', kind } as DeploymentInfo);
}

const button = (label: string) => [...host.querySelectorAll('button')].find((b) => b.textContent === label)!;

async function mountCard() {
  await act(async () =>
    root.render(
      <I18nProvider>
        <JDImportCard hue={0} />
      </I18nProvider>,
    ),
  );
}

async function browseToMissingFolder() {
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            path: '/mnt/user/appdata',
            tail: '',
            exists: false,
            listed: '/',
            parent: '',
            roots: ['/'],
            entries: [{ name: 'data', path: '/data' }],
            truncated: false,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
    ),
  );
  await mountCard();
  await act(async () => button(en['settings.jdimport.folderButton']).click());
}

it('tells a container user to mount the JDownloader folder instead of promising a download will create it', async () => {
  runsIn('container');
  await browseToMissingFolder();
  expect(text()).toContain(en['settings.jdimport.pathHidden']);
  expect(text()).not.toContain(en['folders.new']);
});

it('asks for an existing JDownloader folder outside a container', async () => {
  runsIn('desktop');
  await browseToMissingFolder();
  expect(text()).toContain(en['settings.jdimport.pathAbsent']);
  expect(text()).not.toContain(en['folders.new']);
});

it('explains the container mount when a typed path is not there', async () => {
  runsIn('container');
  vi.mocked(readJDImportPath).mockRejectedValue(
    new ApiError('missing', 'jdimport.pathMissing', { path: '/mnt/user/appdata/jd' }, 400),
  );
  vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
  await mountCard();
  await act(async () => button(en['settings.jdimport.folderButton']).click());
  const field = host.querySelector<HTMLInputElement>(`input[aria-label="${en['folders.path']}"]`)!;
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(field, '/mnt/user/appdata/jd');
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(async () => button(en['folders.use']).click());
  expect(readJDImportPath).toHaveBeenCalledWith('/mnt/user/appdata/jd');
  expect(text()).toContain(en['settings.jdimport.pathHiddenAt'].replace('{path}', '/mnt/user/appdata/jd'));
});

it('offers the zip and the folder as two buttons in one row, as the backup card does', async () => {
  await mountCard();
  const zip = button(en['settings.jdimport.uploadButton']);
  const folder = button(en['settings.jdimport.folderButton']);
  expect(zip.parentElement).toBe(folder.parentElement);
  expect(text()).not.toContain(en['settings.jdimport.uploadText']);
  expect(text()).not.toContain(en['settings.jdimport.pathLabel']);
});
