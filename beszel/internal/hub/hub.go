// Package hub handles updating systems and serving the web UI.
package hub

import (
	"beszel"
	"beszel/internal/alerts"
	"beszel/internal/hub/systems"
	"beszel/internal/records"
	"beszel/internal/users"
	"beszel/site"
	"crypto/ed25519"
	"encoding/pem"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/crypto/ssh"
)

type Hub struct {
	core.App
	*alerts.AlertManager
	um     *users.UserManager
	rm     *records.RecordManager
	sm     *systems.SystemManager
	pubKey string
	appURL string
}

// NewHub creates a new Hub instance with default configuration
func NewHub(app core.App) *Hub {
	hub := &Hub{}
	hub.App = app

	hub.AlertManager = alerts.NewAlertManager(hub)
	hub.um = users.NewUserManager(hub)
	hub.rm = records.NewRecordManager(hub)
	hub.sm = systems.NewSystemManager(hub)
	hub.appURL, _ = GetEnv("APP_URL")
	return hub
}

// GetEnv retrieves an environment variable with a "BESZEL_HUB_" prefix, or falls back to the unprefixed key.
func GetEnv(key string) (value string, exists bool) {
	if value, exists = os.LookupEnv("BESZEL_HUB_" + key); exists {
		return value, exists
	}
	// Fallback to the old unprefixed key
	return os.LookupEnv(key)
}

func (h *Hub) StartHub() error {

	h.App.OnServe().BindFunc(func(e *core.ServeEvent) error {
		// initialize settings / collections
		if err := h.initialize(e); err != nil {
			return err
		}
		// sync systems with config
		if err := syncSystemsWithConfig(e); err != nil {
			return err
		}
		// register api routes
		if err := h.registerApiRoutes(e); err != nil {
			return err
		}
		// register cron jobs
		if err := h.registerCronJobs(e); err != nil {
			return err
		}
		// start server
		if err := h.startServer(e); err != nil {
			return err
		}
		// start system updates
		if err := h.sm.Initialize(); err != nil {
			return err
		}
		return e.Next()
	})

	// TODO: move to users package
	// handle default values for user / user_settings creation
	h.App.OnRecordCreate("users").BindFunc(h.um.InitializeUserRole)
	h.App.OnRecordCreate("user_settings").BindFunc(h.um.InitializeUserSettings)

	if pb, ok := h.App.(*pocketbase.PocketBase); ok {
		// log.Println("Starting pocketbase")
		err := pb.Start()
		if err != nil {
			return err
		}
	}

	return nil
}

// initialize sets up initial configuration (collections, settings, etc.)
func (h *Hub) initialize(e *core.ServeEvent) error {
	// set general settings
	settings := e.App.Settings()
	// batch requests (for global alerts)
	settings.Batch.Enabled = true
	// set URL if BASE_URL env is set
	if h.appURL != "" {
		settings.Meta.AppURL = h.appURL
	}
	// set auth settings
	usersCollection, err := e.App.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	// disable email auth if DISABLE_PASSWORD_AUTH env var is set
	disablePasswordAuth, _ := GetEnv("DISABLE_PASSWORD_AUTH")
	usersCollection.PasswordAuth.Enabled = disablePasswordAuth != "true"
	usersCollection.PasswordAuth.IdentityFields = []string{"email"}
	// disable oauth if no providers are configured (todo: remove this in post 0.9.0 release)
	if usersCollection.OAuth2.Enabled {
		usersCollection.OAuth2.Enabled = len(usersCollection.OAuth2.Providers) > 0
	}
	// allow oauth user creation if USER_CREATION is set
	if userCreation, _ := GetEnv("USER_CREATION"); userCreation == "true" {
		cr := "@request.context = 'oauth2'"
		usersCollection.CreateRule = &cr
	} else {
		usersCollection.CreateRule = nil
	}
	if err := e.App.Save(usersCollection); err != nil {
		return err
	}
	// allow all users to access systems if SHARE_ALL_SYSTEMS is set
	systemsCollection, err := e.App.FindCachedCollectionByNameOrId("systems")
	if err != nil {
		return err
	}
	shareAllSystems, _ := GetEnv("SHARE_ALL_SYSTEMS")
	systemsReadRule := "@request.auth.id != \"\""
	if shareAllSystems != "true" {
		// default is to only show systems that the user id is assigned to
		systemsReadRule += " && users.id ?= @request.auth.id"
	}
	updateDeleteRule := systemsReadRule + " && @request.auth.role != \"readonly\""
	systemsCollection.ListRule = &systemsReadRule
	systemsCollection.ViewRule = &systemsReadRule
	systemsCollection.UpdateRule = &updateDeleteRule
	systemsCollection.DeleteRule = &updateDeleteRule
	if err := e.App.Save(systemsCollection); err != nil {
		return err
	}
	return nil
}

