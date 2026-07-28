package cmd

import (
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackoffFor(t *testing.T) {
	for _, tc := range []struct {
		fails int
		want  time.Duration
	}{
		{0, 30 * time.Second},
		{1, 60 * time.Second},
		{2, 120 * time.Second},
		{3, 240 * time.Second},
		{4, maxBackoff},
		{10, maxBackoff},
		// Shifting by this much used to overflow into a negative duration, which
		// made Ticker.Reset panic after a long stretch offline.
		{62, maxBackoff},
		{1 << 20, maxBackoff},
	} {
		if got := backoffFor(tc.fails); got != tc.want {
			t.Errorf("backoffFor(%d) = %v, want %v", tc.fails, got, tc.want)
		}
	}
}

func TestBackoffForNeverNonPositive(t *testing.T) {
	// Ticker.Reset panics on a non-positive interval, so this must hold for any input.
	for _, fails := range []int{-1, 0, 1, 7, 8, 9, 63, 64, 65, 1 << 30} {
		if d := backoffFor(fails); d <= 0 {
			t.Errorf("backoffFor(%d) = %v, must be positive", fails, d)
		}
	}
}

// newTestPoller wires a poller whose fetch is driven by the returned hooks.
func newTestPoller(fetch func() time.Duration) (poller, chan os.Signal, chan os.Signal, chan struct{}) {
	usr1 := make(chan os.Signal, 1)
	quit := make(chan os.Signal, 1)
	resume := make(chan struct{}, 1)
	return poller{
		fetch:       fetch,
		interval:    time.Hour, // long enough that only explicit triggers fire
		resumeGrace: 10 * time.Millisecond,
		usr1:        usr1,
		quit:        quit,
		resume:      resume,
	}, usr1, quit, resume
}

// runAsync runs the poller and reports when it returns.
func runAsync(p poller) chan struct{} {
	done := make(chan struct{})
	go func() {
		p.run()
		close(done)
	}()
	return done
}

func TestPollerFetchesOnStartup(t *testing.T) {
	fetched := make(chan struct{}, 1)
	p, _, quit, _ := newTestPoller(func() time.Duration {
		fetched <- struct{}{}
		return time.Hour
	})
	done := runAsync(p)

	select {
	case <-fetched:
	case <-time.After(2 * time.Second):
		t.Fatal("no fetch at startup")
	}

	quit <- os.Interrupt
	<-done
}

// The point of moving fetches off the loop: a request in flight must not delay
// shutdown. Previously SIGTERM sat unread until the fetch returned.
func TestPollerQuitsWhileFetchInFlight(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	p, _, quit, _ := newTestPoller(func() time.Duration {
		started <- struct{}{}
		<-release // hold the fetch open, standing in for a hung request
		return time.Hour
	})
	done := runAsync(p)

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("fetch never started")
	}

	quit <- os.Interrupt
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("run did not return while a fetch was in flight")
	}
	close(release)
}

func TestPollerRefetchesOnUsr1(t *testing.T) {
	var calls atomic.Int32
	gate := make(chan struct{}, 8)
	p, usr1, quit, _ := newTestPoller(func() time.Duration {
		calls.Add(1)
		gate <- struct{}{}
		return time.Hour
	})
	done := runAsync(p)

	<-gate // startup fetch
	usr1 <- os.Interrupt
	select {
	case <-gate:
	case <-time.After(2 * time.Second):
		t.Fatal("SIGUSR1 did not trigger a fetch")
	}
	if n := calls.Load(); n < 2 {
		t.Errorf("got %d fetches, want at least 2", n)
	}

	quit <- os.Interrupt
	<-done
}

func TestPollerRefetchesAfterResume(t *testing.T) {
	gate := make(chan struct{}, 8)
	p, _, quit, resume := newTestPoller(func() time.Duration {
		gate <- struct{}{}
		return time.Hour
	})
	done := runAsync(p)

	<-gate // startup fetch
	resume <- struct{}{}
	select {
	case <-gate:
	case <-time.After(2 * time.Second):
		t.Fatal("resume did not trigger a fetch after the grace period")
	}

	quit <- os.Interrupt
	<-done
}

// The resume grace period must not block signal handling either; it used to be a
// time.Sleep inside the select.
func TestPollerQuitsDuringResumeGrace(t *testing.T) {
	p, _, quit, resume := newTestPoller(func() time.Duration { return time.Hour })
	p.resumeGrace = 30 * time.Second
	done := runAsync(p)

	resume <- struct{}{}
	time.Sleep(20 * time.Millisecond) // let the wake timer be armed
	quit <- os.Interrupt

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return during the resume grace period")
	}
}

// Triggers arriving while a fetch runs coalesce rather than queueing up.
func TestPollerCoalescesTriggers(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	p, usr1, quit, _ := newTestPoller(func() time.Duration {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return time.Hour
	})
	done := runAsync(p)
	<-started // startup fetch is now holding

	for i := 0; i < 20; i++ {
		usr1 <- os.Interrupt
	}
	close(release)

	quit <- os.Interrupt
	<-done

	// One in flight plus at most one queued, so far short of 21.
	if n := calls.Load(); n > 3 {
		t.Errorf("got %d fetches from 20 signals, expected coalescing", n)
	}
}

// Jitter must only ever add, so a retry can never come sooner than the backoff
// intended, and Ticker.Reset must never see a non-positive interval.
func TestJitteredStaysWithinBounds(t *testing.T) {
	for _, base := range []time.Duration{minBackoff, 60 * time.Second, maxBackoff, time.Second} {
		for i := 0; i < 500; i++ {
			got := jittered(base)
			if got < base {
				t.Fatalf("jittered(%v) = %v, must not be shorter", base, got)
			}
			if max := base + base/4 + 1; got > max {
				t.Fatalf("jittered(%v) = %v, above the %v ceiling", base, got, max)
			}
			if got <= 0 {
				t.Fatalf("jittered(%v) = %v, must be positive", base, got)
			}
		}
	}
}

// Without spread, clients that failed together retry together and collide on the
// same shared rate limit again.
func TestJitteredActuallyVaries(t *testing.T) {
	seen := map[time.Duration]bool{}
	for i := 0; i < 200; i++ {
		seen[jittered(maxBackoff)] = true
	}
	if len(seen) < 10 {
		t.Errorf("only %d distinct waits from 200 draws; jitter is not spreading retries", len(seen))
	}
}

func TestJitteredLeavesNonPositiveAlone(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		if got := jittered(d); got != d {
			t.Errorf("jittered(%v) = %v, want it returned unchanged for the caller to handle", d, got)
		}
	}
}
