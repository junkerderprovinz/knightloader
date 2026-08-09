package extract

// This file is the format layer: which reader opens a file, and what an
// encrypted zip entry has to prove before any of its bytes reach the disk. The
// seam ends at an io.ReadCloser over correct plaintext. Job scheduling,
// progress, retries and cleaning up after a failed run belong to the worker
// layer above it and are deliberately absent here.
//
// Encrypted zip is implemented rather than imported. The two forks that offer
// it (alexmullins/zip, last touched 2018; yeka/zip, last touched 2023, neither
// ever tagged a release) are hard copies of a pre-Go-1.20 archive/zip, so
// adopting one means running a frozen central-directory parser on untrusted
// input and losing every stdlib fix since - including CVE-2024-24789, where two
// implementations read different entries out of the same file. They also lose
// the prepended-data handling Go 1.21 added, which is what lets a
// self-extracting zip open at all. What is actually needed is small: the WinZip
// AE spec and PKWARE's original cipher on top of the stdlib reader, which keeps
// getting fixed.

import (
	"archive/zip"
	"compress/flate"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/subtle"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"strings"
)

// archiveFormat is what a file actually is, as opposed to what it is called.
type archiveFormat int

const (
	formatUnknown archiveFormat = iota
	formatZip
	formatRar
	formatSevenZip
	formatTar
	formatGzip
	formatBzip2
	formatXz
	formatZstd
	// The three below are recognised for one purpose only: so they can be
	// refused by name instead of failing as if the download were broken.
	formatArj
	formatLzh
	formatAce
)

// probeSize is how much of a file the magic probe reads. 512 covers every
// signature below; the furthest of them is the "ustar" a tar carries at 257.
const probeSize = 512

// magics are the signatures that identify a format outright. The offset-0
// entries are listed first so that a compressed stream is never taken for a tar
// because its payload happened to hold "ustar" at exactly the wrong place.
var magics = []struct {
	off    int
	sig    string
	format archiveFormat
}{
	{0, "PK\x03\x04", formatZip},
	{0, "PK\x05\x06", formatZip},   // an archive with no entries at all
	{0, "PK\x07\x08", formatZip},   // the spanned-archive marker
	{0, "Rar!\x1a\x07", formatRar}, // RAR4 continues \x00, RAR5 \x01\x00
	{0, "7z\xbc\xaf\x27\x1c", formatSevenZip},
	{0, "\x1f\x8b", formatGzip},
	{0, "\xfd7zXZ\x00", formatXz},
	{0, "\x28\xb5\x2f\xfd", formatZstd},
	{7, "**ACE**", formatAce},
	{257, "ustar", formatTar},
}

// sniff reports what the bytes at the head of path say the file is, which is a
// stronger witness than its name. Hosters rename to dodge filters and users
// rename to dodge hosters, so a .rar arriving as a .zip is an ordinary event,
// not an attack - and opening it with the reader the name asked for produces
// "not a valid archive" for a file that is perfectly fine.
func sniff(path string) archiveFormat {
	f, err := os.Open(path)
	if err != nil {
		return formatUnknown
	}
	defer f.Close()
	buf := make([]byte, probeSize)
	// A short read is not an error here. Every matcher length-checks, and a
	// half-downloaded archive is something the reader below reports on far
	// better than the probe could.
	n, _ := io.ReadFull(f, buf)
	return detect(buf[:n])
}

func detect(head []byte) archiveFormat {
	for _, m := range magics {
		if hasAt(head, m.off, m.sig) {
			return m.format
		}
	}
	// The three below carry no signature long enough to stand on its own, so
	// each pays for itself with a structural check.
	switch {
	case isBzip2(head):
		return formatBzip2
	case isLzh(head):
		return formatLzh
	case isArj(head):
		return formatArj
	}
	return formatUnknown
}

func hasAt(head []byte, off int, sig string) bool {
	return len(head) >= off+len(sig) && string(head[off:off+len(sig)]) == sig
}

