// Looks for a newer release of a component KnightLoader pins by hand, out of
// Renovate's reach, and moves its pins together with their checksums.
// pin-updates.yml runs it once per component and turns what it writes to
// GITHUB_OUTPUT into a pull request.
//
//     node scripts/pins/update-pins.mjs temurin|unrar
//
// Nothing is written when there is nothing to move to. A page or file that
// reads differently from what this expects stops the run with an error.

import { createHash, randomUUID } from 'node:crypto'
import { appendFileSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { gunzipSync } from 'node:zlib'

export const pinFiles = {
  jre: 'internal/jre/jre.go',
  dockerfile: 'Dockerfile',
  unrar: 'desktop/unrar.json',
}

export const sources = {
  adoptium: 'https://api.adoptium.net/v3/assets/latest/21/hotspot?image_type=jre&vendor=eclipse',
  rarDownloads: 'https://www.rarlab.com/download.htm',
  rarExtras: 'https://www.rarlab.com/rar_add.htm',
}

const greeting = 'Have a look when you have a moment.'

// compareVersions orders two lists of numbers, a missing tail counting as zero.
export function compareVersions(a, b) {
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    const d = (a[i] ?? 0) - (b[i] ?? 0)
    if (d !== 0) return Math.sign(d)
  }
  return 0
}

export function numbers(version) {
  if (!/^\d+(\.\d+)*$/.test(version)) throw new Error(`${version} is not a version number`)
  return version.split('.').map(Number)
}

// temurinKey makes "21.0.12.1+1", a respin, rank above every 21.0.12+N and
// below 21.0.13+1, which comparing the parts one by one would get wrong.
export function temurinKey(version) {
  const m = /^(\d+(?:\.\d+)*)\+(\d+)$/.exec(version)
  if (!m) throw new Error(`${version} is not a Temurin version`)
  const v = numbers(m[1])
  while (v.length < 4) v.push(0)
  return [...v, Number(m[2])]
}

function find(text, re, what) {
  const m = re.exec(text)
  if (!m) throw new Error(`cannot find ${what}`)
  return m[1]
}

function swap(text, from, to, what) {
  if (!text.includes(from)) throw new Error(`cannot find ${what} (${from})`)
  return text.replaceAll(from, to)
}

const sha256 = (data) => createHash('sha256').update(data).digest('hex')

function upToDate(name, version) {
  console.log(`${name} ${version} is the newest release`)
  return null
}

function notYet(reason) {
  console.log(`::warning::${reason}`)
  return null
}

const pinRow = /\{"(OpenJDK21U-jre_[^"]+)", "[0-9a-f]{64}"\}/g

export function currentTemurin(jreGo) {
  const version = find(jreGo, /\bVersion = "([^"]+)"/, 'the Temurin version in jre.go')
  const archives = [...new Set([...jreGo.matchAll(pinRow)].map((m) => m[1]))]
  if (archives.length === 0) throw new Error('cannot find the pinned archives in jre.go')
  return { version, archives }
}

const fileVersion = (version) => version.replace('+', '_')
const releaseTag = (version) => encodeURIComponent(`jdk-${version}`)

// latestTemurin looks up each pinned archive's successor in Adoptium's list of
// the newest archive per platform. The platforms sometimes lag each other by a
// few days, and until they agree there is no release to move to.
export function latestTemurin(assets, current) {
  const old = fileVersion(current.version)
  const found = current.archives.map((pinned) => {
    const [head, tail] = pinned.split(old)
    const asset = assets.find((a) => {
      const name = a.binary.package.name
      const version = a.release_name.replace(/^jdk-/, '')
      return a.binary.image_type === 'jre' && name === head + fileVersion(version) + tail
    })
    if (!asset) throw new Error(`Adoptium lists nothing that follows ${pinned}`)
    return { old: pinned, asset }
  })
  const releases = [...new Set(found.map((f) => f.asset.release_name))]
  if (releases.length > 1) {
    return { version: null, reason: `Temurin's platforms are on different releases (${releases.join(', ')}), so the pins wait` }
  }
  const version = releases[0].replace(/^jdk-/, '')
  const base = `https://github.com/adoptium/temurin21-binaries/releases/download/${releaseTag(version)}/`
  return {
    version,
    archives: found.map(({ old, asset }) => {
      const { name, checksum, checksum_link: sumURL } = asset.binary.package
      if (sumURL !== `${base}${name}.sha256.txt`) throw new Error(`${name}'s checksum file is at ${sumURL}, not in the release jre.go downloads from`)
      return { old, name, sha256: checksum, sumURL }
    }),
  }
}

