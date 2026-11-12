/**
 * The download button on a video's player (src/video.js, src/video-worker.js,
 * src/video-button.js), run against a stubbed `chrome` and a small stand-in
 * for the page.
 *
 *   1. Every form a video's page comes in sends the one address the video
 *      goes by, the playlist only when asked, and a page that plays no one
 *      video sends nothing. Any other web page goes as it is.
 *   2. The sites are a short list of https addresses inside the optional
 *      access, none of them a static content script, and each is a site
 *      yt-dlp on the instance knows. All other sites run on the access to
 *      all websites that Click'n'Load shares.
 *   3. A site's stored defaults become the choice a click sends, cleaned of
 *      anything the instance would refuse. What the instance says a video
 *      has rules out what it lacks and makes a pick exact for it, and is
 *      kept per site for the options page. A dragged button is stored as
 *      shares of its player, kept inside it, so it keeps its place when the
 *      player changes size. Auto-hide shows the button only with the pointer
 *      over the video or while it is in use. On other sites only a video
 *      somebody watches counts.
 *   4. The worker registers the button only where it runs, sends only the
 *      video of the page that asked, to start at once with its choice, and
 *      tells the button whether it was sent, was held back or was there
 *      already. The menu lists what the instance says the video has, and
 *      what it offered is kept for the site. A page of another site is
 *      checked once per origin and path for the session.
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
 *      video. A player that keeps its video in a shadow root gets the
 *      button too, in fullscreen as well.
 *   6. The keyboard shortcut has the button in the active tab send the site's
 *      defaults, only on a video page of a site the button runs on, and its
 *      default opens none of Firefox's menus.
 *   7. The options card shows the shortcut, changes and resets it in Firefox
 *      within the shortcuts Firefox takes, and in Chromium opens the
 *      browser's shortcuts page. Its tile for all other sites is built as a
 *      site's is and asks for the access to all websites on its own switch.
 *   8. The sites are tiles that say what a click downloads and open a window
 *      with the site's controls, which keeps the keyboard and gives it back.
 *      A site added by its address gets defaults of its own, which the
 *      button and the worker look up by host before all other sites, and it
 *      is removed after a question.
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
  const session = {};
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
      session: {
        get: async (k) => (k == null ? { ...session } : Object.fromEntries([].concat(k).filter((x) => x in session).map((x) => [x, session[x]]))),
        set: async (o) => { Object.assign(session, o); },
        remove: async (k) => { for (const x of [].concat(k)) delete session[x]; },
      },
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
  return { chrome, calls, live, session };
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
    'https://music.youtube.com/watch?v=aqz-KE-bpKQ',
    'https://player.vimeo.com/video/1084537',
    'https://vimeo.com/channels/staffpicks',
    'https://www.twitch.tv/somechannel',
    'https://clips.twitch.tv/embed',
    'https://www.bilibili.com/',
    'not a url',
    'ftp://files.example.org/clip.mp4',
    'file:///home/me/clip.mp4',
    'chrome-extension://test/options.html',
  ]) {
    if (page(href)) fail(`${href} sends ${page(href).url}, want nothing`);
  }

  // Any other web page goes as it is, without the part after #, under the
  // switch for all other sites. A host that only looks like one of the sites
  // is another site.
  for (const [href, url] of [
    ['https://clips.example.org/watch/7?lang=de#t=42', 'https://clips.example.org/watch/7?lang=de'],
    ['http://tv.example.net/live', 'http://tv.example.net/live'],
    ['https://www.youtube.com.example.org/watch?v=aqz-KE-bpKQ', 'https://www.youtube.com.example.org/watch?v=aqz-KE-bpKQ'],
  ]) {
    const got = page(href);
    if (got?.site !== 'other' || got.url !== url || got.playlist !== '') fail(`${href} reads as ${JSON.stringify(got)}, want another site's page at ${url}`);
  }
  const key = vm.runInContext('videoCheckKey', ctx);
  if (key('https://clips.example.org/watch/7?lang=de#t=42') !== 'https://clips.example.org/watch/7' || key('not a url') !== '') fail('the check is not kept by origin and path');
  const own = vm.runInContext('videoOwnPage', ctx);
  const other = page('https://clips.example.org/watch/7');
  if (!own('https://clips.example.org/watch/8', other) || own('https://evil.example.com/watch/7', other) || own('https://www.youtube.com/watch?v=aqz-KE-bpKQ', other)) {
    fail('a frame may speak for a page of another site outside its own origin, or not for one of its own');
  }
  if (!own('https://m.youtube.com/', page(`https://www.youtube.com/watch?v=${ID}`)) || own('https://clips.example.org/', page(`https://www.youtube.com/watch?v=${ID}`))) fail('a frame speaks for a video of another site, or not for one of its own');
  const title = vm.runInContext('videoTitle', ctx);
  if (title('Big Buck Bunny - YouTube', 'other') !== 'Big Buck Bunny - YouTube') fail('the tab title of another site loses a part it never added');
  if (vm.runInContext(`videoName('https://clips.example.org/')`, ctx) !== 'clips.example.org') fail(`a page of another site at its root is called ${vm.runInContext(`videoName('https://clips.example.org/')`, ctx)}`);
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
  const all = vm.runInContext('VIDEO_OTHER', ctx);
  if (all.origins.join() !== '<all_urls>' || !optional.includes('<all_urls>')) fail(`all other sites run on ${all.origins}, want the optional access to all websites`);
  const cnl = read('src', 'shared.js').match(/const CNL_ACCESS = \{ origins: \[([^\]]*)\] \}/)?.[1];
  if (cnl !== `'${all.origins[0]}'`) fail(`all other sites ask for ${all.origins}, Click'n'Load for ${cnl}, want the one access both share`);
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
  // YouTube lists its speech recognition twice, as "de-orig" and as a
  // translation into the same language, so the menu offers it once, as the
  // original, and a wish for German picks that one.
  ctx.twice = run(`videoOffer({ ...answer, subtitles: [], autoCaptions: ['de-orig', 'de', 'en', 'fr'] })`);
  const wantsDe = `videoPick({ parts: ['subtitle'], subLangs: ['de'] })`;
  if (!same(run(`videoSubtitleChoices(twice, ${wantsDe}).auto`), ['de-orig'])) fail(`with de-orig and de listed the menu offers ${run(`videoSubtitleChoices(twice, ${wantsDe}).auto`)}, want de-orig alone`);
  if (!same(run(`videoResolve(${wantsDe}, twice).subLangs`), ['de-orig'])) fail(`a wish for German picks ${run(`videoResolve(${wantsDe}, twice).subLangs`)}, want de-orig`);
  // An instance that converts video lists the conversions it makes. A pick
  // of one keeps its quality and sends the conversion beside it; one the
  // video's instance does not list falls back to Best and sends none.
  ctx.converts = run(`videoOffer({ ...answer, videoConversions: [{ format: 'mkv hevc', converted: true }, { format: 'rm;rf' }] })`);
  if (!same(ctx.converts.videoConversions, ['mkv hevc'])) fail(`the video conversions offered are ${ctx.converts.videoConversions}, want mkv hevc alone`);
  if (!same(run('offer.videoConversions'), [])) fail('an instance that lists no video conversions is offered some');
  const convertSent = (offerName) => run(`videoChoice(videoPick(videoExact(videoResolve(videoPick({ parts: ['video'], quality: '720p', videoFormat: 'mkv hevc' }), ${offerName}), ${offerName})))`);
  const hevc = convertSent('converts');
  if (hevc.videoConvert !== 'mkv hevc' || hevc.quality !== '720p') fail(`a pick of H.265 at 720p sends ${JSON.stringify(hevc)}, want the 720p cap and videoConvert mkv hevc`);
  if ('videoConvert' in convertSent('offer')) fail('a pick of H.265 is sent to an instance that converts no video');
  if ('videoConvert' in run(`videoChoice(videoPick({ parts: ['video'], videoConvert: 'mkv vp8' }))`)) fail('a video conversion the instance does not make is sent');
  ctx.convertSized = { ...ctx.converts, videoSizes: { best: 900000000, '720p': 90000000 }, audioSizes: { best: 3000000 } };
  const hevcTotal = run(`videoPickTotal(videoResolve(videoPick({ parts: ['video', 'audio'], quality: '720p', videoFormat: 'mkv hevc' }), convertSized), convertSized)`);
  if (!same(hevcTotal, { bytes: 3000000, atLeast: true })) fail(`with H.265 picked the download weighs ${JSON.stringify(hevcTotal)}, want at least the audio's 3 MB`);

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

  // MP3 and FLAC are conversions: they go as the audio format, without a
  // track, whatever bitrate was picked, and a default naming one stays.
  for (const audio of ['mp3', 'flac']) {
    const sent = run(`videoChoice(videoExact(videoResolve(videoPick({ parts: ['audio'], audio: '${audio}', audioRate: '128k' }), offer), offer))`);
    if (sent.audioFormat !== audio || 'audioTrack' in sent) fail(`a pick of ${audio} sends ${JSON.stringify(sent)}, want the format alone`);
  }
  if (!same(run('VIDEO_AUDIO_MENU'), ['aac', 'opus', 'mp3', 'flac'])) fail(`the audio formats offered are ${run('VIDEO_AUDIO_MENU')}, want AAC, Opus, MP3 and FLAC`);
  if (!same(run(`videoOffer({ ...answer, subtitleFormats: ['vtt', 'srv3'] }).subtitleFormats`), ['vtt', 'srv3'])) fail('the offer forgets which subtitle formats the host serves');

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
  const plaques = run('videoMenuRows(yt).qualities.map(videoPlaques)');
  if (!same(plaques, [[], ['60 fps'], [], ['60 fps']])) fail(`the qualities carry the plaques ${JSON.stringify(plaques)}, want 60 fps where a track runs above 30 fps and nothing else`);

  // An HDR track's key carries hdr after its quality. The menu keeps it as a
  // quality of its own, above the plain one of its height, with an HDR plaque,
  // and a pick of either sends its own track: a cap alone would let yt-dlp take
  // the HDR track for the plain row, since it ranks HDR first.
  ctx.hdr = run(`videoOffer(${JSON.stringify({
    qualities: ['2160p', '1080p'], audio: false,
    videoTracks: ['2160phdr webm vp9', '2160p webm vp9', '1080p60hdr mp4 av1', '1080p mp4 avc1'], audioTracks: [], audioLangs: [], subtitles: [], autoCaptions: [], subtitleFormats: [],
    thumbnail: null, description: true,
  })})`);
  if (run('hdr.videoTracks').length !== 4) fail(`the offer keeps the tracks ${run('hdr.videoTracks')}, want the HDR ones too`);
  if (!same(run('videoMenuRows(hdr).qualities'), ['2160phdr', '2160p', '1080p60hdr', '1080p'])) fail(`the HDR qualities read ${run('videoMenuRows(hdr).qualities')}, want each HDR one above the plain one of its height`);
  if (!same(run(`videoPlaques('1080p60hdr')`), ['60 fps', 'HDR'])) fail(`1080p60hdr carries ${run(`videoPlaques('1080p60hdr')`)}, want 60 fps and HDR`);
  const hdrSends = (sel) => run(`videoExact({ ...videoResolve(videoPick({ parts: ['video'] }), hdr), ...${JSON.stringify(sel)} }, hdr)`);
  for (const [sel, want] of [
    [{ quality: '2160phdr' }, { quality: 'best', videoTrack: '2160phdr webm vp9' }],
    [{ quality: '2160p' }, { quality: 'best', videoTrack: '2160p webm vp9' }],
    [{ quality: '1080p' }, { quality: 'best', videoTrack: '1080p mp4 avc1' }],
    [{}, { quality: 'best' }],
  ]) {
    const got = hdrSends(sel);
    const picked = { quality: got.quality, ...(got.videoTrack ? { videoTrack: got.videoTrack } : {}) };
    if (!same(picked, want)) fail(`picked ${JSON.stringify(sel)} with HDR at its height, the send is ${JSON.stringify(picked)}, want ${JSON.stringify(want)}`);
  }
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
  // The download button weighs the whole pick. Subtitles, a thumbnail and a
  // description weigh next to nothing; a video or audio part the instance
  // cannot weigh, such as a conversion, makes the sum a lower bound rather
  // than hiding it.
  const totalOf = (sel) => JSON.stringify(vm.runInContext(`videoPickTotal({ ...videoResolve(videoPick({ parts: ['video', 'audio'] }), sized), ...${JSON.stringify(sel)} }, sized)`, ctx));
  for (const [sel, want] of [
    [{}, { bytes: 903400000, atLeast: false }],
    [{ parts: ['video'] }, { bytes: 900000000, atLeast: false }],
    [{ quality: '1080p', audioRate: '49k' }, { bytes: 381200000, atLeast: false }],
    [{ audioRate: '130k' }, { bytes: 900000000, atLeast: true }],
    [{ audio: 'mp3' }, { bytes: 900000000, atLeast: true }],
    [{ parts: ['video', 'audio', 'subtitle', 'thumbnail', 'description'] }, { bytes: 903400000, atLeast: false }],
    [{ parts: ['thumbnail', 'description'] }, undefined],
  ]) {
    if (totalOf(sel) !== JSON.stringify(want)) fail(`the pick ${JSON.stringify(sel)} weighs ${totalOf(sel)} in all, want ${JSON.stringify(want)}`);
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
  const seenWant = { qualities: ['1080p', '720p'], audio: ['aac', 'opus'], subLangs: ['de', 'en'], videoFormats: ['mp4 avc1', 'webm vp9'], audioRates: ['160k', '128k'], thumbFormats: ['webp'], thumbServed: ['webp'] };
  if (!same(seen, seenWant)) fail(`a site is remembered as offering ${JSON.stringify(seen)}`);
  const seenMore = run('videoSeenWith(videoSeenWith(undefined, offer), yt)');
  if (!same(seenMore.videoFormats, ['mp4 avc1', 'mp4 vp9', 'mp4 av1', 'webm vp9']) || !same(seenMore.audioRates, ['160k', '130k', '128k', '124k', '62k', '49k'])) fail(`two videos of a site are remembered as offering ${JSON.stringify(seenMore)}`);
  ctx.stored = { videoSeen: { youtube: { qualities: ['480p', '2160p', '9999p'], audio: ['wav', 'opus'], subLangs: ['de', 'x y'], subFormats: ['vtt'], videoFormats: ['webm vp9', 'mp4;rm'], audioRates: ['128k', 'x'], thumbFormats: ['webp', 'x y'], thumbServed: ['jpg', 'x y'] } } };
  if (!same(run(`videoSiteSeen(stored, 'youtube')`), { qualities: ['2160p', '480p'], audio: ['opus'], subLangs: ['de'], videoFormats: ['webm vp9'], audioRates: ['128k'], thumbFormats: ['webp'], thumbServed: ['jpg'] })) fail('the options offer a site something it has not offered');
  if (!same(run(`videoSiteSeen(stored, 'vimeo')`), { qualities: [], audio: [], subLangs: [], videoFormats: [], audioRates: [], thumbFormats: [], thumbServed: [] })) fail('the options offer choices for a site that has offered none');

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

  // On another site only a video somebody watches is asked about: a player's
  // size, at least half on screen, started, and not a muted loop or a muted
  // video that started itself without controls.
  const watched = vm.runInContext('videoWatched', ctx);
  const playing = { width: 1280, height: 720, seen: 1, muted: false, controls: false, autoplay: false, loop: false, paused: false, time: 3 };
  for (const [change, want, why] of [
    [{}, true, 'a player that plays'],
    [{ paused: true }, true, 'a player paused after it played'],
    [{ controls: true, muted: true, autoplay: true }, true, 'a muted video that starts itself with its controls showing'],
    [{ muted: true }, true, 'a video the viewer muted'],
    [{ width: 199 }, false, 'a video under 200px wide'],
    [{ height: 111 }, false, 'a video under 112px tall'],
    [{ width: 160, height: 90 }, false, 'a small preview'],
    [{ seen: 0.4 }, false, 'a video mostly off screen'],
    [{ seen: 0 }, false, 'a video off screen'],
    [{ paused: true, time: 0 }, false, 'a poster waiting for a click'],
    [{ muted: true, autoplay: true }, false, 'a muted video that starts itself without controls'],
    [{ muted: true, loop: true }, false, 'a muted loop without controls'],
  ]) {
    if (watched({ ...playing, ...change }) !== want) fail(`${why} ${want ? 'is not' : 'is'} taken for a video somebody watches`);
  }

  // What a click downloads on all other sites follows their own defaults, and
  // the options offer what those sites have offered so far.
  ctx.stored = { videoDefaults: { other: { parts: ['audio'], audio: 'mp3' }, youtube: { parts: ['video'], quality: '720p' } }, videoSeen: { other: { qualities: ['720p', '480p'], audio: ['aac'] } } };
  const otherChoice = run(`videoChoice(videoDefaultsFor(stored, 'other'))`);
  if (!same(otherChoice, { ...best, variants: ['audio'], audioFormat: 'mp3' })) fail(`all other sites send ${JSON.stringify(otherChoice)}, want their own default, the audio as MP3`);
  if (!same(run(`videoSiteSeen(stored, 'other')`).qualities, ['720p', '480p'])) fail('the options do not offer what other sites have offered');
  ctx.stored = {};
  if (!same(run(`videoChoice(videoDefaultsFor(stored, 'other'))`), best)) fail('all other sites with nothing set do not send the best video');
  if (!run(`videoButtonWanted({}, 'other')`)) fail('the button is not on all other sites with nothing stored');
  if (run(`videoButtonWanted({ videoButtonHidden: ['other'] }, 'other')`) || !run(`videoButtonWanted({ videoButtonHidden: ['other'] }, 'youtube')`)) fail('hiding the button on all other sites does not hide it there alone');

  // A site added on the options page is found by its host and every host
  // under it, the longest first, and the named sites keep their own.
  ctx.stored = { videoCustomSites: ['example.org', 'sub.example.org'] };
  for (const [href, want] of [
    ['https://example.org/v/1', 'example.org'],
    ['https://www.example.org/v/1', 'example.org'],
    ['https://a.sub.example.org/v', 'sub.example.org'],
    ['https://notexample.org/v', 'other'],
    ['https://www.youtube.com/watch?v=aqz-KE-bpKQ', 'youtube'],
  ]) {
    const got = run(`videoDefaultsKey(stored, videoPage(${JSON.stringify(href)}))`);
    if (got !== want) fail(`the defaults for ${href} are looked up under ${got}, want ${want}`);
  }
}

// 4. The worker.
{
  const store = makeStore();
  const { chrome, calls, live, session } = makeChrome(store);
  const ctx = loadBackground(chrome);
  await vm.runInContext('applyVideoButton', ctx)();
  const want = vm.runInContext('VIDEO_SITES', ctx).flatMap((s) => s.origins);
  const reg = live.get('video-button');
  if (!reg) fail('with the access granted and nothing stored, the button is not registered');
  else {
    if (reg.matches.join('|') !== '<all_urls>') fail(`with the access to all websites the button is registered on ${reg.matches.join(', ')}, want every page, for all other sites`);
    if (!reg.allFrames) fail('the button is not registered in frames, so an embedded player never gets it');
  }
  if (!calls.injected.some((o) => o.files?.includes('video-button.js'))) fail('a tab that was open before the button went on does not get it');

  await store.local.set({ videoButtonHidden: ['other'] });
  await settle();
  if (live.get('video-button')?.matches.join('|') !== want.join('|')) fail(`hidden on all other sites, the button is registered on ${live.get('video-button')?.matches.join(', ')}, want the sites' own addresses`);
  await store.local.set({ videoButtonHidden: ['other', 'vimeo'] });
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
  const oneInstance = async (work) => work({
    siblings: [{ instanceId: 'inst-1' }],
    call: async (target, method, path, body) => { sent.push({ target, method, path, body: body ? JSON.parse(body) : undefined }); return answer; },
  });
  ctx.withGroup = oneInstance;
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
  if (JSON.stringify(store.data.videoSeen?.youtube) !== '{"qualities":["1080p","720p"],"audio":["opus"],"subLangs":["de"],"videoFormats":["mp4 avc1"],"audioRates":["160k"],"thumbFormats":[],"thumbServed":[]}') fail(`YouTube is remembered as offering ${JSON.stringify(store.data.videoSeen?.youtube)}`);
  answer = { status: 502, body: 'ERROR: [youtube] aqz-KE-bpKQ: Private video' };
  menu = await videoFormats(embed, { href: embed });
  if (menu.qualities?.length || menu.known) fail(`a probe that failed offers ${JSON.stringify(menu)}, want nothing known`);
  const asking = sent.length;
  menu = await videoFormats('https://www.youtube.com/watch?v=aqz-KE-bpKQ', { href: 'https://vimeo.com/1084537' });
  if (sent.length !== asking || menu.known) fail('a frame on one site could ask about a video of another');
  answer = { status: 200, body: '{"tasks":[{"id":"a"}],"listed":[]}' };

  // On all other sites the button first asks whether yt-dlp downloads the
  // page, once per origin and path for the session, and the menu gets the
  // offer that answered.
  const videoCheck = vm.runInContext('videoCheck', ctx);
  const clip = 'https://clips.example.org/watch/7?lang=de';
  const clipAnswer = { status: 200, body: JSON.stringify({ qualities: ['720p'], audio: true, videoTracks: ['720p mp4 avc1'], audioTracks: ['aac 128k'], audioLangs: [], subtitles: [], autoCaptions: [], subtitleFormats: [], thumbnail: null, description: true }) };
  answer = clipAnswer;
  let asks = sent.length;
  let check = await videoCheck(clip, { href: `${clip}#t=4` });
  if (sent.length !== asks + 1 || sent.at(-1).method !== 'GET' || sent.at(-1).path !== `/api/ytdlp/link?url=${encodeURIComponent(clip)}`) fail(`another site's page is checked with ${JSON.stringify(sent.slice(asks))}, want one GET /api/ytdlp/link for its address`);
  if (check.claims !== true || check.offer?.videoTracks?.join() !== '720p mp4 avc1') fail(`a page yt-dlp downloads answers ${JSON.stringify(check)}, want yes with its offer`);
  if (JSON.stringify(store.data.videoSeen?.other?.qualities) !== '["720p"]') fail(`all other sites are remembered as offering ${JSON.stringify(store.data.videoSeen?.other)}`);
  asks = sent.length;
  check = await videoCheck('https://clips.example.org/watch/7?lang=en', { href: 'https://clips.example.org/watch/7?lang=en' });
  if (sent.length !== asks || check.claims !== true) fail('a page already checked is asked about again');
  // A site added on the options page keeps what it offered under its host.
  await store.local.set({ videoCustomSites: ['example.net'] });
  await videoCheck('https://media.example.net/v/1', { href: 'https://media.example.net/v/1' });
  if (JSON.stringify(store.data.videoSeen?.['example.net']?.qualities) !== '["720p"]') fail(`a site added on the options page is remembered as offering ${JSON.stringify(store.data.videoSeen?.['example.net'])}`);
  await store.local.set({ videoCustomSites: [] });
  answer = { status: 502, body: 'ERROR: Unsupported URL: https://blog.example.org/post/1' };
  const plain = 'https://blog.example.org/post/1';
  check = await videoCheck(plain, { href: plain });
  if (check.claims !== false || 'offer' in check) fail(`a page yt-dlp cannot download answers ${JSON.stringify(check)}, want no`);
  asks = sent.length;
  await videoCheck(plain, { href: plain });
  if (sent.length !== asks) fail('a page yt-dlp could not download is asked about again at once');
  session.videoChecks[plain].at -= 11 * 60 * 1000;
  await videoCheck(plain, { href: plain });
  if (sent.length !== asks + 1) fail('a page yt-dlp could not download is never asked about again');
  answer = clipAnswer;
  asks = sent.length;
  const twin = 'https://twin.example.org/v';
  await Promise.all([videoCheck(twin, { href: twin }), videoCheck(twin, { href: twin })]);
  if (sent.length !== asks + 1) fail(`two frames of one page checking at once ask ${sent.length - asks} times, want once`);
  ctx.withGroup = async () => { const e = new Error('no-phrase'); e.code = 'no-phrase'; throw e; };
  const later = 'https://later.example.org/v';
  check = await videoCheck(later, { href: later });
  if (check.claims !== null) fail(`with no instance to ask the check answers ${JSON.stringify(check)}, want nothing known`);
  ctx.withGroup = oneInstance;
  asks = sent.length;
  await videoCheck(later, { href: later });
  if (sent.length !== asks + 1) fail('a check no instance answered is kept, so the page is never asked about');
  asks = sent.length;
  for (const [frame, href] of [['https://evil.example.com/', 'https://fresh.example.org/v'], ['https://www.youtube.com/watch?v=aqz-KE-bpKQ', 'https://www.youtube.com/watch?v=aqz-KE-bpKQ']]) {
    check = await videoCheck(frame, { href });
    if (check.claims !== false) fail(`a frame at ${frame} checking ${href} is answered ${JSON.stringify(check)}, want no`);
  }
  await store.local.set({ videoButtonHidden: ['other'] });
  check = await videoCheck('https://fresh.example.org/v', { href: 'https://fresh.example.org/v' });
  if (check.claims !== false) fail('with the button hidden on all other sites a page there is still checked');
  answer = { status: 200, body: '{"tasks":[{"id":"a"}],"listed":[]}' };
  r = await sendVideo(clip, { href: `${clip}#t=4`, pick: { parts: ['audio'], audio: 'mp3' } });
  if (r.result !== 'refused') fail('with the button hidden on all other sites a send from there still goes out');
  if (sent.length !== asks) fail(`checks and sends the button may not make reached the instance: ${JSON.stringify(sent.slice(asks))}`);
  // A send from another site goes as its page's address with the pick's
  // choice, and only from that page's own origin.
  await store.local.set({ videoButtonHidden: [] });
  r = await sendVideo(clip, { href: `${clip}#t=4`, pick: { parts: ['audio'], audio: 'mp3' } });
  if (r.result !== 'sent' || last()?.links !== clip || last()?.source !== clip || JSON.stringify(last()?.choice?.variants) !== '["audio"]' || last()?.choice?.audioFormat !== 'mp3') fail(`a send from another site's page arrives as ${JSON.stringify(last())}, want its address and the audio as MP3`);
  asks = sent.length;
  r = await sendVideo('https://evil.example.com/', { href: clip });
  if (r.result !== 'refused' || sent.length !== asks) fail('a frame on one origin could send the page of another');

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
  listener({ type: 'knightloader-video-send', href: embed }, { tab: { id: 1 }, url: 'chrome-extension://test/options.html' }, () => { answered = true; });
  await settle();
  if (answered) fail('a frame that is no web page got an answer to a send');
}

// Without the access to all websites the button stays on the sites it has
// access to, though all other sites are on, and the first page of another
// site that asks is answered no without a word to the instance.
{
  const store = makeStore();
  const sites = ['https://www.youtube.com/*', 'https://m.youtube.com/*', 'https://www.youtube-nocookie.com/*', 'https://vimeo.com/*', 'https://www.dailymotion.com/*', 'https://www.twitch.tv/*', 'https://clips.twitch.tv/*', 'https://www.bilibili.com/*'];
  const { chrome, live } = makeChrome(store, { granted: sites });
  const ctx = loadBackground(chrome);
  await vm.runInContext('applyVideoButton', ctx)();
  const state = await vm.runInContext('videoButtonState', ctx)();
  if (!state.shown.includes('other') || state.running.includes('other')) fail(`without the access to all websites the button's state is ${JSON.stringify(state)}, want all other sites on but not running`);
  if (live.get('video-button')?.matches.join('|') !== sites.join('|')) fail(`without the access to all websites the button is registered on ${live.get('video-button')?.matches}, want the five sites`);
  let asked = 0;
  ctx.withGroup = async () => { asked++; return null; };
  const check = await vm.runInContext('videoCheck', ctx)('https://clips.example.org/watch/7', { href: 'https://clips.example.org/watch/7' });
  if (check.claims !== false || asked) fail('without the access to all websites a page of another site is checked');
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
  get parentElement() { return this.parentNode?.host ? null : this.parentNode; }
  get isConnected() {
    let n = this;
    while (n.parentNode || n.host) n = n.parentNode ?? n.host;
    return n === this.doc.documentElement;
  }
  getRootNode() {
    let n = this;
    while (n.parentNode) n = n.parentNode;
    return n === this.doc.documentElement ? this.doc : n;
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
  attachShadow({ mode } = {}) {
    this.shadow = new Node('#shadow', this.doc);
    this.shadow.host = this;
    if (mode === 'open') this.shadowRoot = this.shadow;
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
  doc.removeEventListener = (type, f) => {
    const list = doc.listeners[type] ?? [];
    if (list.includes(f)) list.splice(list.indexOf(f), 1);
  };
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
    getComputedStyle: (n) => ({ overflow: 'visible', visibility: 'visible', opacity: '1', ...n.computed }),
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
  send: 'Download', sendTip: 'Send', more: 'More', best: 'Best', language: 'Language', dubbed: 'dubbed', autoCaptions: 'auto', converted: 'converted', quality: 'Quality', format: 'Format',
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
    known: true, videoConversions: [], qualities: ['1080p', '720p'], audio: true, videoTracks: ['1080p mp4 avc1', '1080p webm vp9', '720p mp4 avc1'],
    audioTracks: ['opus 160k', 'aac 128k'], audioLangs: [], subtitles: ['de'], autoCaptions: ['en-orig'], subtitleFormats: ['vtt'],
    thumbnail: { width: 1280, height: 720, ext: 'webp' }, description: false,
    videoSizes: { best: 404000000, '1080p': 303000000, '720p': 90000000 }, audioSizes: { 'aac 128k': 3000000 },
  });
  await settle();
  const listed = '[Video]|Quality|Best|404 MB|[1080p]|303 MB|720p|90 MB|Format|H.264|MP4|[VP9]|WebM|Audio|[Subtitles]|Language|[German]|English|auto|Format|[SRT]|converted|WebVTT|TTML|Thumbnail|Download|· 303 MB';
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
  if (withRole('checkbox').map((c) => `${c.text}:${c.attrs['aria-checked']}`).join('|') !== 'German:true|English:false') fail('the subtitle languages are not checkboxes named by their language, with their state');
  if (radios(groups[0]).find((r) => r.text === '720p')?.attrs['aria-label'] !== '720p, 90 MB') fail('a quality does not say its size to a screen reader');
  if (radios(groups[1]).find((r) => r.text === 'H.264')?.attrs['aria-label'] !== 'H.264, MP4') fail('a format does not say its container to a screen reader');
  // Video and subtitles are ticked, and subtitles weigh next to nothing, so
  // the button weighs the video.
  if (goShows() !== 'Download · 303 MB') fail(`with subtitles ticked the download button reads "${goShows()}", want the video's 303 MB`);

  const foot = menuEl().children.at(-1);
  if (foot?.className !== 'foot' || foot.children.map((b) => b.text).join('|') !== 'Download') fail('Download is not alone at the foot of the menu, where it stays in view');
  const room = parseInt(menuEl().style['max-height'] ?? '0', 10);
  if (!(room > 460 && room <= 720)) fail(`the menu may grow to ${room}px, want the room the player leaves below the button`);
  // On a page too narrow for it where it opens, such as a small embedded
  // player, the menu moves back in view, 8px from the edge.
  menuEl().rect = { left: -50, top: 100, width: 400, height: 600, right: 350, bottom: 700 };
  doc.dispatch('scroll');
  if (menuEl().style.transform !== 'translateX(58px)') fail(`a menu 50px past the page's left edge is moved by ${menuEl().style.transform}, want 58px`);
  menuEl().rect = { left: 1000, top: 100, width: 400, height: 600, right: 1400, bottom: 700 };
  doc.dispatch('scroll');
  if (menuEl().style.transform !== 'translateX(-128px)') fail(`a menu 120px past the page's right edge is moved by ${menuEl().style.transform}, want -128px`);
  menuEl().rect = { left: 400, top: 100, width: 400, height: 600, right: 800, bottom: 700 };
  doc.dispatch('scroll');
  if (menuEl().style.transform !== 'translateX(0px)') fail(`a menu in view is moved by ${menuEl().style.transform}`);
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
  if (!/background: var\(--surface2\)/.test(rule('.menu .card')) || /box-shadow|(^|[\s;])border(-top|-bottom|-left|-right|-width|-style|-color)?:/.test(rule('.menu .card'))) fail('a part is not a sub-card filled one surface up without a border');
  if (!/background: var\(--surface\)/.test(rule('    .menu'))) fail('the menu is not a window on the surface');
  if (/\.list[^{}]*::(before|after)/.test(css)) fail('a list of choices still draws a radio dot or a checkbox');
  // A part's name is its heading, an accent badge with rounded corners that
  // stays filled whether the part is on or off; the track beside it says which.
  const head = rule('.menu .switch');
  // The heading sits half over the sub-card's top edge, as GlimStone's notch:
  // the card's top padding plus half the badge's 22px pulls it up.
  const cardTop = parseFloat(rule('.menu .card').match(/padding-top: (\d+)px/)?.[1]);
  const pulled = parseFloat(head.match(/margin-top: -(\d+)px/)?.[1]);
  if (!(pulled === cardTop + 11)) fail(`a part's heading is pulled up ${pulled}px over a ${cardTop}px top padding, want it centred on the card's edge`);
  const width = parseFloat(rule('    .menu').match(/ width: (\d+)px/)?.[1]);
  if (!(width >= 380 && width <= 420)) fail(`the menu is ${width}px wide, want 380 to 420`);
  if (!/background: var\(--accent\)/.test(head) || !/color: var\(--accent-contrast\)/.test(head) || !/text-transform: uppercase/.test(head) || !/border-radius: min\(var\(--radius\), 6px\)/.test(head)) fail('a part\'s name is not a filled heading badge with rounded corners');
  if (/opacity|var\(--sub\)/.test(head) || /\.switch\[aria-checked="(true|false)"\]\s*\{/.test(css)) fail('a part\'s heading badge changes with its switch');
  // Nothing in the menu draws a line round itself: sub-cards, tiles, format
  // rails, switches and the download button part by their fill alone. The
  // one line left is the window's hairline, GlimStone's card edge.
  const lined = [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)].filter(([, , body]) => /inset 0 0 0 1px|(^|[;\s])border(-top|-bottom|-left|-right)?:/.test(body)).map(([, sel]) => sel.replace(/\/\*[\s\S]*?\*\//g, '').trim());
  if (lined.join() !== '.menu') fail(`inside the menu these draw a line: ${lined.filter((s) => s !== '.menu').join(', ')}`);
  if (!/background: var\(--surface3\)/.test(rule('.segments')) || !/padding: 0\.2rem/.test(rule('.segments'))) fail('a format rail is not the groove one surface below its sub-card, padded 0.2rem');
  // A list is plain rows packed close, unfilled at rest and one surface up
  // under the pointer; only the picked rows are filled.
  if (!/background: transparent/.test(rule('.menu .list button')) || !/height: 26px/.test(rule('.menu .list button')) || !/gap: 0/.test(rule('.list'))) fail('a list row is not a plain 26px row packed close to the next');
  if (!/background: var\(--surface3\)/.test(css.slice(css.indexOf('.list button:not([aria-checked="true"]):hover')).split('}')[0])) fail('a list row does not rise one surface under the pointer');
  if (!/background: var\(--accent\)/.test(rule('.menu .list [aria-checked="true"]'))) fail('a picked row is not filled with the accent');

  // Every list of choices is one column, so it reads top to bottom like the
  // subtitle languages.
  const lists = menuEl().querySelectorAll('div').filter((d) => d.className === 'list');
  if (lists.length !== 2 || lists.some((l) => l.style['grid-template-rows']) || /1fr 1fr/.test(read('src', 'video-button.js'))) fail('a list of choices in the menu is laid out in two columns');

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
  const audioRows = 'Audio]|Quality|[Best]|160k|128k|3 MB|Format|AAC|[Opus]|MP3|converted|FLAC|converted|';
  if (!entries().join('|').includes(audioRows)) fail(`the audio part shows ${entries().join(', ')}, want ${audioRows}`);
  item('128k').fire('click');
  if (!off('Opus') || off('AAC')) fail('at 128k the audio format opus, which the video lacks there, is not off');
  item('English').fire('click');
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
  if (goShows() !== 'Download · ≥ 404 MB' || go()?.attrs['aria-label'] !== 'Download, ≥ 404 MB') fail(`with the best audio's size unknown the download button reads "${goShows()}", want at least the video's 404 MB`);
  item('128k').fire('click');
  if (goShows() !== 'Download · 407 MB') fail(`with video and the 128k audio the download button reads "${goShows()}", want 407 MB`);
  // A conversion has no size until it is made, so the button weighs nothing.
  item('MP3').fire('click');
  if (goShows() !== 'Download · ≥ 404 MB' || !entries().join('|').includes('Quality|[Best]|')) fail(`with MP3 picked the menu shows ${entries().join(', ')} and the button reads "${goShows()}", want Best and at least the video's 404 MB`);
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

  // A preview playing in a list is no player: the home page, search results
  // and a channel get no button, and on a watch page a preview beside a
  // player scrolled out of view does not take it either.
  for (const v of player.querySelectorAll('video')) v.remove();
  const preview = video(doc, player, { left: 100, top: 300, width: 360, height: 202 });
  for (const href of ['https://www.youtube.com/', 'https://www.youtube.com/results?search_query=insel', 'https://www.youtube.com/@channel/videos', 'https://m.youtube.com/']) {
    location.href = href;
    doc.dispatch('yt-navigate-finish');
    if (button()) fail(`on ${href} with a preview playing the button shows`);
  }
  location.href = 'https://www.youtube.com/watch?v=NFHGtAS8Su4';
  const watched = video(doc, player, { left: 0, top: -900, width: 1280, height: 720 });
  doc.dispatch('yt-navigate-finish');
  if (button() && button().style.left === '280px') fail('on a watch page the button moves onto a preview while the player is scrolled out of view');
  preview.remove();
  watched.rect = { left: 0, top: 56, width: 1280, height: 720, right: 1280, bottom: 776 };
  doc.dispatch('yt-navigate-finish');

  // Until it plays, and while an advert plays in it, YouTube parks the video
  // element its own height above the player, which clips it and draws its
  // own picture. The button stays on the player rather than going up with
  // the element out of sight.
  player.computed = { overflow: 'hidden' };
  player.rect = { left: 0, top: 56, width: 1280, height: 720, right: 1280, bottom: 776 };
  watched.rect = { left: 0, top: -664, width: 1280, height: 720, right: 1280, bottom: 56 };
  doc.dispatch('yt-navigate-finish');
  if (!(parseInt(button()?.style.top, 10) > 56) || button()?.style.left !== '640px') fail(`with the video parked above its player the button sits at ${button()?.style.left}, ${button()?.style.top}, want on the player below 56px`);
  watched.rect = { left: 0, top: 56, width: 1280, height: 720, right: 1280, bottom: 776 };
  player.computed = undefined;
  player.rect = { left: 0, top: 0, width: 0, height: 0, right: 0, bottom: 0 };

  // A video the page hides is not on the page: a larger one that is hidden
  // does not make the player a preview beside it.
  const hidden = video(doc, player, { left: 0, top: 0, width: 2560, height: 1440 });
  hidden.computed = { visibility: 'hidden' };
  doc.dispatch('yt-navigate-finish');
  if (button()?.style.left !== '640px') fail(`with a larger hidden video on the page the button sits at ${button()?.style.left}, want on the player at 640px`);
  hidden.remove();
  doc.dispatch('yt-navigate-finish');

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
  const word = (k) => Object.values(ctx.V).map((m) => m[k]);
  if (ctx.V.de['video.converted'] !== 'umgewandelt' || ctx.V.de['video.autoCaptions'] !== 'automatisch' || word('video.converted').some((w) => !w)) fail('the menu does not say "umgewandelt" and "automatisch" in every language');
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
        ? { known: true, videoConversions: [], qualities: ['1080p', '720p'], audio: true, videoTracks: ['1080p webm vp9', '720p mp4 avc1', '720p webm vp9'], audioTracks: ['opus 160k', 'opus 120k', 'aac 128k'], audioLangs: [], subtitles: [], autoCaptions: [], subtitleFormats: [], thumbnail: null, description: true }
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
        known: true, videoConversions: [], qualities: ['720p'], audio: true, videoTracks: ['720p mp4 avc1'], audioTracks: [], audioLangs: [], subtitles: [], autoCaptions: [], subtitleFormats: [],
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
  const want = converts ? 'Thumbnail]|1280×720|Format|[JPEG]|converted|PNG|converted|WebP|Download' : 'Thumbnail]|1280×720 WebP|Download';
  if (!shown.includes(want)) fail(`${converts ? 'from an instance that converts' : 'from one that does not'} the thumbnail card shows ${shown}, want ${want}`);
  if (converts) {
    menu.querySelectorAll('button').find((b) => b.text === 'PNG')?.fire('click');
    menu.querySelectorAll('button').find((b) => b.text === 'Download').fire('click');
    await settle();
    if (sends.at(-1)?.thumbFormat !== 'png') fail(`the thumbnail picked as PNG is sent as ${sends.at(-1)?.thumbFormat}`);
  }
}

// An instance that converts video offers H.265 as a format of its own, MKV
// and converted under its name and an info bubble saying it takes a while.
// Picking it keeps the quality, a quality picked after it keeps it, and the
// download sends it; the total turns into a lower bound. An older instance
// offers no such format.
for (const converts of [true, false]) {
  const store = makeStore({ videoDefaults: { youtube: { parts: ['video', 'audio'], quality: '1080p' } } });
  const { chrome } = makeChrome(store);
  const sends = [];
  chrome.runtime.sendMessage = async (msg) => {
    if (msg.type === 'knightloader-video-labels') return { ...PAGE_LABELS, convertTip: 'Takes a while' };
    if (msg.type === 'knightloader-video-formats') {
      return {
        known: true, qualities: ['1080p', '720p'], audio: true, videoTracks: ['1080p mp4 avc1', '720p mp4 avc1'], audioTracks: ['opus 160k'], audioLangs: [],
        subtitles: [], autoCaptions: [], subtitleFormats: [], thumbnail: null, description: false,
        videoSizes: { '1080p': 9000000, '720p': 4000000 }, audioSizes: { best: 3000000 },
        videoConversions: converts ? ['mkv hevc'] : [],
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
  shadow.querySelectorAll('button').find((b) => b.attrs['aria-haspopup'] === 'dialog').fire('click');
  await settle();
  const menu = shadow.querySelectorAll('div').find((d) => d.attrs.role === 'dialog');
  const shown = () => menu.querySelectorAll('*').filter((n) => n.text).map((n) => (n.attrs['aria-checked'] === 'true' ? `[${n.text}]` : n.text)).join('|');
  const button = (text) => menu.querySelectorAll('button').find((b) => b.text === text);
  if (!converts) {
    if (button('H.265')) fail('an instance that converts no video offers H.265');
    continue;
  }
  if (!shown().includes('Format|[H.264]|MP4|H.265|MKV · converted|[Audio]')) fail(`the video formats read ${shown()}, want H.265 over MKV and converted`);
  if (button('H.265')?.attrs['aria-description'] !== 'Takes a while' || button('H.265')?.dataset.tip !== 'Takes a while') fail('H.265 carries no info bubble saying the conversion takes a while');
  button('H.265').fire('click');
  if (!shown().includes('[1080p]') || !shown().includes('[H.265]')) fail(`after picking H.265 the menu shows ${shown()}, want 1080p kept and H.265 picked`);
  if (!shown().includes('Download|· ≥ 3 MB')) fail(`with H.265 picked the download reads ${shown()}, want at least the audio's 3 MB`);
  button('720p').fire('click');
  if (!shown().includes('[720p]') || !shown().includes('[H.265]')) fail(`a quality picked after H.265 shows ${shown()}, want H.265 kept`);
  button('Download').fire('click');
  await settle();
  if (sends.at(-1)?.videoConvert !== 'mkv hevc' || sends.at(-1)?.quality !== '720p') fail(`the download sends ${JSON.stringify(sends.at(-1))}, want 720p converted to mkv hevc`);
}

// A quality whose track runs above 30 fps carries its rate as a Plakette
// beside the resolution, and a screen reader hears it with the size.
{
  const store = makeStore({ videoDefaults: { youtube: { parts: ['video'], quality: 'best' } } });
  const { chrome } = makeChrome(store);
  chrome.runtime.sendMessage = async (msg) => {
    if (msg.type === 'knightloader-video-labels') return PAGE_LABELS;
    if (msg.type === 'knightloader-video-formats') {
      return {
        known: true, videoConversions: [], qualities: ['1080p', '720p'], audio: false, videoTracks: ['1080p60hdr mp4 avc1', '1080p60 mp4 avc1', '1080p mp4 avc1', '720p50 mp4 avc1'], audioTracks: [], audioLangs: [],
        subtitles: [], autoCaptions: [], subtitleFormats: [], thumbnail: null, description: false, videoSizes: { '1080p60 mp4 avc1': 9000000 },
      };
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
  shadow.querySelectorAll('button').find((b) => b.attrs['aria-haspopup'] === 'dialog').fire('click');
  await settle();
  const menu = shadow.querySelectorAll('div').find((d) => d.attrs.role === 'dialog');
  const shown = menu.querySelectorAll('*').filter((n) => n.text).map((n) => n.text).join('|');
  if (!shown.includes('Quality|Best|1080p|60 fps|HDR|1080p|60 fps|9 MB|1080p|720p|50 fps|Format')) fail(`the qualities read ${shown}, want 60 fps, HDR and 50 fps beside their resolutions`);
  const hdrRow = menu.querySelectorAll('button').find((b) => b.dataset.key === 'video-quality:1080p60hdr');
  if (hdrRow?.attrs['aria-label'] !== '1080p, 60 fps, HDR') fail(`the 60 fps HDR quality is read as ${hdrRow?.attrs['aria-label']}`);
  const rated = menu.querySelectorAll('button').find((b) => b.dataset.key === 'video-quality:1080p60');
  if (rated?.attrs['aria-label'] !== '1080p, 60 fps, 9 MB') fail(`the 60 fps quality is read as ${rated?.attrs['aria-label']}`);
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

// On another site the button waits for a video somebody watches and for the
// instance to say yt-dlp can download the page. It asks once per page, says
// nothing to the worker before that, sends the defaults of all other sites,
// and its menu offers what the check found without asking again.
{
  const store = makeStore({ videoDefaults: { other: { parts: ['audio'], audio: 'mp3' }, youtube: { parts: ['video'] } } });
  const { chrome } = makeChrome(store);
  const asked = { labels: 0, checks: [], formats: 0 };
  let claims = true;
  const sends = [];
  const offer = {
    known: true, videoConversions: [], qualities: ['720p'], audio: true, videoTracks: ['720p mp4 avc1'], audioTracks: ['aac 128k'], audioLangs: [],
    subtitles: [], autoCaptions: [], subtitleFormats: [], thumbnail: null, description: false,
  };
  chrome.runtime.sendMessage = async (msg) => {
    if (msg.type === 'knightloader-video-labels') {
      asked.labels++;
      return PAGE_LABELS;
    }
    if (msg.type === 'knightloader-video-check') {
      asked.checks.push(msg.href);
      return claims === null ? undefined : claims ? { claims: true, offer } : { claims: false };
    }
    if (msg.type === 'knightloader-video-formats') {
      asked.formats++;
      return offer;
    }
    if (msg.type === 'knightloader-video-send') {
      sends.push(msg);
      return { result: 'sent' };
    }
    return undefined;
  };
  const { doc, location, run } = loadPage(chrome, 'https://clips.example.org/watch/7#t=3');
  const player = doc.createElement('div');
  doc.documentElement.append(player);
  run();
  await settle();
  const button = () => doc.documentElement.querySelectorAll('knightloader-video-button')[0];
  const tick = async () => {
    doc.dispatch('yt-navigate-finish');
    await settle();
  };
  if (asked.labels || asked.checks.length || button()) fail('a page of another site without a video asks the worker something or shows the button');

  const clip = (rect, how = {}) => Object.assign(video(doc, player, rect), { muted: false, controls: false, autoplay: false, loop: false, paused: false, currentTime: 2 }, how);
  for (const [why, rect, how] of [
    ['a small preview', { left: 0, top: 0, width: 160, height: 90 }, {}],
    ['a poster waiting for a click', { left: 0, top: 0, width: 1280, height: 720 }, { paused: true, currentTime: 0 }],
    ['a muted video that started itself without controls', { left: 0, top: 0, width: 1280, height: 720 }, { muted: true, autoplay: true }],
    ['a muted loop without controls', { left: 0, top: 0, width: 1280, height: 720 }, { muted: true, loop: true }],
    ['a video off screen', { left: 0, top: 2000, width: 1280, height: 720 }, {}],
    ['a video mostly below the screen', { left: 0, top: 512, width: 1280, height: 720 }, {}],
    ['a hidden video', { left: 0, top: 0, width: 1280, height: 720 }, { computed: { visibility: 'hidden' } }],
  ]) {
    const v = clip(rect, how);
    await tick();
    if (asked.checks.length || button()) fail(`with ${why} on another site the instance is asked or the button shows`);
    v.remove();
  }

  const main = clip({ left: 0, top: 56, width: 1280, height: 720 });
  await tick();
  if (asked.checks.join() !== 'https://clips.example.org/watch/7') fail(`a video playing on another site asks ${JSON.stringify(asked.checks)}, want its page without the part after # once`);
  if (!button()) fail('on a page of another site the instance can download the button does not show');
  else if (button().style.left !== '640px') fail(`on another site the button sits at ${button().style.left}, want the middle of the video's top edge`);
  if (asked.labels !== 1) fail(`the labels are asked for ${asked.labels} times, want once the button shows`);
  await tick();
  if (asked.checks.length !== 1) fail('a page of another site is asked about again on every look');

  const shadow = button()?.shadow;
  shadow?.querySelectorAll('button').find((b) => b.className === 'send').fire('click');
  await settle();
  if (JSON.stringify(sends.at(-1)?.pick) !== '{"parts":["audio"],"quality":"best","videoFormat":"best","audio":"mp3","audioRate":"best","subLangs":[],"subFormat":"srt"}') fail(`a click on another site sends ${JSON.stringify(sends.at(-1)?.pick)}, want the defaults of all other sites`);
  shadow?.querySelectorAll('button').find((b) => b.attrs['aria-haspopup'] === 'dialog').fire('click');
  await settle();
  const menu = shadow?.querySelectorAll('div').find((d) => d.attrs.role === 'dialog');
  const shown = menu?.querySelectorAll('*').filter((n) => n.text).map((n) => n.text).join('|') ?? '';
  if (asked.formats || /Checking/.test(shown) || !shown.includes('Audio|Quality|Best|128k')) fail(`the menu on another site asks ${asked.formats} times and shows ${shown}, want the check's offer at once`);
  shadow?.querySelectorAll('button').find((b) => b.attrs['aria-haspopup'] === 'dialog').fire('click');

  // Paused, or scrolled half away, the video keeps the button it has.
  Object.assign(main, { paused: true, currentTime: 0 });
  main.rect = { left: 0, top: 500, width: 1280, height: 720, right: 1280, bottom: 1220 };
  await tick();
  if (!button()) fail('the button leaves a video on another site that was paused or scrolled half away');
  Object.assign(main, { paused: false, currentTime: 9 });
  main.rect = { left: 0, top: 56, width: 1280, height: 720, right: 1280, bottom: 776 };

  // A page yt-dlp cannot download gets no button and is asked once, the same
  // path with another query not again; one no instance answered is not asked
  // again at once.
  claims = false;
  location.href = 'https://clips.example.org/watch/8';
  await tick();
  await tick();
  if (button() || asked.checks.length !== 2) fail(`on a page yt-dlp cannot download the button shows or the page is asked ${asked.checks.length - 1} times`);
  location.href = 'https://clips.example.org/watch/8?from=feed';
  await tick();
  if (asked.checks.length !== 2) fail('a page with another query is asked about again');
  claims = null;
  location.href = 'https://clips.example.org/watch/9';
  await tick();
  await tick();
  if (button() || asked.checks.length !== 3) fail(`with no instance answering the button shows or the page is asked ${asked.checks.length - 2} times at once`);

  // Hiding the button on all other sites takes it off a page it shows on.
  location.href = 'https://clips.example.org/watch/7';
  await tick();
  if (!button()) fail('back on a page the instance can download the button does not come back');
  // A site added on the options page sends its own defaults on its hosts.
  await store.local.set({ videoCustomSites: ['example.org'], videoDefaults: { ...store.data.videoDefaults, 'example.org': { parts: ['video'], quality: '720p' } } });
  await settle();
  button()?.shadow?.querySelectorAll('button').find((b) => b.className === 'send').fire('click');
  await settle();
  if (sends.at(-1)?.pick?.parts?.join() !== 'video' || sends.at(-1)?.pick?.quality !== '720p') fail(`a click on a site added on the options page sends ${JSON.stringify(sends.at(-1)?.pick)}, want its own defaults`);
  await store.local.set({ videoButtonHidden: ['other'] });
  if (button()) fail('hidden on all other sites, the button stays');
}

// A player that keeps its video in a shadow root of its own, as archive.org's
// JW Player does in <play-av>, which the page's hit test and fullscreen name
// only by that root's host.
{
  const store = makeStore({ videoButtonAutoHide: true });
  const { chrome } = makeChrome(store);
  const checks = [];
  chrome.runtime.sendMessage = async (msg) => {
    if (msg.type === 'knightloader-video-labels') return PAGE_LABELS;
    if (msg.type === 'knightloader-video-check') {
      checks.push(msg.href);
      return { claims: true, offer: { known: true, qualities: ['720p', '360p'], audio: false, videoTracks: ['720p avi', '360p mp4'], audioTracks: [], audioLangs: [], subtitles: [], autoCaptions: [], subtitleFormats: [], thumbnail: null, description: true, videoConversions: [] } };
    }
    return undefined;
  };
  const { doc, run } = loadPage(chrome, 'https://archive.org/details/BigBuckBunny_124');
  const playAv = doc.createElement('play-av');
  playAv.rect = { left: 0, top: 100, width: 1280, height: 540, right: 1280, bottom: 640 };
  doc.documentElement.append(playAv);
  const inside = playAv.attachShadow({ mode: 'open' });
  const jw = doc.createElement('div');
  jw.rect = { left: 160, top: 100, width: 960, height: 540, right: 1120, bottom: 640 };
  inside.append(jw);
  const main = Object.assign(video(doc, jw, { left: 160, top: 100, width: 960, height: 540 }), { muted: false, controls: false, autoplay: false, loop: false, paused: false, currentTime: 4 });
  doc.elementsFromPoint = () => [playAv, doc.documentElement];
  inside.elementsFromPoint = () => [main, jw, playAv, doc.documentElement];
  run();
  await settle();
  doc.dispatch('pointermove', { clientX: 640, clientY: 600 });
  await settle();
  const button = () => doc.documentElement.querySelectorAll('knightloader-video-button')[0];
  const frame = () => button()?.shadow.querySelectorAll('div').find((d) => d.className === 'kl');
  if (checks.join() !== 'https://archive.org/details/BigBuckBunny_124') fail(`a video playing in a shadow root asks ${JSON.stringify(checks)}, want its page once`);
  if (!frame()?.classList.contains('shown')) fail('with the pointer on a player in a shadow root the button does not show');

  const shownOn = button();
  inside.fullscreenElement = jw;
  doc.fullscreenElement = playAv;
  doc.dispatch('fullscreenchange');
  if (!shownOn || shownOn.parentNode !== jw) fail('in fullscreen the button is not inside the element the shadow root shows');
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
    // On another site the page's own button decides, since only it knows
    // whether the instance can download the page.
    await press({ id: 4, url: 'https://clips.example.org/watch/7' });
    if (asked.at(-1)?.tabId !== 4) fail('on another site the shortcut does not ask the page');
    const first = asked.length;
    await press(watch, 'something-else');
    await press({ id: 5, url: 'https://www.youtube.com/feed/subscriptions' });
    await press({ id: 6 });
    await store.local.set({ videoButtonHidden: ['youtube', 'other'] });
    await press(watch);
    await press({ id: 4, url: 'https://clips.example.org/watch/7' });
    await store.local.set({ videoButtonHidden: [], videoButton: false });
    await press(watch);
    if (asked.length !== first) fail(`another command, a page without a video or a site the button is off on still sent: ${JSON.stringify(asked.slice(first))}`);
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

/** The options page's video card, with every element options-video.js finds
 *  by id, hidden where options.html starts it hidden. */
