package par2

import (
	"crypto/subtle"
	"encoding/binary"
)

// The PAR 2.0 field is GF(2^16) over x^16 + x^12 + x^3 + x + 1, with 2 as
// the generator of its multiplicative group.
const (
	fieldSize = 1 << 16
	// fieldMax is the order of the multiplicative group, 3 * 5 * 17 * 257.
	fieldMax = fieldSize - 1
	poly     = 0x1100B
)

var (
	gfLog [fieldSize]uint16
	// gfExp holds two periods, so a product's exponent needs no modulo.
	gfExp [2 * fieldMax]uint16
)

func init() {
	x := uint32(1)
	for i := range fieldMax {
		gfExp[i] = uint16(x)
		gfExp[i+fieldMax] = uint16(x)
		gfLog[x] = uint16(i)
		x <<= 1
		if x&fieldSize != 0 {
			x ^= poly
		}
	}
}

func gfMul(a, b uint16) uint16 {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

// gfDiv divides a by b, which is not 0.
func gfDiv(a, b uint16) uint16 {
	if a == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+fieldMax-int(gfLog[b])]
}

func gfPow(a uint16, e uint32) uint16 {
	switch {
	case e == 0:
		return 1
	case a == 0:
		return 0
	}
	return gfExp[uint64(gfLog[a])*uint64(e)%fieldMax]
}

// maxSlices is how many input slices a set can have: one per exponent
// coprime to fieldMax.
const maxSlices = 32768

// inputConstants returns the constants of the first n input slices: 2 raised
// to each exponent in turn that shares no factor with fieldMax, as the
// specification assigns them.
func inputConstants(n int) []uint16 {
	out := make([]uint16, n)
	e := 0
	for i := range out {
		for e%3 == 0 || e%5 == 0 || e%17 == 0 || e%257 == 0 {
			e++
		}
		out[i] = gfExp[e]
		e++
	}
	return out
}

// mulTable multiplies 16-bit words by one constant, a byte of the word at a
// time.
type mulTable struct {
	c      uint16
	lo, hi [256]uint16
}

func (t *mulTable) set(c uint16) {
	if t.c == c && c != 0 {
		return
	}
	t.c = c
	for i := range 256 {
		t.lo[i] = gfMul(c, uint16(i))
		t.hi[i] = gfMul(c, uint16(i)<<8)
	}
}

// mulAdd adds the table's constant times src to dst, both read as
// little-endian 16-bit words. dst is at least as long as src, whose length is
// even.
func (t *mulTable) mulAdd(dst, src []byte) {
	switch t.c {
	case 0:
		return
	case 1:
		subtle.XORBytes(dst, dst[:len(src)], src)
		return
	}
	i := 0
	for ; i+8 <= len(src); i += 8 {
		s := binary.LittleEndian.Uint64(src[i:])
		r := uint64(t.lo[byte(s)]^t.hi[byte(s>>8)]) |
			uint64(t.lo[byte(s>>16)]^t.hi[byte(s>>24)])<<16 |
			uint64(t.lo[byte(s>>32)]^t.hi[byte(s>>40)])<<32 |
			uint64(t.lo[byte(s>>48)]^t.hi[byte(s>>56)])<<48
		binary.LittleEndian.PutUint64(dst[i:], binary.LittleEndian.Uint64(dst[i:])^r)
	}
	for ; i+2 <= len(src); i += 2 {
		v := t.lo[src[i]] ^ t.hi[src[i+1]]
		dst[i] ^= byte(v)
		dst[i+1] ^= byte(v >> 8)
	}
}

// invert returns the inverse of the square matrix m, or false when m is
// singular. m is left as it was.
func invert(m [][]uint16) ([][]uint16, bool) {
	n := len(m)
	a := make([][]uint16, n)
	inv := make([][]uint16, n)
	for i := range n {
		a[i] = append([]uint16(nil), m[i]...)
		inv[i] = make([]uint16, n)
		inv[i][i] = 1
	}
	for col := range n {
		p := col
		for p < n && a[p][col] == 0 {
			p++
		}
		if p == n {
			return nil, false
		}
		a[col], a[p] = a[p], a[col]
		inv[col], inv[p] = inv[p], inv[col]
		if d := a[col][col]; d != 1 {
			for j := range n {
				a[col][j] = gfDiv(a[col][j], d)
				inv[col][j] = gfDiv(inv[col][j], d)
			}
		}
		for r := range n {
			f := a[r][col]
			if r == col || f == 0 {
				continue
			}
			for j := range n {
				a[r][j] ^= gfMul(f, a[col][j])
				inv[r][j] ^= gfMul(f, inv[col][j])
			}
		}
	}
	return inv, true
}
