package mp4io

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
)

// box builds an atom with the standard 32-bit size header.
func box(tag string, payload ...[]byte) []byte {
	var body []byte
	for _, p := range payload {
		body = append(body, p...)
	}
	b := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(b[:4], uint32(8+len(body)))
	copy(b[4:8], tag)
	return append(b, body...)
}

// box64 builds an atom using the 64-bit largesize form, where the 32-bit size field is 1 and
// the real size follows the tag. Writers that reserve space for a large mdat up front emit this
// even when the payload would fit in 32 bits.
func box64(tag string, payload []byte) []byte {
	b := make([]byte, 16, 16+len(payload))
	binary.BigEndian.PutUint32(b[:4], 1)
	copy(b[4:8], tag)
	binary.BigEndian.PutUint64(b[8:16], uint64(16+len(payload)))
	return append(b, payload...)
}

// elstEntry encodes a single edit list entry in the layout for the given version.
func elstEntry(version uint8, segmentDuration uint64, mediaTime int64, rate int16) []byte {
	var b []byte
	if version == 1 {
		b = make([]byte, 20)
		binary.BigEndian.PutUint64(b[0:8], segmentDuration)
		binary.BigEndian.PutUint64(b[8:16], uint64(mediaTime))
		binary.BigEndian.PutUint16(b[16:18], uint16(rate))
	} else {
		b = make([]byte, 12)
		binary.BigEndian.PutUint32(b[0:4], uint32(segmentDuration))
		binary.BigEndian.PutUint32(b[4:8], uint32(int32(mediaTime)))
		binary.BigEndian.PutUint16(b[8:10], uint16(rate))
	}
	return b
}

// elst builds an edit list atom with the given version, flags and entries. entryCount is passed
// explicitly so tests can declare more entries than the payload actually contains.
func elst(version uint8, flags uint32, entryCount uint32, entries ...[]byte) []byte {
	p := make([]byte, 8)
	p[0] = version
	p[1] = byte(flags >> 16)
	p[2] = byte(flags >> 8)
	p[3] = byte(flags)
	binary.BigEndian.PutUint32(p[4:8], entryCount)
	for _, e := range entries {
		p = append(p, e...)
	}
	return box("elst", p)
}

func TestEditListUnmarshal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		atom    []byte
		version uint8
		flags   uint32
		entries []EditListEntry
	}{
		{
			name:    "version 0",
			atom:    elst(0, 0, 1, elstEntry(0, 175027, 1600, 1)),
			version: 0,
			entries: []EditListEntry{{SegmentDuration: 175027, MediaTime: 1600, MediaRateInteger: 1}},
		},
		{
			// The layout that made this fork's demuxer fail: a 64-bit edit list, which is what
			// writers emit for tracks whose duration doesn't fit a 32-bit field.
			name:    "version 1",
			atom:    elst(1, 0, 1, elstEntry(1, 175027, 1600, 1)),
			version: 1,
			entries: []EditListEntry{{SegmentDuration: 175027, MediaTime: 1600, MediaRateInteger: 1}},
		},
		{
			name:    "version 1 with 64-bit segment duration",
			atom:    elst(1, 0, 1, elstEntry(1, 1<<40, 1<<38, 1)),
			version: 1,
			entries: []EditListEntry{{SegmentDuration: 1 << 40, MediaTime: 1 << 38, MediaRateInteger: 1}},
		},
		{
			// An empty edit is signalled by a media time of -1, so the sign has to survive.
			name:    "negative media time",
			atom:    elst(0, 0, 1, elstEntry(0, 500, -1, 1)),
			version: 0,
			entries: []EditListEntry{{SegmentDuration: 500, MediaTime: -1, MediaRateInteger: 1}},
		},
		{
			name:    "version 1 negative media time",
			atom:    elst(1, 0, 1, elstEntry(1, 500, -1, 1)),
			version: 1,
			entries: []EditListEntry{{SegmentDuration: 500, MediaTime: -1, MediaRateInteger: 1}},
		},
		{
			// Flags occupy the 3 bytes after the version. Reading all 4 as the version is the
			// bug this guards: non-zero flags must not be mistaken for a version.
			name:    "non-zero flags",
			atom:    elst(0, 0x000001, 1, elstEntry(0, 175027, 1600, 1)),
			version: 0,
			flags:   0x000001,
			entries: []EditListEntry{{SegmentDuration: 175027, MediaTime: 1600, MediaRateInteger: 1}},
		},
		{
			name:    "multiple entries",
			atom:    elst(1, 0, 2, elstEntry(1, 100, 0, 1), elstEntry(1, 200, 50, 0)),
			version: 1,
			entries: []EditListEntry{
				{SegmentDuration: 100, MediaTime: 0, MediaRateInteger: 1},
				{SegmentDuration: 200, MediaTime: 50, MediaRateInteger: 0},
			},
		},
		{
			name:    "zero entries",
			atom:    elst(1, 0, 0),
			version: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			el := &EditList{}
			n, err := el.Unmarshal(tc.atom, 0)
			if err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if n != len(tc.atom) {
				t.Errorf("consumed %d bytes, want %d", n, len(tc.atom))
			}
			if el.Version != tc.version {
				t.Errorf("Version = %d, want %d", el.Version, tc.version)
			}
			if el.Flags != tc.flags {
				t.Errorf("Flags = %#x, want %#x", el.Flags, tc.flags)
			}
			if len(el.Entries) != len(tc.entries) {
				t.Fatalf("got %d entries, want %d", len(el.Entries), len(tc.entries))
			}
			for i, want := range tc.entries {
				if got := *el.Entries[i]; got != want {
					t.Errorf("entry %d = %+v, want %+v", i, got, want)
				}
			}
		})
	}
}

