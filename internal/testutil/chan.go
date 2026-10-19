package testutil

import (
	"testing"
	"time"
)

// RecvTimeout bounds every channel receive in tests.
const RecvTimeout = 10 * time.Second

// Recv returns the next value from ch, failing the test after RecvTimeout.
// Call it only from the test goroutine.
func Recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(RecvTimeout):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

// Drain discards the values buffered in ch without blocking.
func Drain[T any](ch <-chan T) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
