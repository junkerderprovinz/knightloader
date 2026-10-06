// Writes the AMO reviewer notes for this version to the web-ext metadata file
// named on the command line. The notes are the AMO block of
// extension/store/SUBMISSION.md with the review instance's phrase and web UI
// password filled in. AMO keeps reviewer notes per version, and a reviewer
// without them has nothing to test the extension against.
//
// Needs REVIEW_PHRASE and REVIEW_PASSWORD.
import { readFileSync, writeFileSync } from "node:fs";

const { REVIEW_PHRASE: phrase, REVIEW_PASSWORD: password } = process.env;
if (!phrase || !password) {
  console.error("::error::REVIEW_PHRASE and REVIEW_PASSWORD are not set, so AMO would get a version its reviewers cannot test.");
  process.exit(1);
}

const lines = readFileSync("extension/store/SUBMISSION.md", "utf8").split("\n");
const start = lines.indexOf("**AMO** (Notes for reviewers)");
if (start < 0) throw new Error("SUBMISSION.md has no AMO reviewer notes");
const notes = [];
for (const line of lines.slice(start + 2)) {
  if (line !== ">" && !line.startsWith("> ")) break;
  notes.push(line.slice(2));
}

const text = notes.join("\n").replaceAll("<PHRASE>", phrase).replaceAll("<WEBUI_PASSWORD>", password);
if (/<[A-Z_]+>/.test(text)) throw new Error("the AMO reviewer notes have a placeholder this script does not fill");
writeFileSync(process.argv[2], JSON.stringify({ version: { approval_notes: text } }));
console.log(`Reviewer notes: ${text.length} characters.`);
