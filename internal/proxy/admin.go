package proxy

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// AdminHandler guards the drain endpoint: POST only, and when token is
// non-empty the request must carry a matching bearer credential.
func (p *Pool) AdminHandler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if token != "" && !bearerMatches(r.Header.Get("Authorization"), token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		p.HandleDrain(w, r)
	})
}

func bearerMatches(header, token string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := sha256.Sum256([]byte(header[len(prefix):]))
	want := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}
