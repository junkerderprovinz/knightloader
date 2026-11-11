/**
 * The download button on a video's player (src/video.js, src/video-worker.js,
 * src/video-button.js), run against a stubbed `chrome` and a small stand-in
 * for the page.
 *
 *   1. Every form a video's page comes in sends the one address the video
 *      goes by, the playlist only when asked, and a page that plays no one
 *      video sends nothing.
 *   2. The sites are a short list of https addresses inside the optional
 *      access, none of them a static content script, and each is a site
 *      yt-dlp on the instance knows.
 *   3. A site's stored defaults become the choice a click sends, cleaned of
 *      anything the instance would refuse. What the instance says a video
 *      has rules out what it lacks and makes a pick exact for it, and is
 *      kept per site for the options page. A dragged button is stored as
 *      shares of its player, kept inside it, so it keeps its place when the
 *      player changes size. Auto-hide shows the button only with the pointer
 *      over the video or while it is in use.
 *   4. The worker registers the button only where it runs, sends only the
 *      video of the page that asked, to start at once with its choice, and
 *      tells the button whether it was sent, was held back or was there
 *      already. The menu lists what the instance says the video has, and
 *      what it offered is kept for the site.
 *   5. On the page, a click sends the site's defaults and the menu a pick of
 *      its own from what the video has, a success cheers with stars (a
 *      colour flash alone with reduced motion) and a refusal does not, the
 *      menu stays open while the player loses its video for a moment, the
 *      button takes the theme's colours, a drag places it and sends nothing,
 *      auto-hide and the keyboard decide when it shows, and the switches take
 *      it off an open
 *      tab at once and back. The button follows the page to another video
 *      and away from one, asks the instance once per video, sits in the
 *      element shown in fullscreen, picks the video that is on screen, and
 *      moves down past the controls a player keeps along the top of its
 *      video.
 *   6. The keyboard shortcut has the button in the active tab send the site's
 *      defaults, only on a video page of a site the button runs on, and its
 *      default opens none of Firefox's menus.
 *   7. The options card shows the shortcut, changes and resets it in Firefox
 *      within the shortcuts Firefox takes, and in Chromium opens the
 *      browser's shortcuts page.
 *
 * Run by CI and by hand: `node extension/check-video.mjs`
 */
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');
const manifest = JSON.parse(read('src', 'manifest.json'));
const problems = [];
const fail = (why) => problems.push(why);
const settle = () => new Promise((r) => setTimeout(r, 30));

function event() {
  const listeners = [];
  return { addListener: (f) => listeners.push(f), listeners };
}

/** One store with chrome.storage's change events, shared by every context
 *  that uses it, as the extension's pages and content scripts share theirs. */
function makeStore(initial = {}) {
  const data = { ...initial };
  const onChanged = event();
  return {
    data,
    onChanged,
    local: {
      get: async (keys) => {
        const list = keys == null ? Object.keys(data) : Array.isArray(keys) ? keys : [keys];
        return Object.fromEntries(list.filter((k) => k in data).map((k) => [k, data[k]]));
      },
      set: async (o) => {
        const changes = Object.fromEntries(Object.entries(o).map(([k, v]) => [k, { oldValue: data[k], newValue: v }]));
        Object.assign(data, o);
        for (const f of onChanged.listeners) await f(changes, 'local');
      },
      remove: async () => {},
    },
  };
}

function makeChrome(store, { granted = ['<all_urls>'] } = {}) {
  const calls = { registered: [], unregistered: [], injected: [], tabs: [] };
  const live = new Map();
  const chrome = {
    runtime: {
      onInstalled: event(), onMessage: event(), onStartup: event(), onConnect: event(),
      openOptionsPage() {}, getManifest: () => manifest,
      getURL: (p) => `chrome-extension://test/${p}`,
      sendMessage: async () => undefined,
    },
    storage: {
      onChanged: store.onChanged,
      local: store.local,
      session: { get: async () => ({}), set: async () => {}, remove: async () => {} },
    },
    permissions: {
      onAdded: event(), onRemoved: event(),
      contains: async (p) => (p.origins ?? []).every((o) => granted.includes('<all_urls>') || granted.includes(o)) && (p.permissions ?? []).every((x) => granted.includes(x)),
      request: async () => false,
      remove: async () => true,
    },
    action: { getUserSettings: async () => ({ isOnToolbar: true }), setBadgeText() {}, setBadgeBackgroundColor() {}, setTitle() {}, openPopup: async () => {} },
    scripting: {
      getRegisteredContentScripts: async () => [...live.values()],
      registerContentScripts: async (s) => {
        for (const x of s) {
          if (live.has(x.id)) throw new Error(`Duplicate script ID '${x.id}'`);
          live.set(x.id, x);
          calls.registered.push(x);
        }
      },
      unregisterContentScripts: async ({ ids }) => { for (const id of ids) live.delete(id); calls.unregistered.push(...ids); },
      executeScript: async (o) => {
        if (o.func) return [{ frameId: 0, result: false }];
        calls.injected.push(o);
        return [];
      },
    },
    declarativeNetRequest: { updateEnabledRulesets: async () => {} },
    tabs: {
      query: async ({ url }) => [{ id: 7, url: url[0] }],
      create: async (o) => { calls.tabs.push(o.url); },
      onRemoved: event(),
    },
  };
  return { chrome, calls, live };
}

// 1. The address each form of a page sends.
{
  const ctx = vm.createContext({ URL, chrome: {} });
  vm.runInContext(read('src', 'video.js'), ctx, { filename: 'video.js' });
  const page = vm.runInContext('videoPage', ctx);
  const ID = 'aqz-KE-bpKQ';
  const cases = [
    [`https://www.youtube.com/watch?v=${ID}&t=42s`, `https://www.youtube.com/watch?v=${ID}`, ''],
    [`https://m.youtube.com/watch?feature=share&v=${ID}`, `https://www.youtube.com/watch?v=${ID}`, ''],
    [`https://www.youtube.com/shorts/${ID}`, `https://www.youtube.com/watch?v=${ID}`, ''],
    [`https://www.youtube.com/live/${ID}?si=x`, `https://www.youtube.com/watch?v=${ID}`, ''],
    [`https://www.youtube-nocookie.com/embed/${ID}?autoplay=1`, `https://www.youtube.com/watch?v=${ID}`, ''],
    [`https://www.youtube.com/watch?v=${ID}&list=PLbpi6ZahtOH6Ar_3GPy3workIMw9vB9wB&index=3`, `https://www.youtube.com/watch?v=${ID}`, 'https://www.youtube.com/playlist?list=PLbpi6ZahtOH6Ar_3GPy3workIMw9vB9wB'],
    [`https://www.youtube.com/watch?v=${ID}&list=WL`, `https://www.youtube.com/watch?v=${ID}`, ''],
    ['https://vimeo.com/1084537', 'https://vimeo.com/1084537', ''],
    ['https://vimeo.com/1084537/8a2b3c4d5e', 'https://vimeo.com/1084537/8a2b3c4d5e', ''],
    ['https://vimeo.com/channels/staffpicks/1084537', 'https://vimeo.com/1084537', ''],
    ['https://www.dailymotion.com/video/x8abcd1?playlist=x6hynp', 'https://www.dailymotion.com/video/x8abcd1', ''],
    ['https://www.twitch.tv/videos/2012345678?t=1h', 'https://www.twitch.tv/videos/2012345678', ''],
    ['https://www.twitch.tv/somechannel/clip/FunnyClipSlug-AbC_123', 'https://clips.twitch.tv/FunnyClipSlug-AbC_123', ''],
    ['https://clips.twitch.tv/FunnyClipSlug-AbC_123', 'https://clips.twitch.tv/FunnyClipSlug-AbC_123', ''],
    ['https://www.bilibili.com/video/BV1GJ411x7h7/?spm_id_from=333&p=1', 'https://www.bilibili.com/video/BV1GJ411x7h7', ''],
    ['https://www.bilibili.com/video/BV1GJ411x7h7?p=3', 'https://www.bilibili.com/video/BV1GJ411x7h7?p=3', ''],
  ];
  for (const [href, url, playlist] of cases) {
    const got = page(href);
    if (got?.url !== url) fail(`${href} sends ${got?.url}, want ${url}`);
    else if (got.playlist !== playlist) fail(`${href} offers the playlist ${got.playlist || 'none'}, want ${playlist || 'none'}`);
  }
  for (const href of [
    'https://www.youtube.com/',
    'https://www.youtube.com/results?search_query=blender',
    'https://www.youtube.com/watch?v=short',
    'https://www.youtube.com/@BlenderOfficial',
    'http://www.youtube.com/watch?v=aqz-KE-bpKQ',
    'https://www.youtube.com.example.org/watch?v=aqz-KE-bpKQ',
    'https://music.youtube.com/watch?v=aqz-KE-bpKQ',
    'https://vimeo.com/channels/staffpicks',
    'https://www.twitch.tv/somechannel',
    'https://clips.twitch.tv/embed',
    'https://www.bilibili.com/',
    'not a url',
  ]) {
    if (page(href)) fail(`${href} sends ${page(href).url}, want nothing`);
  }
}

// 2. The sites.
{
  const ctx = vm.createContext({ URL, chrome: {} });
  vm.runInContext(read('src', 'video.js'), ctx, { filename: 'video.js' });
  const sites = vm.runInContext('VIDEO_SITES', ctx);
  const hostsGo = read('..', 'internal', 'resolver', 'ytdlp', 'hosts.go');
  const optional = manifest.optional_host_permissions ?? [];
  if (sites.length > 8) fail(`the button runs on ${sites.length} sites, want a short list`);
  for (const s of sites) {
    for (const o of s.origins) {
      if (!/^https:\/\/[a-z0-9.-]+\/\*$/.test(o)) fail(`${s.id} asks for ${o}, want one https host`);
      if (!optional.includes('<all_urls>') && !optional.includes(o)) fail(`${o} is outside the manifest's optional access`);
      const domain = new URL(o.slice(0, -1)).hostname.split('.').slice(-2).join('.');
      if (!hostsGo.includes(`"${domain}"`)) fail(`${s.id} (${domain}) is not a site internal/resolver/ytdlp/hosts.go knows`);
    }
  }
  const staticScripts = (manifest.content_scripts ?? []).flatMap((c) => c.js ?? []);
  if (staticScripts.some((f) => f.startsWith('video'))) fail('the button is a static content script, which would need its access at install');
  if ((manifest.host_permissions ?? []).length) fail(`the manifest asks for host access at install: ${manifest.host_permissions.join(', ')}`);
}

/** Loads the background scripts as Firefox does (manifest background.scripts). */
function loadBackground(chrome) {
  const ctx = vm.createContext({
    chrome, console: { log() {}, warn() {}, error() {} }, setTimeout, clearTimeout,
    crypto: globalThis.crypto, TextEncoder, TextDecoder, URL, navigator: { language: 'en-US', languages: ['en-US'], userAgent: 'test' },
    btoa: (s) => Buffer.from(s, 'binary').toString('base64'), atob: (s) => Buffer.from(s, 'base64').toString('binary'),
    WebSocket: class {}, indexedDB: { open: () => ({}) },
    fetch: async (url) => ({ text: async () => (String(url).endsWith('logo.svg') ? '<svg viewBox="0 0 587 901.8"></svg>' : '') }),
  });
  for (const f of manifest.background.scripts) vm.runInContext(read('src', f), ctx, { filename: f });
  return ctx;
}

