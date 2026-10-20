package media

import (
	"strings"
	"testing"
	"time"
)

func TestReadCapabilityIsScopedReadOnlyAndExpires(t *testing.T) {
	now := time.Now()
	token := strings.Repeat("secret", 8)
	hash := strings.Repeat("a", 64)
	grant, e := MintRead(token, "A1", "video", 7, hash, now.Add(time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	if !VerifyRead(grant, token, "A1", "video", 7, hash, now) {
		t.Fatal("valid capability rejected")
	}
	for _, test := range []struct {
		node, video, hash, token, grant string
		extent                          uint64
		when                            time.Time
	}{
		{"B1", "video", hash, token, grant, 7, now}, {"A1", "other", hash, token, grant, 7, now}, {"A1", "video", strings.Repeat("b", 64), token, grant, 7, now}, {"A1", "video", hash, token + "x", grant, 7, now}, {"A1", "video", hash, token, grant + "a", 7, now}, {"A1", "video", hash, token, grant, 8, now}, {"A1", "video", hash, token, grant, 7, now.Add(2 * time.Minute)},
	} {
		if VerifyRead(test.grant, test.token, test.node, test.video, test.extent, test.hash, test.when) {
			t.Fatal("capability escaped scope")
		}
	}
}
