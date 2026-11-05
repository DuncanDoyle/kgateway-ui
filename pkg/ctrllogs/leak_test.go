package ctrllogs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/app"
	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/collect"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/ctrllogs"
	"github.com/DuncanDoyle/kgateway-ui/pkg/extension"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/proxystats"
)

const (
	secret = "AKIAFAKEKEY123456"
	// lower is the secret as a valid Kubernetes name, so the object filter itself is tested.
	lower = "akiafakekey123456"
	// known is a cluster name the nack Gateway's proxy stats report.
	known = "kube_shop_httpbin-nack_8000"
)

// xdsLine is a kgateway "xds error" line with the given fields.
func xdsLine(sec int, gwNS, gwName, resource, errText string) string {
	b, _ := json.Marshal(map[string]string{"time": fmt.Sprintf("2026-11-05T09:00:%02dZ", sec), "level": "warn", "msg": "xds error",
		"component": "xds/envoy", "gateway_name": gwName, "gateway_ns": gwNS, "resource": resource, "error": errText})
	return string(b) + "\n"
}

func deferralLine(sec int, client string) string {
	b, _ := json.Marshal(map[string]string{"time": fmt.Sprintf("2026-11-05T09:01:%02dZ", sec), "level": "info",
		"msg": "per-client inputs not ready; deferring snapshot", "component": "proxy_syncer", "client": client})
	return string(b) + "\n"
}

func deployerLine(sec int, key, errText string) string {
	b, _ := json.Marshal(map[string]string{"level": "error", "time": fmt.Sprintf("2026-11-05T09:02:%02d.000001Z", sec), "scope": "controllers",
		"msg": "error handling " + key + ", retrying (retry count: 1): " + errText, "controller": "GatewayController"})
	return string(b) + "\n"
}

const (
	cds = "envoy.config.cluster.v3.Cluster"
	rds = "envoy.config.route.v3.RouteConfiguration"
	lds = "envoy.config.listener.v3.Listener"
)

// followBody puts the secret in every capture position of every line kind, and
// behind spoofed markers. Each kind also has one line whose fields are known,
// so the test sees the reader keep what it may.
func followBody() string {
	s := secret
	lines := []string{
		// The cluster slot: one name is known, the secret is not.
		xdsLine(1, "infra", "nack", cds, "Error adding/updating cluster(s) "+s+": Failed to initialize cipher suites "+s+", "+known+": Failed to load private key "+s),
		// The listener slot: LDS keeps only the class (R102).
		xdsLine(2, "infra", "nack", lds, "Error adding/updating listener(s) "+s+": cannot bind '"+s+"': Address already in use\n"),
		// Both route slots.
		xdsLine(3, "infra", "nack", rds, "Unexpected RDS configuration (expecting "+s+"): "+s),
		// The Gateway and type fields.
		xdsLine(4, "infra", s, cds, "x"),
		xdsLine(5, s, "nack", cds, "x"),
		xdsLine(9, "infra", lower, cds, "x"),
		xdsLine(6, "infra", "nack", s, "x"),
		xdsLine(7, "infra", "nack", "envoy.config.cluster.v3."+s, "x"),
		// The deferral's client, in its namespace and name slots.
		deferralLine(1, "kgateway-kube-gateway-api~infra~edge~1~infra"),
		deferralLine(2, "kgateway-kube-gateway-api~infra~"+s+"~1~infra"),
		deferralLine(3, "kgateway-kube-gateway-api~"+s+"~edge~1~"+s),
		deferralLine(4, "kgateway-kube-gateway-api~infra~"+lower+"~1~infra"),
		// The deployer's Gateway key and the applied object's group, kind, namespace and name.
		deployerLine(1, "infra/edge", "failed to apply object apps/v1, Kind=Deployment infra/edge: Deployment.apps \"edge\" is invalid: "+s),
		deployerLine(2, "infra/"+s, "no valid ports"),
		deployerLine(3, "infra/edge", "failed to apply object apps/v1, Kind=Deployment infra/"+s+": "+s),
		deployerLine(7, "infra/edge", "failed to apply object apps/v1, Kind=Deployment "+lower+"/"+lower+": x"),
		deployerLine(4, "infra/edge", "failed to apply object "+s+"/v1, Kind=Deployment "+s+"/edge: x"),
		deployerLine(5, "infra/edge", "failed to apply object apps/v1, Kind="+s+" infra/edge: x"),
		// Spoofed markers: inside a quoted value, in a JSON key, two on one line, a repeated msg key, the text format.
		`{"time":"2026-11-05T09:03:00Z","msg":"reconciling","note":"xds error per-client inputs not ready; deferring snapshot ` + s + `"}` + "\n",
		`{"time":"2026-11-05T09:03:01Z","msg":"other","xds error":"` + s + `","panic: ":"` + s + `","error handling infra/edge, x":"` + s + `"}` + "\n",
		deployerLine(6, "infra/edge", "xds error per-client inputs not ready; deferring snapshot "+s),
		`{"time":"2026-11-05T09:03:02Z","msg":"xds error","component":"xds/envoy","msg":"other","gateway_name":"nack","gateway_ns":"infra","resource":"` + cds + `","error":"Error adding/updating cluster(s) ` + s + `: x"}` + "\n",
		`time=2026-11-05T09:03:03Z level=WARN msg="xds error" gateway_name=` + s + ` error="` + s + `"` + "\n",
		// A panic on the running container's stream, with the secret in its frame slot.
		"panic: " + s + "\n\ngoroutine 1 [running]:\ngithub.com/" + s + ".F()\n\tgithub.com/" + s + "/f.go:1 +0x1\n",
	}
	return strings.Join(lines, "")
}

