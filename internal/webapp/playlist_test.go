package webapp

import (
	"strings"
	"testing"

	"github.com/julesdavidou/projet_secu_cloud/pkg/media"
)

func TestSignedPlaylistCannotReadOutsideVerifiedAssets(t *testing.T) {
	valid := "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:3.000000,\nsegment_000.m4s\n#EXT-X-ENDLIST\n"
	m := Movie{LayoutVersion: 3, Playlist: valid, Assets: []media.Asset{{Name: "original.mp4"}, {Name: "init.mp4"}, {Name: "segment_000.m4s"}}}
	if e := validateSignedPlaylist(m); e != nil {
		t.Fatal(e)
	}
	for _, uri := range []string{"/data/cache/other.mp4", "../other.mp4", "file:/run/web-secrets/operator.token", "https://other/video", "data:opaque", "original.mp4"} {
		m.Playlist = strings.Replace(valid, "segment_000.m4s", uri, 1)
		if validateSignedPlaylist(m) == nil {
			t.Fatal("unsafe playlist URI accepted", uri)
		}
	}
	for _, injected := range []string{"#EXT-X-KEY:METHOD=AES-128,URI=\"/run/key\"", "#EXT-X-MAP:URI=\"init.mp4\",BYTERANGE=\"1@0\"", "#EXT-X-MEDIA:TYPE=AUDIO,URI=\"other.mp4\""} {
		m.Playlist = strings.Replace(valid, "#EXT-X-ENDLIST", injected+"\n#EXT-X-ENDLIST", 1)
		if validateSignedPlaylist(m) == nil {
			t.Fatal("unsupported URI directive accepted", injected)
		}
	}
}
