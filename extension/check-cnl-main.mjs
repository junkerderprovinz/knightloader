// Runs cnl-main.js against a stand-in page that behaves like filecrypt's
// Click'n'Load helper: it sets `jdownloader` to false, waits for jdcheck.js
// (which its Content-Security-Policy blocks) and submits its form only once the
// flag reads true. The flag must survive the reset and the form must be handed
// over instead of leaving for port 9666.
//
// Run by CI and by hand: `node extension/check-cnl-main.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const problems = [];

class FakeForm {
  constructor(action, fields) {
    this.action = action;
    this.fields = fields;
    this.sentToNetwork = false;
  }
}
FakeForm.prototype.submit = function () {
  this.sentToNetwork = true;
};

const posted = [];
const ctx = vm.createContext({
  location: { href: 'https://filecrypt.cc/helper.html?1' },
  URL,
  URLSearchParams,
  Response: class {},
  Event: class {},
  FormData: class {
    constructor(form) {
      this.entries = Object.entries(form.fields);
    }
    [Symbol.iterator]() {
      return this.entries[Symbol.iterator]();
    }
  },
  XMLHttpRequest: class {},
  HTMLFormElement: FakeForm,
  navigator: {},
  document: { addEventListener() {} },
  postMessage: (msg) => posted.push(msg),
  setTimeout,
});
ctx.window = ctx;

vm.runInContext(readFileSync(join(here, 'src', 'cnl-main.js'), 'utf8'), ctx, { filename: 'cnl-main.js' });

// The helper's own inline script, then its check once jdcheck.js has failed.
vm.runInContext('jdownloader = false;', ctx);
if (vm.runInContext('jdownloader', ctx) !== true) problems.push('the page set jdownloader to false and it stayed false, so a helper that waits for jdcheck.js never submits');
vm.runInContext('var jdownloader = false;', ctx);
if (vm.runInContext('jdownloader', ctx) !== true) problems.push('a page that declares var jdownloader = false clears the flag');

const form = new FakeForm('http://127.0.0.1:9666/flash/addcrypted2', { jk: 'function f(){ return \'00\';}', crypted: 'AAAA', source: 'filecrypt.cc' });
ctx.form = form;
vm.runInContext('if (jdownloader) form.submit();', ctx);
if (form.sentToNetwork) problems.push('the helper\'s form.submit() went out to port 9666 instead of being caught');
const caught = posted.find((m) => m?.['knightloader-cnl']);
if (!caught) problems.push('the helper\'s form.submit() was not handed to the relay script');
else if (caught.path !== '/flash/addcrypted2' || caught.fields.crypted !== 'AAAA') problems.push(`the handed-over submission reads ${JSON.stringify(caught)}`);

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log('ok: a page cannot clear the jdownloader flag, and a helper that submits once it reads true is caught');
