//go:build !linux

package cmd

// startResumeWatcher detects wake by clock drift. The login1 D-Bus signal used
// on Linux has no equivalent here that works without cgo.
func startResumeWatcher(resume chan<- struct{}) {
	go watchSleep(resume)
}