// previousBody is the crashed container's tail: the secret in the panic
// message, a frame forged inside it, the frame's arguments and file, and in
// a frame past the window. The known top frame is kept.
func previousBody() string {
	s := secret
	return xdsLine(8, "infra", "nack", cds, "Error adding/updating cluster(s) "+s+": x") +
		"panic: " + s + " [recovered]\n\tpanic: " + s + "\n\tgoroutine 9 [running]:\n\t" + s + ".Forged()\n\n" +
		"goroutine 412 [running]:\n" +
		"github.com/kgateway-dev/kgateway/v2/pkg/deployer.(*Deployer).DeployObjs(0x" + s + ", {0x1, 0x2})\n" +
		"\tgithub.com/kgateway-dev/kgateway/v2/pkg/" + s + "/deployer.go:336 +0x2a4\n" +
		"panic: " + s + "\n\tl\n\tl\n\tl\n\tl\n\tl\n\tl\n\tl\n\ngoroutine 1 [running]:\ngithub.com/" + s + ".Late()\n\tf.go:1\n"
}

// capturedLog collects the UI's own slog output.
type capturedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *capturedLog) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *capturedLog) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// probe is a check that keeps the latest controller log value it reads.
type probe struct{ v atomic.Value }

func (*probe) Name() string { return "leak-probe" }
func (*probe) Inputs() health.InputSet {
	return health.InputSet{Collected: []string{ctrllogs.Key}}
}
func (p *probe) Run(ctx health.RunContext) ([]health.Finding, health.CheckResult) {
	if v, ok := ctx.Collected(ctrllogs.Key); ok {
		p.v.Store(v)
	}
	return nil, health.CheckResult{Name: "leak-probe", OK: true}
}

func (p *probe) logs() (ctrllogs.Logs, bool) {
	v, ok := p.v.Load().(collect.Value)
	if !ok {
		return nil, false
	}
	l, ok := v.Data.(ctrllogs.Logs)
	return l, ok
}

// stats reports known for nack's proxy and listener~8080 for both, in Envoy's text format.
type stats struct{}

func (stats) Targets(_ install.Resolved, gw objects.Ref, _ *cache.Snapshot) ([]proxystats.Target, bool) {
	return []proxystats.Target{{ID: "proxy-" + gw.Name, Name: gw.Name + "-0", Gateway: gw, Addr: "proxy"}}, true
}

