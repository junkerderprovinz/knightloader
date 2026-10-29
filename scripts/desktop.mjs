// Builds the desktop app for the system it runs on, the way CI does.
//
//     node scripts/desktop.mjs [--arch amd64|arm64] [--installer] [--updatetest]
//
// Windows gets KnightLoader.exe, macOS KnightLoader.app holding one binary for
// both architectures, and Linux the bare binary, all in desktop/build/bin.
// With --installer, Windows and Linux also get their installer in
// desktop/build/installer.
//
// The version is the tag on a tag build and KL_VERSION when it is set. Any
// other build is "dev", which never updates itself.
//
// yt-dlp comes with the program, at the release named in desktop/ytdlp.version,
// checked against that release's SHA2-256SUMS: in the installer beside the
// program, and in the macOS bundle in Contents/MacOS. The zips of Windows and
// Linux stay the program alone, since every installed copy updates itself from
// them (internal/update) and refuses one that holds anything else.
//
// --updatetest builds with the updatetest tag (desktop/updatetest.go). Its
// installer puts "KnightLoader Test" beside a real installation, so the
// updates can be tried against a local stand-in for GitHub.
//
// The interface is not built here: web/dist is committed and embedded through
// the server module, as in the container build. Wails' generated files
// (manifest, version resource, Info.plist, the installer's helper macros, the
// icons) are written into desktop/build on every run and ignored by git.

