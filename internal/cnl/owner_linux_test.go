package cnl

import (
	"slices"
	"testing"
)

func TestListenInodesReadsTheListeningSocketsOnThePort(t *testing.T) {
	table := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:25C2 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 41234 1 0000000000000000 100 0 0 10 0
   1: 0100007F:25C2 0100007F:D431 01 00000000:00000000 00:00000000 00000000  1000        0 41299 1 0000000000000000 20 4 30 10 -1
   2: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 18001 1 0000000000000000 100 0 0 10 0
`
	if got := listenInodes(table, 9666); !slices.Equal(got, []string{"41234"}) {
		t.Errorf("inodes = %v, want the listening socket on 9666 only", got)
	}
}