// 3. What a click downloads, where the button sits and when it shows.
{
  const ctx = vm.createContext({ URL, chrome: {} });
  vm.runInContext(read('src', 'video.js'), ctx, { filename: 'video.js' });
  const run = (src) => JSON.parse(JSON.stringify(vm.runInContext(src, ctx)));
  const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);

  ctx.stored = {
    videoDefaults: {
      youtube: { parts: ['subtitle', 'video', 'chapters'], quality: '720p', audio: 'opus', subLangs: ['de', 'en', 'd e'], subFormat: 'vtt' },
      vimeo: { parts: [], quality: '1080p' },
      twitch: { parts: ['audio'], quality: '8k', audio: 'wav', subFormat: 'v t t' },
    },
  };
  const choice = (site) => run(`videoChoice(videoDefaultsFor(stored, '${site}'))`);
  const best = { variants: ['video'], quality: 'best', audioFormat: 'best', subtitleLangs: [], subtitleAuto: true, subtitleFormat: 'srt' };
  const youtube = { variants: ['video', 'subtitle'], quality: '720p', audioFormat: 'opus', subtitleLangs: ['de', 'en'], subtitleAuto: true, subtitleFormat: 'vtt' };
  if (!same(choice('youtube'), youtube)) fail(`YouTube's defaults send ${JSON.stringify(choice('youtube'))}, want the video up to 720p, opus preferred and German then English subtitles as vtt`);
  if (!same(choice('vimeo'), best)) fail(`a site set to download no part sends ${JSON.stringify(choice('vimeo'))}, want the best video`);
  if (!same(choice('dailymotion'), best)) fail(`a site with nothing set sends ${JSON.stringify(choice('dailymotion'))}, want the best video`);
  if (!same(choice('twitch'), { ...best, variants: ['audio'] })) fail(`a quality, an audio codec and a subtitle format the instance has not got are sent as ${JSON.stringify(choice('twitch'))}, want the best`);

  // What one video has decides what the menu offers and where it starts.
  ctx.answer = {
    qualities: ['1080p', '720p'], audio: true,
    videoTracks: ['1080p60 webm vp9', '1080p mp4 avc1', '720p mp4 avc1', 'rm -rf'],
    audioTracks: ['opus 160k', 'aac 128k'], audioLangs: [{ code: 'de', dubbed: true }, { code: 'en' }],
    subtitles: ['de'], autoCaptions: ['en-orig', 'fr', 'de', 'x y'], subtitleFormats: ['srv3', 'vtt'],
    thumbnail: { width: 1280, height: 720, ext: 'webp' }, description: false,
  };
  vm.runInContext('offer = videoOffer(answer)', ctx);
  if (run('offer.videoTracks').length !== 3 || run('offer.autoCaptions').includes('x y')) fail('the offer keeps tracks or languages the instance would refuse');
  if (run(`VIDEO_PARTS.filter((p) => videoHas(offer, p))`).join() !== 'video,audio,subtitle,thumbnail') fail('a video without a description still offers its text');
  ctx.want = { parts: ['video', 'audio', 'subtitle', 'description'], quality: '720p', audio: 'aac', subLangs: ['en', 'de'], subFormat: 'vtt' };
  const exact = run('videoResolve(videoPick(want), offer)');
  const wantExact = { parts: ['video', 'audio', 'subtitle'], quality: '720p', videoFormat: 'best', audio: 'aac', audioRate: 'best', audioLang: 'en', subLangs: ['en-orig', 'de'], subAuto: true, subFormat: 'vtt' };
  for (const [k, v] of Object.entries(wantExact)) {
    if (!same(exact[k], v)) fail(`made exact for the video, the pick's ${k} is ${JSON.stringify(exact[k])}, want ${JSON.stringify(v)}`);
  }
  if ('videoTrack' in exact || 'audioTrack' in exact) fail(`the menu starts from a track, ${exact.videoTrack ?? exact.audioTrack}, rather than the quality and format the defaults name`);
  ctx.want = { parts: ['video', 'subtitle'], quality: 'best', audio: 'best', subLangs: [], subFormat: 'best' };
  const bestOf = run('videoResolve(videoPick(want), offer)');
  if (bestOf.quality !== 'best' || bestOf.videoFormat !== 'best' || bestOf.audio !== 'best' || bestOf.audioRate !== 'best' || !same(bestOf.subLangs, ['de']) || bestOf.subAuto || bestOf.subFormat !== 'srt') {
    fail(`with nothing preferred the pick becomes ${JSON.stringify(bestOf)}, want Best everywhere, the subtitles by hand and SRT`);
  }
  if (!same(run(`videoSubtitleChoices(offer, videoPick({ parts: ['subtitle'], subLangs: ['fr'] })).auto`), ['en-orig', 'fr'])) fail('the menu offers generated subtitles beyond the original and the wanted languages');
  const sentExact = run(`videoChoice(videoPick({ ...videoExact({ ...videoResolve(videoPick(want), offer), audioRate: '160k' }, offer), videoTrack: 'evil;rm', audioLang: 'de' }))`);
  if ('videoTrack' in sentExact || sentExact.audioTrack !== 'opus 160k' || sentExact.audioLang !== 'de' || sentExact.subtitleAuto !== false) fail(`an exact pick is sent as ${JSON.stringify(sentExact)}`);
  vm.runInContext(`old = videoOffer({ qualities: ['720p'], audio: true })`, ctx);
  if (run('old.known') || run(`VIDEO_PARTS.filter((p) => videoHas(old, p))`).length !== 5) fail('an older instance that only lists resolutions rules parts out');

  // Subtitles come as SRT, WebVTT or TTML, whatever else the host lists, and
  // have no Best: a default of Best or of YouTube's own srv and json3 reads
  // as SRT, which is also what the instance is sent.
  if (!same(run('VIDEO_SUB_FORMATS'), ['srt', 'vtt', 'ttml'])) fail(`the subtitle formats are ${run('VIDEO_SUB_FORMATS')}, want srt, vtt and ttml`);
  for (const stored of ['best', 'srv3', 'json3', undefined]) {
    ctx.raw = { parts: ['subtitle'], subFormat: stored };
    if (run('videoPick(raw).subFormat') !== 'srt') fail(`a subtitle default stored as ${stored} reads as ${run('videoPick(raw).subFormat')}, want srt`);
    if (run('videoChoice(videoPick(raw)).subtitleFormat') !== 'srt') fail(`a subtitle default stored as ${stored} is sent as ${run('videoChoice(videoPick(raw)).subtitleFormat')}, want srt`);
  }

  // A thumbnail comes in a format the host has, JPEG where it has that and
  // otherwise its first. Best is no choice.
  for (const [want, formats, got] of [
    ['best', ['webp', 'jpg'], 'jpg'],
    ['png', ['webp', 'jpg'], 'jpg'],
    ['webp', ['webp', 'jpg'], 'webp'],
    ['jpg', ['webp'], 'webp'],
    [undefined, ['png', 'webp'], 'png'],
  ]) {
    const picked = vm.runInContext(`videoThumbFormat(${JSON.stringify(want)}, ${JSON.stringify(formats)})`, ctx);
    if (picked !== got) fail(`a thumbnail wanted as ${want} among ${formats} is ${picked}, want ${got}`);
  }
  if (run(`videoResolve(videoPick({ parts: ['thumbnail'], thumbFormat: 'best' }), offer).thumbFormat`) !== 'webp') fail('a video whose thumbnail is webp does not start the menu at WebP');

  // An instance that converts thumbnails lists the formats it writes, and
  // the pick goes to it as thumbnailFormat: JPEG unless another is wanted.
  // One that only names the served picture is sent no format, so it keeps
  // the picture as it is.
  ctx.converts = run(`videoOffer({ ...answer, thumbnail: { width: 1280, height: 720, ext: 'avif', formats: ['jpg', 'png', 'webp', 'avif', 'x y'] } })`);
  if (!same(ctx.converts.thumbnail.formats, ['jpg', 'png', 'webp', 'avif'])) fail(`the thumbnail formats offered are ${ctx.converts.thumbnail.formats}, want those the instance writes`);
  if ('formats' in run('offer.thumbnail')) fail('a thumbnail from an instance that converts nothing is offered in formats');
  const thumbSent = (raw, offerName) => vm.runInContext(`videoChoice(videoPick(videoExact(videoResolve(videoPick(${JSON.stringify(raw)}), ${offerName}), ${offerName}))).thumbnailFormat`, ctx);
  for (const [raw, offerName, want] of [
    [{ parts: ['thumbnail'] }, 'converts', 'jpg'],
    [{ parts: ['thumbnail'], thumbFormat: 'best' }, 'converts', 'jpg'],
    [{ parts: ['thumbnail'], thumbFormat: 'png' }, 'converts', 'png'],
    [{ parts: ['thumbnail'], thumbFormat: 'avif' }, 'converts', 'avif'],
    [{ parts: ['thumbnail'], thumbFormat: 'png' }, 'offer', undefined],
    [{ parts: ['video'], thumbFormat: 'png' }, 'converts', undefined],
  ]) {
    if (thumbSent(raw, offerName) !== want) fail(`a thumbnail pick ${JSON.stringify(raw)} on ${offerName} sends thumbnailFormat ${thumbSent(raw, offerName)}, want ${want}`);
  }

  // Formats and codecs read as they are written, whatever yt-dlp calls them.
  const formatNames = {
    avc1: 'H.264', hevc: 'H.265', vp9: 'VP9', vp8: 'VP8', av1: 'AV1', mp4: 'MP4', webm: 'WebM', aac: 'AAC', mp4a: 'AAC', opus: 'Opus',
    vorbis: 'Vorbis', mp3: 'MP3', flac: 'FLAC', alac: 'ALAC', m4a: 'M4A', srt: 'SRT', vtt: 'WebVTT', ttml: 'TTML',
    srv1: 'SRV1', srv2: 'SRV2', srv3: 'SRV3', json3: 'JSON3', webp: 'WebP', jpg: 'JPEG', png: 'PNG',
  };
  for (const [token, want] of Object.entries(formatNames)) {
    const got = vm.runInContext(`videoFormatName('${token}')`, ctx);
    if (got !== want) fail(`${token} reads ${got}, want ${want}`);
  }

  // A video's quality and format are two rows: the qualities high to low,
  // each once and with a frame rate only where one resolution comes at two,
  // the formats in one fixed order, mp4 before webm and avc1 before vp9
  // before av1, and the audio's bitrates high to low beside its codecs.
  ctx.yt = run(`videoOffer(${JSON.stringify({
    qualities: ['2160p', '1080p', '720p'], audio: true,
    videoTracks: ['2160p webm vp9', '1080p60 mp4 avc1', '1080p mp4 avc1', '1080p mp4 vp9', '1080p webm vp9', '1080p mp4 av1', '720p60 mp4 avc1'],
    audioTracks: ['aac 130k', 'opus 124k', 'opus 62k', 'aac 49k', 'opus 49k'], audioLangs: [], subtitles: [], autoCaptions: [], subtitleFormats: [],
    thumbnail: null, description: true,
  })})`);
  const rows = run('videoMenuRows(yt)');
  if (!same(rows.qualities, ['2160p', '1080p60', '1080p', '720p60'])) fail(`the quality row lists ${rows.qualities}, want each resolution once, highest first`);
  const names = run('videoMenuRows(yt).qualities.map((q) => videoQualityName(q, videoMenuRows(yt).qualities))');
  if (!same(names, ['2160p', '1080p60', '1080p', '720p'])) fail(`the qualities read ${names}, want a frame rate only where 1080p comes at two`);
  if (!same(rows.formats, ['mp4 avc1', 'mp4 vp9', 'mp4 av1', 'webm vp9'])) fail(`the format row lists ${rows.formats}, want mp4 before webm and avc1, vp9, av1`);
  const offered = (q) => run(`videoMenuRows(yt).formats.filter((f) => videoTrackOffered(yt, '${q}', f))`);
  if (!same(offered('best'), rows.formats)) fail(`at the best quality the formats offered are ${offered('best')}, want every one`);
  if (!same(offered('720p60'), ['mp4 avc1'])) fail(`at 720p the formats offered are ${offered('720p60')}, want mp4 avc1 alone`);
  if (!same(offered('1080p'), ['mp4 avc1', 'mp4 vp9', 'mp4 av1', 'webm vp9'])) fail(`at 1080p the formats offered are ${offered('1080p')}`);
  // Where a pick leaves the format to the instance, the menu shows the one
  // the best track at that quality has, and for the audio the codec of the
  // best track at that bitrate.
  for (const [quality, want] of [['best', 'webm vp9'], ['1080p', 'mp4 av1'], ['720p60', 'mp4 avc1']]) {
    if (run(`videoBestFormat(yt, '${quality}')`) !== want) fail(`at ${quality} the best track's format reads ${run(`videoBestFormat(yt, '${quality}')`)}, want ${want}`);
  }
  for (const [rate, want] of [['best', 'opus'], ['130k', 'aac'], ['49k', 'opus']]) {
    if (run(`audioBestCodec(yt, '${rate}')`) !== want) fail(`at ${rate} the best audio's codec reads ${run(`audioBestCodec(yt, '${rate}')`)}, want ${want}`);
  }
  const audioRows = run('audioMenuRows(yt)');
  if (!same(audioRows, { rates: ['130k', '124k', '62k', '49k'], codecs: ['aac', 'opus'] })) fail(`the audio rows list ${JSON.stringify(audioRows)}, want the bitrates high to low and aac, opus`);
  if (!run(`audioTrackOffered(yt, '49k', 'opus')`) || run(`audioTrackOffered(yt, '130k', 'opus')`)) fail('the audio formats offered do not follow the bitrate picked');

  // Quality and format together name the track the instance fetches; Best in
  // one of them leaves the instance its cap or its codec, and Best in both
  // sends no track at all.
  const sends = (sel) => run(`videoExact({ ...videoResolve(videoPick({ parts: ['video', 'audio'] }), yt), ...${JSON.stringify(sel)} }, yt)`);
  for (const [sel, want] of [
    [{}, { quality: 'best', audio: 'best' }],
    [{ quality: '2160p' }, { quality: '2160p', audio: 'best' }],
    [{ quality: '1080p' }, { quality: 'best', videoTrack: '1080p mp4 av1', audio: 'best' }],
    [{ quality: '720p60' }, { quality: '720p', audio: 'best' }],
    [{ videoFormat: 'webm vp9' }, { quality: 'best', videoTrack: '2160p webm vp9', audio: 'best' }],
    [{ quality: '1080p', videoFormat: 'mp4 av1' }, { quality: 'best', videoTrack: '1080p mp4 av1', audio: 'best' }],
    [{ audio: 'opus' }, { quality: 'best', audio: 'opus' }],
    [{ audioRate: '49k' }, { quality: 'best', audio: 'best', audioTrack: 'opus 49k' }],
    [{ audioRate: '49k', audio: 'opus' }, { quality: 'best', audio: 'best', audioTrack: 'opus 49k' }],
  ]) {
    const got = sends(sel);
    const picked = { quality: got.quality, audio: got.audio, ...(got.videoTrack ? { videoTrack: got.videoTrack } : {}), ...(got.audioTrack ? { audioTrack: got.audioTrack } : {}) };
    if (!same(Object.entries(picked).sort(), Object.entries(want).sort())) fail(`picked ${JSON.stringify(sel)}, the send is ${JSON.stringify(picked)}, want ${JSON.stringify(want)}`);
    if ('videoFormat' in got || 'audioRate' in got) fail(`the send carries the menu's own words: ${JSON.stringify(got)}`);
  }

  // Each quality and bitrate says how large the pick it makes downloads,
  // with the format picked beside it, and nothing where the host gave no
  // size.
  ctx.sized = run(`videoOffer(${JSON.stringify({
    qualities: ['2160p', '1080p', '720p'], audio: true,
    videoTracks: ['2160p webm vp9', '1080p60 mp4 avc1', '1080p mp4 avc1', '1080p webm vp9', '720p60 mp4 avc1'],
    audioTracks: ['aac 130k', 'opus 124k', 'aac 49k'], audioLangs: [], subtitles: [], autoCaptions: [], subtitleFormats: [], thumbnail: null, description: true,
    videoSizes: { best: 900000000, '2160p': 900000000, '1080p mp4 avc1': 412000000, '1080p webm vp9': 380000000, '720p': 90000000, 'rm -rf': 1, '1080p60 mp4 avc1': -5, '2160p webm vp9': 'big' },
    audioSizes: { best: 3400000, 'aac 49k': 1200000, opus: 3000000, 'x y z': 2 },
  })})`);
  if (!same(Object.keys(run('sized.videoSizes')), ['best', '2160p', '1080p mp4 avc1', '1080p webm vp9', '720p']) || !same(Object.keys(run('sized.audioSizes')), ['best', 'aac 49k', 'opus'])) {
    fail(`the offer keeps the sizes ${JSON.stringify(run('sized.videoSizes'))} and ${JSON.stringify(run('sized.audioSizes'))}, want those of picks the instance knows, in bytes`);
  }
  const sizeOf = (sel, part) => vm.runInContext(`videoPickSize({ ...videoResolve(videoPick({ parts: ['video', 'audio'] }), sized), ...${JSON.stringify(sel)} }, sized, '${part}')`, ctx);
  for (const [sel, part, want] of [
    [{}, 'video', 900000000],
    [{ quality: '1080p' }, 'video', 380000000],
    [{ quality: '1080p', videoFormat: 'webm vp9' }, 'video', 380000000],
    [{ quality: '720p60' }, 'video', 90000000],
    [{ quality: '1080p60' }, 'video', undefined],
    [{}, 'audio', 3400000],
    [{ audioRate: '49k' }, 'audio', 1200000],
    [{ audio: 'opus' }, 'audio', 3000000],
    [{ audioRate: '130k' }, 'audio', undefined],
  ]) {
    if (sizeOf(sel, part) !== want) fail(`the ${part} pick ${JSON.stringify(sel)} says ${sizeOf(sel, part)} bytes, want ${want}`);
  }
  // The highest frame rate of a resolution is what its cap downloads, so at
  // the top resolution it weighs what Best weighs; the lower rate is a track,
  // in the codec yt-dlp would prefer.
  ctx.twoRates = run(`videoOffer(${JSON.stringify({
    qualities: ['2160p'], audio: true, videoTracks: ['2160p60 mp4 avc1', '2160p60 mp4 av1', '2160p mp4 avc1', '2160p webm vp9'],
    audioTracks: [], audioLangs: [], subtitles: [], autoCaptions: [], subtitleFormats: [], thumbnail: null, description: true,
    videoSizes: { best: 900000000, '2160p': 900000000, '2160p60 mp4 avc1': 700000000, '2160p mp4 avc1': 500000000, '2160p webm vp9': 450000000 }, audioSizes: {},
  })})`);
  const twoRates = (q) => vm.runInContext(`videoPickSize({ ...videoResolve(videoPick({ parts: ['video'] }), twoRates), quality: '${q}' }, twoRates, 'video')`, ctx);
  if (twoRates('2160p60') !== twoRates('best')) fail(`at the top resolution the chip weighs ${twoRates('2160p60')} bytes and Best ${twoRates('best')}, want the same`);
  if (twoRates('2160p') !== 450000000) fail(`the lower frame rate weighs ${twoRates('2160p')} bytes, want its vp9 track's, the codec yt-dlp prefers`);
  // The download button weighs the whole pick, and only when every ticked
  // part has a size: a partial sum would read as the total.
  const totalOf = (sel) => vm.runInContext(`videoPickTotal({ ...videoResolve(videoPick({ parts: ['video', 'audio'] }), sized), ...${JSON.stringify(sel)} }, sized)`, ctx);
  for (const [sel, want] of [
    [{}, 903400000],
    [{ parts: ['video'] }, 900000000],
    [{ quality: '1080p', audioRate: '49k' }, 381200000],
    [{ audioRate: '130k' }, undefined],
    [{ parts: ['video', 'audio', 'description'] }, undefined],
    [{ parts: ['thumbnail'] }, undefined],
  ]) {
    if (totalOf(sel) !== want) fail(`the pick ${JSON.stringify(sel)} weighs ${totalOf(sel)} bytes in all, want ${want}`);
  }
  for (const [bytes, lang, want] of [[412000000, 'en', '412 MB'], [3400000, 'en', '3.4 MB'], [1234000000, 'de', '1,2 GB'], [20000, 'en', '0.1 MB']]) {
    const got = run(`videoSize(${bytes}, '${lang}')`).replace(/ /g, ' ');
    if (got !== want) fail(`${bytes} bytes read ${got} in ${lang}, want ${want}`);
  }

  // The defaults start the menu as they are: Best stays Best, a quality the
  // video lacks becomes the nearest below it, a format the video lacks at
  // that quality falls back to Best, and a bitrate to the nearest one.
  const starts = (d) => {
    const sel = run(`videoResolve(videoPick({ parts: ['video', 'audio'], ...${JSON.stringify(d)} }), yt)`);
    return [sel.quality, sel.videoFormat, sel.audio, sel.audioRate].join(' / ');
  };
  for (const [d, want] of [
    [{}, 'best / best / best / best'],
    [{ quality: '720p', videoFormat: 'webm vp9' }, '720p60 / best / best / best'],
    [{ quality: '1440p', videoFormat: 'webm vp9' }, '1080p60 / best / best / best'],
    [{ quality: '1080p', videoFormat: 'mp4 vp9', audio: 'opus', audioRate: '128k' }, '1080p / mp4 vp9 / opus / 124k'],
    [{ audioRate: '127k' }, 'best / best / best / 130k'],
  ]) {
    if (starts(d) !== want) fail(`the defaults ${JSON.stringify(d)} start the menu at ${starts(d)}, want ${want}`);
  }

  // What a site stored before quality and format were two choices still
  // loads, and the new choices are cleaned like the old ones.
  ctx.stored = { videoDefaults: { youtube: { parts: ['video', 'audio'], quality: '720p', audio: 'opus', subLangs: [], subFormat: 'best' } } };
  const older = run(`videoDefaultsFor(stored, 'youtube')`);
  if (older.quality !== '720p' || older.audio !== 'opus' || older.videoFormat !== 'best' || older.audioRate !== 'best') fail(`defaults stored by an older build load as ${JSON.stringify(older)}`);
  ctx.stored = { videoDefaults: { youtube: { parts: ['video'], videoFormat: 'mp4 avc1', audioRate: '128k' }, vimeo: { parts: ['video'], videoFormat: 'mp4;rm', audioRate: 'loud' } } };
  if (run(`videoDefaultsFor(stored, 'youtube').videoFormat`) !== 'mp4 avc1' || run(`videoDefaultsFor(stored, 'youtube').audioRate`) !== '128k') fail('a stored format and bitrate are not kept');
  if (run(`videoDefaultsFor(stored, 'vimeo').videoFormat`) !== 'best' || run(`videoDefaultsFor(stored, 'vimeo').audioRate`) !== 'best') fail('a stored format or bitrate the instance would refuse is kept');

  const seen = run('videoSeenWith(undefined, offer)');
  const seenWant = { qualities: ['1080p', '720p'], audio: ['aac', 'opus'], subLangs: ['de', 'en'], videoFormats: ['mp4 avc1', 'webm vp9'], audioRates: ['160k', '128k'], thumbFormats: ['webp'] };
  if (!same(seen, seenWant)) fail(`a site is remembered as offering ${JSON.stringify(seen)}`);
  const seenMore = run('videoSeenWith(videoSeenWith(undefined, offer), yt)');
  if (!same(seenMore.videoFormats, ['mp4 avc1', 'mp4 vp9', 'mp4 av1', 'webm vp9']) || !same(seenMore.audioRates, ['160k', '130k', '128k', '124k', '62k', '49k'])) fail(`two videos of a site are remembered as offering ${JSON.stringify(seenMore)}`);
  ctx.stored = { videoSeen: { youtube: { qualities: ['480p', '2160p', '9999p'], audio: ['wav', 'opus'], subLangs: ['de', 'x y'], subFormats: ['vtt'], videoFormats: ['webm vp9', 'mp4;rm'], audioRates: ['128k', 'x'], thumbFormats: ['webp', 'x y'] } } };
  if (!same(run(`videoSiteSeen(stored, 'youtube')`), { qualities: ['2160p', '480p'], audio: ['opus'], subLangs: ['de'], videoFormats: ['webm vp9'], audioRates: ['128k'], thumbFormats: ['webp'] })) fail('the options offer a site something it has not offered');
  if (!same(run(`videoSiteSeen(stored, 'vimeo')`), { qualities: [], audio: [], subLangs: [], videoFormats: [], audioRates: [], thumbFormats: [] })) fail('the options offer choices for a site that has offered none');

  const rect = { left: 100, top: 50, width: 800, height: 450 };
  const size = { width: 160, height: 32 };
  ctx.rect = rect;
  ctx.size = size;
  if (!same(run('videoSpotOf({ x: 500, y: 275 }, rect, size)'), { x: 0.5, y: 0.5 })) fail('a button dropped in the middle of the player is not stored as half its width and height');
  const corner = run('videoSpotOf({ x: 0, y: 1000 }, rect, size)');
  if (!same(corner, { x: 0.1, y: 0.9644 })) fail(`a button dropped past the player's corner is stored at ${JSON.stringify(corner)}, want it kept inside at 0.1, 0.9644`);
  ctx.spot = corner;
  ctx.full = { left: 0, top: 0, width: 1600, height: 900 };
  const scaled = run('videoSpotPoint(spot, full, size)');
  if (Math.round(scaled.x) !== 160 || Math.round(scaled.y) !== 868) fail(`in a player twice the size the button sits at ${scaled.x}, ${scaled.y}, want the same share of it at 160, 868`);
  if (!same(run('videoSpotPoint({ x: 0, y: 0 }, rect, size)'), { x: 180, y: 66 })) fail('a spot at the edge puts part of the button outside the player');
  if (!same(run(`videoSpotPoint({ x: 0.9, y: 0.1 }, { left: 0, top: 0, width: 100, height: 20 }, size)`), { x: 50, y: 10 })) fail('a player smaller than the button does not get it in its middle');
  for (const bad of [{ x: 2, y: 0.5 }, { x: '0.5', y: 0.5 }, { y: 0.5 }, null]) {
    ctx.stored = { videoButtonSpot: { youtube: bad } };
    if (run(`videoSpotFor(stored, 'youtube')`) !== null) fail(`a stored spot of ${JSON.stringify(bad)} is used`);
  }

  const shows = (s) => vm.runInContext('videoButtonShows', ctx)(s);
  if (!shows({ autoHide: false })) fail('without auto-hide the button does not show on its own');
  if (shows({ autoHide: true })) fail('with auto-hide the button shows with the pointer away from the video');
  if (!shows({ autoHide: true, pointerOver: true })) fail('with auto-hide the button does not show with the pointer over the video');
  for (const k of ['hovering', 'focused', 'menuOpen', 'dragging', 'busy']) {
    if (!shows({ autoHide: true, [k]: true })) fail(`with auto-hide the button hides while ${k}`);
  }
}

