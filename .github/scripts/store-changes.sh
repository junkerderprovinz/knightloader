#!/usr/bin/env bash
# Decides what a release sends to the stores, and writes extension=true|false
# and app=true|false to $GITHUB_OUTPUT.
#
# Every release raises the extension's and the app's version with the rest,
# and every store reviews each version it is sent. So the extension goes to the
# browser stores only when something under extension/src changed since the
# previous release, and the app goes to Google Play only when something under
# mobile/ did. The version fields themselves are left out of the comparison.
# FORCE=true sends both.
set -euo pipefail

tag=$GITHUB_REF_NAME
if ! printf '%s' "$tag" | grep -Eq '^v[0-9]+[.][0-9]+[.][0-9]+$'; then
  echo "::error::run this on a release tag such as v1.4.0, not on $tag."
  exit 1
fi

# Prints a JSON file as it was at a revision, without the named fields.
without() {
  local rev=$1 file=$2
  shift 2
  git show "$rev:$file" 2>/dev/null | node -e '
    let s = "";
    process.stdin.on("data", (c) => (s += c)).on("end", () => {
      const doc = JSON.parse(s);
      for (const path of process.argv.slice(1)) {
        const keys = path.split(".");
        const last = keys.pop();
        const parent = keys.reduce((o, k) => (o ? o[k] : undefined), doc);
        if (parent) delete parent[last];
      }
      console.log(JSON.stringify(doc));
    });
  ' "$@"
}

# changed <dir> <version file> <field>...
changed() {
  local dir=$1 file=$2
  shift 2
  if ! git diff --quiet "$prev" "$tag" -- "$dir" ":(exclude)$file"; then
    return 0
  fi
  [ "$(without "$prev" "$file" "$@")" != "$(without "$tag" "$file" "$@")" ]
}

extension=true
app=true
prev=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' "$tag^" 2>/dev/null || true)
if [ "${FORCE:-false}" = true ]; then
  echo "sending everything, as asked"
elif [ -z "$prev" ]; then
  echo "$tag is the first release"
else
  changed extension/src extension/src/manifest.json version || extension=false
  changed mobile mobile/app.json expo.version expo.android.versionCode || app=false
  echo "since $prev: extension changed: $extension, app changed: $app"
fi

{
  echo "extension=$extension"
  echo "app=$app"
} >> "$GITHUB_OUTPUT"