import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { chmodSync, cpSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { basename, dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = dirname(dirname(fileURLToPath(import.meta.url)))
const desktop = join(root, 'desktop')
const bin = join(desktop, 'build', 'bin')

const args = process.argv.slice(2)
const option = (name) => {
  const at = args.indexOf(name)
  return at >= 0 ? args[at + 1] : undefined
}
const hostArch = process.arch === 'arm64' ? 'arm64' : 'amd64'
const arch = option('--arch') ?? hostArch
const installer = args.includes('--installer')
const updatetest = args.includes('--updatetest')
const tags = (extra = '') => (updatetest ? 'production,updatetest' : 'production') + extra

const tagged = process.env.GITHUB_REF?.startsWith('refs/tags/v') ? process.env.GITHUB_REF_NAME : ''
const version = tagged || process.env.KL_VERSION || 'dev'
// NSIS, the version resource and Info.plist take numbers only, so a dev
// build gets 0.0.0.
const numbers = /^v?(\d+\.\d+\.\d+)$/.exec(version)?.[1] ?? '0.0.0'

// The DLC service key comes from the KL_DLC_KEY and KL_DLC_IV secrets in a
// release build and is absent everywhere else (docs/decisions.md).
const dlcKey = process.env.KL_DLC_KEY?.trim() ?? ''
const dlcIV = process.env.KL_DLC_IV?.trim() ?? ''
if ((dlcKey || dlcIV) && (dlcKey.length !== 16 || dlcIV.length !== 16)) {
  console.error('the DLC key and IV must be 16 characters each')
  process.exit(1)
}
const dlcFlags = dlcKey && dlcIV
  ? `-X github.com/junkerderprovinz/knightloader/internal/container.dlcKey=${dlcKey} -X github.com/junkerderprovinz/knightloader/internal/container.dlcIV=${dlcIV}`
  : ''

function redact(text) {
  for (const secret of [dlcKey, dlcIV]) {
    if (secret) text = text.split(secret).join('***')
  }
  return text
}

function run(command, commandArgs, { cwd = desktop, env = {} } = {}) {
  const res = spawnSync(command, commandArgs, { cwd, stdio: 'inherit', env: { ...process.env, ...env } })
  if (res.status !== 0) {
    console.error(redact(`failed: ${command} ${commandArgs.join(' ')}`))
    process.exit(res.status ?? 1)
  }
}

const ytdlpVersion = readFileSync(join(desktop, 'ytdlp.version'), 'utf8').trim()
const ytdlpName = process.platform === 'win32' ? 'yt-dlp.exe' : 'yt-dlp'

// The release asset for the target. yt-dlp_linux is built against glibc, which
// every distribution with WebKitGTK has.
function ytdlpAsset() {
  if (process.platform === 'win32') return arch === 'arm64' ? 'yt-dlp_arm64.exe' : 'yt-dlp.exe'
  if (process.platform === 'darwin') return 'yt-dlp_macos'
  return arch === 'arm64' ? 'yt-dlp_linux_aarch64' : 'yt-dlp_linux'
}

async function download(url) {
  const res = await fetch(url)
  if (!res.ok) {
    console.error(`${url}: ${res.status} ${res.statusText}`)
    process.exit(1)
  }
  return Buffer.from(await res.arrayBuffer())
}

const sha256 = (data) => createHash('sha256').update(data).digest('hex')

// fetchYtdlp returns the verified asset, kept in desktop/build/ytdlp so a
// second build of the same version downloads only the checksum file.
async function fetchYtdlp() {
  const asset = ytdlpAsset()
  const release = `https://github.com/yt-dlp/yt-dlp/releases/download/${ytdlpVersion}`
  const sums = (await download(`${release}/SHA2-256SUMS`)).toString('utf8')
  // sha256sum output, where a leading asterisk marks binary mode.
  const want = sums
    .split('\n')
    .map((line) => line.trim().split(/\s+/))
    .find(([, name]) => name?.replace(/^\*/, '') === asset)?.[0]
  if (!want) {
    console.error(`SHA2-256SUMS of yt-dlp ${ytdlpVersion} lists no ${asset}`)
    process.exit(1)
  }
  const file = join(desktop, 'build', 'ytdlp', ytdlpVersion, asset)
  if (existsSync(file) && sha256(readFileSync(file)) === want) return file
  const data = await download(`${release}/${asset}`)
  if (sha256(data) !== want) {
    console.error(`${asset} of yt-dlp ${ytdlpVersion} has checksum ${sha256(data)}, SHA2-256SUMS says ${want}`)
    process.exit(1)
  }
  mkdirSync(dirname(file), { recursive: true })
  writeFileSync(file, data)
  return file
}

function placeYtdlp(source, dest) {
  cpSync(source, dest)
  chmodSync(dest, 0o755)
}

// checkYtdlp starts the yt-dlp at file where this machine can run the target's
// build, which the ARM64 build for Windows, made on an x64 runner, is not.
function checkYtdlp(file) {
  if (process.platform !== 'darwin' && arch !== hostArch) return
  const res = spawnSync(file, ['--version'], { encoding: 'utf8' })
  const printed = res.stdout?.trim()
  if (res.status !== 0 || printed !== ytdlpVersion) {
    console.error(`${file} --version: exit ${res.status}, printed ${JSON.stringify(printed)}, want ${ytdlpVersion}`)
    console.error(res.error ?? res.stderr)
    process.exit(1)
  }
}

function goBuild(out, { goos, goarch, cgo, tags, ldflags = '', env = {} }) {
  run('go', [
    'build', '-tags', tags, '-trimpath', '-buildvcs=false',
    '-ldflags', `-s -w ${ldflags} ${dlcFlags} -X github.com/junkerderprovinz/knightloader/internal/buildinfo.Version=${version}`,
    '-o', out, '.',
  ], { env: { GOOS: goos, GOARCH: goarch, CGO_ENABLED: cgo, ...env } })
}

// buildInstaller builds desktop/installer around the program at exe and
// yt-dlp, into desktop/build/installer so the zips of desktop/build/bin stay
// the program alone. An updatetest installer installs as "KnightLoader Test".
function buildInstaller(exe, ytdlp, name, { goos, cgo, tags, ldflags = '' }) {
  const payload = join(desktop, 'installer', 'payload')
  rmSync(payload, { recursive: true, force: true })
  mkdirSync(payload, { recursive: true })
  cpSync(exe, join(payload, basename(exe)))
  placeYtdlp(ytdlp, join(payload, ytdlpName))
  checkYtdlp(join(payload, ytdlpName))
  const vars = [`-X main.version=${numbers}`]
  if (updatetest) vars.push("-X 'main.name=KnightLoader Test'", '-X main.id=com.junkerderprovinz.knightloader.test')
  try {
    run('go', [
      'build', '-tags', tags, '-trimpath', '-buildvcs=false',
      '-ldflags', `-s -w ${ldflags} ${vars.join(' ')}`,
      '-o', join(desktop, 'build', 'installer', name), '.',
    ], { cwd: join(desktop, 'installer'), env: { GOOS: goos, GOARCH: arch, CGO_ENABLED: cgo } })
  } finally {
    rmSync(payload, { recursive: true, force: true })
  }
}

// Wails writes its version resource under language 0000, which Windows does
// not read, and the properties dialog would show every string blank.
function writeWindowsVersion() {
  const info = {
    fixed: { file_version: `${numbers}.0`, product_version: `${numbers}.0` },
    info: {
      '0409': {
        ProductVersion: version,
        CompanyName: 'junkerderprovinz',
        FileDescription: 'KnightLoader',
        LegalCopyright: '',
        ProductName: 'KnightLoader',
        Comments: 'Self-hosted, cross-platform download manager',
        InternalName: version,
      },
    },
  }
  writeFileSync(join(desktop, 'build', 'windows', 'info.json'), JSON.stringify(info, null, '\t') + '\n')
}

console.log(`building KnightLoader ${version}, ${dlcFlags ? 'with' : 'without'} the DLC key, with yt-dlp ${ytdlpVersion}`)
const ytdlp = await fetchYtdlp()

rmSync(bin, { recursive: true, force: true })
rmSync(join(desktop, 'build', 'installer'), { recursive: true, force: true })
mkdirSync(bin, { recursive: true })

run('wails3', [
  'update', 'build-assets', '-silent',
  '-name', 'KnightLoader', '-binaryname', 'KnightLoader',
  '-config', 'build/config.yml', '-dir', 'build',
  '-productversion', numbers,
])
run('wails3', [
  'generate', 'icons', '-input', 'build/appicon.png',
  '-windowsfilename', 'build/windows/icon.ico', '-macfilename', 'build/darwin/icons.icns',
])
// Wails scales every frame of its .ico down from the one large PNG. The
// committed one has each size drawn for itself, the logo the full height of
// the frame; see .github/assets/gen-appicon.mjs.
cpSync(join(desktop, 'build', 'appicon.ico'), join(desktop, 'build', 'windows', 'icon.ico'))

if (process.platform === 'win32') {
  writeWindowsVersion()
  const syso = join(desktop, `wails_windows_${arch}.syso`)
  run('wails3', [
    'generate', 'syso', '-arch', arch, '-icon', 'build/windows/icon.ico',
    '-manifest', 'build/windows/wails.exe.manifest', '-info', 'build/windows/info.json', '-out', syso,
  ])
  const exe = join(bin, 'KnightLoader.exe')
  try {
    // WebView2 needs no cgo, so the ARM64 build is cross-compiled on an x64
    // runner without an ARM64 C compiler.
    goBuild(exe, { goos: 'windows', goarch: arch, cgo: '0', tags: tags(), ldflags: '-H windowsgui' })
  } finally {
    // Go links every .syso in the package, and one left behind would end up in
    // the next build for the other architecture.
    rmSync(syso, { force: true })
  }

  if (installer) {
    const syso = join(desktop, 'installer', `installer_windows_${arch}.syso`)
    run('wails3', [
      'generate', 'syso', '-arch', arch, '-icon', 'build/windows/icon.ico',
      '-manifest', 'installer/installer.manifest', '-info', 'build/windows/info.json', '-out', syso,
    ])
    try {
      buildInstaller(exe, ytdlp, `KnightLoader-${arch}-installer.exe`, { goos: 'windows', cgo: '0', tags: '', ldflags: '-H windowsgui' })
    } finally {
      rmSync(syso, { force: true })
    }
  }
} else if (process.platform === 'darwin') {
  // Both architectures in one binary, so one download serves every Mac. The
  // deployment target matches what Wails' own template builds for.
  const mac = { MACOSX_DEPLOYMENT_TARGET: '12.0', CGO_CFLAGS: '-mmacosx-version-min=12.0', CGO_LDFLAGS: '-mmacosx-version-min=12.0' }
  for (const a of ['amd64', 'arm64']) {
    goBuild(join(bin, `KnightLoader-${a}`), { goos: 'darwin', goarch: a, cgo: '1', tags: tags(), env: mac })
  }
  const app = join(bin, 'KnightLoader.app', 'Contents')
  mkdirSync(join(app, 'MacOS'), { recursive: true })
  mkdirSync(join(app, 'Resources'), { recursive: true })
  run('lipo', ['-create', '-output', join(app, 'MacOS', 'KnightLoader'), join(bin, 'KnightLoader-amd64'), join(bin, 'KnightLoader-arm64')])
  rmSync(join(bin, 'KnightLoader-amd64'))
  rmSync(join(bin, 'KnightLoader-arm64'))
  cpSync(join(desktop, 'build', 'darwin', 'icons.icns'), join(app, 'Resources', 'icons.icns'))
  cpSync(join(desktop, 'build', 'darwin', 'Info.plist'), join(app, 'Info.plist'))
  placeYtdlp(ytdlp, join(app, 'MacOS', ytdlpName))
  // An ad-hoc signature over the whole bundle, as Wails' own template does:
  // Apple Silicon refuses to start code that carries none. yt-dlp is signed
  // with it, and run once afterwards to show it still starts.
  run('codesign', ['--force', '--deep', '--sign', '-', join(bin, 'KnightLoader.app')])
  checkYtdlp(join(app, 'MacOS', ytdlpName))
} else {
  // GTK 3 and WebKitGTK 4.1, which every current distribution ships and which
  // the Wails 2 builds already needed. Wails does not cross-compile to Linux,
  // so the ARM64 build runs on an ARM64 runner.
  goBuild(join(bin, 'KnightLoader'), { goos: 'linux', goarch: arch, cgo: '1', tags: tags(',gtk3') })
  if (installer) {
    buildInstaller(join(bin, 'KnightLoader'), ytdlp, `KnightLoader-${arch}-installer`, { goos: 'linux', cgo: '1', tags: 'gtk3' })
  }
}

console.log(`done: ${version}`)
