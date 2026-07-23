package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"claude-monitor/internal/api"
	"claude-monitor/internal/cache"
	"claude-monitor/internal/config"
)

func runDaemon() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: load config: %v\n", err)
		os.Exit(1)
	}

	pidPath := sharedPIDPath()
	if err := writePID(pidPath); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: write PID: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(pidPath)

	credPath := expandHome(cfg.CredentialsPath)
	creds, err := api.ReadCredentials(credPath)
	if err != nil {
		writeErrorCache(cfg, fmt.Sprintf("read credentials: %v", err))
		fmt.Fprintf(os.Stderr, "daemon: %v\n", err)
		os.Remove(pidPath)
		os.Exit(1)
	}

	if cfg.OrgUUID == "" {
		uuid, err := api.FetchBootstrap(creds.AccessToken)
		if err != nil {
			writeErrorCache(cfg, fmt.Sprintf("bootstrap: %v", err))
			fmt.Fprintf(os.Stderr, "daemon: bootstrap: %v\n", err)
			os.Remove(pidPath)
			os.Exit(1)
		}
		cfg.OrgUUID = uuid
		config.Save(cfg)
	}

	usr1 := make(chan os.Signal, 1)
	signal.Notify(usr1, syscall.SIGUSR1)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	consecutiveFails := 0
	var ticker *time.Ticker

	fetch := func() {
		usage, err := api.FetchUsage(creds.AccessToken, cfg.OrgUUID)
		p := cache.Path(cfg.CachePath)
		if err != nil {
			cache.WriteToPath(p, cache.Entry{FetchedAt: time.Now().UTC(), Error: err.Error()})
			if ticker != nil {
				backoff := time.Duration(30*(1<<consecutiveFails)) * time.Second
				if backoff > 300*time.Second {
					backoff = 300 * time.Second
				}
				ticker.Reset(backoff)
			}
			consecutiveFails++
			return
		}
		consecutiveFails = 0
		if ticker != nil {
			ticker.Reset(time.Duration(cfg.PollIntervalSeconds) * time.Second)
		}
		cache.WriteToPath(p, cache.Entry{
			FetchedAt:     time.Now().UTC(),
			MessagesUsed:  usage.MessagesUsed,
			MessagesLimit: usage.MessagesLimit,
			ResetAt:       usage.ResetAt,
		})
	}

	fetch() // immediate fetch on start

	ticker = time.NewTicker(time.Duration(cfg.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fetch()
		case <-usr1:
			fetch()
		case <-quit:
			return
		}
	}
}

func writePID(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600)
}

func writeErrorCache(cfg config.Config, errMsg string) {
	p := cache.Path(cfg.CachePath)
	cache.WriteToPath(p, cache.Entry{
		FetchedAt: time.Now().UTC(),
		Error:     errMsg,
	})
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
