package webapp

import (
	"errors"
	"regexp"
	"strings"
)

var vodDirective = regexp.MustCompile(`^(#EXT-X-(VERSION|TARGETDURATION|MEDIA-SEQUENCE):[0-9]{1,10}|#EXTINF:[0-9]{1,10}(\.[0-9]{1,10})?,|#EXT-X-PLAYLIST-TYPE:VOD|#EXT-X-INDEPENDENT-SEGMENTS|#EXT-X-ENDLIST)$`)

// validateSignedPlaylist accepts only the local, unencrypted VOD profile emitted
// by the existing adapter. File protocol access in FFmpeg must never turn an
// owner-supplied playlist into a way to read another video's cache or host files.
func validateSignedPlaylist(m Movie) error {
	if m.LayoutVersion != 3 {
		return nil
	}
	// Live fragments are a single signed standalone init+fragment MP4. They
	// never enter the FFmpeg playlist path. Do not relax normal VOD validation.
	if strings.HasPrefix(m.RootID, "l-") && strings.HasPrefix(m.OperationKind, "live_segment-") {
		if !m.NativeSource || m.Playlist != "" || len(m.Assets) != 1 || m.Assets[0].Name != "original.mp4" {
			return errors.New("single signed live fragment required")
		}
		return nil
	}
	assets := map[string]bool{}
	for _, a := range m.Assets {
		if a.Name == "index.m3u8" || a.Name == "playback.mp4" {
			return errors.New("reserved playback asset name")
		}
		if a.Name != "original.mp4" {
			assets[a.Name] = true
		}
	}
	lines := strings.Split(strings.TrimSpace(m.Playlist), "\n")
	if len(lines) < 4 || strings.TrimSpace(lines[0]) != "#EXTM3U" || strings.TrimSpace(lines[len(lines)-1]) != "#EXT-X-ENDLIST" {
		return errors.New("complete local VOD playlist required")
	}
	segments := 0
	for _, raw := range lines[1:] {
		line := strings.TrimSpace(raw)
		if line == "" || vodDirective.MatchString(line) {
			continue
		}
		if strings.HasPrefix(line, `#EXT-X-MAP:URI="`) && strings.HasSuffix(line, `"`) {
			name := strings.TrimSuffix(strings.TrimPrefix(line, `#EXT-X-MAP:URI="`), `"`)
			if assets[name] {
				continue
			}
		}
		if !strings.HasPrefix(line, "#") && assets[line] {
			segments++
			continue
		}
		return errors.New("playlist may reference only its verified local assets")
	}
	if segments == 0 {
		return errors.New("playlist has no local segments")
	}
	return nil
}