// parseSumFile reads a sha256sum line for name, the format of Adoptium's
// .sha256.txt files.
export function parseSumFile(text, name) {
  const m = /^([0-9a-f]{64}) [ *]?(\S+)\s*$/.exec(text.trim())
  if (!m || m[2] !== name) throw new Error(`the checksum file for ${name} reads ${JSON.stringify(text.slice(0, 120))}`)
  return m[1]
}

export function rewriteJre(src, current, next) {
  const names = new Map(next.archives.map((a) => [a.old, a]))
  let out = swap(src, `Version = "${current.version}"`, `Version = "${next.version}"`, 'the Temurin version')
  out = swap(out, `/${releaseTag(current.version)}"`, `/${releaseTag(next.version)}"`, 'the release page')
  out = swap(out, `/${releaseTag(current.version)}/`, `/${releaseTag(next.version)}/`, 'the download address')
  out = out.replace(pinRow, (_, name) => `{"${names.get(name).name}", "${names.get(name).sha256}"}`)
  for (const stale of [current.version, fileVersion(current.version), releaseTag(current.version)]) {
    if (out.includes(stale)) throw new Error(`jre.go still names ${stale} after the rewrite`)
  }
  return out
}

export async function updateTemurin(root, get) {
  const path = join(root, pinFiles.jre)
  const src = readFileSync(path, 'utf8')
  const current = currentTemurin(src)
  const next = latestTemurin(JSON.parse(await get(sources.adoptium)), current)
  if (!next.version) return notYet(next.reason)
  if (compareVersions(temurinKey(next.version), temurinKey(current.version)) <= 0) return upToDate('Temurin', current.version)
  for (const a of next.archives) {
    const published = parseSumFile((await get(a.sumURL)).toString('utf8'), a.name)
    if (published !== a.sha256) throw new Error(`${a.name}: Adoptium's API lists ${a.sha256}, its checksum file ${published}`)
  }
  writeFileSync(path, rewriteJre(src, current, next))
  const release = `https://github.com/adoptium/temurin21-binaries/releases/tag/${releaseTag(next.version)}`
  return {
    current: current.version,
    version: next.version,
    branch: `pins/temurin-${next.version}`,
    title: `Take Eclipse Temurin ${next.version} for the desktop app`,
    body: [
      `Eclipse Temurin ${next.version} is out, so this moves the Java runtime the desktop app fetches up from ${current.version}.`,
      `Each SHA-256 comes from the .sha256.txt file beside its archive in [the release](${release}) and matches what Adoptium's API lists.`,
      greeting,
    ].join(' '),
    files: [pinFiles.jre],
  }
}

// RARLAB names a download after its version without the dot, "723" for 7.23.
// A letter after the number marks a beta or a translation, which these leave
// out. The macOS build has been called both rarmacos-arm and rarmacos-arm64.
const rarBuilds = {
  windows: /^\/rar\/winrar-x64-(\d)(\d\d)\.exe$/,
  'linux-amd64': /^\/rar\/rarlinux-x64-(\d)(\d\d)\.tar\.gz$/,
  'darwin-arm64': /^\/rar\/rarmacos-arm(?:64)?-(\d)(\d\d)\.tar\.gz$/,
}

function rarlabPath(url) {
  const u = new URL(url, 'https://www.rarlab.com/')
  return u.host === 'www.rarlab.com' ? u.pathname : ''
}

export function currentUnrar(pins) {
  const versions = new Set(
    Object.entries(rarBuilds).map(([key, re]) => {
      const m = re.exec(rarlabPath(pins[key]?.url ?? ''))
      if (!m) throw new Error(`desktop/unrar.json has no ${key} build from rarlab.com`)
      return `${m[1]}.${m[2]}`
    }),
  )
  if (versions.size > 1) throw new Error(`desktop/unrar.json pins different versions: ${[...versions].join(', ')}`)
  return [...versions][0]
}

