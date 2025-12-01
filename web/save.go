package web

import (
	"log"
	"net/http"

	"github.com/Diniboy1123/usque/config"
)

// Save handles the form submission from the config page.
func Save(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	// Update AppConfig with form values
	config.AppConfig.Username = r.FormValue("username")
	config.AppConfig.PrivateKey = r.FormValue("private_key")
	config.AppConfig.EndpointV4 = r.FormValue("endpoint_v4")
	config.AppConfig.EndpointV6 = r.FormValue("endpoint_v6")
	config.AppConfig.EndpointPubKey = r.FormValue("endpoint_pub_key")
	config.AppConfig.License = r.FormValue("license")
	config.AppConfig.ID = r.FormValue("id")
	config.AppConfig.AccessToken = r.FormValue("access_token")
	config.AppConfig.IPv4 = r.FormValue("ipv4")
	config.AppConfig.IPv6 = r.FormValue("ipv6")

	// Handle password change
	newPassword := r.FormValue("password")
	if newPassword != "" {
		hashedPassword, err := HashPassword(newPassword)
		if err != nil {
			http.Error(w, "Failed to hash password", http.StatusInternalServerError)
			return
		}
		config.AppConfig.Password = hashedPassword
	}

	// Save the updated config
	// We need to get the config path from somewhere. For now, we'll hardcode it.
	// This should be improved later.
	err := config.AppConfig.SaveConfig("config.json")
	if err != nil {
		http.Error(w, "Failed to save config", http.StatusInternalServerError)
		return
	}

	log.Println("Configuration saved. Triggering reload...")

	// Trigger a reload
	reloadChan <- struct{}{}

	// Redirect back to the home page
	http.Redirect(w, r, "/", http.StatusFound)
}
