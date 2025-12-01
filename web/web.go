package web

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"

	"github.com/Diniboy1123/usque/config"
)

//go:embed *.html *.js *.css
var staticFS embed.FS

var reloadChan chan<- struct{}

// StartServer starts the web server for a configured system.
func StartServer(rc chan<- struct{}) {
	reloadChan = rc

	staticContent, _ := fs.Sub(staticFS, "web")
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticContent))))

	http.HandleFunc("/login", Login)
	http.HandleFunc("/login/auth", LoginFunc)
	http.HandleFunc("/", Auth(Home))
	http.HandleFunc("/save", Auth(Save))

	log.Println("Starting web server on 0.0.0.0:8080")
	err := http.ListenAndServe("0.0.0.0:8080", nil)
	if err != nil {
		log.Fatalf("Failed to start web server: %v", err)
	}
}

// StartRegistrationServer starts a web server that only allows device registration.
func StartRegistrationServer(rc chan<- struct{}) {
	reloadChan = rc

	staticContent, _ := fs.Sub(staticFS, "web")
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticContent))))

	http.HandleFunc("/register", Register)
	http.HandleFunc("/register/submit", RegisterSubmit)
	// Redirect root to register page
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/register", http.StatusFound)
	})

	log.Println("Starting registration web server on 0.0.0.0:8080")
	// This will block until registration is complete and the server is shut down.
	// A more robust implementation would handle server shutdown gracefully.
	err := http.ListenAndServe("0.0.0.0:8080", nil)
	if err != nil && err != http.ErrServerClosed {
		log.Fatalf("Failed to start registration server: %v", err)
	}
}

// Home serves the main configuration page.
func Home(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, r, "config.html", config.AppConfig)
}

// Register serves the registration page.
func Register(w http.ResponseWriter, r *http.Request) {
	serveTemplate(w, r, "register.html", nil)
}

// serveTemplate parses and executes a template from the embedded filesystem.
func serveTemplate(w http.ResponseWriter, r *http.Request, templateName string, data interface{}) {
	tmpl, err := template.ParseFS(staticFS, templateName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