// isBzip2 wants the block-size digit as well as "BZh": the three letters alone
// match too much ordinary text.
func isBzip2(head []byte) bool {
	return len(head) >= 4 && string(head[:3]) == "BZh" && head[3] >= '1' && head[3] <= '9'
}

// isLzh looks for the five-character method id an LHA header carries at offset
// 2, "-lh5-" and its relatives. There is no signature at offset 0 to check: the
// first two bytes are a size and a checksum, which can be anything.
func isLzh(head []byte) bool {
	return len(head) >= 7 && head[2] == '-' && head[3] == 'l' &&
		(head[4] == 'h' || head[4] == 'z') && head[6] == '-'
}

// isArj needs more than its two-byte magic, which on its own would fire on any
// file at all. The rest of the basic header has to be plausible too: a size in
// range, a first-header size large enough for the fixed fields, and a host OS
// from the twelve the format ever defined.
func isArj(head []byte) bool {
	if len(head) < 12 || head[0] != 0x60 || head[1] != 0xea {
		return false
	}
	size := int(head[2]) | int(head[3])<<8
	return size >= 30 && size <= 2600 && head[4] >= 30 && head[7] <= 11
}

// formatFromName is the fallback for content that carries no signature: a
// truncated download, an old v7 tar with no "ustar" marker, a split 7z volume.
// The name is the weaker witness, which is why it is consulted second.
func formatFromName(lower string) archiveFormat {
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return formatZip
	case strings.HasSuffix(lower, ".rar"):
		return formatRar
	case strings.HasSuffix(lower, ".7z") || sevenZipVolume.MatchString(lower):
		return formatSevenZip
	case strings.HasSuffix(lower, ".tar"):
		return formatTar
	case strings.HasSuffix(lower, ".arj"):
		return formatArj
	case strings.HasSuffix(lower, ".lzh"), strings.HasSuffix(lower, ".lha"):
		return formatLzh
	case strings.HasSuffix(lower, ".ace"):
		return formatAce
	}
	for _, c := range compressions {
		if strings.HasSuffix(lower, c.suffix) {
			return c.format
		}
	}
	return formatUnknown
}

// formatOf decides which reader opens path: content first, name second.
func formatOf(path string) archiveFormat {
	if f := sniff(path); f != formatUnknown {
		return f
	}
	return formatFromName(strings.ToLower(path))
}

// compressionFor returns the decompressor for a single-stream codec, or nil
// when format is not one of them.
func compressionFor(format archiveFormat) func(io.Reader) (io.ReadCloser, error) {
	for _, c := range compressions {
		if c.format == format {
			return c.open
		}
	}
	return nil
}

// namingSuffix is the spelling a single-stream payload gets named after: the
// first suffix in the table that the file actually carries and that belongs to
// the codec the probe found. It comes back empty when the name and the content
// disagree, which is exactly the case the probe exists to catch.
func namingSuffix(lower string, format archiveFormat) string {
	for _, c := range compressions {
		if c.format == format && strings.HasSuffix(lower, c.suffix) {
			return c.suffix
		}
	}
	return ""
}

// ErrFormatRetired says the archive is in a format KnightLoader will not read.
// It is a different thing from "unsupported archive", which means "I do not
// recognise this file": here the file was recognised perfectly well and turned
// down, and the user is owed the reason rather than a shrug.
var ErrFormatRetired = errors.New("extract: retired archive format")

// retiredReasons is the reason per format, in the words the user gets. Each has
// to survive being read by somebody who only wants their file, so it says what
// the format is, why there is no reader, and what to do instead.
var retiredReasons = map[archiveFormat]string{
	formatArj: "ARJ (.arj) has no pure-Go decoder, and KnightLoader is a single static binary with no C in it, " +
		"so the arj tool cannot be linked in. The format's last release was for MS-DOS in 1998. " +
		"7-Zip still reads .arj: unpack it once by hand and add the result instead.",
	formatLzh: "LHA/LZH (.lzh, .lha) has no maintained pure-Go decoder; it has been an essentially Japan-only " +
		"format since the 1990s and the reference implementation is unmaintained C. " +
		"7-Zip still reads it: unpack it once by hand and add the result instead.",
	formatAce: "ACE (.ace) is proprietary with no published specification, so no pure-Go decoder can exist. " +
		"The only decoder ever shipped is the closed-source unace library, which WinRAR removed in 5.70 " +
		"after CVE-2018-20250 let an .ace file write outside the extraction folder. That one is not coming back.",
}

