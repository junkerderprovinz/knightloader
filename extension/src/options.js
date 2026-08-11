const input = document.getElementById('url');
const status = document.getElementById('status');

readInstanceUrl().then((url) => {
  input.value = url;
});

document.getElementById('save').addEventListener('click', async () => {
  const value = input.value.trim();
  // Validated with the same URL constructor quickAddUrl builds against, so a
  // typo is caught here rather than at the moment a context-menu click tries
  // to open an unusable address.
  try {
    if (value) new URL(value);
  } catch {
    status.textContent = 'That does not look like a full address (include http:// or https://).';
    status.className = '';
    return;
  }
  await writeInstanceUrl(value);
  status.textContent = 'Saved.';
  status.className = 'ok';
});

input.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') document.getElementById('save').click();
});
