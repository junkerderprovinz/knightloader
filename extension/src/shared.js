// Shared by background.js (importScripts), popup.js, picker.js and options.js
// (<script> tag) — one copy of anything more than one of them needs.
//
// It used to hold the whole instance model: a stored {name, url} registry, the
// origin baked into config.default.json at download time, a quickadd URL
// builder, and the entryTarget/entryLabel pair that decided which of a peer's
// two possible addresses to open. All of it went with the phrase rework — see
// group.js, which stores one phrase and asks the relay who is in the group.
//
// What is left is the one label helper the picker and the options page both
// draw, kept here rather than duplicated in each.

/**
 * deploymentLabel names what an instance IS, since in the phrase model there is
 * no address to show instead.
 *
 * Written out rather than built by interpolating the value into the key, which
 * is what it was first: a key assembled at runtime is invisible to
 * check-locales.mjs, which then reported both translations as dead and would
 * have had them deleted by the next person tidying up.
 */
function deploymentLabel(dep) {
  if (dep === 'desktop') return t('picker.deployment.desktop');
  if (dep === 'container') return t('picker.deployment.container');
  return t('picker.viaRelay');
}
