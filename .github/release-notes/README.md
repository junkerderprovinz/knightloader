# Release notes

One file per tag, named after it. A tag with a namespace keeps it as a
directory, because that is how the ref name reads:

| Tag | File |
| --- | --- |
| `v1.2.3` | `v1.2.3.md` |
| `mobile/v1.0.0` | `mobile/v1.0.0.md` |
| `extension/v1.2.0` | `extension/v1.2.0.md` |

The three behave differently on a missing file, on purpose. KnightLoader's own
release falls back to generated notes; the app and the extension **stop**
instead. `softprops/action-gh-release` swallows an unreadable `body_path` and
publishes an empty release with only a warning in the log - after a
three-quarter-hour APK build that is quieter than a failure and just as wrong,
so those two workflows check for the file first.

`release.yml` publishes the matching file as the release body when a `v*.*.*`
tag is pushed, and falls back to generated notes only when the file is missing.

Two rules, both because the GitHub release title already carries the name and
version:

- no heading that repeats the repository or the version
- the body starts with a one-line summary, then `## Added` / `## Changed` /
  `## Fixed`

Write them by hand. A generated list of commit subjects tells a reader what was
touched, not what changed for them.