func TestEditListUnmarshalMalformed(t *testing.T) {
	for _, tc := range []struct {
		name string
		atom []byte
	}{
		{
			// Must be rejected rather than used to compute an entry size.
			name: "unknown version",
			atom: elst(2, 0, 1, elstEntry(1, 100, 0, 1)),
		},
		{
			name: "truncated version",
			atom: box("elst", []byte{0}),
		},
		{
			name: "truncated entry count",
			atom: box("elst", []byte{1, 0, 0, 0, 0, 0}),
		},
		{
			name: "entry count exceeds payload",
			atom: elst(1, 0, 4, elstEntry(1, 100, 0, 1)),
		},
		{
			name: "version 1 entry truncated to version 0 size",
			atom: elst(1, 0, 1, elstEntry(0, 100, 0, 1)),
		},
		{
			// A count this large overflows a 32-bit entry-size calculation; it must still be
			// rejected instead of wrapping to a value that fits the buffer.
			name: "entry count overflows size calculation",
			atom: elst(1, 0, 0xffffffff, elstEntry(1, 100, 0, 1)),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			el := &EditList{}
			if _, err := el.Unmarshal(tc.atom, 0); err == nil {
				t.Fatalf("expected an error, got entries %+v", el.Entries)
			}
		})
	}
}

// synthFile builds a minimal but structurally faithful MP4: a short ftyp, an odd-sized free
// atom, an mdat using the 64-bit largesize form, and a trailing moov holding a single track
// with an edit list. This mirrors the layout produced by writers that place moov last.
func synthFile(editList []byte) []byte {
	var f []byte
	f = append(f, box("ftyp", []byte("isom\x00\x02\x00\x00isomiso2mp41"))...)
	f = append(f, box("free", []byte{0})...)
	f = append(f, box64("mdat", bytes.Repeat([]byte{'Z'}, 64))...)
	f = append(f, box("moov", box("trak", box("edts", editList)))...)
	return f
}

func TestReadFileAtomsEditList(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version uint8
	}{
		{name: "version 0", version: 0},
		{name: "version 1", version: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := synthFile(elst(tc.version, 0, 1, elstEntry(tc.version, 175027, 1600, 1)))

			atoms, err := ReadFileAtoms(context.Background(), bytes.NewReader(data), []Tag{MOOV})
			if err != nil {
				t.Fatalf("ReadFileAtoms: %v", err)
			}

			var moov *Movie
			for _, a := range atoms {
				if m, ok := a.(*Movie); ok {
					moov = m
				}
			}
			if moov == nil {
				t.Fatal("'moov' atom not found")
			}
			if len(moov.Tracks) != 1 {
				t.Fatalf("got %d tracks, want 1", len(moov.Tracks))
			}
			edit := moov.Tracks[0].Edit
			if edit == nil {
				t.Fatal("track has no edit atom")
			}
			if len(edit.EditLists) != 1 {
				t.Fatalf("got %d edit lists, want 1", len(edit.EditLists))
			}
			el := edit.EditLists[0]
			if el.Version != tc.version {
				t.Errorf("Version = %d, want %d", el.Version, tc.version)
			}
			want := EditListEntry{SegmentDuration: 175027, MediaTime: 1600, MediaRateInteger: 1}
			if len(el.Entries) != 1 {
				t.Fatalf("got %d entries, want 1", len(el.Entries))
			}
			if got := *el.Entries[0]; got != want {
				t.Errorf("entry = %+v, want %+v", got, want)
			}
		})
	}
}

