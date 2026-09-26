package extract

// What went wrong with an archive, as values a caller can tell apart without
// reading a library's sentence. Each reader's error is kept, so errors.Is still
// answers for rardecode.ErrBadBlockHeader, and the problem is added beside it.

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/flate"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/nwaples/rardecode/v2"
)

var (
	// ErrDamaged is an archive whose bytes are not what its own headers and
	// checksums say: a bad block header, a CRC that does not match, a volume
	// that ends early.
	ErrDamaged = errors.New("the archive is damaged")
	// ErrPartMissing is a file of the set that is not where the archive looks
	// for it.
	ErrPartMissing = errors.New("a part of the archive is missing")
	// ErrUnsupported is an archive this build cannot read: a format it does not
	// know, or a method or version inside one it does. A retired format
	// (ErrFormatRetired) is reported as this problem too.
	ErrUnsupported = errors.New("extract: unsupported archive")
)

// PartError is a failure inside one file of an archive, named after that file:
// "bad block header" on its own does not say which of forty parts is broken.
type PartError struct {
	// Part is the file's base name.
	Part string
	// Problem is ErrDamaged, ErrPartMissing or ErrUnsupported, or nil when the
	// reader's error is not one of them.
	Problem error
	Err     error
}

// Error keeps one "extract: " in front, where this package's own errors carry
// one already.
func (e *PartError) Error() string {
	return "extract: " + e.Part + ": " + strings.TrimPrefix(e.Err.Error(), "extract: ")
}

func (e *PartError) Unwrap() []error {
	if e.Problem == nil {
		return []error{e.Err}
	}
	return []error{e.Problem, e.Err}
}

// PartOf is the file of the archive err happened in, or "" when err does not
// say.
func PartOf(err error) string {
	var pe *PartError
	if errors.As(err, &pe) {
		return pe.Part
	}
	return ""
}

// inPart ties a failure to the file of the archive it happened in. One that is
// already tied, a password, and one this file names no problem for, such as a
// write to a full disk, come back as they were: those are about the disk, not
// about the archive.
func inPart(err error, part string) error {
	var pe *PartError
	if err == nil || errors.As(err, &pe) || errors.Is(err, ErrPasswordRequired) || problemOf(err) == nil {
		return err
	}
	return partError(err, part)
}

// partError names the file a reader's failure happened in, with what the
// failure means for the archive.
func partError(err error, part string) *PartError {
	problem := problemOf(err)
	if problem == ErrPartMissing {
		// The missing file is the one to name, not the one that was open.
		var path *fs.PathError
		if errors.As(err, &path) {
			part = filepath.Base(path.Path)
		}
	}
	return &PartError{Part: part, Problem: problem, Err: err}
}

// problemOf recognises what a reader's error means for the archive.
func problemOf(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ErrPartMissing
	case isAny(err, unsupportedSigns):
		return ErrUnsupported
	case isAny(err, damageSigns) || isCorruptStream(err):
		return ErrDamaged
	}
	return nil
}

// unsupportedSigns are the errors of an archive that is fine but not one this
// build reads.
var unsupportedSigns = []error{
	ErrUnsupported, ErrFormatRetired, zip.ErrAlgorithm,
	rardecode.ErrUnknownDecoder, rardecode.ErrUnsupportedDecoder, rardecode.ErrUnknownVersion,
	rardecode.ErrMultipleDecoders, rardecode.ErrUnknownEncryptMethod, rardecode.ErrDictionaryTooLarge,
	rardecode.ErrPlatformIntSize,
}

// damageSigns are the errors of an archive whose bytes are wrong. What they
// have in common is that another reader, 7-Zip included, fails on the same
// file: the remedy is a new copy of it, not another program.
var damageSigns = []error{
	io.ErrUnexpectedEOF,
	zip.ErrFormat, zip.ErrChecksum,
	gzip.ErrHeader, gzip.ErrChecksum,
	tar.ErrHeader,
	rardecode.ErrCorruptBlockHeader, rardecode.ErrCorruptFileHeader, rardecode.ErrBadHeaderCRC,
	rardecode.ErrDecoderOutOfData, rardecode.ErrBadBlockHeader, rardecode.ErrNoSig,
	rardecode.ErrShortFile, rardecode.ErrInvalidFileBlock, rardecode.ErrUnexpectedArcEnd,
	rardecode.ErrBadFileChecksum, rardecode.ErrCorruptDecodeHeader, rardecode.ErrHuffDecodeFailed,
	rardecode.ErrInvalidLengthTable, rardecode.ErrCorruptPPM, rardecode.ErrInvalidFilter,
	rardecode.ErrTooManyFilters, rardecode.ErrInvalidVMInstruction, rardecode.ErrInvalidHeaderOff,
	rardecode.ErrBadVolumeNumber, rardecode.ErrNoArchiveBlock, rardecode.ErrVerMismatch,
	rardecode.ErrCorruptEncryptData, rardecode.ErrUnknownFilter,
}

func isAny(err error, signs []error) bool {
	for _, s := range signs {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

// isCorruptStream covers the decoders that report damage as a type rather than
// a value, and the 7z reader, whose errors are unexported.
func isCorruptStream(err error) bool {
	var inflate flate.CorruptInputError
	var bz bzip2.StructuralError
	if errors.As(err, &inflate) || errors.As(err, &bz) {
		return true
	}
	return strings.Contains(err.Error(), "sevenzip: not a valid 7-zip file")
}
