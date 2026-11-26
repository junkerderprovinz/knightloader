// The app's saved views are the web's: the same shape in the same place, so a
// view saved on either side lights its chip on the other, and the same rules
// for saving, overwriting, renaming and deleting, under the same bounds the
// instance holds a member to.
//
// The rules are src/api/savedViews.ts, which imports nothing at run time; the
// chips and their windows are src/components/SavedViews.tsx, mounted with
// stand-ins.
//
// Run by hand and by CI, from mobile/: `node check-saved-views.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { compile, mount } from './stand-in-react.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const load = (...p) => import(pathToFileURL(join(here, 'src', ...p)).href);
const narrowing = await load('api', 'narrowing.ts');
const views = await load('api', 'savedViews.ts');
const { COLLECTOR_FILTERS, DOWNLOAD_FILTERS, NOTHING_NARROWED } = narrowing;
const { MAX_VIEWS, MAX_VIEW_BYTES, MAX_VIEW_NAME, fromStored, matching, removeView, renameView, sameNarrowing, sanitiseViews, saveView, taken, toStored } =
  views;

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

// The bounds are the web's and the instance's.
{
  const web = readFileSync(join(here, '..', 'web', 'src', 'lib', 'savedViews.ts'), 'utf8');
  const go = readFileSync(join(here, '..', 'internal', 'api', 'routes_uistate.go'), 'utf8');
  for (const [name, mine, goName] of [
    ['MAX_VIEWS', MAX_VIEWS, 'maxSavedViews'],
    ['MAX_VIEW_BYTES', MAX_VIEW_BYTES, 'maxSavedViewBytes'],
    ['MAX_VIEW_NAME', MAX_VIEW_NAME, 'maxSavedViewName'],
  ]) {
    expect(`${name} is the web's`, mine, Number(web.match(new RegExp(`export const ${name} = (\\d+)`))?.[1]));
    expect(`${name} is the instance's`, mine, Number(go.match(new RegExp(`${goName}\\s*=\\s*(\\d+)`))?.[1]));
  }
}

const n = (over = {}) => ({ ...NOTHING_NARROWED, ...over });
const facets = (over = {}) => ({ host: new Set(), fileType: new Set(), package: new Set(), ...over });

// The web's shape, both ways.
{
  const mine = n({
    search: 'film',
    category: 'host',
    filters: new Set(['unchecked', 'online']),
    facets: facets({ host: new Set(['b.example', 'a.example']) }),
  });
  expect('stored as the web stores it, filters in the list order and facets sorted', toStored(mine, COLLECTOR_FILTERS), {
    search: { text: 'film', category: 'host' },
    filters: ['online', 'unchecked'],
    facets: { host: ['a.example', 'b.example'], fileType: [], package: [] },
  });
  expect('and read back the same', sameNarrowing(fromStored(toStored(mine, COLLECTOR_FILTERS), COLLECTOR_FILTERS, true), mine), true);
  const old = fromStored({ filters: ['held', 'nonsense'], search: { text: 'x', category: 'size' } }, DOWNLOAD_FILTERS, false);
  expect('the old held filter is the disabled one and an unknown one goes', [...old.filters], ['disabled']);
  expect('an unknown category searches every field', old.category, 'any');
  expect(
    'the download list keeps no facets',
    fromStored({ facets: { host: ['a'] } }, DOWNLOAD_FILTERS, false).facets.host.size,
    0,
  );
  expect('anything else reads as nothing', sameNarrowing(fromStored('junk', DOWNLOAD_FILTERS, false), NOTHING_NARROWED), true);
  expect('text is compared trimmed', sameNarrowing(n({ search: ' film ' }), n({ search: 'film' })), true);
  expect('the category counts only with text', sameNarrowing(n({ category: 'host' }), n()), true);
  expect('and with text it does', sameNarrowing(n({ search: 'a', category: 'host' }), n({ search: 'a' })), false);
  const stored = [{ id: 'a', name: 'A' }, { id: 'a', name: 'B' }, { id: '', name: 'C' }, { name: 'D' }, { id: 'e', name: 'E' }];
  expect('stored views keep the ones with an id of their own', sanitiseViews(stored).map((v) => v.name), ['A', 'E']);
  const many = Array.from({ length: 20 }, (_, i) => ({ id: `v${i}`, name: 'x' }));
  expect('and the first twelve of them', sanitiseViews(many).length, 12);
}