func (stats) Fetch(_ context.Context, t proxystats.Target) ([]byte, error) {
	body := "control_plane.connected_state: 1\nserver.state: 0\nhttp.listener~8080.rds.listener~8080.update_rejected: 0\n"
	if t.Gateway.Name == "nack" {
		body += "cluster." + known + ".update_rejected: 3\n"
	}
	return []byte(body), nil
}

func (stats) Interpret(t proxystats.Target, body []byte) (proxystats.PodStats, error) {
	return proxystats.Kube{}.Interpret(t, body)
}

// walk calls f with every string reachable from v, unexported fields included.
func walk(v reflect.Value, seen map[uintptr]bool, f func(string)) {
	switch v.Kind() {
	case reflect.String:
		f(v.String())
	case reflect.Pointer:
		if v.IsNil() || seen[v.Pointer()] {
			return
		}
		seen[v.Pointer()] = true
		walk(v.Elem(), seen, f)
	case reflect.Interface:
		if !v.IsNil() {
			walk(v.Elem(), seen, f)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			walk(v.Field(i), seen, f)
		}
	case reflect.Slice:
		if v.IsNil() {
			return
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			f(string(v.Bytes()))
			return
		}
		fallthrough
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walk(v.Index(i), seen, f)
		}
	case reflect.Map:
		if v.IsNil() || seen[v.Pointer()] {
			return
		}
		seen[v.Pointer()] = true
		it := v.MapRange()
		for it.Next() {
			walk(it.Key(), seen, f)
			walk(it.Value(), seen, f)
		}
	}
}