// 4. The worker.
{
  const store = makeStore();
  const { chrome, calls, live } = makeChrome(store);
  const ctx = loadBackground(chrome);
  await vm.runInContext('applyVideoButton', ctx)();
  const want = vm.runInContext('VIDEO_SITES', ctx).flatMap((s) => s.origins);
  const reg = live.get('video-button');
  if (!reg) fail('with the access granted and nothing stored, the button is not registered');
  else {
    if (reg.matches.join('|') !== want.join('|')) fail(`the button is registered on ${reg.matches.join(', ')}, want the sites' own addresses`);
    if (!reg.allFrames) fail('the button is not registered in frames, so an embedded player never gets it');
  }
  if (!calls.injected.some((o) => o.files?.includes('video-button.js'))) fail('a tab that was open before the button went on does not get it');

  await store.local.set({ videoButtonHidden: ['vimeo'] });
  await settle();
  if (live.get('video-button')?.matches.some((m) => m.includes('vimeo'))) fail('the button stays registered on a site it was hidden on');
  await store.local.set({ videoButton: false });
  await settle();
  if (live.has('video-button')) fail('switched off, the button stays registered');
  await store.local.set({ videoButton: true, videoButtonHidden: [] });
  await settle();

  const labels = await vm.runInContext('videoLabels', ctx)();
  if (!labels.mark?.startsWith('<svg')) fail('the button gets no mark to show');
  if (Object.keys(labels.parts ?? {}).join() !== 'video,audio,subtitle,thumbnail,description') fail('the button gets no name for every part');

  const sent = [];
  let answer = { status: 200, body: '{"tasks":[{"id":"a"}],"listed":[]}' };
  const withGroup = ctx.withGroup;
  ctx.withGroup = async (work) => work({
    siblings: [{ instanceId: 'inst-1' }],
    call: async (target, method, path, body) => { sent.push({ target, method, path, body: body ? JSON.parse(body) : undefined }); return answer; },
  });
  const sendVideo = vm.runInContext('sendVideo', ctx);
  const embed = 'https://www.youtube-nocookie.com/embed/aqz-KE-bpKQ?list=PLbpi6ZahtOH6Ar_3GPy3workIMw9vB9wB';

  let r = await sendVideo(embed, { href: embed, pick: { parts: ['audio', 'video', 'chapters'], quality: '1080p', audio: 'mp3', videoTrack: '1080p60 webm vp9', audioLang: 'de' } });
  const last = () => sent.at(-1)?.body;
  if (r.result !== 'sent') fail(`a send the instance started answers ${r.result}, want sent`);
  if (last()?.links !== 'https://www.youtube.com/watch?v=aqz-KE-bpKQ') fail(`an embedded player sends ${last()?.links}`);
  if (JSON.stringify(last()?.choice) !== '{"variants":["video","audio"],"quality":"1080p","audioFormat":"mp3","subtitleLangs":[],"subtitleAuto":true,"subtitleFormat":"srt","videoTrack":"1080p60 webm vp9","audioLang":"de"}') fail(`a pick arrives as ${JSON.stringify(last()?.choice)}, want the 1080p60 track and mp3 audio in German`);
  if (last()?.start !== true || last()?.report !== true) fail('the video is not sent to start at once and to report a link already there');
  if ('format' in last()) fail('the send still names a format beside its choice');
  if (sent.at(-1)?.path !== '/api/links' || sent.at(-1)?.method !== 'POST') fail('the video is not sent as POST /api/links');

  await sendVideo(embed, { href: embed, playlist: true, pick: { parts: ['everything'], quality: '8k' } });
  if (last()?.links !== 'https://www.youtube.com/playlist?list=PLbpi6ZahtOH6Ar_3GPy3workIMw9vB9wB') fail(`asked for the playlist, the button sends ${last()?.links}`);
  if (JSON.stringify(last()?.choice) !== '{"variants":["video"],"quality":"best","audioFormat":"best","subtitleLangs":[],"subtitleAuto":true,"subtitleFormat":"srt"}') fail(`a pick the instance would refuse arrives as ${JSON.stringify(last()?.choice)}, want the best video`);

  // The menu offers what the instance says the video has, and the
  // resolutions found are kept for the site.
  const videoFormats = vm.runInContext('videoFormats', ctx);
  answer = { status: 200, body: '{"qualities":["720p","480p","9999p"],"audio":true}' };
  let menu = await videoFormats(embed, { href: embed });
  const asked = sent.at(-1);
  if (asked?.method !== 'GET' || asked.path !== `/api/ytdlp/link?url=${encodeURIComponent('https://www.youtube.com/watch?v=aqz-KE-bpKQ')}`) {
    fail(`the menu asks ${asked?.method} ${asked?.path}, want GET /api/ytdlp/link for the video's address`);
  }
  if (menu.qualities?.join() !== '720p,480p' || menu.audio !== true || menu.known) fail(`an older instance's answer offers ${JSON.stringify(menu)}, want its two resolutions and nothing ruled out`);
  if (store.data.videoSeen) fail('an answer that rules nothing out is kept for the site');
  answer = { status: 200, body: JSON.stringify({ qualities: ['1080p', '720p'], audio: true, videoTracks: ['1080p mp4 avc1'], audioTracks: ['opus 160k'], audioLangs: [], subtitles: ['de'], autoCaptions: [], subtitleFormats: ['vtt'], thumbnail: null, description: true, videoSizes: { '1080p mp4 avc1': 5000000 }, audioSizes: {} }) };
  menu = await videoFormats(embed, { href: embed });
  if (menu.videoSizes?.['1080p mp4 avc1'] !== 5000000) fail(`the menu does not get the sizes the instance reports: ${JSON.stringify(menu.videoSizes)}`);
  if (!menu.known || menu.videoTracks?.join() !== '1080p mp4 avc1' || menu.subtitles?.join() !== 'de') fail(`the menu offers ${JSON.stringify(menu)}, want the video's own tracks and subtitles`);
  if (JSON.stringify(store.data.videoSeen?.youtube) !== '{"qualities":["1080p","720p"],"audio":["opus"],"subLangs":["de"],"videoFormats":["mp4 avc1"],"audioRates":["160k"],"thumbFormats":[]}') fail(`YouTube is remembered as offering ${JSON.stringify(store.data.videoSeen?.youtube)}`);
  answer = { status: 502, body: 'ERROR: [youtube] aqz-KE-bpKQ: Private video' };
  menu = await videoFormats(embed, { href: embed });
  if (menu.qualities?.length || menu.known) fail(`a probe that failed offers ${JSON.stringify(menu)}, want nothing known`);
  const asking = sent.length;
  menu = await videoFormats('https://www.youtube.com/watch?v=aqz-KE-bpKQ', { href: 'https://vimeo.com/1084537' });
  if (sent.length !== asking || menu.known) fail('a frame on one site could ask about a video of another');
  answer = { status: 200, body: '{"tasks":[{"id":"a"}],"listed":[]}' };

  const before = sent.length;
  r = await sendVideo('https://www.youtube.com/watch?v=aqz-KE-bpKQ', { href: 'https://vimeo.com/1084537' });
  if (r.result !== 'refused' || sent.length !== before) fail('a frame on one site could send a video of another');
  await store.local.set({ videoButtonHidden: ['youtube'] });
  r = await sendVideo(embed, { href: embed });
  if (r.result !== 'refused' || sent.length !== before) fail('a site the button is hidden on can still send');
  await store.local.set({ videoButtonHidden: [] });

  for (const [res, want] of [
    [{ status: 200, body: '{"tasks":[],"listed":[{"id":"a"}]}' }, 'already'],
    [{ status: 200, body: '{"tasks":[{"id":"a","skipped":true}],"listed":[]}' }, 'held'],
    [{ status: 200, body: '{"tasks":[],"listed":[]}' }, 'refused'],
    // An instance from before report answers with the bare list.
    [{ status: 200, body: '[]' }, 'already'],
    [{ status: 200, body: '[{"id":"a"}]' }, 'sent'],
    [{ status: 403, body: '{"code":"memberPending"}' }, 'refused'],
  ]) {
    answer = res;
    r = await sendVideo(embed, { href: embed });
    if (r.result !== want) fail(`the instance answering ${res.status} ${res.body} shows ${r.result}, want ${want}`);
  }
  ctx.withGroup = async () => { const e = new Error('no-phrase'); e.code = 'no-phrase'; throw e; };
  r = await sendVideo(embed, { href: embed });
  if (r.result !== 'unpaired') fail(`without a group the button shows ${r.result}, want unpaired`);

  // A relay that cannot be reached, through the real session.
  const t = vm.runInContext('t', ctx);
  ctx.withGroup = withGroup;
  ctx.readPhrase = async () => 'words';
  ctx.keysFromPhrase = async () => ({ key: 'k', frameKey: new Uint8Array(32) });
  ctx.readMember = async () => null;
  ctx.WebSocket = class {
    constructor() { setTimeout(() => this.onerror?.(), 0); }
    close() {}
  };
  r = await sendVideo(embed, { href: embed });
  if (r.result !== 'failed' || r.reason !== t('send.relayFailed')) fail(`with the relay out of reach the button shows ${r.result} "${r.reason ?? ''}", want failed "${t('send.relayFailed')}"`);
  ctx.withGroup = async () => { throw vm.runInContext("Object.assign(new Error('relay: the instance did not answer'), { code: 'no-answer' })", ctx); };
  r = await sendVideo(embed, { href: embed });
  if (r.result !== 'failed' || r.reason !== t('options.followFailed')) fail(`with the instance not answering the button shows ${r.result} "${r.reason ?? ''}", want failed "${t('options.followFailed')}"`);

  const listener = chrome.runtime.onMessage.listeners.at(-1);
  let answered = false;
  listener({ type: 'knightloader-video-send', href: embed }, { tab: { id: 1 }, url: 'https://example.org/' }, () => { answered = true; });
  await settle();
  if (answered) fail('a content script on a site without the button got an answer to a send');
}