// Saving, overwriting, renaming, deleting.
{
  let id = 0;
  const newId = () => `id${++id}`;
  const state = toStored(n({ search: 'big' }), DOWNLOAD_FILTERS);
  const one = saveView([], '  Big  ', state, newId).views;
  expect('a new view gets a fresh id and a trimmed name', one, [{ id: 'id1', name: 'Big', state }]);
  const other = toStored(n({ filters: new Set(['failed']) }), DOWNLOAD_FILTERS);
  expect('a taken name overwrites that view, whatever its case', saveView(one, 'BIG', other, newId).views, [{ id: 'id1', name: 'BIG', state: other }]);
  expect('a blank name saves nothing', saveView(one, '   ', other, newId).views, one);
  const twelve = Array.from({ length: 12 }, (_, i) => ({ id: `v${i}`, name: `V${i}`, state }));
  expect('a thirteenth view is refused', saveView(twelve, 'More', state, newId), { refused: 'full' });
  expect('a full list still takes an overwrite', saveView(twelve, 'v3', state, newId).views?.length, 12);
  const huge = toStored(n({ search: 'x'.repeat(5000) }), DOWNLOAD_FILTERS);
  expect('a view too big to store is refused', saveView([], 'Huge', huge, newId), { refused: 'tooBig' });
  expect('taken ignores the view being renamed', [taken(one, 'big'), taken(one, 'big', 'id1'), taken(one, '')], [true, false, false]);
  expect('a rename keeps the id', renameView(one, 'id1', ' Large '), [{ id: 'id1', name: 'Large', state }]);
  expect('a blank rename changes nothing', renameView(one, 'id1', ' '), one);
  expect('a delete takes the one view', removeView([...one, { id: 'x', name: 'X', state }], 'id1').map((v) => v.id), ['x']);
  expect('the view the list equals is found', matching(one, n({ search: 'big ' }), DOWNLOAD_FILTERS, false)?.id, 'id1');
  expect('a nudged filter matches none', matching(one, n({ search: 'big', filters: new Set(['failed']) }), DOWNLOAD_FILTERS, false), null);
}