// retiredError names the format and the reason, or returns nil for a format we
// do read.
func retiredError(f archiveFormat) error {
	reason, ok := retiredReasons[f]
	if !ok {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrFormatRetired, reason)
}

const (
	// flagEncrypted is general purpose bit 0: the entry's bytes are ciphertext.
	flagEncrypted = 0x1
	// flagDataDescriptor is bit 3: the sizes and CRC follow the data instead of
	// preceding it, which changes which byte ZipCrypto's header has to match.
	flagDataDescriptor = 0x8

	// methodWinZipAES is not a compression method. It is a marker meaning "the
	// real method is in the 0x9901 extra field", and it is the most dangerous
	// value in the format: a reader that treats an unknown method as stored
	// writes AES ciphertext to disk under the entry's name and reports success,
	// which is a corrupt file with a green tick. Everything below exists so
	// that cannot happen here.
	methodWinZipAES = 99
	extraWinZipAES  = 0x9901

	// Fixed by the WinZip AE specification.
	aesIterations = 1000
	pwvLen        = 2
	authLen       = 10
)

// aesInfo is the WinZip AES header carried in an entry's 0x9901 extra field.
type aesInfo struct {
	strength byte   // 1 = AES-128, 2 = AES-192, 3 = AES-256
	method   uint16 // the real compression method, the one Method 99 hides
	ae2      bool   // AE-2 writes a zero CRC field, so the CRC must not be checked
}

// parseAESExtra walks the extra-field records looking for 0x9901. The records
// are (id, size, data) triples; a size that runs past the end truncates the
// walk instead of being skipped, because at that point the whole field is
// untrustworthy and guessing at the rest of it is how a parser gets steered.
func parseAESExtra(extra []byte) *aesInfo {
	for len(extra) >= 4 {
		id := uint16(extra[0]) | uint16(extra[1])<<8
		size := int(extra[2]) | int(extra[3])<<8
		if len(extra) < 4+size {
			return nil
		}
		data := extra[4 : 4+size]
		if id == extraWinZipAES && size >= 7 && data[2] == 'A' && data[3] == 'E' {
			return &aesInfo{
				ae2:      uint16(data[0])|uint16(data[1])<<8 == 2,
				strength: data[4],
				method:   uint16(data[5]) | uint16(data[6])<<8,
			}
		}
		extra = extra[4+size:]
	}
	return nil
}

// aesHeader returns the WinZip AES header of f, nil if f is not an AES entry,
// or an error for every shape a permissive reader turns into silent corruption.
// Method 99 that cannot be fully resolved is refused rather than guessed at: a
// wrong file that exists is worse than an error, because nobody goes looking
// for it.
func aesHeader(f *zip.File) (*aesInfo, error) {
	info := parseAESExtra(f.Extra)
	if info == nil {
		if f.Method == methodWinZipAES {
			return nil, fmt.Errorf("extract: %s: declares WinZip AES (compression method 99) but carries no 0x9901 header, "+
				"so there is no way to know what its bytes are; refusing rather than writing ciphertext to disk", f.Name)
		}
		return nil, nil
	}
	switch {
	case f.Method != methodWinZipAES:
		return nil, fmt.Errorf("extract: %s: carries a WinZip AES header but declares compression method %d instead of 99; "+
			"the entry contradicts itself", f.Name, f.Method)
	case f.Flags&flagEncrypted == 0:
		return nil, fmt.Errorf("extract: %s: declares WinZip AES but is not flagged encrypted; "+
			"a reader that trusts the flag would store the ciphertext as the file", f.Name)
	case info.strength < 1 || info.strength > 3:
		return nil, fmt.Errorf("extract: %s: WinZip AES strength %d is not one of the three the format defines", f.Name, info.strength)
	case info.method == methodWinZipAES:
		return nil, fmt.Errorf("extract: %s: the WinZip AES header names method 99 as the real method, which is circular", f.Name)
	}
	return info, nil
}