// latestUnrar reads the newest release RARLAB's download page offers for each
// platform the desktop app ships UnRAR on.
export function latestUnrar(html) {
  const links = [...html.matchAll(/href="([^"]+)"/g)].map((m) => m[1])
  const latest = {}
  for (const [key, re] of Object.entries(rarBuilds)) {
    for (const link of links) {
      const m = re.exec(rarlabPath(link))
      if (!m) continue
      const version = `${m[1]}.${m[2]}`
      if (!latest[key] || compareVersions(numbers(version), numbers(latest[key].version)) > 0) {
        latest[key] = { version, url: new URL(link, 'https://www.rarlab.com/').href }
      }
    }
    if (!latest[key]) throw new Error(`RARLAB's download page lists no ${key} release`)
  }
  const versions = [...new Set(Object.values(latest).map((l) => l.version))]
  if (versions.length > 1) {
    return { version: null, reason: `RARLAB's download page offers ${versions.join(', ')} across the platforms, so the pins wait` }
  }
  return { version: versions[0], urls: Object.fromEntries(Object.entries(latest).map(([key, l]) => [key, l.url])) }
}

// unrarSourceURL finds the UnRAR source on RARLAB's extras page. Its numbering
// is a series of its own (unrarsrc-7.2.7 is UnRAR 7.23), so the version comes
// from the archive's version.hpp.
export function unrarSourceURL(html) {
  const links = [...new Set([...html.matchAll(/href="([^"]*\bunrarsrc-\d+\.\d+\.\d+\.tar\.gz)"/g)].map((m) => m[1]))]
  if (links.length !== 1) throw new Error(`RARLAB's extras page links ${links.length} UnRAR source archives`)
  return new URL(links[0], 'https://www.rarlab.com/').href
}

export function unrarSourceVersion(url) {
  return find(url, /unrarsrc-(\d+\.\d+\.\d+)\.tar\.gz$/, `the version in ${url}`)
}

export function parseVersionHpp(text) {
  const field = (name) => Number(find(text, new RegExp(`#define\\s+RARVER_${name}\\s+(\\d+)`), `RARVER_${name}`))
  return { version: `${field('MAJOR')}.${String(field('MINOR')).padStart(2, '0')}`, beta: field('BETA') }
}

// readTar lists the regular files in a gzipped tar. RARLAB's archives are
// plain ustar with short names, which is all this reads.
export function readTar(gz) {
  const tar = gunzipSync(gz)
  const files = new Map()
  let at = 0
  while (at + 512 <= tar.length && tar[at] !== 0) {
    const field = (from, len) => tar.toString('latin1', at + from, at + from + len).replace(/\0[\s\S]*$/, '')
    const size = parseInt(field(124, 12).trim(), 8)
    const type = field(156, 1)
    if (type === '0' || type === '') files.set(field(0, 100), tar.subarray(at + 512, at + 512 + size))
    at += 512 + Math.ceil(size / 512) * 512
  }
  return files
}

// checkRarBuild makes sure a download is the kind of file scripts/desktop.mjs
// unpacks it as, holding the program and licence the pin names, before its
// checksum is taken on trust.
export function checkRarBuild(key, data, pin) {
  if (key === 'windows') {
    if (data.subarray(0, 2).toString('latin1') !== 'MZ') throw new Error('the WinRAR download is not a Windows program')
    return
  }
  const tree = readTar(data)
  for (const name of [pin.program, pin.licence]) {
    if (!tree.has(name)) throw new Error(`the ${key} download holds no ${name}`)
  }
}

export function rewriteUnrarJson(text, next) {
  const pins = JSON.parse(text)
  for (const [key, build] of Object.entries(next.builds)) Object.assign(pins[key], build)
  return JSON.stringify(pins, null, 2) + '\n'
}

export function rewriteDockerfile(src, current, next) {
  let out = swap(src, `unrarsrc-${current.source} is UnRAR ${current.version},`, `unrarsrc-${next.source} is UnRAR ${next.version},`, 'the UnRAR comment')
  out = swap(out, `ARG UNRAR_VERSION=${current.source}\n`, `ARG UNRAR_VERSION=${next.source}\n`, 'ARG UNRAR_VERSION')
  return swap(out, `ARG UNRAR_SHA256=${current.sourceSha256}\n`, `ARG UNRAR_SHA256=${next.sourceSha256}\n`, 'ARG UNRAR_SHA256')
}

export async function updateUnrar(root, get) {
  const jsonPath = join(root, pinFiles.unrar)
  const dockerPath = join(root, pinFiles.dockerfile)
  const json = readFileSync(jsonPath, 'utf8')
  const dockerfile = readFileSync(dockerPath, 'utf8')
  const pins = JSON.parse(json)
  const current = {
    version: currentUnrar(pins),
    source: find(dockerfile, /^ARG UNRAR_VERSION=(\d+\.\d+\.\d+)$/m, 'ARG UNRAR_VERSION in the Dockerfile'),
    sourceSha256: find(dockerfile, /^ARG UNRAR_SHA256=([0-9a-f]{64})$/m, 'ARG UNRAR_SHA256 in the Dockerfile'),
  }
  const page = latestUnrar((await get(sources.rarDownloads)).toString('utf8'))
  if (!page.version) return notYet(page.reason)
  if (compareVersions(numbers(page.version), numbers(current.version)) <= 0) return upToDate('UnRAR', current.version)

  const sourceURL = unrarSourceURL((await get(sources.rarExtras)).toString('utf8'))
  const archive = await get(sourceURL)
  const tree = readTar(archive)
  for (const name of ['unrar/makefile', 'unrar/license.txt', 'unrar/version.hpp']) {
    if (!tree.has(name)) throw new Error(`${sourceURL} holds no ${name}`)
  }
  const built = parseVersionHpp(tree.get('unrar/version.hpp').toString('latin1'))
  if (built.version !== page.version || built.beta !== 0) {
    const what = built.beta ? `${built.version} beta ${built.beta}` : built.version
    return notYet(`UnRAR ${page.version} is out, but the source RARLAB offers is ${what}, so the pins wait`)
  }

  const next = { version: page.version, source: unrarSourceVersion(sourceURL), sourceSha256: sha256(archive), builds: {} }
  for (const [key, url] of Object.entries(page.urls)) {
    const data = await get(url)
    checkRarBuild(key, data, pins[key])
    next.builds[key] = { url, sha256: sha256(data) }
  }
  writeFileSync(jsonPath, rewriteUnrarJson(json, next))
  writeFileSync(dockerPath, rewriteDockerfile(dockerfile, current, next))
  return {
    current: current.version,
    version: next.version,
    branch: `pins/unrar-${next.version}`,
    title: `Take UnRAR ${next.version}`,
    body: [
      `UnRAR ${next.version} is out, so this moves the desktop builds and the container image up from ${current.version}.`,
      `RARLAB publishes no checksums, so this run computed each SHA-256 from the files it downloaded from rarlab.com, and checked that unrarsrc-${next.source} calls itself ${next.version} in version.hpp.`,
      greeting,
    ].join(' '),
    files: [pinFiles.unrar, pinFiles.dockerfile],
  }
}

async function download(url) {
  const headers = { 'user-agent': 'knightloader-pin-updates' }
  const res = await fetch(url, { headers, signal: AbortSignal.timeout(120_000) })
  if (!res.ok) throw new Error(`${url} answered ${res.status}`)
  return Buffer.from(await res.arrayBuffer())
}

function emit(result) {
  const out = process.env.GITHUB_OUTPUT
  for (const [key, value] of Object.entries(result)) {
    const text = Array.isArray(value) ? value.join(' ') : value
    console.log(`${key}: ${text}`)
    if (out) {
      const end = `END_${randomUUID()}`
      appendFileSync(out, `${key}<<${end}\n${text}\n${end}\n`)
    }
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const updaters = { temurin: updateTemurin, unrar: updateUnrar }
  const run = updaters[process.argv[2]]
  if (!run) {
    console.error(`usage: node scripts/pins/update-pins.mjs ${Object.keys(updaters).join('|')}`)
    process.exit(2)
  }
  const root = dirname(dirname(dirname(fileURLToPath(import.meta.url))))
  const result = await run(root, download)
  if (result) emit(result)
}