/** A small stand-in for the DOM, enough for the button. */
class Node {
  constructor(tag, doc) {
    this.tagName = tag;
    this.doc = doc;
    this.children = [];
    this.parentNode = null;
    this.listeners = {};
    this.attrs = {};
    this.dataset = {};
    this.style = { setProperty: (k, v) => { this.style[k] = v; } };
    const classes = new Set();
    this.classList = {
      add: (...c) => c.forEach((x) => classes.add(x)),
      remove: (...c) => c.forEach((x) => classes.delete(x)),
      toggle: (c, on) => (on ?? !classes.has(c) ? classes.add(c) : classes.delete(c)),
      contains: (c) => classes.has(c),
    };
    this.rect = { left: 0, top: 0, width: 0, height: 0, right: 0, bottom: 0 };
    this.offsetWidth = 0;
    this.offsetHeight = 0;
    this.offsetLeft = 0;
    this.offsetTop = 0;
  }
  get parentElement() { return this.parentNode; }
  get isConnected() {
    let n = this;
    while (n.parentNode) n = n.parentNode;
    return n === this.doc.documentElement;
  }
  append(...nodes) {
    for (const n of nodes) {
      n.parentNode?.children.splice(n.parentNode.children.indexOf(n), 1);
      n.parentNode = this;
      this.children.push(n);
    }
  }
  prepend(n) {
    this.append(n);
    this.children.unshift(this.children.pop());
  }
  replaceChildren(...nodes) {
    for (const n of this.children) n.parentNode = null;
    this.children = [];
    this.append(...nodes);
  }
  remove() {
    if (!this.parentNode) return;
    this.parentNode.children.splice(this.parentNode.children.indexOf(this), 1);
    this.parentNode = null;
  }
  matches(selector) {
    return selector.split(',').some((part) => part.trim() === this.tagName);
  }
  closest(selector) {
    for (let n = this; n; n = n.parentNode) if (n.matches?.(selector)) return n;
    return null;
  }
  contains(n) {
    for (; n; n = n.parentNode) if (n === this) return true;
    return false;
  }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  getAttribute(k) { return this.attrs[k] ?? null; }
  removeAttribute(k) { delete this.attrs[k]; }
  addEventListener(type, f, opts) { (this.listeners[type] ??= []).push({ f, capture: opts === true || opts?.capture === true }); }
  dispatch(type, e = {}) { for (const l of this.listeners[type] ?? []) l.f({ isTrusted: true, stopPropagation() {}, preventDefault() {}, composedPath: () => [], ...e }); }
  /** Dispatches as the browser does: capturing listeners from the top down,
   *  then the bubbling ones back up, until one stops it. */
  fire(type, e = {}) {
    const path = [];
    for (let n = this; n; n = n.parentNode) path.unshift(n);
    let stopped = false;
    const ev = { isTrusted: true, preventDefault() {}, composedPath: () => [], ...e, stopPropagation() { stopped = true; } };
    for (const n of path) {
      for (const l of n.listeners[type] ?? []) if (l.capture || n === this) l.f(ev);
      if (stopped) return;
    }
    for (const n of path.slice(0, -1).reverse()) {
      for (const l of n.listeners[type] ?? []) if (!l.capture) l.f(ev);
      if (stopped) return;
    }
  }
  setPointerCapture() {}
  animate(keyframes, options) {
    (this.doc.animations ??= []).push({ node: this, keyframes, options });
    return { finished: Promise.resolve(), cancel() {} };
  }
  attachShadow() {
    this.shadow = new Node('#shadow', this.doc);
    Object.defineProperty(this.shadow, 'activeElement', { get: () => this.doc.activeElement ?? null });
    return this.shadow;
  }
  get textContent() { return this.text ?? ''; }
  set textContent(s) { this.text = s; }
  getBoundingClientRect() { return this.rect; }
  querySelectorAll(selector) {
    const tags = selector.split(',').map((s) => s.trim());
    const out = [];
    const walk = (n) => n.children.forEach((c) => { if (tags.includes(c.tagName) || selector === '*') out.push(c); walk(c); });
    walk(this);
    return out;
  }
  querySelector(selector) {
    const cls = selector.startsWith('.') ? selector.slice(1) : null;
    return this.querySelectorAll('*').find((n) => (cls ? n.attrs.class === cls || n.className === cls : n.tagName === selector)) ?? null;
  }
  focus() { this.doc.activeElement = this; }
  blur() { if (this.doc.activeElement === this) this.doc.activeElement = null; }
}

function makeDocument() {
  const doc = { listeners: {}, fullscreenElement: null };
  doc.documentElement = new Node('html', doc);
  doc.createElement = (tag) => new Node(tag, doc);
  doc.createElementNS = (_, tag) => new Node(tag, doc);
  doc.importNode = (n) => n;
  doc.querySelectorAll = (tag) => doc.documentElement.querySelectorAll(tag);
  doc.getElementById = (id) => doc.documentElement.querySelectorAll('*').find((n) => n.attrs.id === id) ?? doc.byId?.[id];
  doc.elementsFromPoint = () => doc.documentElement.querySelectorAll('video');
  doc.addEventListener = (type, f) => (doc.listeners[type] ??= []).push(f);
  doc.dispatch = (type, e = {}) => (doc.listeners[type] ?? []).forEach((f) => f({ composedPath: () => [], ...e }));
  return doc;
}

function video(doc, parent, rect) {
  const v = doc.createElement('video');
  v.rect = { ...rect, right: rect.left + rect.width, bottom: rect.top + rect.height };
  parent.append(v);
  return v;
}