// The chips and their windows.
{
  const t = (key, vars) => (vars ? `${key}${JSON.stringify(vars)}` : key);
  const { SavedViews } = compile(join(here, 'src', 'components', 'SavedViews.tsx'), {
    'react-native': {
      Modal: 'Modal',
      Pressable: 'Pressable',
      ScrollView: 'ScrollView',
      StyleSheet: { create: (s) => s },
      TouchableOpacity: 'TouchableOpacity',
      View: 'View',
    },
    'expo-crypto': { getRandomBytes: (len) => new Uint8Array(len).fill(171) },
    '../api/narrowing': narrowing,
    '../api/savedViews': views,
    '../i18n/I18nContext': { useT: () => ({ t }) },
    '../theme/AppearanceContext': {
      useAppearance: () => ({
        c: {},
        accent: '#336699',
        accentContrast: '#ffffff',
        accentInk: '#000000',
        corners: { pill: {}, control: {} },
        hueAt: () => undefined,
      }),
    },
    '../theme/MotionContext': { useMotion: () => ({ motion: 'subtle' }) },
    '../theme/tokens': { TYPE: {} },
    '../theme/appearance': { contrastOn: () => '#ffffff', valid: () => true },
    './ConfirmDialog': { ConfirmDialog: 'ConfirmDialog' },
    './glim': { GlimButton: 'GlimButton', NotchCard: 'NotchCard' },
    './IconBadge': { __esModule: true, default: 'IconBadge', Cross: 'Cross', Pencil: 'Pencil', Pin: 'Pin', Save: 'Save', Trash: 'Trash' },
    './InfoTip': { InfoTip: 'InfoTip' },
    './Moving': { NoArrival: 'NoArrival' },
    './Text': { Text: 'Text', TextInput: 'TextInput' },
  });

  const big = toStored(n({ search: 'big' }), DOWNLOAD_FILTERS);
  const props = {
    views: [{ id: 'v1', name: 'Big', state: big }],
    narrowing: n({ search: 'big' }),
    allowed: DOWNLOAD_FILTERS,
    keepFacets: false,
    applied: [],
    stored: [],
    onApply: (x) => props.applied.push(x),
    onViews: async (next) => {
      props.stored.push(next);
      props.views = next;
    },
  };
  const view = mount(() => SavedViews(props));
  const find = (test) => view.shown().elements.find((e) => test(e.type, e.props))?.props;
  const chip = (name) => find((type, p) => type === 'TouchableOpacity' && p.accessibilityLabel === `views.label: ${name}`);
  const badge = (label) => find((type, p) => type === 'IconBadge' && p.accessibilityLabel === label);
  const button = (label) => find((type, p) => type === 'GlimButton' && p.label === label);
  const field = () => find((type, p) => type === 'TextInput' && p.placeholder === 'views.namePlaceholder');

  await view.settle();
  expect('the chip of the view the list equals is lit', chip('Big')?.accessibilityState, { checked: true });
  chip('Big').onPress();
  expect('a press on the lit chip clears the list', sameNarrowing(props.applied.at(-1), NOTHING_NARROWED), true);

  props.narrowing = n({ filters: new Set(['failed']) });
  await view.settle();
  expect('a nudged filter unlights it', chip('Big')?.accessibilityState, { checked: false });
  chip('Big').onPress();
  expect('a press on another chip applies its view', props.applied.at(-1)?.search, 'big');

  badge('views.save').onPress();
  await view.settle();
  expect('the pin asks for a name', field() !== undefined, true);
  expect('without a name nothing can be saved', button('views.saveConfirm')?.disabled, true);
  field().onChangeText('big');
  await view.settle();
  expect('a taken name says it overwrites', button('views.overwriteConfirm') !== undefined, true);
  button('views.overwriteConfirm').onPress();
  await view.settle();
  expect('the overwrite keeps the id and stores what is set now', props.stored.at(-1)?.map((v) => [v.id, v.name, v.state.filters]), [
    ['v1', 'big', ['failed']],
  ]);
  expect('the window closes after the save', field(), undefined);

  badge('views.save').onPress();
  await view.settle();
  field().onChangeText('Failed ones');
  await view.settle();
  button('views.saveConfirm').onPress();
  await view.settle();
  expect('a new name adds a view with a fresh id', props.views.map((v) => [v.id.length, v.name]), [
    [2, 'big'],
    [32, 'Failed ones'],
  ]);

  badge('views.manage').onPress();
  await view.settle();
  badge('views.rename: Failed ones').onPress();
  await view.settle();
  expect('a rename starts from the old name', field()?.value, 'Failed ones');
  field().onChangeText('BIG');
  await view.settle();
  expect('a rename onto a taken name is refused', button('views.saveConfirm')?.disabled, true);
  field().onChangeText('Broken');
  await view.settle();
  button('views.saveConfirm').onPress();
  await view.settle();
  expect('the rename is stored', props.views.map((v) => v.name), ['big', 'Broken']);

  badge('views.delete: big').onPress();
  await view.settle();
  find((type) => type === 'ConfirmDialog').onConfirm();
  await view.settle();
  expect('a confirmed delete takes the one view', props.views.map((v) => v.name), ['Broken']);

  props.onViews = async () => {
    throw new Error('the instance said no');
  };
  badge('views.save').onPress();
  await view.settle();
  field().onChangeText('Another');
  await view.settle();
  button('views.saveConfirm').onPress();
  await view.settle();
  expect(
    'a refused save stays open and says why',
    view.shown().elements.some((e) => e.type === 'Text' && [e.props.children].flat().includes('the instance said no')),
    true,
  );
  view.unmount();
}

if (problems.length) {
  console.error(`${problems.length} problem(s):\n  ${problems.join('\n  ')}`);
  process.exit(1);
}
console.log("ok: saved views keep the web's shape and rules, and the chips save, overwrite, rename and delete them");
