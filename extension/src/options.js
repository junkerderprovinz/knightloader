const list = document.getElementById('list');
const status = document.getElementById('status');
const addForm = document.getElementById('addForm');
const addName = document.getElementById('addName');
const addUrl = document.getElementById('addUrl');

function say(text, ok) {
  status.textContent = text;
  status.className = ok ? 'ok' : '';
}

async function render() {
  const { instances, defaultName } = await readInstances();
  list.innerHTML = '';
  if (instances.length === 0) {
    const p = document.createElement('p');
    p.className = 'empty';
    p.textContent = 'No instances yet — add one below.';
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
      badge.textContent = 'Default';
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
      makeDefault.textContent = 'Make default';
      makeDefault.addEventListener('click', async () => {
        await writeInstances(instances, inst.name);
        say(`"${inst.name}" is now the default.`, true);
        render();
      });
      row.appendChild(makeDefault);
    }

    const remove = document.createElement('button');
    remove.className = 'danger';
    remove.type = 'button';
    remove.textContent = 'Remove';
    remove.addEventListener('click', async () => {
      const next = instances.filter((i) => i.name !== inst.name);
      await writeInstances(next, defaultName);
      say(`Removed "${inst.name}".`, true);
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
    say('Give this instance a name.', false);
    return;
  }
  try {
    new URL(urlValue);
  } catch {
    say('That does not look like a full address (include http:// or https://).', false);
    return;
  }
  const { instances, defaultName } = await readInstances();
  if (instances.some((i) => i.name === name)) {
    say(`"${name}" is already in the list — remove it first to replace it.`, false);
    return;
  }
  const next = [...instances, { name, url: urlValue }];
  // The first instance ever added becomes the default automatically; later
  // ones keep whichever default was already set.
  await writeInstances(next, defaultName ?? name);
  addName.value = '';
  addUrl.value = '';
  say(`Added "${name}".`, true);
  render();
});

render();
