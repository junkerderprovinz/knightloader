package cnl

import (
	"context"
	"encoding/binary"
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetExtendedTcpTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

// tcpTableOwnerPIDListener asks GetExtendedTcpTable for listening sockets
// with the process that owns each.
const tcpTableOwnerPIDListener = 3

// The rows of MIB_TCPTABLE_OWNER_PID and MIB_TCP6TABLE_OWNER_PID: their size,
// where the local port sits and where the owning process id sits.
const (
	row4Size, row4Port, row4PID = 24, 8, 20
	row6Size, row6Port, row6PID = 56, 20, 52
)

func addrInUse(err error) bool {
	return errors.Is(err, windows.WSAEADDRINUSE)
}

// portOwner asks the system which process listens on port, over IPv4 and
// then IPv6.
func portOwner(_ context.Context, port int) (Process, bool) {
	for _, fam := range []struct {
		af              uint32
		size, at, pidAt int
	}{
		{windows.AF_INET, row4Size, row4Port, row4PID},
		{windows.AF_INET6, row6Size, row6Port, row6PID},
	} {
		table, err := listenerTable(fam.af)
		if err != nil {
			continue
		}
		if pid, ok := pidOnPort(table, port, fam.size, fam.at, fam.pidAt); ok {
			return describe(pid), true
		}
	}
	return Process{}, false
}

func listenerTable(af uint32) ([]byte, error) {
	var size uint32
	for range 3 {
		var buf []byte
		var p unsafe.Pointer
		if size > 0 {
			buf = make([]byte, size)
			p = unsafe.Pointer(&buf[0])
		}
		r, _, _ := procGetExtendedTcpTable.Call(uintptr(p), uintptr(unsafe.Pointer(&size)), 0, uintptr(af), tcpTableOwnerPIDListener, 0)
		switch windows.Errno(r) {
		case 0:
			if buf == nil {
				return nil, nil
			}
			return buf[:size], nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			// Sockets opened between the two calls make the table grow.
			continue
		default:
			return nil, windows.Errno(r)
		}
	}
	return nil, windows.ERROR_INSUFFICIENT_BUFFER
}

// pidOnPort finds port in a table GetExtendedTcpTable filled: a count, then
// rows of size bytes. The port is in network order in the low two bytes of
// its field.
func pidOnPort(table []byte, port, size, at, pidAt int) (int, bool) {
	if len(table) < 4 {
		return 0, false
	}
	n := int(binary.LittleEndian.Uint32(table))
	for i := range n {
		row := table[4+i*size:]
		if len(row) < size {
			break
		}
		if int(binary.BigEndian.Uint16(row[at:])) == port {
			return int(binary.LittleEndian.Uint32(row[pidAt:])), true
		}
	}
	return 0, false
}

// describe names process pid. A process this user may not look into, such
// as one of another user, still has a name in the process list.
func describe(pid int) Process {
	p := Process{PID: pid}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err == nil {
		defer windows.CloseHandle(h)
		buf := make([]uint16, windows.MAX_LONG_PATH)
		n := uint32(len(buf))
		if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil {
			p.Path = windows.UTF16ToString(buf[:n])
		}
		p.Cmdline = commandLine(h)
	}
	if p.Path == "" {
		p.Path = listedName(pid)
	}
	return p
}

func commandLine(h windows.Handle) string {
	size := uint32(4096)
	for range 2 {
		// uint64 keeps the UNICODE_STRING at the start aligned.
		buf := make([]uint64, (size+7)/8)
		err := windows.NtQueryInformationProcess(h, windows.ProcessCommandLineInformation, unsafe.Pointer(&buf[0]), uint32(len(buf)*8), &size)
		if err == nil {
			return (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0])).String()
		}
		if !errors.Is(err, windows.STATUS_INFO_LENGTH_MISMATCH) {
			return ""
		}
	}
	return ""
}

func listedName(pid int) string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if int(e.ProcessID) == pid {
			return windows.UTF16ToString(e.ExeFile[:])
		}
	}
	return ""
}
