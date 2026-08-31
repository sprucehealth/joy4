package mp4

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"testing"
	"time"

	"github.com/sprucehealth/joy4/format/mp4/mp4io"
)

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

// unparsableFile builds a file whose moov is found but fails to parse: the edit list declares a
// version the parser doesn't know how to read.
func unparsableFile() []byte {
	elst := box("elst", []byte{
		2, 0, 0, 0, // version 2 (unknown), flags 0
		0, 0, 0, 1, // entry count
	})
	var f []byte
	f = append(f, box("ftyp", []byte("isom\x00\x02\x00\x00isomiso2mp41"))...)
	f = append(f, box("mdat", bytes.Repeat([]byte{'Z'}, 64))...)
	f = append(f, box("moov", box("trak", box("edts", elst)))...)
	return f
}

// A failed probe must not leave the reader positioned mid-file. If it does, the next probe starts
// from wherever parsing stopped, finds no atoms, and reports "'moov' atom not found" instead of
// the error that actually caused the failure.
func TestProbeErrorIsRepeatable(t *testing.T) {
	ctx := context.Background()
	d := NewDemuxer(bytes.NewReader(unparsableFile()))

	_, first := d.Streams(ctx)
	if first == nil {
		t.Fatal("expected the first Streams call to fail")
	}
	_, second := d.Streams(ctx)
	if second == nil {
		t.Fatal("expected the second Streams call to fail")
	}
	if first.Error() != second.Error() {
		t.Errorf("second probe reported a different error:\n first: %v\nsecond: %v", first, second)
	}
	if got := first.Error(); got == "mp4: 'moov' atom not found" {
		t.Errorf("probe masked the parse error with %q", got)
	}
}

// testStream builds a stream whose sample table describes sampleCount samples, one per chunk,
// each of the same duration. Sizes are per-sample when variable is true, otherwise a single
// constant size is declared the way tracks with fixed-size samples do.
func testStream(d *Demuxer, idx int, timeScale int64, sampleCount int, sampleDuration uint32, variable bool) *Stream {
	sample := &mp4io.SampleTable{
		SampleToChunk: &mp4io.SampleToChunk{
			Entries: []mp4io.SampleToChunkEntry{{FirstChunk: 1, SamplesPerChunk: 1, SampleDescId: 1}},
		},
		TimeToSample: &mp4io.TimeToSample{
			Entries: []mp4io.TimeToSampleEntry{{Count: uint32(sampleCount), Duration: sampleDuration}},
		},
		ChunkOffset: &mp4io.ChunkOffset{},
		SampleSize:  &mp4io.SampleSize{},
	}
	for i := range sampleCount {
		sample.ChunkOffset.Entries = append(sample.ChunkOffset.Entries, uint32(1024+i*100))
	}
	if variable {
		for range sampleCount {
			sample.SampleSize.Entries = append(sample.SampleSize.Entries, 100)
		}
	} else {
		sample.SampleSize.SampleSize = 100
	}
	return &Stream{
		sample:    sample,
		timeScale: timeScale,
		idx:       idx,
		demuxer:   d,
	}
}

// readAll drains the demuxer, returning how many packets each stream produced.
func readAll(t *testing.T, d *Demuxer) []int {
	t.Helper()
	counts := make([]int, len(d.streams))
	for range 10000 {
		pkt, err := d.ReadPacket(context.Background(), true)
		if err == io.EOF {
			return counts
		}
		if err != nil {
			t.Fatalf("ReadPacket: %v", err)
		}
		if int(pkt.Idx) >= len(counts) {
			t.Fatalf("packet index %d out of range", pkt.Idx)
		}
		counts[pkt.Idx]++
	}
	t.Fatal("ReadPacket did not reach EOF")
	return nil
}

// Tracks routinely have different durations. A stream that runs out keeps its final dts, so
// without skipping exhausted streams it stays the lowest forever and its EOF cuts the demux
// short while the longer streams still have packets.
func TestReadPacketStreamsOfDifferentLengths(t *testing.T) {
	for _, tc := range []struct {
		name  string
		short int
		long  int
	}{
		{name: "short track first", short: 5, long: 20},
		{name: "single sample track", short: 1, long: 12},
		{name: "equal lengths", short: 8, long: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Demuxer{movieAtom: &mp4io.Movie{}}
			d.streams = []*Stream{
				testStream(d, 0, 1000, tc.short, 100, true),
				testStream(d, 1, 1000, tc.long, 100, true),
			}

			counts := readAll(t, d)
			if counts[0] != tc.short {
				t.Errorf("stream 0 produced %d packets, want %d", counts[0], tc.short)
			}
			if counts[1] != tc.long {
				t.Errorf("stream 1 produced %d packets, want %d", counts[1], tc.long)
			}
		})
	}
}

// The longer track being first must work the same way.
func TestReadPacketLongStreamFirst(t *testing.T) {
	d := &Demuxer{movieAtom: &mp4io.Movie{}}
	d.streams = []*Stream{
		testStream(d, 0, 1000, 20, 100, true),
		testStream(d, 1, 1000, 5, 100, true),
	}

	counts := readAll(t, d)
	if counts[0] != 20 {
		t.Errorf("stream 0 produced %d packets, want 20", counts[0])
	}
	if counts[1] != 5 {
		t.Errorf("stream 1 produced %d packets, want 5", counts[1])
	}
}

// Packets must still come out in non-decreasing time order across streams.
func TestReadPacketOrdering(t *testing.T) {
	d := &Demuxer{movieAtom: &mp4io.Movie{}}
	d.streams = []*Stream{
		testStream(d, 0, 1000, 6, 100, true),
		testStream(d, 1, 1000, 15, 250, true),
	}

	var last time.Duration
	total := 0
	for {
		pkt, err := d.ReadPacket(context.Background(), true)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ReadPacket: %v", err)
		}
		if pkt.Time < last {
			t.Fatalf("packet %d went backwards: %v after %v", total, pkt.Time, last)
		}
		last = pkt.Time
		total++
	}
	if total != 21 {
		t.Errorf("read %d packets, want 21", total)
	}
}

// A track that declares one constant sample size carries no per-sample size table. It must still
// produce its samples rather than being treated as exhausted immediately.
func TestReadPacketConstantSampleSize(t *testing.T) {
	d := &Demuxer{movieAtom: &mp4io.Movie{}}
	d.streams = []*Stream{testStream(d, 0, 1000, 7, 100, false)}

	counts := readAll(t, d)
	if counts[0] != 7 {
		t.Errorf("stream 0 produced %d packets, want 7", counts[0])
	}
}

// An empty stream list is an error, not an EOF, and a demuxer whose streams are all exhausted
// keeps reporting EOF rather than panicking.
func TestReadPacketExhaustedIsStable(t *testing.T) {
	d := &Demuxer{movieAtom: &mp4io.Movie{}}
	d.streams = []*Stream{testStream(d, 0, 1000, 2, 100, true)}

	readAll(t, d)
	for range 3 {
		if _, err := d.ReadPacket(context.Background(), true); err != io.EOF {
			t.Fatalf("ReadPacket after exhaustion = %v, want io.EOF", err)
		}
	}
}
