package browsercookies

import (
	"errors"
	"os"
	"slices"
	"unsafe"

	"golang.org/x/sys/windows"
)

// dpapiUnprotect opens a blob DPAPI sealed to this user.
func dpapiUnprotect(sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, errors.New("nothing to unprotect")
	}
	in := windows.DataBlob{Size: uint32(len(sealed)), Data: &sealed[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return slices.Clone(unsafe.Slice(out.Data, out.Size)), nil
}

// openShared opens a file for reading while letting its owner go on reading,
// writing and deleting it, which a running browser insists on.
func openShared(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// lockedOut reports a file another process opened without sharing it, or
// whose bytes it has locked.
func lockedOut(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