// openZipEntry hands back the decrypted, decompressed, checksum-checked
// contents of one zip entry. Every entry goes through here, so there is no path
// that writes bytes which were not both decrypted and checked.
func openZipEntry(f *zip.File, password string) (io.ReadCloser, error) {
	info, err := aesHeader(f)
	if err != nil {
		return nil, err
	}
	switch {
	case info != nil:
		return openAESEntry(f, info, password)
	case f.Flags&flagEncrypted != 0:
		return openZipCryptoEntry(f, password)
	default:
		// Plain entry: the stdlib reader decompresses and checks the CRC
		// itself, and it is the one that keeps receiving the format's security
		// fixes. Nothing here improves on it.
		return f.Open()
	}
}

// aesKeyLen turns the strength byte into a key length. Callers validate the
// strength through aesHeader first.
func aesKeyLen(strength byte) int { return 8 * (int(strength) + 1) }

// aesLayout is where the parts of an encrypted entry sit inside its raw bytes:
// salt, password verification value, ciphertext, authentication code.
type aesLayout struct {
	keyLen  int
	saltLen int
	dataOff int64
	dataLen int64
}

func aesLayoutOf(f *zip.File, info *aesInfo) (aesLayout, error) {
	keyLen := aesKeyLen(info.strength)
	saltLen := keyLen / 2
	overhead := int64(saltLen + pwvLen + authLen)
	if int64(f.CompressedSize64) < overhead {
		// Subtracting the overhead in unsigned arithmetic is how the fork
		// libraries turn a two-byte entry into a multi-gigabyte read: the
		// length underflows and the reader walks off the end of the archive.
		return aesLayout{}, fmt.Errorf("extract: %s: encrypted entry is %d bytes, too short to hold even its own salt "+
			"and authentication code", f.Name, f.CompressedSize64)
	}
	return aesLayout{
		keyLen:  keyLen,
		saltLen: saltLen,
		dataOff: int64(saltLen + pwvLen),
		dataLen: int64(f.CompressedSize64) - overhead,
	}, nil
}

// aesUnlock reads the salt and the password verification value from the front
// of an encrypted entry and derives its keys. A wrong password is rejected here
// on two bytes, before any ciphertext is touched. The reader comes back
// positioned at the start of the ciphertext.
func aesUnlock(f *zip.File, password string, lay aesLayout) (raw io.Reader, encKey, authKey []byte, err error) {
	if password == "" {
		return nil, nil, nil, ErrPasswordRequired
	}
	raw, err = f.OpenRaw()
	if err != nil {
		return nil, nil, nil, err
	}
	head := make([]byte, lay.saltLen+pwvLen)
	if _, err := io.ReadFull(raw, head); err != nil {
		return nil, nil, nil, fmt.Errorf("extract: %s: encrypted entry has no salt: %w", f.Name, err)
	}
	key, err := pbkdf2.Key(sha1.New, password, head[:lay.saltLen], aesIterations, 2*lay.keyLen+pwvLen)
	if err != nil {
		return nil, nil, nil, err
	}
	if subtle.ConstantTimeCompare(head[lay.saltLen:], key[2*lay.keyLen:]) != 1 {
		return nil, nil, nil, ErrPasswordRequired
	}
	return raw, key[:lay.keyLen], key[lay.keyLen : 2*lay.keyLen], nil
}

