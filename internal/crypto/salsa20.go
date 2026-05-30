package crypto

import "fmt"

const (
	sigma16Word0 = 0x61707865
	sigma16Word1 = 0x3120646e
	sigma16Word2 = 0x79622d36
	sigma16Word3 = 0x6b206574

	sigma32Word0 = 0x61707865
	sigma32Word1 = 0x3320646e
	sigma32Word2 = 0x79622d32
	sigma32Word3 = 0x6b206574
)

type Salsa20 struct {
	rounds     int
	sigma      [4]uint32
	keyWords   [8]uint32
	nonceWords [2]uint32
	counter    [2]uint32
	block      [64]byte
	blockUsed  int
}

func NewSalsa20(nonce []byte, key []byte, rounds int) (*Salsa20, error) {
	if len(nonce) != 8 {
		return nil, fmt.Errorf("unexpected nonce length: 8 bytes expected, got %d", len(nonce))
	}
	if len(key) != 16 && len(key) != 32 {
		return nil, fmt.Errorf("unexpected key length: 16 or 32 bytes expected, got %d", len(key))
	}

	s := &Salsa20{
		rounds:    rounds,
		blockUsed: 64,
	}

	if len(key) == 16 {
		s.sigma = [4]uint32{sigma16Word0, sigma16Word1, sigma16Word2, sigma16Word3}
	} else {
		s.sigma = [4]uint32{sigma32Word0, sigma32Word1, sigma32Word2, sigma32Word3}
	}

	expandedKey := make([]byte, 32)
	copy(expandedKey, key)
	if len(key) == 16 {
		copy(expandedKey[16:], key)
	}

	for i, j := 0, 0; i < 8; i, j = i+1, j+4 {
		s.keyWords[i] = uint32(expandedKey[j]) |
			uint32(expandedKey[j+1])<<8 |
			uint32(expandedKey[j+2])<<16 |
			uint32(expandedKey[j+3])<<24
	}

	s.setNonce(nonce)
	return s, nil
}

func (s *Salsa20) setNonce(nonce []byte) {
	s.nonceWords[0] = uint32(nonce[0]) |
		uint32(nonce[1])<<8 |
		uint32(nonce[2])<<16 |
		uint32(nonce[3])<<24
	s.nonceWords[1] = uint32(nonce[4]) |
		uint32(nonce[5])<<8 |
		uint32(nonce[6])<<16 |
		uint32(nonce[7])<<24

	s.counter[0] = 0
	s.counter[1] = 0
	s.blockUsed = 64
}

func (s *Salsa20) GetBytes(n int) []byte {
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		if s.blockUsed == 64 {
			s.generateBlock()
			s.increment()
			s.blockUsed = 0
		}
		out[i] = s.block[s.blockUsed]
		s.blockUsed++
	}
	return out
}

func (s *Salsa20) Process(dst, src []byte) {
	keystream := s.GetBytes(len(src))
	for i := range src {
		dst[i] = keystream[i] ^ src[i]
	}
}

func (s *Salsa20) increment() {
	s.counter[0] = (s.counter[0] + 1) & 0xffffffff
	if s.counter[0] == 0 {
		s.counter[1] = (s.counter[1] + 1) & 0xffffffff
	}
}

