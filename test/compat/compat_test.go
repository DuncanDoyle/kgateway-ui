//go:build compat

package compat

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestCompat runs Check against the cluster behind COMPAT_UI_URL until it
// passes or three minutes end, covering status writes and finding grace periods.
func TestCompat(t *testing.T) {
	e, err := EnvFromOS()
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{Timeout: 30 * time.Second}
	for _, err := range Settle(context.Background(), c, e, 3*time.Minute, 2*time.Second) {
		t.Error(err)
	}
}