// co64 builds a 64-bit chunk offset atom. entryCount is passed explicitly so tests can declare
// more entries than the payload holds.
func co64(entryCount uint32, offsets ...uint64) []byte {
	p := make([]byte, 8)
	binary.BigEndian.PutUint32(p[4:8], entryCount)
	for _, o := range offsets {
		var e [8]byte
		binary.BigEndian.PutUint64(e[:], o)
		p = append(p, e[:]...)
	}
	return box("co64", p)
}

// stco builds a 32-bit chunk offset atom.
func stco(offsets ...uint32) []byte {
	p := make([]byte, 8)
	binary.BigEndian.PutUint32(p[4:8], uint32(len(offsets)))
	for _, o := range offsets {
		var e [4]byte
		binary.BigEndian.PutUint32(e[:], o)
		p = append(p, e[:]...)
	}
	return box("stco", p)
}

func TestChunkOffset64Unmarshal(t *testing.T) {
	// Offsets past 4GB are the reason co64 exists, so they have to survive intact.
	want := []uint64{36, 1 << 32, 1<<40 + 12345, 1<<63 - 1}
	atom := co64(uint32(len(want)), want...)

	co := &ChunkOffset64{}
	n, err := co.Unmarshal(atom, 0)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if n != len(atom) {
		t.Errorf("consumed %d bytes, want %d", n, len(atom))
	}
	if len(co.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(co.Entries), len(want))
	}
	for i, w := range want {
		if co.Entries[i] != w {
			t.Errorf("entry %d = %d, want %d", i, co.Entries[i], w)
		}
	}
}

func TestChunkOffset64UnmarshalMalformed(t *testing.T) {
	for _, tc := range []struct {
		name string
		atom []byte
	}{
		{name: "truncated version", atom: box("co64", []byte{0})},
		{name: "truncated entry count", atom: box("co64", []byte{0, 0, 0, 0, 0, 0})},
		{name: "entry count exceeds payload", atom: co64(4, 36, 64)},
		{name: "truncated final entry", atom: append(co64(2, 36), 0, 0, 0, 0)},
		{
			// Must be rejected rather than used to size an allocation.
			name: "entry count overflows size calculation",
			atom: co64(0xffffffff, 36),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			co := &ChunkOffset64{}
			if _, err := co.Unmarshal(tc.atom, 0); err == nil {
				t.Fatalf("expected an error, got entries %+v", co.Entries)
			}
		})
	}
}

func TestChunkOffset64MarshalRoundTrip(t *testing.T) {
	in := &ChunkOffset64{Entries: []uint64{36, 1 << 32, 1<<40 + 12345}}

	b := make([]byte, in.Len())
	n := in.Marshal(b)
	if n != len(b) {
		t.Fatalf("Marshal wrote %d bytes, Len reported %d", n, len(b))
	}
	if tag := Tag(binary.BigEndian.Uint32(b[4:8])); tag != CO64 {
		t.Errorf("marshalled tag = %s, want co64", tag)
	}

	out := &ChunkOffset64{}
	if _, err := out.Unmarshal(b, 0); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(out.Entries) != len(in.Entries) {
		t.Fatalf("got %d entries, want %d", len(out.Entries), len(in.Entries))
	}
	for i, w := range in.Entries {
		if out.Entries[i] != w {
			t.Errorf("entry %d = %d, want %d", i, out.Entries[i], w)
		}
	}
}

// A sample table carries chunk offsets as either stco or co64. Both have to reach the demuxer
// through the same accessors.
func TestSampleTableChunkOffsets(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stbl  []byte
		count int
		first int64
		last  int64
	}{
		{
			name:  "stco",
			stbl:  box("stbl", stco(36, 4096, 8192)),
			count: 3,
			first: 36,
			last:  8192,
		},
		{
			name:  "co64",
			stbl:  box("stbl", co64(3, 36, 1<<32, 1<<40)),
			count: 3,
			first: 36,
			last:  1 << 40,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &SampleTable{}
			if _, err := st.Unmarshal(tc.stbl, 0); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got := st.ChunkOffsetCount(); got != tc.count {
				t.Fatalf("ChunkOffsetCount() = %d, want %d", got, tc.count)
			}
			if got := st.ChunkOffsetEntry(0); got != tc.first {
				t.Errorf("ChunkOffsetEntry(0) = %d, want %d", got, tc.first)
			}
			if got := st.ChunkOffsetEntry(tc.count - 1); got != tc.last {
				t.Errorf("ChunkOffsetEntry(%d) = %d, want %d", tc.count-1, got, tc.last)
			}
		})
	}

	t.Run("neither", func(t *testing.T) {
		st := &SampleTable{}
		if _, err := st.Unmarshal(box("stbl"), 0); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if got := st.ChunkOffsetCount(); got != 0 {
			t.Errorf("ChunkOffsetCount() = %d, want 0", got)
		}
	})
}