/** The page's context: a site with the button's content scripts in it. */
function loadPage(chrome, href, { reduced = false } = {}) {
  const doc = makeDocument();
  const location = { href };
  const ctx = vm.createContext({
    chrome, document: doc, location, URL, console, setTimeout, clearTimeout,
    setInterval: () => 0, requestAnimationFrame: () => 0, performance,
    matchMedia: (q) => ({ matches: reduced && q.includes('reduce'), addEventListener() {} }),
    window: { addEventListener() {} }, innerWidth: 1280, innerHeight: 800,
    navigator: { userAgent: 'test' }, CSS: { supports: () => false },
    DOMParser: class { parseFromString() { return { documentElement: doc.createElementNS('', 'svg') }; } },
  });
  ctx.globalThis = ctx;
  return { ctx, doc, location, run: () => {
    for (const f of ['appearance.js', 'video.js', 'video-button.js']) vm.runInContext(read('src', f), ctx, { filename: f });
  } };
}

const PAGE_LABELS = {
  send: 'Download', sendTip: 'Send', more: 'More', best: 'Best', language: 'Language', dubbed: 'dubbed', autoCaptions: 'Auto-generated', quality: 'Quality', format: 'Format',
  parts: { video: 'Video', audio: 'Audio', subtitle: 'Subtitles', thumbnail: 'Thumbnail', description: 'Text' },
  checking: 'Checking', unknown: 'Unknown', sent: 'Sent', already: 'Already there',
  mark: '<svg viewBox="0 0 587 901.8"></svg>', dir: 'ltr', lang: 'en',
};

