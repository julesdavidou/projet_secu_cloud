package main

import (
	"crypto/subtle"
	"errors"
	"github.com/julesdavidou/projet_secu_cloud/internal/storage"
	"net/http"
	"os"
	"strings"
)

func registerVerifiedRoutes(mux *http.ServeMux, store *storage.Store) {
	mux.HandleFunc("PUT /v1/verified/{tenant}/{object}", func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("HTTP_AUTH_TOKEN_FILE") == "" {
			http.Error(w, "verified storage requires configured authentication", 503)
			return
		}
		if r.ContentLength <= 0 {
			http.Error(w, "positive content length required", 400)
			return
		}
		digest := r.Header.Get("X-Content-SHA256")
		size, e := store.PutVerified(r.PathValue("tenant"), r.PathValue("object"), r.Body, r.ContentLength, digest)
		if e != nil {
			code := 500
			if errors.Is(e, storage.ErrIntegrity) || errors.Is(e, storage.ErrInvalidID) {
				code = 422
			}
			http.Error(w, e.Error(), code)
			return
		}
		writeJSON(w, 201, map[string]any{"size_bytes": size, "sha256": digest, "verified": true})
	})
}
func storageGuard(next http.Handler) (http.Handler, error) {
	path := os.Getenv("HTTP_AUTH_TOKEN_FILE")
	if path == "" {
		return next, nil
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	token := strings.TrimSpace(string(raw))
	if len(token) < 24 {
		return nil, errors.New("HTTP token too short")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(h, "Bearer ")), []byte(token)) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}
