package web

import (
	"net/http"

	"github.com/Diniboy1123/usque/config"
)

// Auth is a middleware that checks for a valid user session.
func Auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if config.AppConfig.Username == "" || config.AppConfig.Password == "" {
			// No username/password set, allow access
			next.ServeHTTP(w, r)
			return
		}

		// For now, we'll use a simple cookie-based session.
		// This will be improved later.
		cookie, err := r.Cookie("usque_session")
		if err != nil || cookie.Value != "valid" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		next.ServeHTTP(w, r)
	}
}