// openAESEntry decrypts a WinZip AES entry, having first proven it intact.
func openAESEntry(f *zip.File, info *aesInfo, password string) (io.ReadCloser, error) {
	lay, err := aesLayoutOf(f, info)
	if err != nil {
		return nil, err
	}
	raw, encKey, authKey, err := aesUnlock(f, password, lay)
	if err != nil {
		return nil, err
	}

	// Pass one: authenticate. WinZip AES is encrypt-then-MAC over the
	// ciphertext, so the entry can be proven whole without decrypting a byte of
	// it. The two alternatives are both worse. Streaming and checking at the
	// end means the plaintext is already on disk by the time the entry turns
	// out to be truncated or altered. Buffering the plaintext instead, which is
	// what the fork libraries do, makes every encrypted entry an
	// out-of-memory switch: a 4 GB entry wants 4 GB of RAM. Reading a file that
	// is already on local disk a second time is the cheapest of the three.
	mac := hmac.New(sha1.New, authKey)
	if _, err := io.CopyN(mac, raw, lay.dataLen); err != nil {
		return nil, fmt.Errorf("extract: %s: encrypted entry ends early: %w", f.Name, err)
	}
	want := make([]byte, authLen)
	if _, err := io.ReadFull(raw, want); err != nil {
		return nil, fmt.Errorf("extract: %s: authentication code is missing: %w", f.Name, err)
	}
	if !hmac.Equal(mac.Sum(nil)[:authLen], want) {
		return nil, fmt.Errorf("extract: %s: authentication failed, so the encrypted entry has been altered or is damaged", f.Name)
	}

	// Pass two: decrypt what is now known to be intact.
	body, err := f.OpenRaw()
	if err != nil {
		return nil, err
	}
	if _, err := io.CopyN(io.Discard, body, lay.dataOff); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	plain := cipher.StreamReader{S: newWinZipCTR(block), R: io.LimitReader(body, lay.dataLen)}
	// AE-2 zeroes the CRC field deliberately, so there is nothing to compare
	// against; the authentication above is what it traded the CRC for. On AE-1
	// the CRC is real, and a mismatch after a passing HMAC means the archive
	// disagrees with itself rather than that the password was wrong.
	fail := fmt.Errorf("extract: %s: decrypted contents fail their CRC32 even though the archive's own authentication "+
		"passed, so the archive is internally inconsistent", f.Name)
	return decompressed(f, info.method, plain, !info.ae2, fail)
}

// winZipCTR is AES in counter mode with WinZip's counter, a little-endian
// 128-bit integer starting at 1. The stdlib's cipher.NewCTR counts big-endian
// per NIST SP 800-38A and cannot stand in for this: it produces a plausible
// first block and garbage from the second one on, which reads as "the archive
// is corrupt after the first 16 bytes".
type winZipCTR struct {
	block cipher.Block
	ctr   [aes.BlockSize]byte
	ks    [aes.BlockSize]byte
	used  int
}

func newWinZipCTR(block cipher.Block) cipher.Stream {
	// used == len(ks) means "no keystream yet", so the first refill increments
	// the zero counter to 1, which is where WinZip starts.
	return &winZipCTR{block: block, used: aes.BlockSize}
}

func (c *winZipCTR) refill() {
	for i := range c.ctr {
		c.ctr[i]++
		if c.ctr[i] != 0 {
			break
		}
	}
	c.block.Encrypt(c.ks[:], c.ctr[:])
	c.used = 0
}

func (c *winZipCTR) XORKeyStream(dst, src []byte) {
	for len(src) > 0 {
		if c.used == len(c.ks) {
			c.refill()
		}
		n := subtle.XORBytes(dst, src, c.ks[c.used:])
		dst, src = dst[n:], src[n:]
		c.used += n
	}
}

// zipCrypto is PKWARE's original stream cipher, the one every tool still calls
// ZipCrypto. It is broken - a known-plaintext attack recovers the keys from a
// dozen known bytes - but it is what "legacy encryption" in WinRAR and 7-Zip
// still produces and what most encrypted zips in circulation use, so reading it
// is not optional. Nothing here ever writes one.
type zipCrypto struct{ k0, k1, k2 uint32 }

