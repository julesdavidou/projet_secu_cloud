package webapp

import (
	"github.com/julesdavidou/projet_secu_cloud/pkg/media"
	"strings"
	"testing"
)

func TestLivePlaylistOrderWindowEndAndSafeVODProfile(t *testing.T) {
	state := media.LiveState{}
	for i := 0; i < 9; i++ {
		state.Chunks = append(state.Chunks, media.LiveChunk{Sequence: uint64(i), LiveEvent: media.LiveEvent{DurationMS: 4000, ProgramMS: int64(1000 + i*4000)}})
	}
	playlist := liveManifest(state)
	if !strings.Contains(playlist, "#EXT-X-MEDIA-SEQUENCE:3") || strings.Contains(playlist, "segments/2/") || strings.Contains(playlist, "#EXT-X-ENDLIST") {
		t.Fatal(playlist)
	}
	state.Ended = true
	if !strings.HasSuffix(liveManifest(state), "#EXT-X-ENDLIST\n") {
		t.Fatal("no end")
	}
	movie := Movie{LayoutVersion: 3, RootID: "l-" + strings.Repeat("a", 32), OperationKind: "live_segment-0", NativeSource: true, Assets: []media.Asset{{Name: "original.mp4"}}}
	if e := validateSignedPlaylist(movie); e != nil {
		t.Fatal(e)
	}
	movie.Playlist = "#EXTM3U\nfile:///etc/passwd\n#EXT-X-ENDLIST"
	if validateSignedPlaylist(movie) == nil {
		t.Fatal("live relaxed playlist safety")
	}
	movie.RootID = ""
	movie.Playlist = ""
	if validateSignedPlaylist(movie) == nil {
		t.Fatal("normal VOD accepted without verified playlist")
	}
}
