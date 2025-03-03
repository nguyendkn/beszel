package main

import (
	"beszel"
	"beszel/internal/hub"
	_ "beszel/migrations"
	"os"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/spf13/cobra"
)

func main() {
	pocketBase := getPocketBase()
	h := hub.NewHub(pocketBase)
	h.Run()
	h.Start()
}

// getPocketBase creates a new PocketBase app with the default config
func getPocketBase() *pocketbase.PocketBase {
	isDev := os.Getenv("ENV") == "dev"

	pocketBase := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir: beszel.AppName + "_data",
		DefaultDev:     isDev,
	})
	pocketBase.RootCmd.Version = beszel.Version
	pocketBase.RootCmd.Use = beszel.AppName
	pocketBase.RootCmd.Short = ""
	// add update command
	pocketBase.RootCmd.AddCommand(&cobra.Command{
		Use:   "update",
		Short: "Update " + beszel.AppName + " to the latest version",
		Run:   hub.Update,
	})

	// enable auto creation of migration files when making collection changes in the Admin UI
	migratecmd.MustRegister(pocketBase, pocketBase.RootCmd, migratecmd.Config{
		Automigrate: isDev,
		Dir:         "../../migrations",
	})

	return pocketBase
}
