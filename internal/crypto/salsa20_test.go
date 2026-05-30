package crypto

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestSalsa20New(t *testing.T) {
	nonce := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")

	s, err := NewSalsa20(nonce, key, 20)
	if err != nil {
		t.Fatalf("new salsa20: %v", err)
	}
	if s == nil {
		t.Fatal("nil instance")
	}
}

func TestSalsa20InvalidNonceLength(t *testing.T) {
	nonce := []byte{0x00, 0x01, 0x02}
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")

	_, err := NewSalsa20(nonce, key, 20)
	if err == nil {
		t.Fatal("expected error for 3-byte nonce")
	}
}

func TestSalsa20InvalidKeyLength(t *testing.T) {
	nonce := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	key := []byte{0x00, 0x01, 0x02}

	_, err := NewSalsa20(nonce, key, 20)
	if err == nil {
		t.Fatal("expected error for 3-byte key")
	}
}

func TestSalsa20RoundTrip(t *testing.T) {
	nonce := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")

	enc, _ := NewSalsa20(nonce, key, 20)
	dec, _ := NewSalsa20(nonce, key, 20)

	plaintext := []byte("Hello, World! Salsa20 encryption test.")
	ciphertext := make([]byte, len(plaintext))
	decrypted := make([]byte, len(plaintext))

	enc.Process(ciphertext, plaintext)
	dec.Process(decrypted, ciphertext)

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("round-trip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestSalsa20KeystreamDeterminism(t *testing.T) {
	nonce := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")

	s1, _ := NewSalsa20(nonce, key, 20)
	s2, _ := NewSalsa20(nonce, key, 20)

	src := make([]byte, 128)
	for i := range src {
		src[i] = byte(i)
	}

	out1 := make([]byte, 128)
	out2 := make([]byte, 128)

	s1.Process(out1, src)
	s2.Process(out2, src)

	if !bytes.Equal(out1, out2) {
		t.Fatal("non-deterministic keystream")
	}
}

func TestSalsa20LargeData(t *testing.T) {
	nonce := []byte{0x31, 0x41, 0x59, 0x26, 0x53, 0x58, 0x97, 0x93}
	key, _ := hex.DecodeString("a1b2c3d4e5f600112233445566778899aabbccddeeff00112233445566778899")

	enc, _ := NewSalsa20(nonce, key, 20)
	dec, _ := NewSalsa20(nonce, key, 20)

	plaintext := make([]byte, 1024)
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}

	ciphertext := make([]byte, 1024)
	decrypted := make([]byte, 1024)

	enc.Process(ciphertext, plaintext)
	dec.Process(decrypted, ciphertext)

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatal("large data round-trip failed")
	}
}

func TestSalsa20128BitKey(t *testing.T) {
	nonce := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")

	enc, _ := NewSalsa20(nonce, key, 20)
	dec, _ := NewSalsa20(nonce, key, 20)

	plaintext := []byte("128-bit key test message.")
	ciphertext := make([]byte, len(plaintext))
	decrypted := make([]byte, len(plaintext))

	enc.Process(ciphertext, plaintext)
	dec.Process(decrypted, ciphertext)

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("128-bit key round-trip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestSalsa20GetBytes(t *testing.T) {
	nonce := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")

	s, _ := NewSalsa20(nonce, key, 20)

	out1 := s.GetBytes(64)
	out2 := s.GetBytes(64)

	// Different blocks should produce different keystream
	if bytes.Equal(out1, out2) {
		t.Fatal("different blocks should produce different keystream")
	}
}

func TestSalsa20DifferentKeysDifferentOutput(t *testing.T) {
	nonce := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	key1, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	key2, _ := hex.DecodeString("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

	s1, _ := NewSalsa20(nonce, key1, 20)
	s2, _ := NewSalsa20(nonce, key2, 20)

	src := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	out1 := make([]byte, 8)
	out2 := make([]byte, 8)

	s1.Process(out1, src)
	s2.Process(out2, src)

	if bytes.Equal(out1, out2) {
		t.Fatal("different keys should produce different output")
	}
}
