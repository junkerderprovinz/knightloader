// The popup's list of the video and audio the current tab played, as media.js
// in the service worker found them. Each stream goes to the chosen instance
// with the page it played on, through the same hand-over as every other send
// (handOver in popup.js).

const mediaEl = document.getElementById('media');
const mediaListEl = document.getElementById('mediaList');

const MEDIA_KIND_LABEL = {
  hls: () => 'HLS',
  dash: () => 'DASH',
  video: () => t('popup.mediaVideo'),
  audio: () => t('popup.mediaAudio'),
};

/** The last part of a stream's path, which is the closest it has to a name. */
function mediaName(url) {
  try {
    const u = new URL(url);
    return decodeURIComponent(u.pathname.split('/').filter(Boolean).pop() || u.hostname);
  } catch {
    return url;
  }
}

async function renderMedia() {
  if (!(await mediaState()).on) return;
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab?.id) return;
  const key = mediaKey(tab.id);
  const list = (await chrome.storage.session.get(key))[key];
  const items = Array.isArray(list?.items) ? list.items : [];

  document.getElementById('mediaLabel').textContent = t('popup.mediaLabel');
  mediaEl.hidden = false;
  if (items.length === 0) {
    const none = document.createElement('p');
    none.className = 'mediaNone';
    none.textContent = t('popup.mediaNone');
    mediaListEl.replaceChildren(none);
    return;
  }
  mediaListEl.replaceChildren(...items.map((item) => mediaRow(item, tab, list.page)));
}

function mediaRow(item, tab, page) {
  const row = document.createElement('div');
  row.className = 'mediaRow';

  const kind = document.createElement('span');
  kind.className = 'mediaKind';
  kind.textContent = (MEDIA_KIND_LABEL[item.kind] ?? MEDIA_KIND_LABEL.video)();

  const name = document.createElement('span');
  name.className = 'mediaName';
  const title = document.createElement('span');
  title.textContent = mediaName(item.url);
  title.title = item.url;
  const host = document.createElement('span');
  host.className = 'mediaHost';
  host.textContent = [hostOf(item.url), item.size > 0 ? fmtBytes(item.size) : ''].filter(Boolean).join(' · ');
  name.append(title, host);

  const send = document.createElement('button');
  send.type = 'button';
  send.className = 'iconBadge';
  send.setAttribute('aria-label', t('popup.mediaSend'));
  send.setAttribute('data-tip', t('popup.mediaSend'));
  send.replaceChildren(glyph(G_SEND));
  send.addEventListener('click', async () => {
    cancelCountdown();
    send.disabled = true;
    await handOver({
      type: 'knightloader-send-media',
      target: chosen,
      url: item.url,
      // The page the list was started for, else the tab's own address.
      page: page || tab.url || '',
      title: tab.title || '',
      incognito: !!tab.incognito,
      cookieStoreId: tab.cookieStoreId,
    });
  });

  row.append(kind, name, send);
  return row;
}

(async () => {
  await loadLanguage();
  await renderMedia();
})();
