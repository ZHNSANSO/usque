package web

import (
	"html/template"
	"log"
	"net/http"

	"github.com/Diniboy1123/usque/config"
	"github.com/Diniboy1123/usque/internal"
	"github.com/Diniboy1123/usque/internal/registration"
)

// StartRegistrationServer starts a web server that only allows device registration.
func StartRegistrationServer(rc chan<- struct{}) {
	reloadChan = rc
	http.HandleFunc("/register", Register)
	http.HandleFunc("/register/submit", RegisterSubmit)
	// Redirect root to register page
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/register", http.StatusFound)
	})

	log.Println("Starting registration web server on :8080")
	// This will block until registration is complete and the server is shut down.
	// A more robust implementation would handle server shutdown gracefully.
	err := http.ListenAndServe(":8080", nil)
	if err != nil && err != http.ErrServerClosed {
		log.Fatalf("Failed to start registration server: %v", err)
	}
}

// Register serves the registration page.
func Register(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, r, "register.html", nil)
}

// RegisterSubmit handles the registration form submission.
func RegisterSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	deviceName := r.FormValue("deviceName")
	jwt := r.FormValue("jwt")

	if username == "" || password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	// Use default values for model and locale
	model := internal.DefaultModel
	locale := internal.DefaultLocale

	err := registration.RegisterDevice(deviceName, model, locale, jwt, username, password)
	if err != nil {
		log.Printf("Registration failed: %v", err)
		// In a real app, we would render the error on the page.
		http.Error(w, "Registration failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Println("Registration successful. Saving config...")

	err = config.AppConfig.SaveConfig("config.json")
	if err != nil {
		log.Printf("Failed to save config: %v", err)
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Println("Config saved. Triggering reload...")
	reloadChan <- struct{}{}

	// Respond to the user
	w.Write([]byte("Registration successful! The service will now start. You can close this window."))

	// In a real app, we might want to shut down the registration server here.
}