// startServer starts the server for the Beszel (not PocketBase)
func (h *Hub) startServer(se *core.ServeEvent) error {
	// TODO: exclude dev server from production binary
	switch h.IsDev() {
	case true:
		proxy := httputil.NewSingleHostReverseProxy(&url.URL{
			Scheme: "http",
			Host:   "localhost:5173",
		})
		se.Router.GET("/{path...}", func(e *core.RequestEvent) error {
			proxy.ServeHTTP(e.Response, e.Request)
			return nil
		})
	default:
		// parse app url
		parsedURL, err := url.Parse(h.appURL)
		if err != nil {
			return err
		}
		// fix base paths in html if using subpath
		basePath := strings.TrimSuffix(parsedURL.Path, "/") + "/"
		indexFile, _ := fs.ReadFile(site.DistDirFS, "index.html")
		indexContent := strings.ReplaceAll(string(indexFile), "./", basePath)
		indexContent = strings.Replace(indexContent, "{{V}}", beszel.Version, 1)
		// set up static asset serving
		staticPaths := [2]string{"/static/", "/assets/"}
		serveStatic := apis.Static(site.DistDirFS, false)
		// get CSP configuration
		csp, cspExists := GetEnv("CSP")
		// add route
		se.Router.GET("/{path...}", func(e *core.RequestEvent) error {
			// serve static assets if path is in staticPaths
			for i := range staticPaths {
				if strings.Contains(e.Request.URL.Path, staticPaths[i]) {
					e.Response.Header().Set("Cache-Control", "public, max-age=2592000")
					return serveStatic(e)
				}
			}
			if cspExists {
				e.Response.Header().Del("X-Frame-Options")
				e.Response.Header().Set("Content-Security-Policy", csp)
			}
			return e.HTML(http.StatusOK, indexContent)
		})
	}
	return nil
}

// registerCronJobs sets up scheduled tasks
func (h *Hub) registerCronJobs(_ *core.ServeEvent) error {
	// delete old records once every hour
	h.Cron().MustAdd("delete old records", "8 * * * *", h.rm.DeleteOldRecords)
	// create longer records every 10 minutes
	h.Cron().MustAdd("create longer records", "*/10 * * * *", h.rm.CreateLongerRecords)
	return nil
}

// custom api routes
func (h *Hub) registerApiRoutes(se *core.ServeEvent) error {
	// returns public key and version
	se.Router.GET("/api/beszel/getkey", func(e *core.RequestEvent) error {
		info, _ := e.RequestInfo()
		if info.Auth == nil {
			return apis.NewForbiddenError("Forbidden", nil)
		}
		return e.JSON(http.StatusOK, map[string]string{"key": h.pubKey, "v": beszel.Version})
	})
	// check if first time setup on login page
	se.Router.GET("/api/beszel/first-run", func(e *core.RequestEvent) error {
		total, err := h.CountRecords("users")
		return e.JSON(http.StatusOK, map[string]bool{"firstRun": err == nil && total == 0})
	})
	// send test notification
	se.Router.GET("/api/beszel/send-test-notification", h.SendTestNotification)
	// API endpoint to get config.yml content
	se.Router.GET("/api/beszel/config-yaml", h.getYamlConfig)
	// create first user endpoint only needed if no users exist
	if totalUsers, _ := h.CountRecords("users"); totalUsers == 0 {
		se.Router.POST("/api/beszel/create-user", h.um.CreateFirstUser)
	}
	return nil
}

// generates key pair if it doesn't exist and returns private key bytes
func (h *Hub) GetSSHKey() ([]byte, error) {
	dataDir := h.DataDir()
	// check if the key pair already exists
	existingKey, err := os.ReadFile(dataDir + "/id_ed25519")
	if err == nil {
		if pubKey, err := os.ReadFile(h.DataDir() + "/id_ed25519.pub"); err == nil {
			h.pubKey = strings.TrimSuffix(string(pubKey), "\n")
		}
		// return existing private key
		return existingKey, nil
	}

	// Generate the Ed25519 key pair
	pubKey, privKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		// h.Logger().Error("Error generating key pair:", "err", err.Error())
		return nil, err
	}

	// Get the private key in OpenSSH format
	privKeyBytes, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		// h.Logger().Error("Error marshaling private key:", "err", err.Error())
		return nil, err
	}

	// Save the private key to a file
	privateFile, err := os.Create(dataDir + "/id_ed25519")
	if err != nil {
		// h.Logger().Error("Error creating private key file:", "err", err.Error())
		return nil, err
	}
	defer privateFile.Close()

	if err := pem.Encode(privateFile, privKeyBytes); err != nil {
		// h.Logger().Error("Error writing private key to file:", "err", err.Error())
		return nil, err
	}

	// Generate the public key in OpenSSH format
	publicKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return nil, err
	}

	pubKeyBytes := ssh.MarshalAuthorizedKey(publicKey)
	h.pubKey = strings.TrimSuffix(string(pubKeyBytes), "\n")

	// Save the public key to a file
	publicFile, err := os.Create(dataDir + "/id_ed25519.pub")
	if err != nil {
		return nil, err
	}
	defer publicFile.Close()

	if _, err := publicFile.Write(pubKeyBytes); err != nil {
		return nil, err
	}

	h.Logger().Info("ed25519 SSH key pair generated successfully.")
	h.Logger().Info("Private key saved to: " + dataDir + "/id_ed25519")
	h.Logger().Info("Public key saved to: " + dataDir + "/id_ed25519.pub")

	existingKey, err = os.ReadFile(dataDir + "/id_ed25519")
	if err == nil {
		return existingKey, nil
	}
	return nil, err
}