func newZipCrypto(password string) zipCrypto {
	z := zipCrypto{0x12345678, 0x23456789, 0x34567890}
	for i := 0; i < len(password); i++ {
		z.update(password[i])
	}
	return z
}

// update folds one plaintext byte into the key schedule. The table is the same
// IEEE polynomial the entry checksum uses, which is an accident of the format's
// history rather than a relationship worth relying on.
func (z *zipCrypto) update(c byte) {
	z.k0 = z.k0>>8 ^ crc32.IEEETable[byte(z.k0)^c]
	z.k1 = (z.k1+z.k0&0xff)*134775813 + 1
	z.k2 = z.k2>>8 ^ crc32.IEEETable[byte(z.k2)^byte(z.k1>>24)]
}

// next is the keystream byte for the next plaintext byte. The arithmetic is
// 16-bit and wraps, and that is load-bearing: widening it changes the output.
func (z *zipCrypto) next() byte {
	t := uint16(z.k2) | 2
	return byte((t * (t ^ 1)) >> 8)
}

func (z *zipCrypto) decrypt(p []byte) {
	for i, c := range p {
		c ^= z.next()
		z.update(c)
		p[i] = c
	}
}

// zipCryptoReader decrypts in place as it reads. The cipher's state advances
// per byte and depends on the plaintext, so every byte has to pass through here
// exactly once and in order, which is why this reader cannot be rewound or
// shared.
type zipCryptoReader struct {
	r io.Reader
	z zipCrypto
}

func (d *zipCryptoReader) Read(p []byte) (int, error) {
	n, err := d.r.Read(p)
	d.z.decrypt(p[:n])
	return n, err
}

// zipCryptoHeaderLen is the encryption header ZipCrypto puts in front of every
// entry: eleven random bytes and one check byte.
const zipCryptoHeaderLen = 12

// zipCryptoCheck is the byte the decrypted header has to end with. With a data
// descriptor (bit 3) the CRC was not yet known when the entry was written, so
// the writer put the high byte of the DOS timestamp there instead. That reads
// the raw DOS field rather than f.Modified on purpose: the NTFS and Unix
// extended-timestamp extra fields overwrite f.Modified with a different clock,
// and the check byte was computed from the DOS one.
func zipCryptoCheck(f *zip.File) byte {
	if f.Flags&flagDataDescriptor != 0 {
		return byte(f.ModifiedTime >> 8)
	}
	return byte(f.CRC32 >> 24)
}

// zipCryptoUnlock consumes the 12-byte header and reports whether the password
// reproduced the check byte. One byte is a weak test - one wrong password in
// 256 passes it - which is why the CRC of the decompressed contents is checked
// as well, and why the pre-flight tries several entries.
func zipCryptoUnlock(f *zip.File, password string) (io.Reader, error) {
	if password == "" {
		return nil, ErrPasswordRequired
	}
	if f.CompressedSize64 < zipCryptoHeaderLen {
		return nil, fmt.Errorf("extract: %s: encrypted entry is %d bytes, too short to hold its own header", f.Name, f.CompressedSize64)
	}
	raw, err := f.OpenRaw()
	if err != nil {
		return nil, err
	}
	r := &zipCryptoReader{r: raw, z: newZipCrypto(password)}
	var head [zipCryptoHeaderLen]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, fmt.Errorf("extract: %s: encrypted entry has no header: %w", f.Name, err)
	}
	if head[zipCryptoHeaderLen-1] != zipCryptoCheck(f) {
		return nil, ErrPasswordRequired
	}
	return r, nil
}

func openZipCryptoEntry(f *zip.File, password string) (io.ReadCloser, error) {
	r, err := zipCryptoUnlock(f, password)
	if err != nil {
		return nil, err
	}
	// A CRC mismatch on a ZipCrypto entry is nearly always the one-in-256 wrong
	// password that slipped past the header byte, so it is reported as a
	// password failure. That is what lets ExtractWith move on to the next
	// password in the list instead of stopping at "corrupt archive" while the
	// right password sits two entries further down.
	fail := fmt.Errorf("extract: %s: contents fail their checksum, which on a ZipCrypto entry almost always means "+
		"the password is wrong: %w", f.Name, ErrPasswordRequired)
	return decompressed(f, f.Method, r, true, fail)
}