function loadOptions(chrome) {
  const doc = makeDocument();
  const source = read('src', 'options-video.js');
  const html = read('src', 'options.html');
  doc.byId = Object.fromEntries([...source.matchAll(/getElementById\('(\w+)'\)/g)].map(([, id]) => {
    const tag = /Reset|Action|Enabled|AutoHide$|Remove$|Cancel$|Commit$|Close$/.test(id) ? 'button' : /^(videoShortcut|videoAddField)$/.test(id) ? 'input' : 'div';
    const n = new Node(tag, doc);
    n.attrs.id = id;
    n.hidden = new RegExp(`id="${id}"[^>]* hidden[ >]`).test(html);
    return [id, n];
  }));
  for (const id of ['videoAutoHide', 'videoSpotReset', 'videoShortcut', 'videoShortcutAction', 'videoSites']) doc.byId.videoMore.append(doc.byId[id]);
  for (const id of ['videoSiteHeading', 'videoSiteBody', 'videoSiteRemove', 'videoSiteCancel', 'videoSiteCommit', 'videoSiteClose']) doc.byId.videoSiteWindow.append(doc.byId[id]);
  for (const id of ['videoAddHead', 'videoAddField', 'videoAddPreview', 'videoAddError', 'videoAddCancel', 'videoAddCommit']) doc.byId.videoAddWindow.append(doc.byId[id]);
  doc.createTextNode = (text) => Object.assign(new Node('#text', doc), { text });
  const shaken = [];
  const ctx = vm.createContext({
    chrome, document: doc, URL, Intl, console, setTimeout, clearTimeout, navigator: { platform: 'Win32', userAgent: 'test' },
    t: (key) => key, loadLanguage: async () => {}, rowLabel() {}, glimSetInfo() {}, currentLanguage: () => 'en',
    glimGlyph: (name) => Object.assign(new Node('svg', doc), { glyph: name }), shake: (el) => shaken.push(el),
  });
  vm.runInContext(read('src', 'video.js'), ctx, { filename: 'video.js' });
  vm.runInContext(source, ctx, { filename: 'options-video.js' });
  return { doc, ctx, shaken, html };
}