func (s *Salsa20) generateBlock() {
	j0 := s.sigma[0]
	j1 := s.keyWords[0]
	j2 := s.keyWords[1]
	j3 := s.keyWords[2]
	j4 := s.keyWords[3]
	j5 := s.sigma[1]
	j6 := s.nonceWords[0]
	j7 := s.nonceWords[1]
	j8 := s.counter[0]
	j9 := s.counter[1]
	j10 := s.sigma[2]
	j11 := s.keyWords[4]
	j12 := s.keyWords[5]
	j13 := s.keyWords[6]
	j14 := s.keyWords[7]
	j15 := s.sigma[3]

	x0, x1, x2, x3 := j0, j1, j2, j3
	x4, x5, x6, x7 := j4, j5, j6, j7
	x8, x9, x10, x11 := j8, j9, j10, j11
	x12, x13, x14, x15 := j12, j13, j14, j15

	var u uint32
	for i := 0; i < s.rounds; i += 2 {
		// Column round
		u = x0 + x12
		x4 ^= (u << 7) | (u >> (32 - 7))
		u = x4 + x0
		x8 ^= (u << 9) | (u >> (32 - 9))
		u = x8 + x4
		x12 ^= (u << 13) | (u >> (32 - 13))
		u = x12 + x8
		x0 ^= (u << 18) | (u >> (32 - 18))

		u = x5 + x1
		x9 ^= (u << 7) | (u >> (32 - 7))
		u = x9 + x5
		x13 ^= (u << 9) | (u >> (32 - 9))
		u = x13 + x9
		x1 ^= (u << 13) | (u >> (32 - 13))
		u = x1 + x13
		x5 ^= (u << 18) | (u >> (32 - 18))

		u = x10 + x6
		x14 ^= (u << 7) | (u >> (32 - 7))
		u = x14 + x10
		x2 ^= (u << 9) | (u >> (32 - 9))
		u = x2 + x14
		x6 ^= (u << 13) | (u >> (32 - 13))
		u = x6 + x2
		x10 ^= (u << 18) | (u >> (32 - 18))

		u = x15 + x11
		x3 ^= (u << 7) | (u >> (32 - 7))
		u = x3 + x15
		x7 ^= (u << 9) | (u >> (32 - 9))
		u = x7 + x3
		x11 ^= (u << 13) | (u >> (32 - 13))
		u = x11 + x7
		x15 ^= (u << 18) | (u >> (32 - 18))

		// Row round
		u = x0 + x3
		x1 ^= (u << 7) | (u >> (32 - 7))
		u = x1 + x0
		x2 ^= (u << 9) | (u >> (32 - 9))
		u = x2 + x1
		x3 ^= (u << 13) | (u >> (32 - 13))
		u = x3 + x2
		x0 ^= (u << 18) | (u >> (32 - 18))

		u = x5 + x4
		x6 ^= (u << 7) | (u >> (32 - 7))
		u = x6 + x5
		x7 ^= (u << 9) | (u >> (32 - 9))
		u = x7 + x6
		x4 ^= (u << 13) | (u >> (32 - 13))
		u = x4 + x7
		x5 ^= (u << 18) | (u >> (32 - 18))

		u = x10 + x9
		x11 ^= (u << 7) | (u >> (32 - 7))
		u = x11 + x10
		x8 ^= (u << 9) | (u >> (32 - 9))
		u = x8 + x11
		x9 ^= (u << 13) | (u >> (32 - 13))
		u = x9 + x8
		x10 ^= (u << 18) | (u >> (32 - 18))

		u = x15 + x14
		x12 ^= (u << 7) | (u >> (32 - 7))
		u = x12 + x15
		x13 ^= (u << 9) | (u >> (32 - 9))
		u = x13 + x12
		x14 ^= (u << 13) | (u >> (32 - 13))
		u = x14 + x13
		x15 ^= (u << 18) | (u >> (32 - 18))
	}

	x0 += j0
	x1 += j1
	x2 += j2
	x3 += j3
	x4 += j4
	x5 += j5
	x6 += j6
	x7 += j7
	x8 += j8
	x9 += j9
	x10 += j10
	x11 += j11
	x12 += j12
	x13 += j13
	x14 += j14
	x15 += j15

	s.block[0] = byte(x0)
	s.block[1] = byte(x0 >> 8)
	s.block[2] = byte(x0 >> 16)
	s.block[3] = byte(x0 >> 24)
	s.block[4] = byte(x1)
	s.block[5] = byte(x1 >> 8)
	s.block[6] = byte(x1 >> 16)
	s.block[7] = byte(x1 >> 24)
	s.block[8] = byte(x2)
	s.block[9] = byte(x2 >> 8)
	s.block[10] = byte(x2 >> 16)
	s.block[11] = byte(x2 >> 24)
	s.block[12] = byte(x3)
	s.block[13] = byte(x3 >> 8)
	s.block[14] = byte(x3 >> 16)
	s.block[15] = byte(x3 >> 24)
	s.block[16] = byte(x4)
	s.block[17] = byte(x4 >> 8)
	s.block[18] = byte(x4 >> 16)
	s.block[19] = byte(x4 >> 24)
	s.block[20] = byte(x5)
	s.block[21] = byte(x5 >> 8)
	s.block[22] = byte(x5 >> 16)
	s.block[23] = byte(x5 >> 24)
	s.block[24] = byte(x6)
	s.block[25] = byte(x6 >> 8)
	s.block[26] = byte(x6 >> 16)
	s.block[27] = byte(x6 >> 24)
	s.block[28] = byte(x7)
	s.block[29] = byte(x7 >> 8)
	s.block[30] = byte(x7 >> 16)
	s.block[31] = byte(x7 >> 24)
	s.block[32] = byte(x8)
	s.block[33] = byte(x8 >> 8)
	s.block[34] = byte(x8 >> 16)
	s.block[35] = byte(x8 >> 24)
	s.block[36] = byte(x9)
	s.block[37] = byte(x9 >> 8)
	s.block[38] = byte(x9 >> 16)
	s.block[39] = byte(x9 >> 24)
	s.block[40] = byte(x10)
	s.block[41] = byte(x10 >> 8)
	s.block[42] = byte(x10 >> 16)
	s.block[43] = byte(x10 >> 24)
	s.block[44] = byte(x11)
	s.block[45] = byte(x11 >> 8)
	s.block[46] = byte(x11 >> 16)
	s.block[47] = byte(x11 >> 24)
	s.block[48] = byte(x12)
	s.block[49] = byte(x12 >> 8)
	s.block[50] = byte(x12 >> 16)
	s.block[51] = byte(x12 >> 24)
	s.block[52] = byte(x13)
	s.block[53] = byte(x13 >> 8)
	s.block[54] = byte(x13 >> 16)
	s.block[55] = byte(x13 >> 24)
	s.block[56] = byte(x14)
	s.block[57] = byte(x14 >> 8)
	s.block[58] = byte(x14 >> 16)
	s.block[59] = byte(x14 >> 24)
	s.block[60] = byte(x15)
	s.block[61] = byte(x15 >> 8)
	s.block[62] = byte(x15 >> 16)
	s.block[63] = byte(x15 >> 24)
}
