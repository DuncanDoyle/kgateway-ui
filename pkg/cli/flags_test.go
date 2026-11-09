package cli

import (
	"flag"
	"testing"
	"time"
)

// The defaults are the stock binary's; class-only installs are off unless asked for.
func TestParseDefaultsAndClassOnly(t *testing.T) {
	kc, o, err := Parse(flag.NewFlagSet("t", flag.ContinueOnError), nil)
	if err != nil {
		t.Fatal(err)
	}
	if kc != "" || o.PollInterval != 30*time.Second || o.MetricsInterval != 10*time.Second || o.DelegationEdgeCap != 0 || o.ClassOnlyInstalls ||
		o.Diagnostics.NoEvents || o.Diagnostics.NoCRDs || o.Diagnostics.NoProxyStats || o.Diagnostics.NoEndpoints || o.Diagnostics.ControllerLogs || o.UI == nil {
		t.Fatalf("defaults %+v", o)
	}
	kc, o, err = Parse(flag.NewFlagSet("t", flag.ContinueOnError), []string{"--kubeconfig=/k", "--class-only-installs", "--diagnostics-events=false", "--install=kgw/kgateway"})
	if err != nil || kc != "/k" || !o.ClassOnlyInstalls || !o.Diagnostics.NoEvents || o.Pinned != "kgw/kgateway" {
		t.Fatalf("parsed %q %+v, %v", kc, o, err)
	}
}
