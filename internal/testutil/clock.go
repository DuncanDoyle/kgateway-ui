package testutil

import clocktesting "k8s.io/utils/clock/testing"

// FakeClock returns a fake clock at Now.
func FakeClock() *clocktesting.FakeClock { return clocktesting.NewFakeClock(Now) }
