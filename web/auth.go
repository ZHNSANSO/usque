package web

import (
	"net/http"

	"github.com/Diniboy1123/usque/config"
	"golang.org/x/crypto/bcrypt"
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

// HashPassword generates a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

// CheckPasswordHash compares a password with a hash.
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
