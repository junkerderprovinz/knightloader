# Release notes

One file per tag, named after it: `v1.2.3` has `v1.2.3.md`. One tag releases
the server, the desktop apps, the Android app and the browser extension, so one
file describes all of them.

For a `v*.*.*` tag, `release.yml` checks for the file first and stops when it is
missing, before the long builds. It then builds everything and only then
creates the release, with this file as its body and every download attached in
the same step. A release is never public without its downloads.

Two rules, both because the GitHub release title already carries the name and
version:

- no heading that repeats the repository or the version
- the body starts with a one-line summary, then `## Added` / `## Changed` /
  `## Fixed`

Write them by hand. A generated list of commit subjects tells a reader what was
touched, not what changed for them.
