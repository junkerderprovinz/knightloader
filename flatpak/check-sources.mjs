// The Flatpak build runs without network and installs npm packages and Go
// modules only from node-sources.json and go-sources.json. A dependency update
// that leaves them behind passes every other job and fails the release build,
// so this holds them to web/package-lock.json and desktop/go.mod.
//
// Every package in the lock file needs its tarball, under the same URL and
// hash. For Go, a module the build fetches at one version has to be there at
// the version desktop/go.mod requires. Modules missing at every version are
// left out, since those are the ones only other platforms load.
//
// Run by CI and by hand: `node flatpak/check-sources.mjs`.

import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const root = dirname(here)
const read = (path) => readFileSync(join(root, path), 'utf8')
const problems = []

const node = JSON.parse(read('flatpak/node-sources.json'))
const tarballs = new Map(node.filter((s) => s.type === 'file').map((s) => [s.url, s]))
const lock = JSON.parse(read('web/package-lock.json'))
for (const [name, pkg] of Object.entries(lock.packages)) {
  if (!pkg.resolved) continue
  const source = tarballs.get(pkg.resolved)
  if (!source) {
    problems.push(`web/package-lock.json: ${name} ${pkg.version} has no tarball in node-sources.json`)
    continue
  }
  for (const integrity of pkg.integrity?.split(/\s+/) ?? []) {
    const at = integrity.indexOf('-')
    const algorithm = integrity.slice(0, at)
    if (source[algorithm] !== Buffer.from(integrity.slice(at + 1), 'base64').toString('hex')) {
      problems.push(`web/package-lock.json: ${name} ${pkg.version} has another ${algorithm} in node-sources.json`)
    }
  }
}

// The proxy writes an upper-case letter in a module path or version as '!'
// and its lower-case form.
const escape = (path) => path.replace(/[A-Z]/g, (c) => `!${c.toLowerCase()}`)
const proxy = 'https://proxy.golang.org/'
const fetched = new Map()
for (const { url } of JSON.parse(read('flatpak/go-sources.json'))) {
  const m = url.startsWith(proxy) && url.slice(proxy.length).match(/^(.+)\/@v\/(.+)\.(mod|zip)$/)
  if (!m) continue
  const [, path, version, kind] = m
  if (!fetched.has(path)) fetched.set(path, { mod: new Set(), zip: new Set() })
  fetched.get(path)[kind].add(version)
}

const gomod = read('desktop/go.mod').replace(/\/\/.*$/gm, '')
const replaced = new Map()
for (const [, from, to, version] of gomod.matchAll(/^replace\s+(\S+)\s+=>\s+(\S+)(?:\s+(\S+))?\s*$/gm)) {
  replaced.set(from, version ? { path: to, version } : null)
}
const required = []
for (const [, block] of gomod.matchAll(/^require\s*\(([^)]*)\)/gm)) {
  for (const line of block.split('\n')) {
    const [path, version] = line.trim().split(/\s+/)
    if (version) required.push({ path, version })
  }
}
for (const [, path, version] of gomod.matchAll(/^require\s+([^\s(]+)\s+(\S+)/gm)) required.push({ path, version })

for (const req of required) {
  if (replaced.has(req.path) && !replaced.get(req.path)) continue
  const { path, version } = replaced.get(req.path) ?? req
  const have = fetched.get(escape(path))
  if (!have) continue
  for (const kind of ['mod', 'zip']) {
    if (have[kind].size && !have[kind].has(escape(version))) {
      problems.push(`desktop/go.mod: ${path} ${version} has no .${kind} in go-sources.json`)
    }
  }
}

if (problems.length) {
  console.error(`check-sources: ${problems.length} problem(s). Run \`node flatpak/update-sources.mjs\` on Linux and commit both JSON files.`)
  for (const p of problems) console.error(`  ${p}`)
  process.exit(1)
}
console.log('ok: the Flatpak sources hold every npm package in the lock file and every Go module version the desktop build fetches')
