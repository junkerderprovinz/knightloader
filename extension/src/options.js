const list = document.getElementById('list');
const status = document.getElementById('status');
const addForm = document.getElementById('addForm');
const addName = document.getElementById('addName');
const addUrl = document.getElementById('addUrl');
const subtitleEl = document.getElementById('subtitle');
const languageHeadingEl = document.getElementById('languageHeading');
const languageSubEl = document.getElementById('languageSub');
const languageSelect = document.getElementById('languageSelect');
const instancesHeadingEl = document.getElementById('instancesHeading');
const addHeadingEl = document.getElementById('addHeading');
const addButton = document.getElementById('add');
const noteEl = document.getElementById('note');

function say(text, ok) {
  status.textContent = text;
  status.className = ok ? 'ok' : '';
}

/**
 * applyStaticText fills in every fixed label on the page from the current
 * language — called once on load and again whenever the language picker
 * below changes it, so nothing needs a full page reload to update (jdp:
 * "Die sprache soll eingestellt werden können und soll die sprache die im
 * Bowser eingestellt ist standardmäßig übernehmen"). "KnightLoader" itself
 * (the <h1>) is left alone — a product name, not translated.
 */
function applyStaticText() {
  subtitleEl.textContent = t('options.subtitle');
  languageHeadingEl.textContent = t('options.languageHeading');
  languageSubEl.textContent = t('options.languageSub');
  instancesHeadingEl.textContent = t('options.instancesHeading');
  addHeadingEl.textContent = t('options.addHeading');
  addName.placeholder = t('options.addNamePlaceholder');
  addUrl.placeholder = t('options.addUrlPlaceholder');
  addButton.textContent = t('options.addButton');
  noteEl.textContent = t('options.note');
}

/** buildLanguageSelect fills the dropdown once; renderLanguageSelect (below) only updates which option is selected. */
function buildLanguageSelect() {
  languageSelect.innerHTML = '';
  const auto = document.createElement('option');
  auto.value = '';
  languageSelect.appendChild(auto);
  for (const lang of LANGUAGES) {
    const opt = document.createElement('option');
    opt.value = lang.code;
    opt.textContent = lang.label;
    languageSelect.appendChild(opt);
  }
}

async function renderLanguageSelect() {
  languageSelect.firstElementChild.textContent = t('options.languageAuto');
  const stored = await chrome.storage.local.get('language');
  languageSelect.value = typeof stored.language === 'string' ? stored.language : '';
}

languageSelect.addEventListener('change', async () => {
  await setLanguage(languageSelect.value || null);
  await loadLanguage();
  applyStaticText();
  await renderLanguageSelect();
  render();
});

async function render() {
  const { instances, defaultName } = await readInstances();
  list.innerHTML = '';
  if (instances.length === 0) {
    const p = document.createElement('p');
    p.className = 'empty';
    p.textContent = t('options.empty');
    list.appendChild(p);
    return;
  }
  for (const inst of instances) {
    const row = document.createElement('div');
    row.className = 'row' + (inst.name === defaultName ? ' isDefault' : '');

    const info = document.createElement('div');
    info.className = 'info';
    const name = document.createElement('div');
    name.className = 'name';
    name.textContent = inst.name;
    if (inst.name === defaultName) {
      const badge = document.createElement('span');
      badge.className = 'badge';
      badge.textContent = t('options.defaultBadge');
      name.appendChild(badge);
    }
    const url = document.createElement('div');
    url.className = 'url';
    url.textContent = inst.url;
    info.append(name, url);
    row.appendChild(info);

    if (inst.name !== defaultName) {
      const makeDefault = document.createElement('button');
      makeDefault.className = 'secondary';
      makeDefault.type = 'button';
      makeDefault.textContent = t('options.makeDefault');
      makeDefault.addEventListener('click', async () => {
        await writeInstances(instances, inst.name);
        say(t('options.setDefault', { name: inst.name }), true);
        render();
      });
      row.appendChild(makeDefault);
    }

    const remove = document.createElement('button');
    remove.className = 'danger';
    remove.type = 'button';
    remove.textContent = t('options.remove');
    remove.addEventListener('click', async () => {
      const next = instances.filter((i) => i.name !== inst.name);
      await writeInstances(next, defaultName);
      say(t('options.removed', { name: inst.name }), true);
      render();
    });
    row.appendChild(remove);

    list.appendChild(row);
  }
}

addForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  const name = addName.value.trim();
  const urlValue = addUrl.value.trim();
  if (!name) {
    say(t('options.needName'), false);
    return;
  }
  try {
    new URL(urlValue);
  } catch {
    say(t('options.badUrl'), false);
    return;
  }
  const { instances, defaultName } = await readInstances();
  if (instances.some((i) => i.name === name)) {
    say(t('options.duplicate', { name }), false);
    return;
  }
  const next = [...instances, { name, url: urlValue }];
  // The first instance ever added becomes the default automatically; later
  // ones keep whichever default was already set.
  await writeInstances(next, defaultName ?? name);
  addName.value = '';
  addUrl.value = '';
  say(t('options.added', { name }), true);
  render();
});

(async () => {
  await loadLanguage();
  buildLanguageSelect();
  applyStaticText();
  await renderLanguageSelect();
  render();
})();
