package setup_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/DuncanDoyle/snellius/pkg/ctrllogs"
	"github.com/DuncanDoyle/snellius/pkg/ctrlmetrics"
	"github.com/DuncanDoyle/snellius/pkg/engine"
	"github.com/DuncanDoyle/snellius/pkg/extension"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/proxystats"
	"github.com/DuncanDoyle/snellius/pkg/setup"
)

// rejectingScraper serves a controller whose proxies reject infra/edge's listeners.
type rejectingScraper struct{}

func (rejectingScraper) Scrape(context.Context, *corev1.Pod, int32) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(`# TYPE kgateway_envoy_xds_rejects_active gauge
kgateway_envoy_xds_rejects_active{gateway_name="edge",gateway_namespace="infra",type_url="envoy.config.listener.v3.Listener"} 1
# TYPE kgateway_resources_managed gauge
kgateway_resources_managed{namespace="infra",parent="edge",resource="Gateway"} 1
`)), nil
}

// rejected is m's KGWUI-001 on infra/edge as one line, without its since and
// first-seen times; "" when there is none.
func rejected(m *engine.Model) string {
	for _, f := range m.Installs[0].Health.Findings {
		if f.Code == health.CodeXDSRejected {
			return fmt.Sprintf("%s %v %q %q %v", f.ID, f.Resource, f.Message, f.Detail, f.Evidence)
		}
	}
	return ""
}

// E8 (Phase 5 design §7.3): the one-shot sequence, CollectOnce then Publish,
// raises the KGWUI-001 the long-running app raises over the same controller.
// Without Publish, the xDS check reports metrics not collected.
func TestOneShotMetricsMatchTheApp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	objs := world()
	one := oneShotSequence(t, ctx, objs, extension.New(), rejectingScraper{}, true, false).at(t0)
	app := newRunningWith(t, ctx, objs, rejectingScraper{}).first(t)
	got, want := rejected(one), rejected(app)
	if want == "" || got != want {
		t.Fatalf("one-shot 001 %q, app 001 %q", got, want)
	}
	none := oneShotSequence(t, ctx, objs, extension.New(), rejectingScraper{}, false, false).at(t0)
	if r := rejected(none); r != "" {
		t.Fatalf("without Publish: 001 %s", r)
	}
	for _, c := range none.Installs[0].Health.Checks {
		if c.Name == "xDS rejects" && (c.OK || c.Error != "controller metrics not collected yet") {
			t.Fatalf("without Publish: %+v; want not collected", c)
		}
	}
}

// The pack's collector names stay reserved: a downstream collector cannot take them.
func TestBuildReservesThePackCollectorNames(t *testing.T) {
	dyn, kube := fakeClients(t, nil)
	c := setup.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery()}
	for _, name := range []string{ctrlmetrics.CollectorName, proxystats.CollectorName, ctrllogs.CollectorName} {
		_, err := setup.Build(c, setup.Options{MetricsInterval: time.Second}, extension.New(extension.WithCollectors(named(name))))
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("a downstream collector named %q: %v", name, err)
		}
	}
}

// named is a collector that only has a name.
type named string

func (n named) Name() string                              { return string(n) }
func (named) Start(context.Context, extension.Deps) error { return nil }

// bodyScraper serves body, or fails when body is empty.
type bodyScraper struct{ body string }

func (s bodyScraper) Scrape(context.Context, *corev1.Pod, int32) (io.ReadCloser, error) {
	if s.body == "" {
		return nil, errors.New("connection refused")
	}
	return io.NopCloser(strings.NewReader(s.body)), nil
}

// Review 5.2 #4: through extension.New and setup.Build, the kgateway pack's
// Reported hook picks the rule set from kgateway_build_info, keeps it while a
// scrape fails, and falls back to the Deployment's version when a scrape has none.
func TestReportedVersionThroughThePack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	one := oneShotSequence(t, ctx, world(), extension.New(), emptyScraper{}, false, false)
	insts, _ := install.Discover(one.cfg.Engine.Snapshots.Snapshot(), one.cfg.Engine.Detectors)
	managed := "# TYPE kgateway_resources_managed gauge\nkgateway_resources_managed{namespace=\"infra\",parent=\"edge\",resource=\"Gateway\"} 1\n"
	for _, step := range []struct {
		name, body, rules, reported string
	}{
		{"build_info", managed + "# TYPE kgateway_build_info gauge\nkgateway_build_info{version=\"v2.5.0\"} 1\n", "kgateway-2.5", "2.5.0"},
		{"failed scrape", "", "kgateway-2.5", "2.5.0"},
		{"no build_info", managed, "kgateway-2.4", ""},
	} {
		ctrlmetrics.Publish(one.cfg.Collected, ctrlmetrics.CollectOnce(ctx, bodyScraper{step.body}, insts, time.Hour))
		im := one.at(t0).Installs[0]
		if im.Rules.Rules.Version() != step.rules || im.Resolved.ReportedVersion != step.reported {
			t.Errorf("%s: rules %s, reported %q; want %s, %q", step.name, im.Rules.Rules.Version(), im.Resolved.ReportedVersion, step.rules, step.reported)
		}
	}
}
