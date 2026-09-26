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
keep a ledger of them in `web/untranslated.json` and `mobile/untranslated.json`:
what still waits for a translation, and what was checked and stays English. The
`check-untranslated.mjs` next to each ledger holds it against the catalogues.
`seed-untranslated.mjs` adds a new key to every catalogue in English and lists
it as owed, and `prune-untranslated.mjs` takes out what has been translated
since.

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
