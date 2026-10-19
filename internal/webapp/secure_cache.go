package webapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"time"
)

func movieProofHash(m Movie) string {
	raw, _ := json.Marshal(struct {
		ID, Owner string
		Revision  uint64
		Public    bool
		Proofs    any
	}{m.ID, m.Owner, m.Revision, m.Public, m.Proofs})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func hashCachedFile(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), e
}
func (s *Server) secureCacheHit(m Movie) bool {
	s.mediaMu.Lock()
	entry, ok := s.secureCache[m.ID]
	s.mediaMu.Unlock()
	if !ok || time.Now().After(entry.Expires) || entry.ProofHash != movieProofHash(m) {
		return false
	}
	digest, e := hashCachedFile(entry.Path)
	return e == nil && digest == entry.ContentHash
}
func (s *Server) rememberSecure(m Movie, path string) {
	digest, e := hashCachedFile(path)
	if e != nil {
		return
	}
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	if s.secureCache == nil {
		s.secureCache = map[string]secureCacheEntry{}
	}
	s.secureCache[m.ID] = secureCacheEntry{Path: path, ContentHash: digest, ProofHash: movieProofHash(m), Expires: time.Now().Add(time.Minute)}
}
