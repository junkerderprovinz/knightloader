// Builds the desktop app for the system it runs on, the way CI does.
//
//     node scripts/desktop.mjs [--arch amd64|arm64] [--installer] [--updatetest]
//
// Windows gets KnightLoader.exe and, with --installer, the NSIS installer.
// macOS gets KnightLoader.app holding one binary for both architectures. Linux
// gets the bare binary. Everything lands in desktop/build/bin.
//
// The version is the tag on a tag build and KL_VERSION when it is set. Any
// other build is "dev", which never updates itself.
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
import { cpSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
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

function goBuild(out, { goos, goarch, cgo, tags, ldflags = '', env = {} }) {
  run('go', [
    'build', '-tags', tags, '-trimpath', '-buildvcs=false',
    '-ldflags', `-s -w ${ldflags} ${dlcFlags} -X github.com/junkerderprovinz/knightloader/internal/buildinfo.Version=${version}`,
    '-o', out, '.',
  ], { env: { GOOS: goos, GOARCH: goarch, CGO_ENABLED: cgo, ...env } })
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

console.log(`building KnightLoader ${version}, ${dlcFlags ? 'with' : 'without'} the DLC key`)

rmSync(bin, { recursive: true, force: true })
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
    const nsis = join(desktop, 'build', 'windows', 'nsis')
    run('wails3', ['generate', 'webview2bootstrapper', '-dir', nsis])
    run('makensis', [
      ...(updatetest ? ['-DINFO_PRODUCTNAME=KnightLoader Test', '-DINFO_PROJECTNAME=KnightLoaderTest'] : []),
      `-DARG_WAILS_${arch.toUpperCase()}_BINARY=${exe}`,
      join(nsis, 'project.nsi'),
    ])
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
  // An ad-hoc signature over the whole bundle, as Wails' own template does:
  // Apple Silicon refuses to start code that carries none.
  run('codesign', ['--force', '--deep', '--sign', '-', join(bin, 'KnightLoader.app')])
} else {
  // GTK 3 and WebKitGTK 4.1, which every current distribution ships and which
  // the Wails 2 builds already needed. Wails does not cross-compile to Linux,
  // so the ARM64 build runs on an ARM64 runner.
  goBuild(join(bin, 'KnightLoader'), { goos: 'linux', goarch: arch, cgo: '1', tags: tags(',gtk3') })
}

console.log(`done: ${version}`)
