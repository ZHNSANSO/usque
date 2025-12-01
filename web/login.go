package web

import (
	"net/http"
	"time"

	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
)

// Login serves the login page.
func Login(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, r, "login.html", nil)
}

// LoginFunc handles the login form submission.
func LoginFunc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == config.AppConfig.Username && internal.CheckPasswordHash(password, config.AppConfig.Password) {
		// Set a simple session cookie
		http.SetCookie(w, &http.Cookie{
			Name:    "usque_session",
			Value:   "valid",
			Expires: time.Now().Add(24 * time.Hour),
		})
		http.Redirect(w, r, "/", http.StatusFound)
	} else {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
	}
}