/** The words on a tile: its name, and what a click downloads there. */
const tileWords = (tile) => tile.children[0]?.children[1]?.children.map((n) => n.text);
const tileButton = (doc, key) => doc.byId.videoSites.querySelectorAll('button').find((b) => b.dataset.videoKey === key);
const press = (doc, key, e = {}) => doc.dispatch('keydown', { key, shiftKey: false, preventDefault() {}, ...e });

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
  const { doc, ctx, shaken, html } = loadOptions(chrome);
  await settle();
  // The site's defaults name formats and codecs as they are written, and
  // offer subtitles as SRT, WebVTT or TTML and a thumbnail in a format the
  // site has, neither with a Best.
  tileButton(doc, 'youtube-open')?.dispatch('click');
  const options = doc.byId.videoSiteBody.querySelectorAll('option');
  const names = options.map((o) => o.text);
  for (const name of ['MP4 · H.264', 'WebM · VP9', 'H.265 (MKV, video.converted)', 'AAC', 'Opus', 'MP3 (video.converted)', 'FLAC (video.converted)', 'SRT', 'WebVTT', 'TTML', 'WebP', 'JPEG']) if (!names.includes(name)) fail(`${browser}: the site's defaults offer ${names.join(', ')}, want ${name} among them`);
  if (names.some((n) => /srv/i.test(n))) fail(`${browser}: the site's defaults offer YouTube's own subtitle format`);
  const select = (key) => doc.byId.videoSiteBody.querySelectorAll('select').find((s) => s.dataset.videoKey === `youtube-${key}`);
  const choices = (key) => select(key)?.children.map((o) => (o.selected ? `[${o.value}]` : o.value)).join(' ');
  if (choices('subformat') !== '[srt] vtt ttml') fail(`${browser}: with srv3 stored the subtitle formats read ${choices('subformat')}, want SRT picked and no Best`);
  if (choices('thumbformat') !== 'webp [jpg]') fail(`${browser}: with Best stored the thumbnail formats read ${choices('thumbformat')}, want JPEG picked and no Best`);
  doc.byId.videoSiteClose.dispatch('click');
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

