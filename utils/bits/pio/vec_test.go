package pio

import (
	"bytes"
	"testing"
)

func vecBytes(vec [][]byte) []byte {
	return bytes.Join(vec, nil)
}

func TestVecLen(t *testing.T) {
	vec := [][]byte{{1, 2, 3}, {4, 5, 6, 7, 8, 9}, {10, 11, 12, 13}}
	if n := VecLen(vec); n != 13 {
		t.Fatalf("VecLen = %d, want 13", n)
	}
	if n := VecLen(nil); n != 0 {
		t.Fatalf("VecLen(nil) = %d, want 0", n)
	}
}

func TestVecSlice(t *testing.T) {
	vec := [][]byte{{1, 2, 3}, {4, 5, 6, 7, 8, 9}, {10, 11, 12, 13}}

	for _, tc := range []struct {
		name string
		s, e int
		want []byte
	}{
		{"whole", 0, -1, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}},
		{"drop first byte", 1, -1, []byte{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}},
		{"within first buffer", 1, 2, []byte{2}},
		{"spanning buffers", 2, 8, []byte{3, 4, 5, 6, 7, 8}},
		{"start on a boundary", 3, -1, []byte{4, 5, 6, 7, 8, 9, 10, 11, 12, 13}},
		{"end on a boundary", 0, 9, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{"empty", 8, 8, nil},
		{"empty at end", 13, 13, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := VecSlice(vec, tc.s, tc.e)
			if got := vecBytes(out); !bytes.Equal(got, tc.want) {
				t.Fatalf("VecSlice(%d, %d) = %v, want %v", tc.s, tc.e, got, tc.want)
			}
			if n := VecLen(out); n != len(tc.want) {
				t.Fatalf("VecLen = %d, want %d", n, len(tc.want))
			}
		})
	}
}

// TestVecSliceRepeated slices an already sliced vec, the way format code peels
// off a header and then reslices the remainder.
func TestVecSliceRepeated(t *testing.T) {
	vec := [][]byte{{1, 2, 3}, {4, 5, 6, 7, 8, 9}, {10, 11, 12, 13}}

	vec = VecSlice(vec, 1, -1)
	if want := []byte{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}; !bytes.Equal(vecBytes(vec), want) {
		t.Fatalf("first slice = %v, want %v", vecBytes(vec), want)
	}

	vec = VecSlice(vec, 2, -1)
	if want := []byte{4, 5, 6, 7, 8, 9, 10, 11, 12, 13}; !bytes.Equal(vecBytes(vec), want) {
		t.Fatalf("second slice = %v, want %v", vecBytes(vec), want)
	}

	vec = VecSlice(vec, 8, 8)
	if n := VecLen(vec); n != 0 {
		t.Fatalf("third slice len = %d, want 0", n)
	}
}

func TestVecSliceOutOfRange(t *testing.T) {
	vec := [][]byte{{1, 2, 3}}

	for _, tc := range []struct {
		name string
		s, e int
	}{
		{"start past end", 4, -1},
		{"end past end", 0, 4},
		{"end before start", 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("VecSlice(%d, %d) did not panic", tc.s, tc.e)
				}
			}()
			VecSlice(vec, tc.s, tc.e)
		})
	}
}