// 5. The switch, the page and the button.
{
  const store = makeStore({ videoDefaults: { youtube: { parts: ['video', 'subtitle'], quality: '1080p', audio: 'best' } } });
  const { chrome } = makeChrome(store);
  let formatsAnswer;
  let formatsAsked = 0;
  const sends = [];
  chrome.runtime.sendMessage = async (msg) => {
    if (msg.type === 'knightloader-video-labels') return PAGE_LABELS;
    if (msg.type === 'knightloader-video-formats') {
      formatsAsked++;
      return new Promise((resolve) => { formatsAnswer = resolve; });
    }
    if (msg.type === 'knightloader-video-send') {
      sends.push(msg);
      return { result: 'already' };
    }
    return undefined;
  };

  const { doc, location, run } = loadPage(chrome, 'https://www.youtube.com/watch?v=aqz-KE-bpKQ');
  const player = doc.createElement('div');
  player.rect = { left: 0, top: 56, width: 1280, height: 720, right: 1280, bottom: 776 };
  doc.documentElement.append(player);
  const main = video(doc, player, { left: 0, top: 56, width: 1280, height: 720 });
  run();
  await settle();
  const button = () => doc.documentElement.querySelectorAll('knightloader-video-button')[0];
  const where = () => button()?.parentNode;
  if (!button()) fail('on a watch page the button does not appear');
  else if (button().style.left !== '640px') fail(`the button sits at ${button().style.left}, want the middle of the video's top edge`);

  const shadow = button()?.shadow;
  const buttons = () => shadow.querySelectorAll('button');
  const send = () => buttons().find((b) => b.className === 'send');
  const more = () => buttons().find((b) => b.attrs['aria-haspopup'] === 'dialog');
  const frame = () => shadow.querySelectorAll('div').find((d) => d.className === 'kl');
  const bar = () => shadow.querySelectorAll('div').find((d) => d.className === 'bar');
  if (!send()?.children.some((c) => c.attrs.class === 'mark')) fail('the button shows no mark in front of its label');
  if (!frame()?.classList.contains('shown')) fail('without auto-hide the button does not show over the video');

  // A click sends the site's defaults; the instance having the video already
  // shows on the button.
  send().fire('click');
  await settle();
  if (JSON.stringify(sends.at(-1)?.pick) !== '{"parts":["video","subtitle"],"quality":"1080p","videoFormat":"best","audio":"best","audioRate":"best","subLangs":[],"subFormat":"srt"}') fail(`a click sends ${JSON.stringify(sends.at(-1)?.pick)}, want YouTube's defaults`);
  const stars = () => (doc.animations ?? []).filter((a) => a.node.attrs.class === 'star').length;
  if (stars() < 5 || stars() > 7) fail(`a video already there cheers with ${stars()} stars, want five to seven`);
  if (frame()?.dataset.state !== 'already' || send().children.at(-1)?.textContent !== 'Already there') fail(`a video already there shows ${frame()?.dataset.state} "${send().children.at(-1)?.textContent}", want already`);

  // The menu lists the parts with the site's defaults switched on, says it
  // is checking until the instance answers, then offers what the video has,
  // and opening it again asks nothing more.
  const menuEl = () => shadow.querySelectorAll('div').find((d) => d.attrs.role === 'dialog');
  const entries = () => menuEl().querySelectorAll('*').filter((n) => n.text).map((n) => (n.attrs['aria-checked'] === 'true' ? `[${n.text}]` : n.text));
  const withRole = (role) => menuEl().querySelectorAll('*').filter((n) => n.attrs.role === role);
  const radios = (group) => group.querySelectorAll('button');
  const picked = (group) => radios(group).find((r) => r.attrs['aria-checked'] === 'true');
  const key = (node, k) => node.fire('keydown', { key: k });
  const go = () => menuEl().children.at(-1)?.children[0];
  const goShows = () => [go()?.text, ...(go()?.children ?? []).map((c) => c.text)].join(' ');
  more().fire('click');
  await settle();
  if (more().attrs['aria-expanded'] !== 'true' || !menuEl()?.attrs['aria-label']) fail('the arrow does not say it opened a named dialog');
  const checking = 'Video|Audio|Subtitles|Thumbnail|Text|Checking|Download';
  if (entries().join('|').replace(/[[\]]/g, '') !== checking) fail(`while the instance is asked the menu shows ${entries().join(', ')}, want ${checking}`);
  formatsAnswer({
    known: true, qualities: ['1080p', '720p'], audio: true, videoTracks: ['1080p mp4 avc1', '1080p webm vp9', '720p mp4 avc1'],
    audioTracks: ['opus 160k', 'aac 128k'], audioLangs: [], subtitles: ['de'], autoCaptions: ['en-orig'], subtitleFormats: ['vtt'],
    thumbnail: { width: 1280, height: 720, ext: 'webp' }, description: false,
    videoSizes: { best: 404000000, '1080p': 303000000, '720p': 90000000 }, audioSizes: { 'aac 128k': 3000000 },
  });
  await settle();
  const listed = '[Video]|Quality|Best|404 MB|[1080p]|303 MB|720p|90 MB|Format|H.264|MP4|[VP9]|WebM|Audio|[Subtitles]|Language|[de]|Auto-generated|en|Format|[SRT]|WebVTT|TTML|Thumbnail|Download';
  if (entries().join('|') !== listed) fail(`the menu shows ${entries().join(', ')}, want ${listed}, made exact for the video and without the text it lacks`);

  // Each part is a switch, a quality a radio in a list, a format a radio in
  // a segmented row, a subtitle language a checkbox; nothing is left a menu
  // item, which a panel of switches is not.
  for (const role of ['menu', 'menuitem', 'menuitemcheckbox', 'menuitemradio']) if (withRole(role).length) fail(`the menu still has a ${role}`);
  const switches = withRole('switch').map((s) => `${s.text}:${s.attrs['aria-checked']}`).join('|');
  if (switches !== 'Video:true|Audio:false|Subtitles:true|Thumbnail:false') fail(`the parts read ${switches}, want a switch each with its state`);
  // Each part is a card of its own, its switch on top and, while it is on,
  // its choices under it.
  const cards = menuEl().children.filter((c) => c.className === 'card');
  if (cards.map((c) => c.children[0]?.text).join('|') !== 'Video|Audio|Subtitles|Thumbnail') fail(`the menu's cards are ${cards.map((c) => c.children[0]?.text).join(', ')}, want one per part with its switch first`);
  if (!cards[0]?.querySelectorAll('*').some((n) => n.attrs.role === 'radiogroup') || cards[1]?.children.length !== 1) fail("a part's choices are not in its own card, or a part switched off shows choices");
  const groups = withRole('radiogroup');
  if (groups.map((g) => g.className).join('|') !== 'list|segments|segments') fail(`the choices come as ${groups.map((g) => g.className).join(', ')}, want the video's qualities as a list and the formats as segments`);
  const ids = new Set(menuEl().querySelectorAll('*').map((n) => n.attrs.id).filter(Boolean));
  for (const g of groups) {
    const named = (g.attrs['aria-labelledby'] ?? '').split(' ').filter(Boolean);
    if (named.length !== 2 || !named.every((id) => ids.has(id))) fail(`the ${g.className} of choices is not named by its part and its row: ${g.attrs['aria-labelledby']}`);
    const stops = radios(g).filter((r) => r.attrs.tabindex !== '-1');
    if (radios(g).some((r) => r.attrs.role !== 'radio') || stops.length !== 1 || stops[0] !== picked(g)) fail(`a radio group has ${stops.length} tab stops, want its picked radio alone`);
  }
  if (withRole('checkbox').map((c) => `${c.text}:${c.attrs['aria-checked']}`).join('|') !== 'de:true|en:false') fail('the subtitle languages are not checkboxes with their state');
  if (radios(groups[0]).find((r) => r.text === '720p')?.attrs['aria-label'] !== '720p, 90 MB') fail('a quality does not say its size to a screen reader');
  if (radios(groups[1]).find((r) => r.text === 'H.264')?.attrs['aria-label'] !== 'H.264, MP4') fail('a format does not say its container to a screen reader');
  // Video and subtitles are ticked, and subtitles have no size, so the
  // button weighs nothing rather than less than it downloads.
  if (goShows() !== 'Download') fail(`with subtitles ticked the download button reads "${goShows()}", want no total`);

  const foot = menuEl().children.at(-1);
  if (foot?.className !== 'foot' || foot.children.map((b) => b.text).join('|') !== 'Download') fail('Download is not alone at the foot of the menu, where it stays in view');
  const room = parseInt(menuEl().style['max-height'] ?? '0', 10);
  if (!(room > 460 && room <= 720)) fail(`the menu may grow to ${room}px, want the room the player leaves below the button`);
  const item = (text) => menuEl().querySelectorAll('button').find((b) => b.text === text);
  const off = (text) => item(text)?.disabled === true;
  // Best is a quality, never a format: a format row offers what the video
  // has and marks the one the best track takes until another is picked.
  for (const g of withRole('radiogroup').filter((r) => r.className === 'segments')) if (radios(g).some((r) => r.text === 'Best')) fail('a format row offers Best');
  item('H.264').fire('click');
  if (!entries().includes('[H.264]')) fail('a format picked in the menu is not marked');
  item('VP9').fire('click');
  item('720p').fire('click');
  if (!entries().join('|').includes('Format|[H.264]|MP4|VP9|WebM') || !off('VP9') || off('H.264')) {
    fail(`at a quality without VP9 the menu shows ${entries().join(', ')}, want H.264 marked and VP9 listed but off`);
  }

  // The menu is a window and each part a sub-card one surface up, without
  // a border. Every list is one column of tiles without a marker, whether one
  // or several can be picked; only the roles tell the two apart.
  const css = read('src', 'video-button.js');
  const rule = (selector) => css.slice(css.indexOf(`${selector} {`)).split('}')[0];
  if (!/background: var\(--surface2\)/.test(rule('.menu .card')) || /box-shadow|border(?!-radius)/.test(rule('.menu .card'))) fail('a part is not a sub-card filled one surface up without a border');
  if (!/background: var\(--surface\)/.test(rule('    .menu'))) fail('the menu is not a window on the surface');
  if (/\.list[^{}]*::(before|after)/.test(css)) fail('a list of choices still draws a radio dot or a checkbox');
  // A part's name is its heading, an accent badge with rounded corners that
  // stays filled whether the part is on or off; the track beside it says which.
  const head = rule('.menu .switch');
  if (!/background: var\(--accent\)/.test(head) || !/color: var\(--accent-contrast\)/.test(head) || !/text-transform: uppercase/.test(head) || !/border-radius: min\(var\(--radius\), 6px\)/.test(head)) fail('a part\'s name is not a filled heading badge with rounded corners');
  if (/opacity|var\(--sub\)/.test(head) || /\.switch\[aria-checked="(true|false)"\]\s*\{/.test(css)) fail('a part\'s heading badge changes with its switch');
  // Nothing in the menu draws a line round itself: sub-cards, tiles, format
  // rails, switches and the download button part by their fill alone. The
  // one line left is the window's hairline, GlimStone's card edge.
  const lined = [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)].filter(([, , body]) => /inset 0 0 0 1px|(^|[;\s])border(-top|-bottom|-left|-right)?:/.test(body)).map(([, sel]) => sel.replace(/\/\*[\s\S]*?\*\//g, '').trim());
  if (lined.join() !== '.menu') fail(`inside the menu these draw a line: ${lined.filter((s) => s !== '.menu').join(', ')}`);
  if (!/background: var\(--surface3\)/.test(rule('.segments')) || !/padding: 0\.2rem/.test(rule('.segments'))) fail('a format rail is not the groove one surface below its sub-card, padded 0.2rem');
  if (!/background: var\(--surface3\)/.test(rule('.menu .list button')) || !/background: var\(--raised\)/.test(css.slice(css.indexOf('.list button:not([aria-checked="true"]):hover')))) fail('a tile does not rest one surface above its sub-card and rise one more under the pointer');

  // Every list of choices is one column, so it reads top to bottom like the
  // subtitle languages.
  const lists = menuEl().querySelectorAll('div').filter((d) => d.className === 'list');
  if (lists.length !== 3 || lists.some((l) => l.style['grid-template-rows']) || /1fr 1fr/.test(read('src', 'video-button.js'))) fail('a list of choices in the menu is laid out in two columns');

  // The arrow keys move the pick and the keyboard together inside a group,
  // past a format the quality rules out and round the ends, and left and
  // right step as up and down do, as the browser's own radios. Outside the
  // groups they go from one stop to the next.
  const quality = () => withRole('radiogroup')[0];
  const format = () => withRole('radiogroup')[1];
  key(picked(format()), 'ArrowRight');
  if (picked(format())?.text !== 'H.264' || doc.activeElement !== picked(format())) fail(`at 720p the arrow key in the format row lands on ${picked(format())?.text}, want past VP9, which the quality rules out, and round to H.264`);
  key(picked(quality()), 'ArrowUp');
  if (picked(quality())?.text !== '1080p' || doc.activeElement !== picked(quality())) fail('the up arrow in the quality list does not pick the quality above');
  key(picked(quality()), 'ArrowDown');
  key(picked(quality()), 'ArrowLeft');
  if (picked(quality())?.text !== '1080p') fail(`the left arrow from 720p picks ${picked(quality())?.text}, want 1080p above it`);
  key(picked(quality()), 'ArrowRight');
  if (picked(quality())?.text !== '720p' || picked(format())?.text !== 'H.264') fail('the right arrow from 1080p does not go back to 720p, or the format did not stay');
  key(picked(quality()), 'ArrowDown');
  if (picked(quality())?.text !== 'Best') fail(`the down arrow from the last quality picks ${picked(quality())?.text}, want round to Best`);
  key(picked(quality()), 'ArrowUp');
  withRole('switch')[0].focus();
  key(withRole('switch')[0], 'ArrowDown');
  if (doc.activeElement !== picked(quality())) fail('the down arrow from the Video switch does not reach the quality picked');

  item('Audio').fire('click');
  if (withRole('switch')[1]?.attrs['aria-checked'] !== 'true') fail('a click on the Audio switch does not turn it on');
  const audioRows = 'Audio]|Quality|[Best]|160k|128k|3 MB|Format|AAC|[Opus]|';
  if (!entries().join('|').includes(audioRows)) fail(`the audio part shows ${entries().join(', ')}, want ${audioRows}`);
  item('128k').fire('click');
  if (!off('Opus') || off('AAC')) fail('at 128k the audio format opus, which the video lacks there, is not off');
  item('en').fire('click');
  item('WebVTT').fire('click');
  item('Thumbnail').fire('click');
  // A thumbnail the host has in one format only says which. A row of one
  // choice would offer nothing.
  if (!entries().join('|').includes('Thumbnail]|1280×720 WebP|Download')) fail(`the menu does not say which picture the thumbnail is, in the host's format: ${entries().join(', ')}`);
  item('Download').fire('click');
  await settle();
  const norm = (o) => JSON.stringify(Object.fromEntries(Object.entries(o ?? {}).sort()));
  const wantPick = { parts: ['video', 'audio', 'subtitle', 'thumbnail'], quality: 'best', audio: 'best', subLangs: ['de', 'en-orig'], subFormat: 'vtt', videoTrack: '720p mp4 avc1', audioTrack: 'aac 128k', subAuto: true };
  if (norm(sends.at(-1)?.pick) !== norm(wantPick)) fail(`the menu's own pick sends ${JSON.stringify(sends.at(-1)?.pick)}, want ${JSON.stringify(wantPick)}`);
  more().fire('click');
  await settle();
  if (formatsAsked !== 1) fail(`opening the menu again asked the instance ${formatsAsked} times, want once`);
  if (!entries().includes('[1080p]')) fail('opened again, the menu does not start from the defaults');
  more().fire('click');

  // Best in the defaults stays Best in the menu, rather than the track the
  // instance would take for it. The download button weighs what is ticked
  // while every part of it has a size.
  await store.local.set({ videoDefaults: { youtube: { parts: ['video'], quality: 'best' } } });
  more().fire('click');
  await settle();
  if (!entries().join('|').startsWith('[Video]|Quality|[Best]|404 MB|1080p|303 MB|720p|90 MB|Format|H.264|MP4|[VP9]|')) fail(`with Best stored the menu starts at ${entries().join(', ')}, want Best marked and the best track's VP9`);
  if (goShows() !== 'Download · 404 MB' || go()?.attrs['aria-label'] !== 'Download, 404 MB') fail(`with the video alone ticked the download button reads "${goShows()}", want its size`);
  item('Audio').fire('click');
  if (goShows() !== 'Download') fail(`with the best audio's size unknown the download button reads "${goShows()}", want no total`);
  item('128k').fire('click');
  if (goShows() !== 'Download · 407 MB') fail(`with video and the 128k audio the download button reads "${goShows()}", want 407 MB`);
  item('Audio').fire('click');
  item('Download').fire('click');
  await settle();
  if (sends.at(-1)?.pick?.videoTrack || sends.at(-1)?.pick?.quality !== 'best') fail(`Best and Best send ${JSON.stringify(sends.at(-1)?.pick)}, want no track and the best quality`);
  await store.local.set({ videoDefaults: { youtube: { parts: ['video', 'subtitle'], quality: '1080p', audio: 'best' } } });

  // A drag places the button and keeps the place as shares of the player,
  // and sends nothing.
  bar().offsetWidth = 160;
  bar().offsetHeight = 32;
  button().rect = { left: 560, top: 68, width: 160, height: 32, right: 720, bottom: 100 };
  const before = sends.length;
  bar().dispatch('pointerdown', { button: 0, pointerId: 1, clientX: 600, clientY: 80 });
  bar().dispatch('pointermove', { pointerId: 1, clientX: 602, clientY: 81 });
  if (frame()?.classList.contains('dragging')) fail('a press that barely moves already drags');
  bar().dispatch('pointermove', { pointerId: 1, clientX: 1000, clientY: 700 });
  bar().dispatch('pointermove', { pointerId: 1, clientX: 1400, clientY: 700 });
  bar().dispatch('pointerup', { pointerId: 1, clientX: 1400, clientY: 700 });
  send().fire('click');
  await settle();
  if (sends.length !== before) fail('the click that ends a drag sends the video');
  const spot = store.data.videoButtonSpot?.youtube;
  if (JSON.stringify(spot) !== '{"x":0.9375,"y":0.9}') fail(`the dragged button is stored at ${JSON.stringify(spot)}, want shares of the player kept inside it, 0.9375 and 0.9`);
  if (button().style.left !== '1200px' || button().style.top !== '704px') fail(`after the drag the button sits at ${button().style.left}, ${button().style.top}, want 1200px, 704px`);
  send().fire('click');
  await settle();
  if (sends.length !== before + 1) fail('after a drag the next click sends nothing');

  // Auto-hide: away until the pointer moves over the video, and back while
  // the keyboard is on it.
  await store.local.set({ videoButtonAutoHide: true });
  await settle();
  await new Promise((r) => setTimeout(r, 4100));
  doc.dispatch('pointermove', { clientX: 5, clientY: 5 });
  if (frame()?.classList.contains('shown')) fail('with auto-hide the button shows with the pointer away from the video');
  doc.dispatch('pointermove', { clientX: 300, clientY: 300 });
  if (!frame()?.classList.contains('shown')) fail('with auto-hide the button does not show with the pointer over the video');
  doc.dispatch('pointermove', { clientX: 5, clientY: 900 });
  if (frame()?.classList.contains('shown')) fail('with auto-hide the button stays once the pointer leaves the video');
  shadow.dispatch('focusin');
  if (!frame()?.classList.contains('shown')) fail('with auto-hide the button does not show when the keyboard reaches it');
  await store.local.set({ videoButtonAutoHide: false });

  // The switch and the site's own switch take the button off an open page
  // at once, without a reload, and bring it back.
  await store.local.set({ videoButton: false });
  if (button()) fail('switched off, the button stays on the open page until a reload');
  await store.local.set({ videoButton: true });
  if (!button()) fail('switched on again, the button does not come back to the open page');

  await store.local.set({ videoButtonHidden: ['youtube'] });
  if (button()) fail('hidden on this site, the button stays');
  await store.local.set({ videoButtonHidden: [] });
  if (!button()) fail('shown on this site again, the button does not come back');

  location.href = 'https://www.youtube.com/feed/subscriptions';
  doc.dispatch('yt-navigate-finish');
  if (button()) fail('on a page that plays no one video the button stays');
  location.href = 'https://www.youtube.com/watch?v=dQw4w9WgXcQ';
  doc.dispatch('yt-navigate-finish');
  if (!button()) fail('on the next video the button does not come back');

  doc.fullscreenElement = player;
  doc.dispatch('fullscreenchange');
  if (where() !== player) fail('in fullscreen the button is not inside the element shown, so it is not drawn');
  doc.fullscreenElement = main;
  doc.dispatch('fullscreenchange');
  if (button()) fail('with the video element itself in fullscreen the button stays where nothing draws it');
  doc.fullscreenElement = null;
  doc.dispatch('fullscreenchange');
  if (where() !== doc.documentElement) fail('out of fullscreen the button does not return to the page');

  // Shorts keep the next video loaded below the screen. The place dragged to
  // goes with the site, so the reset puts the button back on the top edge.
  await store.local.set({ videoButtonSpot: undefined });
  location.href = 'https://www.youtube.com/shorts/aqz-KE-bpKQ';
  main.remove();
  video(doc, player, { left: 400, top: 1000, width: 480, height: 850 });
  video(doc, player, { left: 500, top: 0, width: 400, height: 710 });
  doc.dispatch('yt-navigate-finish');
  if (button()?.style.left !== '700px') fail(`on Shorts the button sits at ${button()?.style.left}, want over the video on screen at 700px`);

  // The button takes the extension's theme, and its corners are rounded in
  // every shape but never so far that the 32px bar reads as a pill.
  for (const theme of ['light', 'dark']) {
    await store.local.set({ theme });
    if (frame()?.dataset.theme !== theme) fail(`with the ${theme} theme the button is ${frame()?.dataset.theme}`);
  }
  for (const shape of ['round', 'soft', 'square', 'leaf']) {
    await store.local.set({ shape });
    const radius = parseFloat(frame()?.style['--radius']);
    if (!(radius < 16)) fail(`with the ${shape} shape the button's corners are ${frame()?.style['--radius']}, want under half its height`);
    if (shape === 'round' && !(radius >= 8)) fail(`with the round shape the button's corners are ${radius}px, want clearly rounded`);
  }
}

// The menu stays open, and fills in, while the player lets go of its video for
// a moment; a refusal cheers with nothing, and with reduced motion a success
// flashes without stars.
for (const reduced of [false, true]) {
  const store = makeStore();
  const { chrome } = makeChrome(store);
  let formatsAnswer;
  let answer = { result: 'refused' };
  chrome.runtime.sendMessage = async (msg) => {
    if (msg.type === 'knightloader-video-labels') return PAGE_LABELS;
    if (msg.type === 'knightloader-video-formats') return new Promise((resolve) => { formatsAnswer = resolve; });
    if (msg.type === 'knightloader-video-send') return answer;
    return undefined;
  };
  const { doc, run } = loadPage(chrome, 'https://www.youtube.com/watch?v=aqz-KE-bpKQ', { reduced });
  const player = doc.createElement('div');
  doc.documentElement.append(player);
  const main = video(doc, player, { left: 0, top: 56, width: 1280, height: 720 });
  run();
  await settle();
  const host = doc.documentElement.querySelectorAll('knightloader-video-button')[0];
  const shadow = host.shadow;
  const send = shadow.querySelectorAll('button').find((b) => b.className === 'send');
  const more = shadow.querySelectorAll('button').find((b) => b.attrs['aria-haspopup'] === 'dialog');
  const frame = shadow.querySelectorAll('div').find((d) => d.className === 'kl');
  const menu = shadow.querySelectorAll('div').find((d) => d.attrs.role === 'dialog');
  const stars = () => (doc.animations ?? []).filter((a) => a.node.attrs.class === 'star').length;
  const flashes = () => (doc.animations ?? []).filter((a) => a.node === send).length;

  if (!reduced) {
    more.fire('click');
    await settle();
    main.rect = { left: 0, top: 0, width: 0, height: 0, right: 0, bottom: 0 };
    // The spot under the button now reads as the page, which is far taller
    // than the player.
    doc.elementsFromPoint = () => [doc.documentElement];
    doc.documentElement.rect = { left: 0, top: 0, width: 1280, height: 5000, right: 1280, bottom: 5000 };
    doc.dispatch('yt-navigate-finish');
    doc.dispatch('scroll');
    if (!host.isConnected || menu.hidden) fail('the menu closes while the player lets go of its video for a moment');
    if (!frame.classList.contains('shown')) fail('the open menu fades out where the spot under the button reads as the site');
    formatsAnswer({ known: false, qualities: [], audio: true });
    await settle();
    if (menu.hidden || !menu.children.some((c) => c.text === 'Unknown')) fail('the menu does not fill in once the instance answers');
    more.fire('click');
    main.rect = { left: 0, top: 56, width: 1280, height: 720, right: 1280, bottom: 776 };
    doc.documentElement.rect = { left: 0, top: 0, width: 0, height: 0, right: 0, bottom: 0 };
    doc.elementsFromPoint = () => doc.documentElement.querySelectorAll('video');
    doc.dispatch('yt-navigate-finish');
  }

  send.fire('click');
  await settle();
  if (stars() || flashes()) fail(`a refused send cheers with ${stars()} stars and ${flashes()} flashes`);
  answer = { result: 'sent' };
  send.fire('click');
  await settle();
  if (reduced && (stars() || flashes() !== 1)) fail(`with reduced motion a success shows ${stars()} stars and ${flashes()} animations of the button, want one flash alone`);
  if (!reduced && (stars() < 5 || stars() > 7 || flashes() !== 2)) fail(`a success shows ${stars()} stars and ${flashes()} animations of the button, want five to seven and a flash with a pulse`);
  const scaled = (doc.animations ?? []).some((a) => a.node === send && a.keyframes.some((k) => /scale/.test(k.transform ?? '')));
  if (reduced && scaled) fail('with reduced motion the button still grows');
}

// The part a video's description is saved as reads as its description in
// every language, not as plain text.
{
  const ctx = vm.createContext({ MESSAGES: {} });
  vm.runInContext(`${read('src', 'i18n-video.js')}\nglobalThis.V = VIDEO_MESSAGES;`, ctx);
  const named = Object.fromEntries(Object.entries(ctx.V).map(([l, m]) => [l, m['video.partDescription']]));
  if (named.en !== 'Description' || named.de !== 'Beschreibung') fail(`the description part reads ${named.en} and ${named.de}, want Description and Beschreibung`);
  const texts = ['Text', 'Texte', 'Texto', 'Testo', 'Tekst', 'Текст', 'Teksti', 'Metin', 'نص', 'Κείμενο', 'Testua', 'متن', 'טקסט', 'टेक्स्ट', 'Szöveg', 'Teks', 'Texti', 'テキスト', '텍스트', 'Tekstas', 'Teksts', 'Besedilo', 'ข้อความ', 'Văn bản'];
  const plain = Object.entries(named).filter(([, v]) => texts.includes(v)).map(([l]) => l);
  if (plain.length) fail(`the description part still reads as plain text in ${plain.join(', ')}`);
}

// The button's words stay readable on both themes and on the accent it turns
// to under the pointer (WCAG AA, 4.5:1).
{
  const ctx = vm.createContext({ matchMedia: () => ({ matches: false }), navigator: { userAgent: 'test' }, CSS: { supports: () => false } });
  vm.runInContext(read('src', 'appearance.js'), ctx, { filename: 'appearance.js' });
  const ratio = vm.runInContext('contrastRatio', ctx);
  const ink = vm.runInContext('contrastOn(DEFAULT_ACCENT)', ctx);
  const css = read('src', 'video-button.js');
  const tokens = (selector) => {
    const block = css.slice(css.indexOf(selector)).split('}')[0];
    return Object.fromEntries([...block.matchAll(/--([a-z0-9]+): (#[0-9a-f]{6})/g)].map((m) => [m[1], m[2]]));
  };
  for (const [theme, t] of [['dark', tokens('.kl {')], ['light', tokens('.kl[data-theme="light"] {')]]) {
    for (const k of ['text', 'sub', 'ok', 'fail']) {
      if (ratio(t[k], t.surface) < 4.5) fail(`in the ${theme} theme --${k} on the surface is ${ratio(t[k], t.surface).toFixed(2)}:1`);
    }
    // The labels sit on the sub-card, a tile's words on the tile at rest and
    // under the pointer.
    for (const [k, ground] of [['sub', 'surface2'], ['text', 'surface2'], ['sub', 'surface3'], ['text', 'raised']]) {
      if (!t[ground] || ratio(t[k], t[ground]) < 4.5) fail(`in the ${theme} theme --${k} on --${ground} is ${t[ground] ? ratio(t[k], t[ground]).toFixed(2) : 'missing'}`);
    }
    // A switch's knob shows whether it is on, so on its sub-card it needs the
    // 3:1 a control's outline does.
    if (ratio(t.knob, t.surface2) < 3) fail(`in the ${theme} theme the knob on a sub-card is ${ratio(t.knob, t.surface2).toFixed(2)}:1, want 3:1`);
  }
  const gold = vm.runInContext('DEFAULT_ACCENT', ctx);
  if (ratio(ink, gold) < 4.5) fail(`the label on the accent is ${ratio(ink, gold).toFixed(2)}:1`);
}

// An embedded player keeps its title, a link, along the top of the video.
{
  const store = makeStore();
  const { chrome } = makeChrome(store);
  chrome.runtime.sendMessage = async (msg) => (msg.type === 'knightloader-video-labels' ? { send: 'Download', sendTip: 'Send', more: 'More', dir: 'ltr', lang: 'en' } : undefined);
  const { doc, run } = loadPage(chrome, 'https://www.youtube-nocookie.com/embed/aqz-KE-bpKQ');
  const player = doc.createElement('div');
  doc.documentElement.append(player);
  const v = video(doc, player, { left: 0, top: 0, width: 800, height: 450 });
  const title = doc.createElement('a');
  player.append(title);
  doc.elementsFromPoint = (x, y) => (y < 60 ? [title, v] : [v]);
  run();
  await settle();
  const top = parseInt(doc.documentElement.querySelectorAll('knightloader-video-button')[0]?.style.top ?? '0', 10);
  if (top < 60) fail(`in an embedded player the button sits at ${top}px, over the title link along the top`);
}

// A click with a format or a bitrate in the defaults sends the track of this
// video they name, which only the instance's answer knows, and with no answer
// the cap and the codec alone.
for (const known of [true, false]) {
  const store = makeStore({ videoDefaults: { youtube: { parts: ['video', 'audio'], quality: '720p', videoFormat: 'webm vp9', audio: 'opus', audioRate: '128k' } } });
  const { chrome } = makeChrome(store);
  const sends = [];
  chrome.runtime.sendMessage = async (msg) => {
    if (msg.type === 'knightloader-video-labels') return PAGE_LABELS;
    if (msg.type === 'knightloader-video-formats') {
      return known
        ? { known: true, qualities: ['1080p', '720p'], audio: true, videoTracks: ['1080p webm vp9', '720p mp4 avc1', '720p webm vp9'], audioTracks: ['opus 160k', 'opus 120k', 'aac 128k'], audioLangs: [], subtitles: [], autoCaptions: [], subtitleFormats: [], thumbnail: null, description: true }
        : { known: false, qualities: [], audio: true, videoTracks: [], audioTracks: [], audioLangs: [], subtitles: [], autoCaptions: [], subtitleFormats: [], thumbnail: null, description: true };
    }
    if (msg.type === 'knightloader-video-send') {
      sends.push(msg.pick);
      return { result: 'sent' };
    }
    return undefined;
  };
  const { doc, run } = loadPage(chrome, 'https://www.youtube.com/watch?v=aqz-KE-bpKQ');
  const player = doc.createElement('div');
  doc.documentElement.append(player);
  video(doc, player, { left: 0, top: 56, width: 1280, height: 720 });
  run();
  await settle();
  const shadow = doc.documentElement.querySelectorAll('knightloader-video-button')[0].shadow;
  shadow.querySelectorAll('button').find((b) => b.className === 'send').fire('click');
  await settle();
  const got = sends.at(-1);
  const want = known ? { videoTrack: '720p webm vp9', audioTrack: 'opus 120k', quality: 'best', audio: 'best' } : { quality: '720p', audio: 'opus' };
  const picked = { quality: got?.quality, audio: got?.audio, ...(got?.videoTrack ? { videoTrack: got.videoTrack } : {}), ...(got?.audioTrack ? { audioTrack: got.audioTrack } : {}) };
  if (JSON.stringify(Object.entries(picked).sort()) !== JSON.stringify(Object.entries(want).sort())) {
    fail(`${known ? 'with' : 'without'} the video's tracks known, a click with a format and a bitrate in the defaults sends ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
  }
}
// A thumbnail from an instance that converts is offered as JPEG, PNG and
// WebP, JPEG picked, and the pick is sent; a click with the part on sends the
// site's default, and an instance that converts nothing is sent no format.
for (const converts of [true, false]) {
  const store = makeStore({ videoDefaults: { youtube: { parts: ['thumbnail'], thumbFormat: 'webp' } } });
  const { chrome } = makeChrome(store);
  const sends = [];
  chrome.runtime.sendMessage = async (msg) => {
    if (msg.type === 'knightloader-video-labels') return PAGE_LABELS;
    if (msg.type === 'knightloader-video-formats') {
      return {
        known: true, qualities: ['720p'], audio: true, videoTracks: ['720p mp4 avc1'], audioTracks: [], audioLangs: [], subtitles: [], autoCaptions: [], subtitleFormats: [],
        thumbnail: { width: 1280, height: 720, ext: 'webp', ...(converts ? { formats: ['jpg', 'png', 'webp'] } : {}) }, description: false,
      };
    }
    if (msg.type === 'knightloader-video-send') {
      sends.push(msg.pick);
      return { result: 'sent' };
    }
    return undefined;
  };
  const { doc, run } = loadPage(chrome, 'https://www.youtube.com/watch?v=aqz-KE-bpKQ');
  const player = doc.createElement('div');
  doc.documentElement.append(player);
  video(doc, player, { left: 0, top: 56, width: 1280, height: 720 });
  run();
  await settle();
  const shadow = doc.documentElement.querySelectorAll('knightloader-video-button')[0].shadow;
  const button = (cls) => shadow.querySelectorAll('button').find((b) => b.className === cls);
  button('send').fire('click');
  await settle();
  if (sends.at(-1)?.thumbFormat !== (converts ? 'webp' : undefined)) fail(`${converts ? 'to an instance that converts' : 'to one that does not'} a click sends the thumbnail as ${sends.at(-1)?.thumbFormat}, want ${converts ? "the site's WebP" : 'no format'}`);
  await store.local.set({ videoDefaults: { youtube: { parts: ['thumbnail'] } } });
  shadow.querySelectorAll('button').find((b) => b.attrs['aria-haspopup'] === 'dialog').fire('click');
  await settle();
  const menu = shadow.querySelectorAll('div').find((d) => d.attrs.role === 'dialog');
  const shown = menu.querySelectorAll('*').filter((n) => n.text).map((n) => (n.attrs['aria-checked'] === 'true' ? `[${n.text}]` : n.text)).join('|');
  const want = converts ? 'Thumbnail]|1280×720|Format|[JPEG]|PNG|WebP|Download' : 'Thumbnail]|1280×720 WebP|Download';
  if (!shown.includes(want)) fail(`${converts ? 'from an instance that converts' : 'from one that does not'} the thumbnail card shows ${shown}, want ${want}`);
  if (converts) {
    menu.querySelectorAll('button').find((b) => b.text === 'PNG')?.fire('click');
    menu.querySelectorAll('button').find((b) => b.text === 'Download').fire('click');
    await settle();
    if (sends.at(-1)?.thumbFormat !== 'png') fail(`the thumbnail picked as PNG is sent as ${sends.at(-1)?.thumbFormat}`);
  }
}

// With more than one instance the popup asks where to: the button keeps
// sending until the worker says what became of it, and a popup closed
// without a pick takes it back to rest with nothing shown.
{
  const store = makeStore();
  const { chrome } = makeChrome(store);
  chrome.runtime.sendMessage = async (msg) => {
    if (msg.type === 'knightloader-video-labels') return { ...PAGE_LABELS, sending: 'Sending', failed: 'Could not send' };
    if (msg.type === 'knightloader-video-send') return { result: 'asking' };
    return undefined;
  };
  const { doc, run } = loadPage(chrome, 'https://www.youtube.com/watch?v=aqz-KE-bpKQ');
  const player = doc.createElement('div');
  doc.documentElement.append(player);
  video(doc, player, { left: 0, top: 0, width: 1280, height: 720 });
  run();
  await settle();
  const shadow = doc.documentElement.querySelectorAll('knightloader-video-button')[0]?.shadow;
  const send = () => shadow.querySelectorAll('button').find((b) => b.className === 'send');
  const frame = () => shadow.querySelectorAll('div').find((d) => d.className === 'kl');
  const shows = () => `${frame()?.dataset.state} "${send().children.at(-1)?.textContent}"`;
  const answer = (a) => { for (const f of chrome.runtime.onMessage.listeners) f({ type: 'knightloader-video-result', answer: a }, {}); };

  send().fire('click');
  await settle();
  if (shows() !== 'sending "Sending"') fail(`while the popup asks the button shows ${shows()}, want sending`);
  answer(null);
  await settle();
  if (shows() !== ' "Download"') fail(`after the popup closed without a pick the button shows ${shows()}, want it at rest`);
  answer({ result: 'sent' });
  await settle();
  if (shows() !== ' "Download"') fail(`an answer nobody waits for shows ${shows()} on the button`);

  send().fire('click');
  await settle();
  answer({ result: 'sent' });
  await settle();
  if (shows() !== 'sent "Sent"') fail(`after the popup's pick went through the button shows ${shows()}, want sent`);
}

// 6. The keyboard shortcut sends the video in the active tab, through the
// button on its page, as a click would.
{
  const command = Object.keys(manifest.commands ?? {})[0];
  if (!command) fail('the manifest has no keyboard shortcut for the button');
  else if (!manifest.commands[command].description) fail('the shortcut has no description for the browser to list it by');

  for (const granted of [['<all_urls>'], []]) {
    const store = makeStore();
    const { chrome } = makeChrome(store, { granted });
    chrome.commands = { onCommand: event() };
    const asked = [];
    chrome.tabs.sendMessage = async (tabId, msg, options) => { asked.push({ tabId, msg, options }); };
    const ctx = loadBackground(chrome);
    const listens = vm.runInContext('typeof VIDEO_COMMAND === "string" ? VIDEO_COMMAND : undefined', ctx);
    if (listens !== command) fail(`the worker listens for ${listens}, the manifest names ${command}`);
    const press = async (tab, name = command) => {
      for (const f of chrome.commands.onCommand.listeners) f(name, tab);
      await settle();
    };
    const watch = { id: 3, url: 'https://www.youtube.com/watch?v=aqz-KE-bpKQ' };
    await press(watch);
    if (!granted.length) {
      if (asked.length) fail('without access to the site the shortcut still asks the tab to send');
      continue;
    }
    const got = asked.at(-1);
    if (asked.length !== 1 || got.tabId !== 3 || got.msg?.type !== 'knightloader-video-shortcut' || got.options?.frameId !== 0) {
      fail(`on a watch page the shortcut asks ${JSON.stringify(asked)}, want the top frame of tab 3 to send once`);
    }
    const first = asked.length;
    await press(watch, 'something-else');
    await press({ id: 4, url: 'https://example.org/watch?v=aqz-KE-bpKQ' });
    await press({ id: 5, url: 'https://www.youtube.com/feed/subscriptions' });
    await press({ id: 6 });
    await store.local.set({ videoButtonHidden: ['youtube'] });
    await press(watch);
    await store.local.set({ videoButtonHidden: [], videoButton: false });
    await press(watch);
    if (asked.length !== first) fail(`another command, a page without a video or a site the button is off on still sent: ${JSON.stringify(asked.slice(1))}`);
  }

  // On the page the button sends the site's defaults and shows how it went,
  // even with auto-hide keeping it out of sight until then.
  const store = makeStore({ videoButtonAutoHide: true, videoDefaults: { youtube: { parts: ['audio'], quality: 'best', audio: 'opus' } } });
  const { chrome } = makeChrome(store);
  const sends = [];
  chrome.runtime.sendMessage = async (msg) => {
    if (msg.type === 'knightloader-video-labels') return PAGE_LABELS;
    if (msg.type === 'knightloader-video-send') {
      sends.push(msg);
      return { result: 'sent' };
    }
    return undefined;
  };
  const { doc, location, run } = loadPage(chrome, 'https://www.youtube.com/watch?v=aqz-KE-bpKQ');
  const player = doc.createElement('div');
  doc.documentElement.append(player);
  video(doc, player, { left: 0, top: 56, width: 1280, height: 720 });
  run();
  await settle();
  const shortcut = async () => {
    for (const f of chrome.runtime.onMessage.listeners) f({ type: 'knightloader-video-shortcut' }, { id: 'test' }, () => {});
    await settle();
  };
  const host = doc.documentElement.querySelectorAll('knightloader-video-button')[0];
  const frame = host?.shadow.querySelectorAll('div').find((d) => d.className === 'kl');
  if (frame?.classList.contains('shown')) fail('with auto-hide the button shows before the shortcut');
  await shortcut();
  if (sends.length !== 1 || JSON.stringify(sends[0].pick) !== '{"parts":["audio"],"quality":"best","videoFormat":"best","audio":"opus","audioRate":"best","subLangs":[],"subFormat":"srt"}') {
    fail(`the shortcut sends ${JSON.stringify(sends.map((s) => s.pick))}, want YouTube's defaults once`);
  }
  if (frame?.dataset.state !== 'sent') fail(`after the shortcut the button shows ${frame?.dataset.state}, want sent`);
  if (!frame?.classList.contains('shown')) fail('with auto-hide the button stays out of sight while it shows what the shortcut did');
  if (!(doc.animations ?? []).some((a) => a.node.attrs.class === 'star')) fail('a send by the shortcut does not cheer');
  location.href = 'https://www.youtube.com/feed/subscriptions';
  doc.dispatch('yt-navigate-finish');
  const sent = sends.length;
  await shortcut();
  if (sends.length !== sent) fail('on a page without a video the shortcut sends');
}

// Firefox on Windows and Linux takes Alt with a menu's access key for the menu
// bar, Shift held or not, so the default's letter opens no menu in the common
// locales (from Firefox's menubar.ftl, Profiles included).
{
  const menuKeys = {
    de: 'DBACLPXH', en: 'FEVSBPTH', fr: 'FNAHMPOE', es: 'AEVSMPTU', it: 'FMVCEPSA',
    nl: 'BWLGAPXH', pl: 'PEWHZONC', 'pt-BR': 'AEXHVPFU', sv: 'ARSOBPVH',
  };
  const shortcut = Object.values(manifest.commands ?? {})[0]?.suggested_key?.default ?? '';
  const letter = shortcut.split('+').at(-1).toUpperCase();
  for (const [locale, keys] of Object.entries(menuKeys)) {
    if (keys.includes(letter)) fail(`the default shortcut ${shortcut} opens a menu in Firefox's ${locale} menu bar`);
  }
}

// 7. The shortcut's row on the options page: the current shortcut, changed
// and reset in place where the browser allows it (Firefox), and otherwise a
// way to the browser's own shortcuts page.
for (const editable of [true, false]) {
  const browser = editable ? 'Firefox' : 'Chromium';
  const store = makeStore({
    videoDefaults: { youtube: { parts: ['video', 'audio', 'subtitle', 'thumbnail'], subFormat: 'srv3', thumbFormat: 'best' } },
    videoSeen: { youtube: { videoFormats: ['mp4 avc1', 'webm vp9'], audio: ['aac', 'opus'], subFormats: ['srv3', 'vtt'], thumbFormats: ['webp', 'jpg'] } },
  });
  const { chrome, calls } = makeChrome(store);
  const command = Object.keys(manifest.commands ?? {})[0] ?? 'send-video';
  const fallback = manifest.commands?.[command]?.suggested_key?.default ?? 'Alt+Shift+K';
  let current = 'Alt+Shift+Y';
  const updates = [];
  chrome.commands = {
    getAll: async () => [{ name: command, shortcut: current, description: 'Send' }],
    ...(editable && {
      update: async ({ name, shortcut }) => {
        updates.push({ name, shortcut });
        current = shortcut;
      },
      reset: async (name) => {
        updates.push({ name, reset: true });
        current = fallback;
      },
    }),
  };
  const doc = makeDocument();
  const source = read('src', 'options-video.js');
  const html = read('src', 'options.html');
  doc.byId = Object.fromEntries([...source.matchAll(/getElementById\('(\w+)'\)/g)].map(([, id]) => {
    const n = new Node(/Reset|Action|Enabled|AutoHide$/.test(id) ? 'button' : id === 'videoShortcut' ? 'input' : 'div', doc);
    n.attrs.id = id;
    n.hidden = new RegExp(`id="${id}"[^>]* hidden[ >]`).test(html);
    return [id, n];
  }));
  for (const id of ['videoAutoHide', 'videoSpotReset', 'videoShortcut', 'videoShortcutAction', 'videoSites']) if (doc.byId[id]) doc.byId.videoMore.append(doc.byId[id]);
  doc.createTextNode = (text) => Object.assign(new Node('#text', doc), { text });
  const shaken = [];
  const ctx = vm.createContext({
    chrome, document: doc, URL, console, setTimeout, clearTimeout, navigator: { platform: 'Win32', userAgent: 'test' },
    t: (key) => key, loadLanguage: async () => {}, rowLabel() {}, glimGlyph: (name) => Object.assign(new Node('svg', doc), { glyph: name }), shake: (el) => shaken.push(el),
  });
  vm.runInContext(read('src', 'video.js'), ctx, { filename: 'video.js' });
  vm.runInContext(source, ctx, { filename: 'options-video.js' });
  await settle();
  // The site's defaults name formats and codecs as they are written, and
  // offer subtitles as SRT, WebVTT or TTML and a thumbnail in a format the
  // site has, neither with a Best.
  const options = doc.byId.videoSites.querySelectorAll('option');
  const names = options.map((o) => o.text);
  for (const name of ['MP4 · H.264', 'WebM · VP9', 'AAC', 'Opus', 'SRT', 'WebVTT', 'TTML', 'WebP', 'JPEG']) if (!names.includes(name)) fail(`${browser}: the site's defaults offer ${names.join(', ')}, want ${name} among them`);
  if (names.some((n) => /srv/i.test(n))) fail(`${browser}: the site's defaults offer YouTube's own subtitle format`);
  const select = (key) => doc.byId.videoSites.querySelectorAll('select').find((s) => s.dataset.videoKey === `youtube-${key}`);
  const choices = (key) => select(key)?.children.map((o) => (o.selected ? `[${o.value}]` : o.value)).join(' ');
  if (choices('subformat') !== '[srt] vtt ttml') fail(`${browser}: with srv3 stored the subtitle formats read ${choices('subformat')}, want SRT picked and no Best`);
  if (choices('thumbformat') !== 'webp [jpg]') fail(`${browser}: with Best stored the thumbnail formats read ${choices('thumbformat')}, want JPEG picked and no Best`);
  const field = doc.byId.videoShortcut;
  const action = doc.byId.videoShortcutAction;
  const problem = doc.byId.videoShortcutError;
  if (!field || !action || !problem) {
    fail(`${browser}: the video card has no shortcut row`);
    continue;
  }
  if (field.value !== 'Alt+Shift+Y') fail(`${browser}: the row shows "${field.value}", want the current Alt+Shift+Y`);
  const key = (e) => field.dispatch('keydown', { ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, code: '', ...e });

  if (editable) {
    const valid = vm.runInContext('videoShortcutValid', ctx);
    if (!valid(fallback)) fail(`the manifest's shortcut ${fallback} is one Firefox would refuse`);
    for (const s of ['Alt+Shift+D', 'Ctrl+Comma', 'MacCtrl+Shift+Up', 'Command+K', 'F5', 'Shift+F12', 'MediaPlayPause']) if (!valid(s)) fail(`${s} is refused, Firefox takes it`);
    for (const s of ['D', 'Shift+D', 'Ctrl+Alt+Shift+D', 'Alt+Alt+D', 'Meta+D', 'Ctrl+Tab', 'Ctrl+F13', 'Ctrl+MediaStop', '']) if (valid(s)) fail(`${JSON.stringify(s)} is taken, Firefox refuses it`);

    field.focus();
    field.dispatch('focus');
    key({ key: 'Control', ctrlKey: true });
    key({ key: 'Shift', ctrlKey: true, shiftKey: true });
    if (updates.length || !problem.hidden) fail('holding the modifiers alone already changes the shortcut or complains');
    key({ key: 'K', code: 'KeyK', ctrlKey: true, shiftKey: true });
    await settle();
    if (JSON.stringify(updates) !== JSON.stringify([{ name: command, shortcut: 'Ctrl+Shift+K' }])) fail(`${browser}: Ctrl+Shift+K changes the shortcut with ${JSON.stringify(updates)}`);
    // A QWERTZ keyboard has Z where QWERTY has Y, and Firefox matches the
    // letter the key types.
    key({ key: 'z', code: 'KeyY', altKey: true });
    await settle();
    if (updates.at(-1)?.shortcut !== 'Alt+Z') fail(`Alt with the key that types z becomes ${updates.at(-1)?.shortcut}, want Alt+Z`);
    key({ key: '!', code: 'Digit1', altKey: true, shiftKey: true });
    await settle();
    if (updates.at(-1)?.shortcut !== 'Alt+Shift+1') fail(`Alt+Shift with the 1 key becomes ${updates.at(-1)?.shortcut}, want Alt+Shift+1`);
    const before = updates.length;
    key({ key: 'K', code: 'KeyK', shiftKey: true });
    await settle();
    if (updates.length !== before) fail('Shift with a letter changes the shortcut, which Firefox refuses');
    if (problem.hidden || problem.text !== 'options.videoShortcutInvalid') fail('a shortcut Firefox refuses shows no error');
    if (!shaken.includes(field)) fail('a refused shortcut does not shake the field');
    field.blur();
    field.dispatch('blur');
    await settle();
    if (field.value !== 'Alt+Shift+1') fail(`after the change the row shows "${field.value}", want Alt+Shift+1`);
    if (!problem.hidden) fail('the error stays after the field is left');
    if (action.disabled) fail('the reset is off while the shortcut differs from the default');
    action.dispatch('click');
    await settle();
    if (updates.at(-1)?.reset !== true || field.value !== fallback) fail(`the reset leaves "${field.value}", want ${fallback}`);
    if (!action.disabled) fail('the reset stays on at the default');
  } else {
    if (!/<input id="videoShortcut"[^>]* readonly/.test(html)) fail('the shortcut field can be typed in, where only a key press should change it');
    if (field.tabIndex !== -1) fail('Chromium: the shortcut field takes the keyboard, but the page cannot change the shortcut');
    key({ key: 'K', code: 'KeyK', ctrlKey: true, shiftKey: true });
    await settle();
    if (field.value !== 'Alt+Shift+Y') fail('Chromium: a key press in the field changes what the row shows');
    if (!action.children.some((c) => c.text === 'options.videoShortcutChange')) fail('Chromium: the row offers no way to change the shortcut');
    action.dispatch('click');
    await settle();
    if (calls.tabs.at(-1) !== 'chrome://extensions/shortcuts') fail(`Chromium: the row opens ${calls.tabs.at(-1)}, want chrome://extensions/shortcuts`);
    current = '';
    doc.hidden = false;
    doc.dispatch('visibilitychange');
    await settle();
    if (field.value !== '' || field.placeholder !== 'options.videoShortcutNone') fail(`Chromium: back from its shortcuts page with none set, the row shows "${field.value || field.placeholder}"`);
  }

  await store.local.set({ videoButton: false });
  await settle();
  if (!field.disabled || !action.disabled) fail(`${browser}: with the button off the shortcut row stays usable`);
}

if (problems.length) {
  console.error(`${problems.length} problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log("ok: every form of a video page sends its one address, the sites are a short list yt-dlp knows inside the optional access, a click sends the site's defaults to start at once and says when the video was there already, the menu offers what the video has, a drag keeps its place as shares of the player and sends nothing, auto-hide shows the button only over the video, and the switches take it off an open page at once and back");