// The well for all other sites is built as a site's is: its switch, on by
// default wherever the access to all websites is there, and the defaults a
// click there downloads, from what other sites have offered. Without that
// access its switch is off, and turning it on asks the browser for it.
for (const all of [true, false]) {
  const sites = ['https://www.youtube.com/*', 'https://m.youtube.com/*', 'https://www.youtube-nocookie.com/*', 'https://vimeo.com/*', 'https://www.dailymotion.com/*', 'https://www.twitch.tv/*', 'https://clips.twitch.tv/*', 'https://www.bilibili.com/*'];
  const store = makeStore({ videoSeen: { other: { qualities: ['720p', '480p'] } } });
  const { chrome } = makeChrome(store, { granted: all ? ['<all_urls>'] : sites });
  const requests = [];
  chrome.permissions.request = async (p) => {
    requests.push(p);
    return true;
  };
  chrome.commands = { getAll: async () => [] };
  const { doc } = loadOptions(chrome);
  await settle();
  const tiles = () => doc.byId.videoSites.children;
  const other = () => tiles().at(-1);
  const toggle = () => other()?.querySelectorAll('button').find((b) => b.dataset.videoKey === 'other-shown');
  const label = all ? 'with the access to all websites' : 'without the access to all websites';
  if (tiles().length !== 7 || tileWords(other())?.[0] !== 'options.videoOtherSites') fail(`${label} the tiles are ${tiles().map((w) => tileWords(w)?.[0]).join(', ')}, want the five, Add site and all other sites last`);
  if (other()?.className !== tiles()[0]?.className || other()?.children[0]?.className !== tiles()[0]?.children[0]?.className || toggle()?.className !== 'glim-switch') fail(`${label} the tile for all other sites is not built like a site's`);
  if (toggle()?.attrs['aria-checked'] !== String(all)) fail(`${label} the switch for all other sites is ${toggle()?.attrs['aria-checked']}, want ${all}`);
  tileButton(doc, 'other-open')?.dispatch('click');
  const quality = () => doc.byId.videoSiteBody.querySelectorAll('select').find((x) => x.dataset.videoKey === 'other-quality');
  if (quality()?.children.map((o) => o.value).join() !== 'best,720p,480p') fail(`${label} all other sites offer the qualities ${quality()?.children.map((o) => o.value)}, want what they have offered`);
  quality().value = '720p';
  quality().dispatch('change');
  await settle();
  if (store.data.videoDefaults?.other?.quality !== '720p' || store.data.videoDefaults?.youtube) fail(`${label} a quality picked for all other sites is stored as ${JSON.stringify(store.data.videoDefaults)}`);
  if (tileWords(other())?.[1] !== 'video.partVideo · 720p') fail(`${label} the tile for all other sites says ${tileWords(other())?.[1]}, want the video at 720p`);
  doc.byId.videoSiteClose.dispatch('click');
  toggle().dispatch('click');
  await settle();
  if (all) {
    if (requests.length || JSON.stringify(store.data.videoButtonHidden) !== '["other"]') fail(`switched off, all other sites ask ${JSON.stringify(requests)} and hide ${JSON.stringify(store.data.videoButtonHidden)}, want nothing asked and the button hidden there`);
  } else if (JSON.stringify(requests) !== '[{"origins":["<all_urls>"]}]' || store.data.videoButtonHidden?.includes('other')) {
    fail(`switched on, all other sites ask ${JSON.stringify(requests)}, want the access to all websites`);
  }
  // The card's switch asks for the five sites, so a "no" to all websites
  // never keeps the button off them.
  if (!all) {
    requests.length = 0;
    await store.local.set({ videoButton: false });
    await settle();
    doc.byId.videoEnabled.dispatch('click');
    await settle();
    if (requests.some((p) => p.origins.includes('<all_urls>'))) fail(`the card's switch asks for ${JSON.stringify(requests)}, want the five sites alone`);
  }
}

