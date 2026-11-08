// Writes the offline sources the Flatpak build reads, after go.mod,
// desktop/go.mod or web/package-lock.json changed:
//
//     node flatpak/update-sources.mjs
//
// go-sources.json holds every module file the desktop build fetches, as
// proxy.golang.org serves it, and the manifest points GOPROXY at the folder
// they land in. The list comes from a module cache of its own, filled by
// loading the build's packages for both architectures, so it holds what the
// build needs and nothing the developer's cache happens to carry.
//
// node-sources.json is flatpak-node-generator's, which has to be on the PATH:
//
//     pipx install "git+https://github.com/flatpak/flatpak-builder-tools.git#subdirectory=node"
//
// Run it on Linux: on Windows the generator writes its paths with backslashes.

import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, relative, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const root = dirname(here)
const proxy = 'https://proxy.golang.org'

function run(command, args, { cwd = root, env = {} } = {}) {
  const res = spawnSync(command, args, { cwd, stdio: 'inherit', env: { ...process.env, ...env } })
  if (res.error || res.status !== 0) {
    console.error(`failed: ${command} ${args.join(' ')}${res.error ? `: ${res.error.message}` : ''}`)
    process.exit(res.status ?? 1)
  }
}

function* files(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) yield* files(path)
    else yield path
  }
}

function goSources() {
  const cache = mkdtempSync(join(tmpdir(), 'knightloader-gomod-'))
  const env = {
    GOMODCACHE: cache,
    GOPROXY: proxy,
    GOFLAGS: '-modcacherw',
    GOOS: 'linux',
    CGO_ENABLED: '1',
  }
  try {
    for (const arch of ['amd64', 'arm64']) {
      run('go', ['list', '-deps', '-tags', 'production,gtk3', './...'], {
        cwd: join(root, 'desktop'),
        env: { ...env, GOARCH: arch },
      })
    }
    // The cache's download folder has the proxy's layout already, module
    // paths escaped the same way.
    const download = join(cache, 'cache', 'download')
    const sources = []
    for (const path of files(download)) {
      // A build from exact versions reads only these two. The .info files Go
      // keeps differ from what the proxy serves, so they could not be checked.
      if (!/\.(mod|zip)$/.test(path)) continue
      const rel = relative(download, path).split(sep).join('/')
      if (!rel.includes('/@v/')) continue
      const at = rel.lastIndexOf('/')
      sources.push({
        type: 'file',
        url: `${proxy}/${rel}`,
        sha256: createHash('sha256').update(readFileSync(path)).digest('hex'),
        dest: `gomod/${rel.slice(0, at)}`,
        'dest-filename': rel.slice(at + 1),
      })
    }
    sources.sort((a, b) => a.url.localeCompare(b.url))
    return sources
  } finally {
    rmSync(cache, { recursive: true, force: true })
  }
}

const go = goSources()
writeFileSync(join(here, 'go-sources.json'), JSON.stringify(go, null, 4) + '\n')
console.log(`go-sources.json: ${go.length} files`)

run('flatpak-node-generator', [
  'npm', join(root, 'web', 'package-lock.json'),
  '-o', join(here, 'node-sources.json'),
])
console.log('node-sources.json written')
