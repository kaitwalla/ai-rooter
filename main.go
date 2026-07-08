package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var (
	version    = "dev"
	repository = ""
)

func main() {
	addr := flag.String("addr", envOr("ROOTER_ADDR", ":8080"), "HTTP listen address")
	configPath := flag.String("config", envOr("ROOTER_CONFIG", defaultConfigPath()), "settings JSON path")
	showVersion := flag.Bool("version", false, "print version and exit")
	updateNow := flag.Bool("update", false, "update this executable from the latest GitHub Release and exit")
	autoUpdate := flag.Bool("auto-update", false, "check for an update before starting, install it, and exit if updated")
	updateRepo := flag.String("update-repo", envOr("ROOTER_UPDATE_REPO", repository), "GitHub repository for updates, in owner/repo form")
	updateAsset := flag.String("update-asset", envOr("ROOTER_UPDATE_ASSET", defaultUpdateAsset()), "GitHub Release asset name to install")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *updateNow || *autoUpdate {
		updated, err := updateExecutable(*updateRepo, *updateAsset)
		if err != nil {
			if *updateNow {
				log.Fatalf("update: %v", err)
			}
			log.Printf("update skipped: %v", err)
		} else if updated {
			log.Printf("updated to latest release; restart rooter to use the new executable")
			return
		} else if *updateNow {
			log.Printf("already up to date")
			return
		}
	}

	store, err := NewStore(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	app := NewApp(store, os.Getenv("ROOTER_ADMIN_TOKEN"))
	server := &http.Server{
		Addr:    *addr,
		Handler: app.routes(),
	}

	log.Printf("rooter listening on %s", *addr)
	log.Printf("settings: %s", *configPath)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

func updateExecutable(repo, assetName string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return updateFromGitHub(ctx, updateOptions{
		Repository: repo,
		AssetName:  assetName,
		Version:    version,
		Client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	})
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func defaultConfigPath() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "rooter", "config.json")
	}
	return fmt.Sprintf(".%crooter.json", os.PathSeparator)
}
