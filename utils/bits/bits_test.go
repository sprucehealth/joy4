package bits

import (
	"bytes"
	"testing"
)

func TestReader(t *testing.T) {
	r := &Reader{R: bytes.NewReader([]byte{0xf3, 0xb3, 0x45, 0x60})}

	for _, tc := range []struct {
		n    int
		want uint
	}{{4, 0xf}, {4, 0x3}, {2, 0x2}, {2, 0x3}} {
		bits, err := r.ReadBits(tc.n)
		if err != nil {
			t.Fatalf("ReadBits(%d) = %v", tc.n, err)
		}
		if bits != tc.want {
			t.Fatalf("ReadBits(%d) = %#x, want %#x", tc.n, bits, tc.want)
		}
	}

	b := make([]byte, 2)
	if _, err := r.Read(b); err != nil {
		t.Fatalf("Read = %v", err)
	}
	if want := []byte{0x34, 0x56}; !bytes.Equal(b, want) {
		t.Fatalf("Read = %#x, want %#x", b, want)
	}
}

func TestWriter(t *testing.T) {
	var buf bytes.Buffer
	w := &Writer{W: &buf}

	for _, tc := range []struct {
		bits uint
		n    int
	}{{0xf, 4}, {0x3, 4}, {0x2, 2}, {0x3, 2}} {
		if err := w.WriteBits(tc.bits, tc.n); err != nil {
			t.Fatalf("WriteBits(%#x, %d) = %v", tc.bits, tc.n, err)
		}
	}

	n, err := w.Write([]byte{0x34, 0x56})
	if err != nil {
		t.Fatalf("Write = %v", err)
	}
	if n != 2 {
		t.Fatalf("Write = %d, want 2", n)
	}
	if err := w.FlushBits(); err != nil {
		t.Fatalf("FlushBits = %v", err)
	}

	if want := []byte{0xf3, 0xb3, 0x45, 0x60}; !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("wrote %#x, want %#x", buf.Bytes(), want)
	}
}

// TestWriteBits64 checks that a value wider than a byte is written big endian.
func TestWriteBits64(t *testing.T) {
	var buf bytes.Buffer
	w := &Writer{W: &buf}
	if err := w.WriteBits64(0x11223344, 32); err != nil {
		t.Fatalf("WriteBits64 = %v", err)
	}
	if err := w.FlushBits(); err != nil {
		t.Fatalf("FlushBits = %v", err)
	}
	if want := []byte{0x11, 0x22, 0x33, 0x44}; !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("wrote %#x, want %#x", buf.Bytes(), want)
	}

	r := &Reader{R: &buf}
	bits, err := r.ReadBits64(32)
	if err != nil {
		t.Fatalf("ReadBits64 = %v", err)
	}
	if bits != 0x11223344 {
		t.Fatalf("ReadBits64 = %#x, want 0x11223344", bits)
	}
}
