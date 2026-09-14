// Package safego starts goroutines that cannot take the process down.
//
// A panic in any goroutine kills the whole program — there is no per-goroutine
// boundary in Go the way there is per-request in an HTTP server. For an
// alerting platform that is the worst failure mode available: one nil map
// inside one rule's worker stops every rule, on every schedule, until somebody
// notices the process is gone. And nobody notices quickly, because the symptom
// of a dead alerting system is silence, which is also the symptom of a quiet
// night.
//
// cron.Recover covers the goroutine cron itself spawns per job. It does not
// cover a goroutine the job then starts, and it does not cover work started
// from an HTTP handler. This does.
package safego

import (
	"log"
	"runtime/debug"
)

// Go runs fn in a new goroutine, logging and containing any panic. name appears
// in the log so the site is identifiable without reading the stack.
func Go(name string, fn func()) {
	go func() {
		defer Recover(name)
		fn()
	}()
}

// Recover is the deferred half, for callers that need to start the goroutine
// themselves — a worker taking a semaphore slot, say, where the release has to
// be deferred in the same frame.
//
// The stack is logged with the panic: the value alone rarely says which of
// several concurrent workers failed, and this is the only record that will
// exist.
func Recover(name string) {
	if rec := recover(); rec != nil {
		log.Printf("[Panic] recovered in %s: %v\n%s", name, rec, debug.Stack())
	}
}
