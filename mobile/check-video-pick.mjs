// The format and quality a video link is added with: the menus read off what
// the instance says a video offers, the choice POST /api/links is sent, and
// the picker that shows them, which waits for the field to hold still and
// says nothing about a link that is no video.
//
// src/api/videoPick.ts imports nothing at run time, so node strips its types
// and these checks call the real functions. The picker is compiled and
// rendered on stand-in-react.mjs.
//
// Run by hand and by CI, from mobile/: `node check-video-pick.mjs`
import { dirname, join } from 'node:path';
import { mock } from 'node:test';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { compile, mount } from './stand-in-react.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const v = await import(pathToFileURL(join(here, 'src', 'api', 'videoPick.ts')).href);

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const x = JSON.stringify(want);
  if (g !== x) problems.push(`${what}: got ${g}, want ${x}`);
};

const menus = {
  qualities: ['2160p', '1080p', '720p'],
  audio: true,
  videoTracks: ['2160p60hdr webm vp9', '1080p60 webm vp9', '1080p mp4 avc1', '720p mp4 avc1'],
  audioTracks: ['opus 160k', 'aac 129k', 'opus 70k'],
};

expect('a video with sound offers both rows', v.partsOf(menus), ['video', 'audio']);
expect('a song offers audio alone', v.partsOf({ audio: true, audioTracks: ['opus 160k'] }), ['audio']);
expect('an answer with nothing in it offers no choice', v.offersChoice({ qualities: [], videoTracks: [], audioTracks: [] }), false);
expect('the video formats', v.formatsOf(menus, 'video'), ['best', 'webm vp9', 'mp4 avc1']);
expect('the audio formats', v.formatsOf(menus, 'audio'), ['best', 'opus', 'aac']);
expect('Auto offers the caps', v.qualitiesOf(menus, 'video', 'best'), ['best', '2160p', '1080p', '720p']);
expect('a format offers its own tracks', v.qualitiesOf(menus, 'video', 'webm vp9'), ['2160p60hdr', '1080p60']);
expect('an audio format offers its bitrates', v.qualitiesOf(menus, 'audio', 'opus'), ['', '160', '70']);
expect('audio on Auto has no bitrate to pick', v.qualitiesOf(menus, 'audio', 'best'), []);

expect('the first pick is Auto', v.firstPick(menus), { part: 'video', format: 'best', quality: 'best' });
const capped = { part: 'video', format: 'best', quality: '1080p' };
expect('another format keeps a quality it has', v.withFormat(menus, { ...capped, format: 'mp4 avc1' }, 'webm vp9').quality, '1080p60');
expect('another format drops to the nearest quality below', v.withFormat(menus, capped, 'mp4 avc1'), {
  part: 'video',
  format: 'mp4 avc1',
  quality: '1080p',
});
expect('another format with nothing below takes its first', v.withFormat(menus, { ...capped, quality: '480p' }, 'webm vp9').quality, '2160p60hdr');

expect('Auto sends best', v.choiceOf({ part: 'video', format: 'best', quality: 'best' }), { variants: ['video'], quality: 'best' });
expect('a cap goes as the quality', v.choiceOf(capped), { variants: ['video'], quality: '1080p' });
expect('a track goes as the track', v.choiceOf({ part: 'video', format: 'webm vp9', quality: '1080p60' }), {
  variants: ['video'],
  quality: 'best',
  videoTrack: '1080p60 webm vp9',
});
expect('audio on Auto', v.choiceOf({ part: 'audio', format: 'best', quality: '' }), { variants: ['audio'] });
expect('an audio format at its best', v.choiceOf({ part: 'audio', format: 'opus', quality: '' }), {
  variants: ['audio'],
  audioFormat: 'opus',
});
expect('an audio track', v.choiceOf({ part: 'audio', format: 'opus', quality: '160' }), {
  variants: ['audio'],
  audioTrack: 'opus 160k',
});

expect('formats read as people know them', ['best', 'webm vp9', 'mp4 avc1', 'aac', 'mp3'].map((f) => v.formatLabel(f, 'Auto')), [
  'Auto',
  'WebM (VP9)',
  'MP4 (H.264)',
  'AAC (M4A)',
  'MP3',
]);
expect('one link is a video to look at', v.singleLink('  https://video.example/watch?v=1 \n'), 'https://video.example/watch?v=1');
expect('several links are not', v.singleLink('https://a.example/1\nhttps://b.example/2'), null);
expect('words are not', v.singleLink('look at this'), null);

// The picker.
const home = { id: 'home' };
let answer = null;
const asked = [];
const { VideoChoice, VideoPicker } = compile(join(here, 'src', 'components', 'VideoChoice.tsx'), {
  'react-native': { ActivityIndicator: 'ActivityIndicator', View: 'View', StyleSheet: { create: (s) => s } },
  '../api/videoLink': {
    fetchLinkMenus: async (_conn, url) => {
      asked.push(url);
      return answer;
    },
  },
  '../api/videoPick': v,
  '../i18n/I18nContext': { useT: () => ({ t: (key, vars) => (vars ? `${key} ${JSON.stringify(vars)}` : key) }) },
  '../theme/AppearanceContext': { useAppearance: () => ({ c: {} }) },
  '../theme/tokens': { TYPE: {} },
  './glim': { NotchCard: 'NotchCard', WellSelector: 'WellSelector' },
  './Text': { Text: 'Text' },
});

{
  mock.timers.enable({ apis: ['setTimeout'] });
  answer = menus;
  const choices = [];
  const onChoice = (c) => choices.push(c);
  let url = 'https://video.example/watch?v=1';
  const view = mount(() => VideoChoice({ conn: home, url, onChoice }));
  expect('nothing is asked while the field still moves', asked, []);
  mock.timers.tick(700);
  mock.timers.reset();
  let shown = await view.settle();
  expect('the link is looked at once it holds still', asked, [url]);
  const picker = shown.elements.find((e) => e.type === VideoPicker);
  expect('a video is offered its formats', picker?.props.menus, menus);
  view.unmount();

  const picked = mount(() => VideoPicker({ menus, onChoice }));
  shown = await picked.settle();
  const wells = () => shown.elements.filter((e) => e.type === 'WellSelector');
  expect('a video with sound offers its row, format and quality', wells().length, 3);
  expect('the picker starts on Auto', choices.at(-1), { variants: ['video'], quality: 'best' });
  wells()[1].props.onPick('webm vp9');
  shown = await picked.settle();
  expect('a format picked goes in with its best track', choices.at(-1), {
    variants: ['video'],
    quality: 'best',
    videoTrack: '2160p60hdr webm vp9',
  });
  expect('the qualities follow the format', wells()[2].props.options.map((o) => o.label), ['2160p60hdr', '1080p60']);
  wells()[0].props.onPick('audio');
  shown = await picked.settle();
  expect('audio on Auto has no bitrate to pick', wells().length, 2);
  expect('switching to audio sends audio', choices.at(-1), { variants: ['audio'] });
  picked.unmount();

  answer = null;
  url = 'https://files.example/a.zip';
  mock.timers.enable({ apis: ['setTimeout'] });
  const other = mount(() => VideoChoice({ conn: home, url, onChoice }));
  mock.timers.tick(700);
  mock.timers.reset();
  shown = await other.settle();
  expect('a link that is no video shows nothing', shown.elements, []);
  expect('and sends no choice', choices.at(-1), null);
  other.unmount();
}

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('ok: a video link is added with the format and quality picked for it');
