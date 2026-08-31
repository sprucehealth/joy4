package h264parser

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func decodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex.DecodeString: %v", err)
	}
	return b
}

func TestSplitNALUs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		frame   string
		wantTyp int
		want    []string
	}{
		{
			// Two 4-byte start codes, one 3-byte start code, then trailing
			// start codes with no payload.
			name:    "annexb",
			frame:   "00000001223322330000000122332233223300000133000001000001",
			wantTyp: NALU_ANNEXB,
			want:    []string{"22332233", "223322332233", "33"},
		},
		{
			name:    "avcc",
			frame:   "00000008aabbccaabbccaabb00000001aa",
			wantTyp: NALU_AVCC,
			want:    []string{"aabbccaabbccaabb", "aa"},
		},
		{
			name:    "raw too short",
			frame:   "aabbcc",
			wantTyp: NALU_RAW,
			want:    []string{"aabbcc"},
		},
		{
			name:    "raw no start code",
			frame:   "aabbccddeeff",
			wantTyp: NALU_RAW,
			want:    []string{"aabbccddeeff"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := decodeHex(t, tc.frame)

			nalus, typ := SplitNALUs(b)
			if typ != tc.wantTyp {
				t.Fatalf("SplitNALUs typ = %d, want %d", typ, tc.wantTyp)
			}
			if len(nalus) != len(tc.want) {
				t.Fatalf("SplitNALUs = %d NALUs, want %d", len(nalus), len(tc.want))
			}
			for i, want := range tc.want {
				if !bytes.Equal(nalus[i], decodeHex(t, want)) {
					t.Errorf("NALU %d = %x, want %s", i, nalus[i], want)
				}
			}

			if typ := CheckNALUsType(b); typ != tc.wantTyp {
				t.Errorf("CheckNALUsType = %d, want %d", typ, tc.wantTyp)
			}
		})
	}
}

func TestParseSPS(t *testing.T) {
	// Main profile, level 3.0, 40x30 macroblocks with no frame cropping.
	sps := decodeHex(t, "674d001e95a8280f64")

	info, err := ParseSPS(sps)
	if err != nil {
		t.Fatalf("ParseSPS: %v", err)
	}
	if info.Width != 640 || info.Height != 480 {
		t.Errorf("dimensions = %dx%d, want 640x480", info.Width, info.Height)
	}
	if info.ProfileIdc != 77 {
		t.Errorf("ProfileIdc = %d, want 77", info.ProfileIdc)
	}
	if info.LevelIdc != 30 {
		t.Errorf("LevelIdc = %d, want 30", info.LevelIdc)
	}
	if info.MbWidth != 40 || info.MbHeight != 30 {
		t.Errorf("macroblocks = %dx%d, want 40x30", info.MbWidth, info.MbHeight)
	}
}

func TestParseSPSTruncated(t *testing.T) {
	sps := decodeHex(t, "674d001e95a8280f64")
	for i := range len(sps) {
		if _, err := ParseSPS(sps[:i]); err == nil {
			t.Errorf("ParseSPS(sps[:%d]) = nil error, want an error", i)
		}
	}
}

func TestCodecDataFromSPSAndPPS(t *testing.T) {
	sps := decodeHex(t, "674d001e95a8280f64")
	pps := decodeHex(t, "68ee3c80")

	codec, err := NewCodecDataFromSPSAndPPS(sps, pps)
	if err != nil {
		t.Fatalf("NewCodecDataFromSPSAndPPS: %v", err)
	}
	if codec.Width() != 640 || codec.Height() != 480 {
		t.Fatalf("dimensions = %dx%d, want 640x480", codec.Width(), codec.Height())
	}

	// A round trip through the AVCDecoderConfRecord must preserve both sets.
	rt, err := NewCodecDataFromAVCDecoderConfRecord(codec.AVCDecoderConfRecordBytes())
	if err != nil {
		t.Fatalf("NewCodecDataFromAVCDecoderConfRecord: %v", err)
	}
	if !bytes.Equal(rt.SPS(), sps) {
		t.Errorf("SPS = %x, want %x", rt.SPS(), sps)
	}
	if !bytes.Equal(rt.PPS(), pps) {
		t.Errorf("PPS = %x, want %x", rt.PPS(), pps)
	}
	if rt.Width() != 640 || rt.Height() != 480 {
		t.Errorf("dimensions = %dx%d, want 640x480", rt.Width(), rt.Height())
	}
}
