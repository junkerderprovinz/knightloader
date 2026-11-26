// Separate module: the Wails desktop wrapper. Kept out of the server module so
// the Wails toolchain never affects the container/server build. Built in CI.
module github.com/junkerderprovinz/knightloader/desktop

// Must never be older than the root module's go directive. The replace at the
// bottom pulls the server sources in from ../, and a module cannot consume a
// dependency asking for a newer language version than it declares - with CI's
// GOTOOLCHAIN=local that reads "module ../ requires go >= 1.26.6" and the job
// stops at vet. Bump this whenever ../go.mod moves.
go 1.27.3

toolchain go1.27.4

require (
	github.com/coder/websocket v1.8.15
	// godbus is how Wails talks StatusNotifierItem on Linux and BSD.
	// tray_probe_linux.go calls it too, for the availability probe, and
	// accent_linux.go for the settings portal.
	github.com/godbus/dbus/v5 v5.2.4
	github.com/junkerderprovinz/knightloader v0.0.0
	github.com/junkerderprovinz/parleyport v1.3.8
	github.com/wailsapp/wails/v3 v3.1.0
	github.com/zalando/go-keyring v0.2.8
	golang.org/x/sys v0.50.0
)

require (
	dario.cat/mergo v1.0.2 // indirect
	github.com/GopeedLab/gopeed v1.9.3 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/ProtonMail/go-crypto v1.5.2 // indirect
	github.com/STARRY-S/zip v0.2.3 // indirect
	github.com/andybalholm/brotli v1.2.8 // indirect
	github.com/bodgit/plumbing v1.3.0 // indirect
	github.com/bodgit/sevenzip v1.6.5 // indirect
	github.com/bodgit/windows v1.0.1 // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/coreos/go-oidc/v3 v3.21.0 // indirect
	github.com/cyphar/filepath-securejoin v0.6.1 // indirect
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/dlclark/regexp2/v2 v2.11.0 // indirect
	github.com/dop251/goja v0.0.0-20261122185237-35fdc4b88e39 // indirect
	github.com/dop251/goja_nodejs v0.0.0-20240728170619-29b559befffc // indirect
	github.com/dsnet/compress v0.0.2-0.20230904184137-39efe44ab707 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/fxamacker/cbor/v2 v2.9.4 // indirect
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-git/go-billy/v5 v5.9.0 // indirect
	github.com/go-git/go-git/v5 v5.19.2 // indirect
	github.com/go-jose/go-jose/v4 v4.1.9 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/webauthn v0.18.2 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/go-querystring v1.2.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/pprof v0.0.0-20261016150608-1facf8f91b50 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/icholy/digest v1.2.0 // indirect
	github.com/imroc/req/v3 v3.61.0 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/jlaffaye/ftp v0.2.4 // indirect
	github.com/kevinburke/ssh_config v1.4.0 // indirect
	github.com/klauspost/compress v1.20.5 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/klauspost/pgzip v1.2.6 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/matoous/go-nanoid/v2 v2.0.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-ieproxy v0.0.12 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mholt/archives v0.1.5 // indirect
	github.com/mikelolasagasti/xz v1.0.1 // indirect
	github.com/minio/minlz v1.0.1 // indirect
	github.com/modelcontextprotocol/go-sdk v1.8.0 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/nwaples/rardecode/v2 v2.4.6 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.27 // indirect
	github.com/pjbgf/sha1cd v0.6.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/sftp v1.13.12 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/quic-go/quic-go v0.61.0 // indirect
	github.com/refraction-networking/utls v1.8.2 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/sergi/go-diff v1.4.0 // indirect
	github.com/skeema/knownhosts v1.3.2 // indirect
	github.com/sorairolake/lzip-go v0.3.8 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/stangelandcl/ppmd v0.1.1 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/ulikunitz/xz v0.5.17 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xanzy/ssh-agent v0.3.3 // indirect
	github.com/xiaoqidun/setft v0.0.0-20220310121541-be86327699ad // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.etcd.io/bbolt v1.4.3 // indirect
	go.opentelemetry.io/otel/log v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go4.org v0.0.0-20260112195520-a5071408f32f // indirect
	golang.org/x/crypto v0.59.0 // indirect
	golang.org/x/exp v0.0.0-20260410095643-746e56fc9e2f // indirect
	golang.org/x/image v0.48.0 // indirect
	golang.org/x/mod v0.43.0 // indirect
	golang.org/x/net v0.63.0 // indirect
	golang.org/x/oauth2 v0.38.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/term v0.48.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	golang.zx2c4.com/wireguard v0.0.0-20261024220952-b3bc704ef0ee // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
	gvisor.dev/gvisor v0.0.0-20250503011706-39ed1f5ac29c // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.63.0 // indirect
	rsc.io/qr v0.2.0 // indirect
)

// Use the server sources from the parent checkout.
replace github.com/junkerderprovinz/knightloader => ../

// The same gopeed fork as the server module, since a dependency's replace
// does not reach this one.
replace github.com/GopeedLab/gopeed => github.com/junkerderprovinz/gopeed v0.0.0-20261114165838-d7e4c5d8e23c
