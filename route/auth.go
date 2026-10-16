package route

import (
	"crypto/ecdsa"
	"net/http"
	"strings"

	"github.com/IceWhaleTech/CasaOS-Common/external"
	"github.com/IceWhaleTech/CasaOS-Common/utils/jwt"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/localauth"
)

// AuthenticateExtensions applies the same identity requirement to standalone
// APIs on every OS. Network addresses and forwarding headers are not identities.
func AuthenticateExtensions(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !localauth.Authenticated(r) {
			token := r.Header.Get("Authorization")
			if token == "" {
				token = r.URL.Query().Get("token")
			}
			token = strings.TrimPrefix(token, "Bearer ")
			if token == "" {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}
			valid, _, err := jwt.Validate(token, func() (*ecdsa.PublicKey, error) { return external.GetPublicKey(config.CommonInfo.RuntimePath) })
			if err != nil || !valid {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
