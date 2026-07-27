//go:build linux

package cmd

import "github.com/godbus/dbus/v5"

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
