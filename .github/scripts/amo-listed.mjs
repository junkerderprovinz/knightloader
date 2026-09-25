// Decides whether this extension version goes to Firefox Add-ons, and writes
// submit=true or submit=false to $GITHUB_OUTPUT.
//
// A version is submitted only once the add-on has a listed version, because
// the first listing, with its description, screenshots and reviewer notes, is
// filled in by hand. A version AMO already has is skipped: AMO takes each
// number once, across the listed and unlisted channels.
//
// Needs AMO_JWT_ISSUER and AMO_JWT_SECRET (API credentials from
// https://addons.mozilla.org/developers/addon/api/key/).
import { createHmac, randomUUID } from "node:crypto";
import { appendFileSync, readFileSync } from "node:fs";

const { AMO_JWT_ISSUER: issuer, AMO_JWT_SECRET: secret, GITHUB_OUTPUT: output } = process.env;
if (!issuer || !secret) {
  console.error("::error::AMO_JWT_ISSUER and AMO_JWT_SECRET are not set, so the version cannot go to Firefox Add-ons.");
  process.exit(1);
}

const manifest = JSON.parse(readFileSync("extension/src/manifest.json", "utf8"));
const guid = manifest.browser_specific_settings.gecko.id;
const version = manifest.version;

const b64 = (value) => Buffer.from(JSON.stringify(value)).toString("base64url");

// AMO refuses a token that lives longer than five minutes, and each request
// needs a fresh jti.
function token() {
  const now = Math.floor(Date.now() / 1000);
  const unsigned = `${b64({ alg: "HS256", typ: "JWT" })}.${b64({ iss: issuer, jti: randomUUID(), iat: now, exp: now + 60 })}`;
  return `${unsigned}.${createHmac("sha256", secret).update(unsigned).digest("base64url")}`;
}

async function versions() {
  const all = [];
  let url = `https://addons.mozilla.org/api/v5/addons/addon/${encodeURIComponent(guid)}/versions/?filter=all_with_unlisted&page_size=50`;
  while (url) {
    const res = await fetch(url, { headers: { Authorization: `JWT ${token()}` } });
    if (!res.ok) throw new Error(`AMO answered ${res.status} for ${url}: ${await res.text()}`);
    const page = await res.json();
    all.push(...page.results);
    url = page.next;
  }
  return all;
}

const known = await versions();
const same = known.find((v) => v.version === version);
let submit = true;
if (same) {
  console.log(`AMO already has ${version} (${same.channel}); nothing to submit.`);
  submit = false;
} else if (!known.some((v) => v.channel === "listed")) {
  console.log(`::notice::${guid} has no listed version yet. Upload ${version} by hand as the first one (extension/store/SUBMISSION.md); later tags go to AMO from here.`);
  submit = false;
} else {
  console.log(`${version} goes to Firefox Add-ons for review.`);
}
appendFileSync(output, `submit=${submit}\n`);