// decompressed wraps a decrypted stream in the entry's real compression method
// and, where the format leaves a checksum worth checking, in a CRC32 check.
// The caller supplies the mismatch error, because what a bad checksum means
// depends on how the entry was protected.
func decompressed(f *zip.File, method uint16, r io.Reader, checkCRC bool, fail error) (io.ReadCloser, error) {
	var rc io.ReadCloser
	switch method {
	case zip.Store:
		rc = io.NopCloser(r)
	case zip.Deflate:
		rc = flate.NewReader(r)
	default:
		// Store and Deflate are also the only two the stdlib reader registers,
		// so stopping here keeps encrypted entries from quietly supporting more
		// of the format than plain ones do.
		return nil, fmt.Errorf("extract: %s: compression method %d inside an encrypted entry is not one this reader implements", f.Name, method)
	}
	if !checkCRC {
		return rc, nil
	}
	return &crcReader{r: rc, sum: crc32.NewIEEE(), want: f.CRC32, fail: fail}, nil
}

// crcReader passes bytes through and refuses to let the last Read succeed when
// the CRC32 does not match. The check has to live here rather than after the
// copy: the caller writes straight from this reader into the file, so the read
// that hits EOF is the last moment a wrong payload can be stopped from being
// reported as a success.
type crcReader struct {
	r    io.ReadCloser
	sum  hash.Hash32
	want uint32
	fail error
}

func (c *crcReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.sum.Write(p[:n])
	if err == io.EOF && c.sum.Sum32() != c.want {
		return n, c.fail
	}
	return n, err
}

func (c *crcReader) Close() error { return c.r.Close() }

// passwordProbeEntries caps how many entries the pre-flight tests. Testing more
// than one is for ZipCrypto, whose check is a single byte: eight entries take a
// wrong password from one-in-256 to one-in-2^64. The ninth buys nothing, and a
// thousand-entry archive would otherwise pay a PBKDF2 pass for every entry.
const passwordProbeEntries = 8

// verifyZipPassword decides whether password opens this archive before a single
// byte is written, and refuses outright the entries that must never be written
// at all.
//
// Two passes over the entries, because they cost different things. The
// structural refusal is free and has to see every entry: a poisoned method 99
// could be the five-hundredth. The password check derives a PBKDF2 key per
// entry, so it stops after a handful.
//
// An archive whose entries carry different passwords fails here even though a
// per-entry reader could unpack part of it. That shape is vanishingly rare, and
// the alternative - finding out entry by entry - is what leaves a directory of
// half-written files behind every wrong password in the list.
func verifyZipPassword(zr *zip.Reader, password string) error {
	type candidate struct {
		f    *zip.File
		info *aesInfo
	}
	var encrypted []candidate
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			// A directory entry has no contents to unlock, and some writers
			// still stamp the encrypted flag on it. Letting one into the
			// pre-flight fails the whole archive on an entry that extractZip
			// skips anyway.
			continue
		}
		info, err := aesHeader(f)
		if err != nil {
			return err
		}
		if info != nil || f.Flags&flagEncrypted != 0 {
			encrypted = append(encrypted, candidate{f, info})
		}
	}
	if len(encrypted) == 0 {
		return nil
	}
	if password == "" {
		return ErrPasswordRequired
	}
	for _, c := range encrypted[:min(len(encrypted), passwordProbeEntries)] {
		var err error
		if c.info != nil {
			var lay aesLayout
			if lay, err = aesLayoutOf(c.f, c.info); err == nil {
				_, _, _, err = aesUnlock(c.f, password, lay)
			}
		} else {
			_, err = zipCryptoUnlock(c.f, password)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
