package par2

import "testing"

func BenchmarkMulAdd(b *testing.B) {
	src := make([]byte, 1<<20)
	dst := make([]byte, 1<<20)
	for i := range src {
		src[i] = byte(i * 7)
	}
	var t mulTable
	t.set(0x1234)
	b.SetBytes(int64(len(src)))
	for b.Loop() {
		t.mulAdd(dst, src)
	}
}
