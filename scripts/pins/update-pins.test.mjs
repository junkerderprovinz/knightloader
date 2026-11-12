// Runs the pin updater against saved pages and copies of the real pin files,
// without the network: node --test scripts/pins/update-pins.test.mjs

import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { afterEach, beforeEach, describe, test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'

import {
  compareVersions,
  currentTemurin,
  latestTemurin,
  latestUnrar,
  pinFiles,
  rewriteUnrarJson,
  sources,
  temurinKey,
  unrarSourceURL,
  updateTemurin,
  updateUnrar,
} from './update-pins.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const repo = dirname(dirname(here))
const fixture = (name) => readFileSync(join(here, 'testdata', name), 'utf8')
const sha256 = (data) => createHash('sha256').update(data).digest('hex')

// offline answers the given addresses and fails on any other, so a test also
// proves what the updater asks for.
const offline = (routes) => async (url) => {
  if (!(url in routes)) throw new Error(`unexpected fetch of ${url}`)
  return Buffer.from(routes[url])
}

function tarGz(files) {
  const blocks = []
  for (const [name, body] of Object.entries(files)) {
    const data = Buffer.from(body)
    const header = Buffer.alloc(512)
    header.write(name, 0, 'latin1')
    header.write('0000644\0', 100)
    header.write(data.length.toString(8).padStart(11, '0') + '\0', 124)
    header.write('0', 156)
    header.write('ustar\x0000', 257)
    header.fill(' ', 148, 156)
    const sum = header.reduce((a, b) => a + b, 0)
    header.write(sum.toString(8).padStart(6, '0') + '\0 ', 148)
    blocks.push(header, data, Buffer.alloc((512 - (data.length % 512)) % 512))
  }
  blocks.push(Buffer.alloc(1024))
  return gzipSync(Buffer.concat(blocks))
}

let root
beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), 'pins-'))
  for (const file of Object.values(pinFiles)) {
    mkdirSync(dirname(join(root, file)), { recursive: true })
    cpSync(join(repo, file), join(root, file))
  }
})
afterEach(() => rmSync(root, { recursive: true, force: true }))

const read = (file) => readFileSync(join(root, file), 'utf8')

describe('versions', () => {
  test('a missing tail counts as zero', () => {
    assert.equal(compareVersions([7, 23], [7, 23, 0]), 0)
    assert.equal(compareVersions([7, 30], [7, 23]), 1)
    assert.equal(compareVersions([3, 8, 0], [3, 10, 0]), -1)
  })

  test('a Temurin respin ranks between its release and the next one', () => {
    const order = ['21.0.12+7', '21.0.12+8', '21.0.12.1+1', '21.0.13+1', '21.0.13+9']
    for (let i = 1; i < order.length; i++) {
      assert.equal(compareVersions(temurinKey(order[i]), temurinKey(order[i - 1])), 1, `${order[i]} > ${order[i - 1]}`)
    }
    assert.throws(() => temurinKey('21.0.13'))
  })
})

