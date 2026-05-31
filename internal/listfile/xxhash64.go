package listfile

import (
	"encoding/binary"
	"math/bits"
)

const (
	prime64x1 uint64 = 11400714785074694791
	prime64x2 uint64 = 14029467366897019727
	prime64x3 uint64 = 1609587929392839161
	prime64x4 uint64 = 9650029242287828579
	prime64x5 uint64 = 2870177450012600261
)

// XXHash64String matches the legacy implementation's seed-0 xxHash64 implementation.
func XXHash64String(value string) uint64 {
	return XXHash64([]byte(value))
}

func XXHash64(input []byte) uint64 {
	var h uint64
	p := 0
	length := len(input)

	if length >= 32 {
		v1 := prime64x1
		v1 += prime64x2
		v2 := prime64x2
		v3 := uint64(0)
		v4 := uint64(0)
		v4 -= prime64x1
		limit := length - 32
		for p <= limit {
			v1 = round(v1, binary.LittleEndian.Uint64(input[p:p+8]))
			p += 8
			v2 = round(v2, binary.LittleEndian.Uint64(input[p:p+8]))
			p += 8
			v3 = round(v3, binary.LittleEndian.Uint64(input[p:p+8]))
			p += 8
			v4 = round(v4, binary.LittleEndian.Uint64(input[p:p+8]))
			p += 8
		}
		h = bits.RotateLeft64(v1, 1) + bits.RotateLeft64(v2, 7) + bits.RotateLeft64(v3, 12) + bits.RotateLeft64(v4, 18)
		h = mergeRound(h, v1)
		h = mergeRound(h, v2)
		h = mergeRound(h, v3)
		h = mergeRound(h, v4)
	} else {
		h = prime64x5
	}

	h += uint64(length)
	for p <= length-8 {
		k1 := round(0, binary.LittleEndian.Uint64(input[p:p+8]))
		h ^= k1
		h = bits.RotateLeft64(h, 27)*prime64x1 + prime64x4
		p += 8
	}
	if p+4 <= length {
		h ^= uint64(binary.LittleEndian.Uint32(input[p:p+4])) * prime64x1
		h = bits.RotateLeft64(h, 23)*prime64x2 + prime64x3
		p += 4
	}
	for p < length {
		h ^= uint64(input[p]) * prime64x5
		h = bits.RotateLeft64(h, 11) * prime64x1
		p++
	}

	h ^= h >> 33
	h *= prime64x2
	h ^= h >> 29
	h *= prime64x3
	h ^= h >> 32
	return h
}

func round(acc, input uint64) uint64 {
	acc += input * prime64x2
	acc = bits.RotateLeft64(acc, 31)
	acc *= prime64x1
	return acc
}

func mergeRound(acc, value uint64) uint64 {
	value = round(0, value)
	acc ^= value
	acc = acc*prime64x1 + prime64x4
	return acc
}
