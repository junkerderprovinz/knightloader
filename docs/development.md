# Building and testing

```sh
go test ./... -count=1        # server
cd web && npm ci && npx tsc --noEmit && npm run build
node check-docs-claims.mjs    # the numbers the README and this manual assert
node check-queue-reach.mjs    # the browser offers the queue verbs the server takes
```

A handful of tests open a socket on a real network interface instead of on
loopback: discovery joins its multicast group, and the torrent tests reach a
real swarm. They are skipped unless `KNIGHTLOADER_NET_TESTS` is set, because
Windows puts a firewall dialog in front of every new test binary that listens
that widely and blocks the run until it is clicked away. CI sets the variable,
so those tests still run on every push; set it yourself to run them locally.

The UI is built into `web/dist`, which is committed and embedded into the
binary, so a plain `go build` produces a working server. English is the source
locale and every other one is typed against it, which makes `tsc` the gate that
catches a missing or stray translation key.

A value that is still English passes that gate, so the web UI and the app each
keep a ledger in the same format, `web/untranslated.json` and
`mobile/untranslated.json`: `locales` lists what still waits for a translation,
and `identical` what was checked and stays English. German is written by hand
and never appears in either. The `check-untranslated.mjs` next to each ledger
holds it against the catalogues, but the two checks catch different things. The
app's fails on any value that equals the English one and is in neither list, so
the app's ledger names every English value outside German. The web's fails only
on a whole English sentence that neither list names. Many short web values equal
the English one by right and were never listed, so the web ledger is not a full
count. `seed-untranslated.mjs` fills a key missing from a catalogue other than
German with the English text and lists it as owed; the app's seed also lists
English values already in a catalogue that neither list names.
`prune-untranslated.mjs` drops the owed entries that have since been
translated, and the `identical` entries that got a word of their own.

The desktop app and the container image are built as described under
[Installing](installing.md).

## This manual

```sh
pip install -r docs-requirements.txt
mkdocs serve                  # then open http://127.0.0.1:8000
```

CI builds it with `mkdocs build --strict`, so a link to a page that does not
exist fails the build.

## Screenshots

`scripts/screenshots/shoot.mjs` draws the README's screenshots from made-up
sample data, in both themes. Run `npm install` and
`npx playwright install chromium` in that folder once, then `node shoot.mjs`.
