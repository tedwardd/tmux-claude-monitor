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

	"github.com/godbus/dbus/v5"

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

	usr1 := make(chan os.Signal, 1)
	signal.Notify(usr1, syscall.SIGUSR1)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	resume := make(chan struct{}, 1)
	startResumeWatcher(resume)

	consecutiveFails := 0
	var ticker *time.Ticker

	fetch := func() {
		usage, err := api.FetchUsage(creds.AccessToken)
		p := cache.Path(cfg.CachePath)
		if err != nil {
			cache.WriteToPath(p, cache.Entry{FetchedAt: time.Now().UTC(), Error: err.Error()})
			backoff := time.Duration(30*(1<<consecutiveFails)) * time.Second
			if backoff > 300*time.Second {
				backoff = 300 * time.Second
			}
			consecutiveFails++
			if ticker != nil {
				ticker.Reset(backoff)
			}
			return
		}
		consecutiveFails = 0
		if ticker != nil {
			ticker.Reset(time.Duration(cfg.PollIntervalSeconds) * time.Second)
		}
		cache.WriteToPath(p, cache.Entry{
			FetchedAt:          time.Now().UTC(),
			SessionUtilization: usage.SessionUtilization,
			SessionResetsAt:    usage.SessionResetsAt,
			WeeklyUtilization:  usage.WeeklyUtilization,
			WeeklyResetsAt:     usage.WeeklyResetsAt,
			ExtraUsageEnabled:  usage.ExtraUsageEnabled,
			ExtraUsedDollars:   usage.ExtraUsedDollars,
			ExtraLimitDollars:  usage.ExtraLimitDollars,
			ExtraUtilization:   usage.ExtraUtilization,
		})
	}

	fetch() // immediate fetch on start

	if cfg.PollIntervalSeconds <= 0 {
		cfg.PollIntervalSeconds = 300
	}
	ticker = time.NewTicker(time.Duration(cfg.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fetch()
		case <-usr1:
			fetch()
		case <-resume:
			time.Sleep(5 * time.Second) // let network reconnect after wake
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

// startResumeWatcher tries to use the D-Bus login1 PrepareForSleep signal for
// immediate wake detection. Falls back to clock-drift polling if unavailable.
func startResumeWatcher(resume chan<- struct{}) {
	if tryDBusResumeWatcher(resume) {
		return
	}
	go watchSleep(resume)
}

func tryDBusResumeWatcher(resume chan<- struct{}) bool {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return false
	}
	err = conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
		dbus.WithMatchMember("PrepareForSleep"),
	)
	if err != nil {
		conn.Close()
		return false
	}
	signals := make(chan *dbus.Signal, 1)
	conn.Signal(signals)
	go func() {
		defer conn.Close()
		for sig := range signals {
			if len(sig.Body) == 0 {
				continue
			}
			sleeping, ok := sig.Body[0].(bool)
			if ok && !sleeping {
				select {
				case resume <- struct{}{}:
				default:
				}
			}
		}
	}()
	return true
}

// watchSleep is the fallback resume detector used when D-Bus is unavailable.
// It detects wake by comparing wall-clock elapsed time against monotonic elapsed
// time — the monotonic clock is paused during sleep, the wall clock is not.
func watchSleep(resume chan<- struct{}) {
	const checkInterval = 30 * time.Second
	const sleepThreshold = 15 * time.Second
	prev := time.Now()
	for {
		time.Sleep(checkInterval)
		now := time.Now()
		monotonicElapsed := now.Sub(prev)
		wallElapsed := now.Round(0).Sub(prev.Round(0)) // Round(0) strips monotonic
		if wallElapsed-monotonicElapsed > sleepThreshold {
			select {
			case resume <- struct{}{}:
			default:
			}
		}
		prev = now
	}
}