// get returns the body of a GET to path.
func get(t *testing.T, srv *httptest.Server, path string) []byte {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The privacy gate (D-037, review finding 5): the secret in every capture
// position of every marker, and behind spoofed markers, on the running and
// the previous container, through the real app with both rule sets. It is in
// no API body, nowhere in the published model or the controller log value,
// and not in the UI's own logs, while the fields the reader may keep arrive.
func TestLogSecretLeavesNoTrace(t *testing.T) {
	logs := &capturedLog{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	slog.Info("leak test capture")

	for _, version := range []string{"v2.3.11", "v2.4.7"} {
		t.Run(version, func(t *testing.T) {
			d := testutil.ControllerDeployment("kgw", "kgateway", "cr.kgateway.dev/kgateway-dev/kgateway:"+version, 1, 1)
			ctrl := testutil.ControllerPod(d, "kgateway-0", true)
			ctrl.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "kgateway", RestartCount: 1, Ready: true}}
			programmed := []metav1.Condition{testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}
			dyn, kube := testutil.Clients(t, testutil.Namespace("kgw", nil), testutil.Namespace("infra", nil), d, ctrl,
				testutil.ProxyDeployment("infra", "edge", 1, 1), testutil.ProxyDeployment("infra", "nack", 1, 1),
				testutil.GatewayClass("kgateway", testutil.Controller, true),
				testutil.Gateway("infra", "edge", "kgateway", programmed), testutil.Gateway("infra", "nack", "kgateway", programmed))
			var follows atomic.Int32
			kube.PrependReactor("get", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
				if a.GetSubresource() != "log" {
					return false, nil, nil
				}
				o := a.(k8stesting.GenericAction).GetValue().(*corev1.PodLogOptions)
				body := ""
				switch {
				case o.Previous:
					body = previousBody()
				case follows.Add(1) == 1:
					body = followBody()
				}
				return true, &runtime.Unknown{Raw: []byte(body)}, nil
			})
			p := &probe{}
			a, err := app.Build(app.Clients{Dynamic: dyn, Kube: kube, Discovery: kube.Discovery(), Scraper: emptyScraper{}},
				app.Options{PollInterval: 20 * time.Millisecond, MetricsInterval: time.Hour, UI: fstest.MapFS{"index.html": {Data: []byte("x")}},
					Diagnostics: app.Diagnostics{ControllerLogs: true}},
				extension.New(extension.WithProxyStatsSource(stats{}), extension.WithChecks("LEAK-", p)))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			a.Start(ctx)
			srv := httptest.NewServer(a.Handler)
			t.Cleanup(srv.Close)

			// The reader keeps what it may: the known name, the deferral, the
			// deployer's object, the panic's top frame. Only then is absence meaningful.
			const install = "kgw.kgateway"
			var got ctrllogs.InstallLogs
			deadline := time.Now().Add(20 * time.Second)
			for !kept(got) {
				if time.Now().After(deadline) {
					t.Fatalf("the reader never kept the known fields: %+v", got)
				}
				time.Sleep(20 * time.Millisecond)
				l, _ := p.logs()
				got = l[install]
			}

			bodies := map[string][]byte{}
			for _, path := range []string{"/api/v1/meta", "/api/v1/installs"} {
				bodies[path] = get(t, srv, path)
			}
			base := "/api/v1/installs/" + install
			for _, sub := range []string{"/overview", "/health", "/findings", "/diagnostics", "/processes", "/routes", "/gateways",
				"/listenersets", "/gateways/infra/edge", "/gateways/infra/nack", "/refs",
				"/resources/gateway.networking.k8s.io/Gateway/infra/edge", "/resources/gateway.networking.k8s.io/Gateway/infra/nack"} {
				bodies[base+sub] = get(t, srv, base+sub)
			}
			var findings api.HealthResponse
			if err := json.Unmarshal(bodies[base+"/health"], &findings); err != nil {
				t.Fatal(err)
			}
			for _, f := range findings.Findings {
				bodies[base+"/findings/"+f.ID] = get(t, srv, base+"/findings/"+f.ID)
			}
			if !bytes.Contains(bodies["/api/v1/meta"], []byte(`"name":"controllerLogs","state":"on"`)) {
				t.Fatalf("controllerLogs is not on: %s", bodies["/api/v1/meta"])
			}
			for path, b := range bodies {
				if bytes.Contains(b, []byte(secret)) || bytes.Contains(b, []byte(lower)) {
					t.Errorf("the secret is in %s", path)
				}
			}
			check := func(what string, v any) {
				walk(reflect.ValueOf(v), map[uintptr]bool{}, func(s string) {
					if strings.Contains(s, secret) || strings.Contains(s, lower) {
						t.Errorf("the secret is in the %s: %.200q", what, s)
					}
				})
			}
			check("published model", a.Engine.Current())
			l, _ := p.logs()
			check("controller log value", l)
			if s := fmt.Sprintf("%+v", l); strings.Contains(s, secret) || strings.Contains(s, lower) {
				t.Errorf("the secret is in the controller log value: %s", s)
			}
		})
	}
	out := logs.String()
	if !strings.Contains(out, "leak test capture") {
		t.Fatal("the UI's slog output is not captured")
	}
	if strings.Contains(out, secret) || strings.Contains(out, lower) {
		t.Fatalf("the secret is in the UI's own logs:\n%s", out)
	}
}

// kept reports whether every field the reader may keep from the bodies arrived.
func kept(l ctrllogs.InstallLogs) bool {
	var name, deferral, object, frame bool
	for _, e := range l.Events {
		switch {
		case e.Marker == "xds error" && slices.Contains(e.Names, known) && e.Class == computed.LogClassTLS:
			name = true
		case e.Marker == "per-client inputs not ready; deferring snapshot" && e.Gateway.Name == "edge":
			deferral = true
		case e.Marker == "error handling " && e.Object != nil && e.Object.Kind == "Deployment" && e.Object.Name == "edge":
			object = true
		case e.Class == computed.LogClassPanic && e.Frame == "github.com/kgateway-dev/kgateway/v2/pkg/deployer.(*Deployer).DeployObjs":
			frame = true
		}
	}
	return l.State == ctrllogs.StateOn && name && deferral && object && frame
}

// emptyScraper serves no controller metrics.
type emptyScraper struct{}

func (emptyScraper) Scrape(context.Context, *corev1.Pod, int32) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
