# Release notes

One file per tag, named after it: `v1.2.3.md`.

`release.yml` publishes the matching file as the release body when a `v*.*.*`
tag is pushed, and falls back to generated notes only when the file is missing.

Two rules, both because the GitHub release title already carries the name and
version:

- no heading that repeats the repository or the version
- the body starts with a one-line summary, then `## Added` / `## Changed` /
  `## Fixed`

Write them by hand. A generated list of commit subjects tells a reader what was
touched, not what changed for them.
