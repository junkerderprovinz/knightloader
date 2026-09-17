# Release notes

One file per tag, named after it. A tag with a namespace keeps it as a
directory, because that is how the ref name reads:

| Tag | File |
| --- | --- |
| `v1.2.3` | `v1.2.3.md` |
| `mobile/v1.0.0` | `mobile/v1.0.0.md` |
| `extension/v1.2.0` | `extension/v1.2.0.md` |

All three **stop** on a missing file. `softprops/action-gh-release` swallows an
unreadable `body_path` and publishes an empty release with only a warning in the
log - after a three-quarter-hour APK build that is quieter than a failure and
just as wrong, so the app and extension workflows check for the file first.
KnightLoader's own release used to fall back to generated notes and no longer
does: a generated list of commit subjects is exactly what these files replace.

For a `v*.*.*` tag, `release.yml` checks for the file first, runs the desktop
build, and only then creates the release, with this file as its body and the
zips attached in the same step. A release is never public without its downloads.

Two rules, both because the GitHub release title already carries the name and
version:

- no heading that repeats the repository or the version
- the body starts with a one-line summary, then `## Added` / `## Changed` /
  `## Fixed`

Write them by hand. A generated list of commit subjects tells a reader what was
touched, not what changed for them.