describe('Temurin', () => {
  const assets = () => JSON.parse(fixture('adoptium-latest.json'))
  const sumFiles = (list) =>
    Object.fromEntries(list.map((a) => [a.binary.package.checksum_link, `${a.binary.package.checksum}  ${a.binary.package.name}\n`]))

  test('moves every pinned archive to the release they all share', async () => {
    const list = assets()
    const before = currentTemurin(read(pinFiles.jre))
    const result = await updateTemurin(root, offline({ [sources.adoptium]: JSON.stringify(list), ...sumFiles(list) }))

    assert.equal(result.version, '21.0.99+1')
    assert.equal(result.current, before.version)
    assert.equal(result.branch, 'pins/temurin-21.0.99+1')
    assert.deepEqual(result.files, [pinFiles.jre])
    assert.match(result.body, new RegExp(`up from ${before.version.replace('+', '\\+')}\\.`))

    const jre = read(pinFiles.jre)
    assert.match(jre, /Version = "21\.0\.99\+1"/)
    assert.match(jre, /releases\/tag\/jdk-21\.0\.99%2B1"/)
    assert.match(jre, /releases\/download\/jdk-21\.0\.99%2B1\/"/)
    assert.ok(!jre.includes(before.version))
    const byName = new Map(list.map((a) => [a.binary.package.name, a.binary.package.checksum]))
    const rows = [...jre.matchAll(/"(\w+\/\w+)": +\{"([^"]+)", "([0-9a-f]{64})"\}/g)]
    assert.equal(rows.length, 6)
    for (const [, , name, sum] of rows) assert.equal(sum, byName.get(name), name)
    const row = (platform) => rows.find((r) => r[1] === platform).slice(2)
    assert.deepEqual(row('windows/arm64'), row('windows/amd64'))
    assert.equal(row('darwin/arm64')[0], 'OpenJDK21U-jre_aarch64_mac_hotspot_21.0.99_1.tar.gz')
  })

  test('waits while one platform is still on the old release', () => {
    const list = assets()
    const mac = list.find((a) => a.binary.os === 'mac' && a.binary.architecture === 'aarch64')
    mac.release_name = 'jdk-21.0.98+3'
    mac.binary.package.name = 'OpenJDK21U-jre_aarch64_mac_hotspot_21.0.98_3.tar.gz'
    const next = latestTemurin(list, currentTemurin(read(pinFiles.jre)))
    assert.equal(next.version, null)
    assert.match(next.reason, /jdk-21\.0\.98\+3/)
  })

  test('refuses a checksum file that disagrees with the API', async () => {
    const list = assets()
    const sums = sumFiles(list)
    const link = list.find((a) => a.binary.os === 'linux' && a.binary.architecture === 'x64').binary.package.checksum_link
    sums[link] = sums[link].replace(/^[0-9a-f]{64}/, '9'.repeat(64))
    const original = read(pinFiles.jre)
    await assert.rejects(updateTemurin(root, offline({ [sources.adoptium]: JSON.stringify(list), ...sums })), /checksum file 9{64}/)
    assert.equal(read(pinFiles.jre), original)
  })

  test('leaves the pins alone when they name the newest release', async () => {
    const pinned = currentTemurin(read(pinFiles.jre)).version
    const list = JSON.parse(JSON.stringify(assets()).replaceAll('21.0.99+1', pinned).replaceAll('21.0.99_1', pinned.replace('+', '_')).replaceAll('21.0.99%2B1', encodeURIComponent(pinned)))
    assert.equal(await updateTemurin(root, offline({ [sources.adoptium]: JSON.stringify(list) })), null)
  })
})

describe('UnRAR', () => {
  const page = fixture('rarlab-download.htm')

  test('reads the newest stable English build for each platform', () => {
    assert.deepEqual(latestUnrar(page), {
      version: '7.23',
      urls: {
        windows: 'https://www.rarlab.com/rar/winrar-x64-723.exe',
        'linux-amd64': 'https://www.rarlab.com/rar/rarlinux-x64-723.tar.gz',
        'darwin-arm64': 'https://www.rarlab.com/rar/rarmacos-arm-723.tar.gz',
      },
    })
  })

  test('takes the macOS build under its arm64 name too', () => {
    assert.equal(latestUnrar(page.replaceAll('rarmacos-arm-723', 'rarmacos-arm64-723')).urls['darwin-arm64'], 'https://www.rarlab.com/rar/rarmacos-arm64-723.tar.gz')
  })

  test('waits while the platforms disagree', () => {
    assert.equal(latestUnrar(page.replace('rarlinux-x64-723', 'rarlinux-x64-724')).version, null)
  })

  test('finds the source archive on the extras page', () => {
    assert.equal(unrarSourceURL(fixture('rarlab-extras.htm')), 'https://www.rarlab.com/rar/unrarsrc-7.3.2.tar.gz')
  })

  test('writes desktop/unrar.json back unchanged when nothing moves', () => {
    const json = read(pinFiles.unrar)
    assert.equal(rewriteUnrarJson(json, { builds: {} }), json)
  })

  const release = (hpp) => {
    const newer = page.replaceAll('-723', '-990')
    const source = tarGz({ 'unrar/version.hpp': hpp, 'unrar/makefile': 'all:\n', 'unrar/license.txt': 'licence\n' })
    const tarball = tarGz({ 'rar/unrar': 'ELF', 'rar/license.txt': 'licence\n' })
    const builds = {
      'https://www.rarlab.com/rar/winrar-x64-990.exe': 'MZ windows',
      'https://www.rarlab.com/rar/rarlinux-x64-990.tar.gz': tarball,
      'https://www.rarlab.com/rar/rarmacos-arm-990.tar.gz': tarball,
    }
    const routes = {
      [sources.rarDownloads]: newer,
      [sources.rarExtras]: fixture('rarlab-extras.htm').replace('unrarsrc-7.3.2', 'unrarsrc-9.9.1'),
      'https://www.rarlab.com/rar/unrarsrc-9.9.1.tar.gz': source,
      ...builds,
    }
    return { routes, source, builds }
  }
  const hpp = (minor, beta) => `#define RARVER_MAJOR     9\n#define RARVER_MINOR    ${minor}\n#define RARVER_BETA      ${beta}\n`

  test('moves the desktop builds and the image to a new release with computed checksums', async () => {
    const { routes, source, builds } = release(hpp(90, 0))
    const before = JSON.parse(read(pinFiles.unrar))
    const result = await updateUnrar(root, offline(routes))

    assert.equal(result.version, '9.90')
    assert.equal(result.branch, 'pins/unrar-9.90')
    assert.deepEqual(result.files, [pinFiles.unrar, pinFiles.dockerfile])
    assert.match(result.body, /RARLAB publishes no checksums/)

    const pins = JSON.parse(read(pinFiles.unrar))
    for (const [key, pin] of Object.entries(pins)) {
      assert.equal(pin.sha256, sha256(Buffer.from(builds[pin.url])), key)
      assert.equal(pin.program, before[key].program)
    }
    const dockerfile = read(pinFiles.dockerfile)
    assert.match(dockerfile, /^# unrarsrc-9\.9\.1 is UnRAR 9\.90, the release/m)
    assert.match(dockerfile, /^ARG UNRAR_VERSION=9\.9\.1$/m)
    assert.match(dockerfile, new RegExp(`^ARG UNRAR_SHA256=${sha256(source)}$`, 'm'))
  })

  test('waits while the source RARLAB offers is a beta', async () => {
    const { routes } = release(hpp(90, 2))
    const before = [read(pinFiles.unrar), read(pinFiles.dockerfile)]
    assert.equal(await updateUnrar(root, offline(routes)), null)
    assert.deepEqual([read(pinFiles.unrar), read(pinFiles.dockerfile)], before)
  })

  test('refuses a Linux download that does not hold the program', async () => {
    const { routes } = release(hpp(90, 0))
    routes['https://www.rarlab.com/rar/rarlinux-x64-990.tar.gz'] = tarGz({ 'rar/rar': 'ELF' })
    await assert.rejects(updateUnrar(root, offline(routes)), /holds no rar\/unrar/)
  })

  test('leaves the pins alone when they name the newest release', async () => {
    assert.equal(await updateUnrar(root, offline({ [sources.rarDownloads]: page })), null)
  })
})