// 8. The sites on the options card are tiles that keep the card short: the
// named sites, those added there, Add site and all other sites last, each
// saying what a click downloads. A tile opens a window with the site's
// controls, which save as they change, keep the keyboard inside and give it
// back to the tile. A site is added by its address and removed from its
// window after a question.
{
  const store = makeStore({
    videoDefaults: { youtube: { parts: ['video'], quality: '1080p', videoFormat: 'mp4 avc1' }, vimeo: { parts: ['audio'], audio: 'mp3' }, 'clips.example.org': { parts: ['video', 'subtitle'] } },
    videoSeen: { 'clips.example.org': { qualities: ['720p', '480p'] } },
    videoCustomSites: ['clips.example.org'],
  });
  const { chrome } = makeChrome(store);
  chrome.commands = { getAll: async () => [] };
  const { doc, shaken, html } = loadOptions(chrome);
  await settle();
  const tiles = () => doc.byId.videoSites.children;
  const names = () => tiles().map((tile) => tileWords(tile)?.[0]).join(', ');
  if (names() !== 'YouTube, Vimeo, Dailymotion, Twitch, Bilibili, clips.example.org, options.videoAddSite, options.videoOtherSites') fail(`the tiles are ${names()}, want the five, the added site, Add site and all other sites last`);
  const words = (key) => tileWords(tileButton(doc, key)?.parentNode)?.[1];
  for (const [key, want] of [['youtube-open', 'video.partVideo · 1080p · MP4'], ['vimeo-open', 'video.partAudio · MP3'], ['clips.example.org-open', 'video.partVideo, video.partSubtitle · video.qualityBest']]) {
    if (words(key) !== want) fail(`the tile ${key} says ${words(key)}, want ${want}`);
  }
  if (!tileButton(doc, 'youtube-shown') || tileButton(doc, 'clips.example.org-shown')) fail('a named site has no switch on its tile, or a site added here has one of its own');

  // The mark is the site's favicon, its first letter where none loads.
  const mark = (key) => tileButton(doc, key)?.children[0];
  if (mark('youtube-open')?.children[0]?.src !== 'https://www.youtube.com/favicon.ico') fail(`YouTube's tile shows ${mark('youtube-open')?.children[0]?.src}, want its favicon`);
  const img = mark('clips.example.org-open')?.children[0];
  img?.dispatch('error');
  if (img?.src !== 'https://www.clips.example.org/favicon.ico') fail(`without a favicon at its root the added site tries ${img?.src}, want the one under www`);
  img?.dispatch('error');
  if (mark('clips.example.org-open')?.children[0]?.text !== 'c') fail('a site without a favicon shows no letter');

  // The window: the site's controls, the keyboard kept inside, Escape back
  // to the tile, and no way out but the bottom row.
  const win = doc.byId.videoSiteWindow;
  tileButton(doc, 'youtube-open').dispatch('click');
  if (win.hidden || doc.byId.videoSiteTitle.text !== 'YouTube') fail(`a click on YouTube's tile shows ${win.hidden ? 'no window' : doc.byId.videoSiteTitle.text}`);
  if (!win.contains(doc.activeElement) || doc.activeElement === doc.byId.videoSiteClose) fail('the window opens without the keyboard on its first control');
  if (!doc.byId.videoSiteRemove.hidden) fail('a named site can be removed');
  const markup = html.slice(html.indexOf('id="videoSiteWindow"'), html.indexOf('id="videoAddWindow"'));
  if ((markup.slice(0, markup.indexOf('glim-modal-actions')).match(/<button/g) ?? []).length) fail('the site window has a button outside its bottom row');
  doc.byId.videoSiteClose.focus();
  press(doc, 'Tab');
  if (doc.activeElement?.dataset.videoKey !== 'youtube-video') fail(`Tab from the window's last button goes to ${doc.activeElement?.dataset.videoKey ?? doc.activeElement?.attrs.id}, want its first control`);
  press(doc, 'Tab', { shiftKey: true });
  if (doc.activeElement !== doc.byId.videoSiteClose) fail('Shift+Tab from the first control leaves the window');
  press(doc, 'Escape');
  if (!win.hidden || doc.activeElement?.dataset.videoKey !== 'youtube-open') fail('Escape leaves the window open or the keyboard elsewhere than on the tile');

  // An added site saves under its host, live.
  tileButton(doc, 'clips.example.org-open').dispatch('click');
  if (doc.byId.videoSiteRemove.hidden) fail('a site added here cannot be removed');
  const quality = doc.byId.videoSiteBody.querySelectorAll('select').find((x) => x.dataset.videoKey === 'clips.example.org-quality');
  if (quality?.children.map((o) => o.value).join() !== 'best,720p,480p') fail(`the added site offers ${quality?.children.map((o) => o.value)}, want what it offered`);
  quality.value = '720p';
  quality.dispatch('change');
  await settle();
  if (store.data.videoDefaults['clips.example.org']?.quality !== '720p' || store.data.videoDefaults.other) fail(`a quality picked for the added site is stored as ${JSON.stringify(store.data.videoDefaults)}`);
  if (win.hidden) fail('a change closes the window');
  doc.byId.videoSiteClose.dispatch('click');

  // Add site takes an address, refuses one it cannot use, and opens the new
  // site's window.
  const field = doc.byId.videoAddField;
  const add = doc.byId.videoAddWindow;
  tileButton(doc, 'add-open').dispatch('click');
  if (add.hidden || doc.activeElement !== field) fail('Add site opens no window with the keyboard on its field');
  for (const [typed, why] of [['not an address', 'options.videoAddInvalid'], ['localhost', 'options.videoAddInvalid'], ['m.youtube.com', 'options.videoAddKnown'], ['clips.example.org', 'options.videoAddKnown']]) {
    field.value = typed;
    field.dispatch('input');
    field.dispatch('keydown', { key: 'Enter', preventDefault() {} });
    await settle();
    if (doc.byId.videoAddError.hidden || doc.byId.videoAddError.text !== why || !shaken.includes(field)) fail(`${typed} is not refused with ${why}`);
  }
  if (store.data.videoCustomSites.length !== 1) fail(`refused addresses are added: ${store.data.videoCustomSites}`);
  field.value = 'https://WWW.Example.net/watch?v=1';
  field.dispatch('input');
  if (doc.byId.videoAddPreview.hidden || doc.byId.videoAddPreview.children[1]?.text !== 'example.net') fail('the field shows no preview of the site it will add');
  doc.byId.videoAddCommit.dispatch('click');
  await settle();
  if (JSON.stringify(store.data.videoCustomSites) !== '["clips.example.org","example.net"]') fail(`adding stores ${JSON.stringify(store.data.videoCustomSites)}`);
  if (!add.hidden || win.hidden || doc.byId.videoSiteTitle.text !== 'example.net') fail('an added site does not open its own window');
  if (names().split(', ').indexOf('example.net') !== 6) fail(`the added site's tile is not the last before Add site: ${names()}`);

  // Removing asks first in the window, Escape goes back to the site, and
  // the site goes with its settings.
  await store.local.set({ videoDefaults: { ...store.data.videoDefaults, 'example.net': { parts: ['audio'] } } });
  await settle();
  doc.byId.videoSiteRemove.dispatch('click');
  if (doc.byId.videoSiteTitle.text !== 'options.videoRemoveSite' || doc.byId.videoSiteCommit.hidden || !doc.byId.videoSiteClose.hidden || doc.activeElement !== doc.byId.videoSiteCancel) fail('Remove site does not ask first, with the keyboard on Cancel');
  press(doc, 'Escape');
  if (win.hidden || doc.byId.videoSiteTitle.text !== 'example.net') fail('Escape on the question does not go back to the site');
  doc.byId.videoSiteRemove.dispatch('click');
  doc.byId.videoSiteCommit.dispatch('click');
  await settle();
  if (JSON.stringify(store.data.videoCustomSites) !== '["clips.example.org"]' || store.data.videoDefaults['example.net']) fail(`removing leaves ${JSON.stringify(store.data.videoCustomSites)} and ${JSON.stringify(store.data.videoDefaults)}`);
  if (!win.hidden || doc.activeElement?.dataset.videoKey !== 'add-open') fail('after removing, the window stays or the keyboard is not on Add site');

  // Off, every tile still opens to show its settings, which refuse.
  await store.local.set({ videoButton: false });
  await settle();
  if (tileButton(doc, 'youtube-open').disabled || !tileButton(doc, 'youtube-shown').disabled || !tileButton(doc, 'add-open').disabled) fail('with the button off a tile does not open, or its switch or Add site still works');
  tileButton(doc, 'youtube-open').dispatch('click');
  if (doc.byId.videoSiteBody.querySelectorAll('button, select').some((el) => !el.disabled) || doc.byId.videoSiteClose.disabled) fail('with the button off the window takes changes, or cannot be closed');
}

if (problems.length) {
  console.error(`${problems.length} problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log("ok: every form of a video page sends its one address, the sites are a short list yt-dlp knows inside the optional access, a click sends the site's defaults to start at once and says when the video was there already, the menu offers what the video has, a drag keeps its place as shares of the player and sends nothing, auto-hide shows the button only over the video, the switches take it off an open page at once and back, and on all other sites it shows on a video somebody watches once the instance says yt-dlp can download the page");
