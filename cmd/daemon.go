package cmd

import (
	"errors"
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
	creds, err := api.LoadCredentials(credPath)
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

	if cfg.PollIntervalSeconds <= 0 {
		cfg.PollIntervalSeconds = 300
	}
	normal := time.Duration(cfg.PollIntervalSeconds) * time.Second
	cachePath := cache.Path(cfg.CachePath)

	// consecutiveFails is touched only here, and fetch only ever runs on the
	// poller's fetch goroutine, so it needs no synchronisation.
	consecutiveFails := 0

	fetch := func() time.Duration {
		usage, err := api.FetchUsage(creds.AccessToken)
		if err != nil {
			// Keep the last good reading; the status line tolerates a gap up to the
			// staleness threshold, and blanking it on one bad poll wasted that.
			cache.RecordFailure(cachePath, err)

			// A 429 comes with the server's own wait, which beats guessing and
			// avoids re-tripping the limit. Failures still count so that a
			// rate-limit followed by other errors keeps escalating.
			var limited *api.RateLimitError
			if errors.As(err, &limited) && limited.RetryAfter > 0 {
				consecutiveFails++
				return limited.RetryAfter
			}

			backoff := backoffFor(consecutiveFails)
			consecutiveFails++
			return backoff
		}
		consecutiveFails = 0
		cache.WriteToPath(cachePath, cache.Entry{
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
		return normal
	}

	poller{
		fetch:       fetch,
		interval:    normal,
		resumeGrace: resumeGrace,
		usr1:        usr1,
		quit:        quit,
		resume:      resume,
	}.run()
}

const (
	minBackoff  = 30 * time.Second
	maxBackoff  = 300 * time.Second
	resumeGrace = 5 * time.Second

	// Shifting past this would overflow the duration and hand Ticker.Reset a
	// negative interval, which panics. Reached after roughly five hours offline.
	maxFailShift = 8
)

// backoffFor returns how long to wait after consecutiveFails failed fetches.
func backoffFor(consecutiveFails int) time.Duration {
	if consecutiveFails < 0 {
		return minBackoff
	}
	if consecutiveFails > maxFailShift {
		return maxBackoff
	}
	d := minBackoff << consecutiveFails
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// poller owns the timing loop. Fetches are handed to a separate goroutine so
// that a request in flight can never delay a signal: the daemon used to run
// fetch inline, leaving SIGTERM unread in its channel until the HTTP timeout
// expired, which stalled anything waiting on the process to exit.
type poller struct {
	// fetch performs one fetch and returns the interval to wait before the next.
	fetch       func() time.Duration
	interval    time.Duration
	resumeGrace time.Duration
	usr1        <-chan os.Signal
	quit        <-chan os.Signal
	resume      <-chan struct{}
}

func (p poller) run() {
	trigger := make(chan struct{}, 1)
	next := make(chan time.Duration, 1)

	go func() {
		for range trigger {
			select {
			case next <- p.fetch():
			default:
			}
		}
	}()

	// A full buffer means a fetch is already queued or running, so asking again
	// coalesces instead of stacking up.
	request := func() {
		select {
		case trigger <- struct{}{}:
		default:
		}
	}

	request()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	// Nil until a wake is seen; receiving on a nil channel blocks, which keeps
	// the case inert without a second select.
	var wake <-chan time.Time

	for {
		select {
		case <-ticker.C:
			request()
		case <-p.usr1:
			request()
		case <-p.resume:
			wake = time.After(p.resumeGrace)
		case <-wake:
			wake = nil
			request()
		case d := <-next:
			ticker.Reset(d)
		case <-p.quit:
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
