package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowserBoundary(t *testing.T) {
	for _, tc := range []struct {
		host, origin string
		want         int
	}{
		{"localhost:18790", "", 204}, {"127.0.0.1:18790", "http://127.0.0.1:18790", 204},
		{"localhost:18790", "http://evil.test", 403}, {"evil.test:18790", "", 403},
		{"192.168.2.64:18790", "", 403}, {"localhost", "", 403},
		{"localhost:18790", "null", 403},
	} {
		t.Run(tc.host+tc.origin, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://"+tc.host+"/api/videos", nil)
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-Verified-Identity", "admin")
			w := httptest.NewRecorder()
			localOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Verified-Identity") != "" {
					t.Fatal("untrusted identity reached upstream")
				}
				w.WriteHeader(204)
			})).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d", w.Code, tc.want)
			}
		})
	}
}
