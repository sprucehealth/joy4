package sdp

import (
	"bytes"
	"testing"

	"github.com/sprucehealth/joy4/av"
)

const liveMediaSDP = `v=0
o=- 1459325504777324 1 IN IP4 192.168.0.123
s=RTSP/RTP stream from Network Video Server
i=mpeg4cif
t=0 0
a=tool:LIVE555 Streaming Media v2009.09.28
a=type:broadcast
a=control:*
a=range:npt=0-
a=x-qt-text-nam:RTSP/RTP stream from Network Video Server
a=x-qt-text-inf:mpeg4cif
m=video 0 RTP/AVP 96
c=IN IP4 0.0.0.0
b=AS:300
a=rtpmap:96 H264/90000
a=fmtp:96 profile-level-id=420029; packetization-mode=1; sprop-parameter-sets=Z00AHpWoKA9k,aO48gA==
a=x-dimensions: 720, 480
a=x-framerate: 15
a=control:track1
m=audio 0 RTP/AVP 96
c=IN IP4 0.0.0.0
b=AS:256
a=rtpmap:96 MPEG4-GENERIC/16000/2
a=fmtp:96 streamtype=5;profile-level-id=1;mode=AAC-hbr;sizelength=13;indexlength=3;indexdeltalength=3;config=1408
a=control:track2
m=audio 0 RTP/AVP 0
c=IN IP4 0.0.0.0
b=AS:50
a=recvonly
a=control:rtsp://109.195.127.207:554/mpeg4cif/trackID=2
a=rtpmap:0 PCMU/8000
a=Media_header:MEDIAINFO=494D4B48010100000400010010710110401F000000FA000000000000000000000000000000000000;
a=appversion:1.0
`

func TestParse(t *testing.T) {
	_, medias := Parse(liveMediaSDP)

	if len(medias) != 3 {
		t.Fatalf("parsed %d medias, want 3", len(medias))
	}

	video := medias[0]
	if video.AVType != "video" {
		t.Errorf("video AVType = %q, want %q", video.AVType, "video")
	}
	if video.Type != av.H264 {
		t.Errorf("video Type = %v, want %v", video.Type, av.H264)
	}
	if video.TimeScale != 90000 {
		t.Errorf("video TimeScale = %d, want 90000", video.TimeScale)
	}
	if video.PayloadType != 96 {
		t.Errorf("video PayloadType = %d, want 96", video.PayloadType)
	}
	if video.Rtpmap != 96 {
		t.Errorf("video Rtpmap = %d, want 96", video.Rtpmap)
	}
	// The per-media control attribute must win over the session level "a=control:*".
	if video.Control != "track1" {
		t.Errorf("video Control = %q, want %q", video.Control, "track1")
	}
	// sprop-parameter-sets carries a base64 SPS and PPS, comma separated.
	wantSets := [][]byte{
		{0x67, 0x4d, 0x00, 0x1e, 0x95, 0xa8, 0x28, 0x0f, 0x64},
		{0x68, 0xee, 0x3c, 0x80},
	}
	if len(video.SpropParameterSets) != len(wantSets) {
		t.Fatalf("got %d parameter sets, want %d", len(video.SpropParameterSets), len(wantSets))
	}
	for i, want := range wantSets {
		if !bytes.Equal(video.SpropParameterSets[i], want) {
			t.Errorf("parameter set %d = %x, want %x", i, video.SpropParameterSets[i], want)
		}
	}

	aac := medias[1]
	if aac.AVType != "audio" {
		t.Errorf("aac AVType = %q, want %q", aac.AVType, "audio")
	}
	if aac.Type != av.AAC {
		t.Errorf("aac Type = %v, want %v", aac.Type, av.AAC)
	}
	if aac.TimeScale != 16000 {
		t.Errorf("aac TimeScale = %d, want 16000", aac.TimeScale)
	}
	if aac.Control != "track2" {
		t.Errorf("aac Control = %q, want %q", aac.Control, "track2")
	}
	if want := []byte{0x14, 0x08}; !bytes.Equal(aac.Config, want) {
		t.Errorf("aac Config = %x, want %x", aac.Config, want)
	}
	if aac.SizeLength != 13 {
		t.Errorf("aac SizeLength = %d, want 13", aac.SizeLength)
	}
	if aac.IndexLength != 3 {
		t.Errorf("aac IndexLength = %d, want 3", aac.IndexLength)
	}

	// PCMU is not a codec this package maps, but the rest of the media must
	// still parse, including a control attribute that is an absolute URL.
	pcmu := medias[2]
	if pcmu.AVType != "audio" {
		t.Errorf("pcmu AVType = %q, want %q", pcmu.AVType, "audio")
	}
	if pcmu.PayloadType != 0 {
		t.Errorf("pcmu PayloadType = %d, want 0", pcmu.PayloadType)
	}
	if pcmu.TimeScale != 8000 {
		t.Errorf("pcmu TimeScale = %d, want 8000", pcmu.TimeScale)
	}
	if want := "rtsp://109.195.127.207:554/mpeg4cif/trackID=2"; pcmu.Control != want {
		t.Errorf("pcmu Control = %q, want %q", pcmu.Control, want)
	}
}

func TestParseSessionURI(t *testing.T) {
	sess, medias := Parse("v=0\nu=rtsp://example.com/stream\nm=video 0 RTP/AVP 96\na=rtpmap:96 H264/90000\n")
	if sess.Uri != "rtsp://example.com/stream" {
		t.Errorf("Uri = %q, want %q", sess.Uri, "rtsp://example.com/stream")
	}
	if len(medias) != 1 {
		t.Fatalf("parsed %d medias, want 1", len(medias))
	}
}

// TestParseMalformed checks that garbage input returns rather than panics.
func TestParseMalformed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"blank lines", "\n\n\n"},
		{"bare equals", "="},
		{"empty attribute", "a="},
		{"empty media", "m="},
		{"media type only", "m=video"},
		{"media missing payload type", "m=video 0"},
		{"attribute before any media", "a=rtpmap:96 H264/90000"},
		{"truncated media then attribute", "m=video 0\na=control:track"},
		{"unparsable fmtp values", "m=video 0 RTP/AVP 96\na=fmtp:96 config=zz;sizelength=x"},
		{"rtpmap missing clock rate", "m=audio 0 RTP/AVP 96\na=rtpmap:96 H264"},
		{"no trailing newline", "m=video 0 RTP/AVP 96"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			Parse(tc.content)
		})
	}
}
