package kgw23

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

const logsDir = "../testdata/kgateway-2.3/logs"

// logLines reads a capture, without its # provenance lines.
func logLines(t *testing.T, name string) [][]byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(logsDir, name))
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	for _, l := range bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n")) {
		if !bytes.HasPrefix(l, []byte("# ")) {
			out = append(out, l)
		}
	}
	return out
}

// parseAll reads lines as the controller log reader does: only marked lines,
// and a window marker gets the lines after it. Errors fail the test.
func parseAll(t *testing.T, lines [][]byte, previous bool) []computed.LogFields {
	t.Helper()
	p := New().(computed.LogParser)
	w := New().(computed.LogWindow)
	var out []computed.LogFields
	for i, l := range lines {
		m := ""
		for _, mk := range p.Markers() {
			if bytes.Contains(l, []byte(mk)) {
				m = mk
				break
			}
		}
		if m == "" {
			continue
		}
		var f computed.LogFields
		var ok bool
		var err error
		if n := w.Window(l, m, previous); n > 0 {
			f, ok, err = w.ParseWindow(l, lines[i+1:min(len(lines), i+1+n)], previous)
		} else {
			f, ok, err = p.Parse(l)
		}
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		if ok {
			out = append(out, f)
		}
	}
	return out
}

func logTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}

func gwRef(ns, name string) objects.Ref {
	return objects.Ref{Group: objects.GatewayAPIGroup, Kind: "Gateway", Namespace: ns, Name: name}
}

const (
	cdsURL = "type.googleapis.com/envoy.config.cluster.v3.Cluster"
	rdsURL = "type.googleapis.com/envoy.config.route.v3.RouteConfiguration"
	ldsURL = "type.googleapis.com/envoy.config.listener.v3.Listener"
)

func check(t *testing.T, got, want []computed.LogFields) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d events, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("event %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

// The NACK fixture's captured "xds error" lines on v2.3.11: the cluster name
// from Envoy's CDS shape, none from the RDS message, a class from the list.
func TestCapturedXDSErrors(t *testing.T) {
	check(t, parseAll(t, logLines(t, "xds-error.txt"), false), []computed.LogFields{
		{Time: logTime("2026-11-05T07:59:11.27382959Z"), Marker: markerXDS, Gateway: gwRef("infra", "edge"), TypeURL: cdsURL,
			Names: []string{"kube_shop_httpbin-nack_8000"}, Class: computed.LogClassTLS},
		{Time: logTime("2026-11-05T07:59:13.265324202Z"), Marker: markerXDS, Gateway: gwRef("infra", "nack"), TypeURL: cdsURL,
			Names: []string{"kube_shop_httpbin-nack_8000"}, Class: computed.LogClassTLS},
		{Time: logTime("2026-11-05T07:59:28.95950381Z"), Marker: markerXDS, Gateway: gwRef("infra", "nack"), TypeURL: rdsURL, Class: computed.LogClassRegex},
	})
}

// The deferral line names the client's Gateway; the "defer building" line before it is not the marker.
func TestCapturedDeferral(t *testing.T) {
	check(t, parseAll(t, logLines(t, "deferral.txt"), false), []computed.LogFields{
		{Time: logTime("2026-11-05T07:59:10.269550712Z"), Marker: markerDeferral, Gateway: gwRef("infra", "edge")},
	})
}

// Gateway reconcile errors: the GatewayClass queue's line is not one, a
// missing GatewayParameters keeps the Gateway, a rejected apply the Deployment.
func TestCapturedDeployerErrors(t *testing.T) {
	check(t, parseAll(t, logLines(t, "deployer.txt"), false), []computed.LogFields{
		{Time: logTime("2026-11-05T08:03:40.936126Z"), Marker: markerDeployer, Gateway: gwRef("infra", "noparams"),
			Object: ptr(gwRef("infra", "noparams")), Class: computed.LogClassParameters},
		{Time: logTime("2026-11-05T08:03:40.959096Z"), Marker: markerDeployer, Gateway: gwRef("infra", "badapply"),
			Object: &objects.Ref{Group: "apps", Kind: "Deployment", Namespace: "infra", Name: "badapply"}, Class: computed.LogClassApply},
	})
}

func ptr[T any](v T) *T { return &v }

// One synthetic line per class: an LDS reject keeps no listener name (R102),
// two clusters both names, the earliest phrase picks the class, and a text no
// class names reads other.
func TestSyntheticClasses(t *testing.T) {
	edge := gwRef("infra", "edge")
	deploy := func(sec int, class string, obj *objects.Ref) computed.LogFields {
		if obj == nil {
			obj = ptr(edge)
		}
		return computed.LogFields{Time: logTime("2026-11-05T09:00:00.000001Z").Add(time.Duration(sec) * time.Second), Marker: markerDeployer,
			Gateway: edge, Object: obj, Class: class}
	}
	xds := func(sec int, url, class string, names ...string) computed.LogFields {
		return computed.LogFields{Time: logTime("2026-11-05T09:00:00Z").Add(time.Duration(sec) * time.Second), Marker: markerXDS,
			Gateway: edge, TypeURL: url, Names: names, Class: class}
	}
	check(t, parseAll(t, logLines(t, "synthetic.txt"), false), []computed.LogFields{
		xds(1, ldsURL, computed.LogClassBind),
		xds(2, cdsURL, computed.LogClassTLS, "kube_shop_secure_443"),
		xds(3, cdsURL, computed.LogClassDuplicate, "kube_shop_a_80", "kube_shop_b_80"),
		xds(4, cdsURL, computed.LogClassValidation, "kube_shop_c_80"),
		xds(5, ldsURL, computed.LogClassUnknownExtension),
		xds(6, rdsURL, computed.LogClassOther, "listener~8080", "listener~9090"),
		xds(7, cdsURL, computed.LogClassOther, "kube_shop_d_80"),
		deploy(8, computed.LogClassRender, nil),
		deploy(9, computed.LogClassNoValidPorts, nil),
		deploy(10, computed.LogClassPrune, nil),
		deploy(11, computed.LogClassStatusUpdate, nil),
		deploy(12, computed.LogClassApply, &objects.Ref{Kind: "Service", Namespace: "infra", Name: "edge"}),
		deploy(13, computed.LogClassNoValidPorts, nil),
	})
}

// A panic is read from the previous container only, with the first frame
// that is not the runtime's; the frames' arguments and files are dropped.
func TestPanicTopFrame(t *testing.T) {
	lines := logLines(t, "panic.txt")
	check(t, parseAll(t, lines, true), []computed.LogFields{
		{Marker: markerPanic, Class: computed.LogClassPanic, Frame: "github.com/kgateway-dev/kgateway/v2/pkg/kgateway/proxy_syncer.snapshotPerClient.func3"},
		{Marker: markerPanic, Class: computed.LogClassPanic, Frame: "github.com/kgateway-dev/kgateway/v2/pkg/deployer.(*Deployer).DeployObjs"},
	})
	if got := parseAll(t, lines, false); len(got) != 0 {
		t.Fatalf("a panic from the current container: %+v", got)
	}
	w := New().(computed.LogWindow)
	raw := []byte("panic: x")
	if n := w.Window(raw, markerPanic, false); n != 0 {
		t.Fatalf("window for the current container = %d", n)
	}
	if n := w.Window(raw, markerPanic, true); n != computed.MaxLogWindow {
		t.Fatalf("window = %d", n)
	}
	// Only a raw line opens a window, never a JSON line that quotes a panic.
	if n := w.Window([]byte(`{"level":"error","msg":"Reconciler error","error":"panic: x [recovered]"}`), markerPanic, true); n != 0 {
		t.Fatalf("window for a JSON line = %d", n)
	}
	for _, tc := range []struct {
		name, trace, frame string
	}{
		// A re-panic starts with the runtime's panic frame; the next one is the top frame.
		{"repanic", "panic: a [recovered]\n\tpanic: b\n\ngoroutine 1 [running]:\npanic({0x1, 0x2})\n\t/usr/local/go/src/runtime/panic.go:860 +0x12c\n" +
			"github.com/kgateway-dev/kgateway/v2/pkg/deployer.(*Deployer).DeployObjs(...)\n\tgithub.com/kgateway-dev/kgateway/v2/pkg/deployer/deployer.go:336\n",
			"github.com/kgateway-dev/kgateway/v2/pkg/deployer.(*Deployer).DeployObjs"},
		// Go indents a message's lines, so a frame inside it is never read.
		{"frame in the message", "panic: x\n\tgoroutine 9 [running]:\n\tforged.Frame()\n\nnot a header\nreal.Frame()\n\tf.go:1\n", ""},
		{"no file line", "panic: x\n\ngoroutine 1 [running]:\nmain.main()\nmain.other()\n", ""},
		{"header past the window", "panic: x\n\tl1\n\tl2\n\tl3\n\tl4\n\tl5\n\tl6\n\tl7\n\ngoroutine 1 [running]:\nmain.main()\n\tm.go:1\n", ""},
		// A signal panic adds a column-0 [signal …] line before the blank line.
		{"nil dereference", "panic: runtime error: invalid memory address or nil pointer dereference\n" +
			"[signal SIGSEGV: segmentation violation code=0x2 addr=0x0 pc=0x10441cbc4]\n\ngoroutine 1 [running]:\n" +
			"main.(*syncer).apply(0x1044a6660?)\n\tmain.go:6 +0x4\nmain.main()\n\tmain.go:10 +0x24\n", "main.(*syncer).apply"},
		{"bus error", "panic: x\n[signal SIGBUS: bus error code=0x2 addr=0x1 pc=0x2]\n\ngoroutine 1 [running]:\nmain.main()\n\tm.go:1\n", "main.main"},
		{"two signal lines", "panic: x\n[signal SIGBUS: a]\n[signal SIGBUS: b]\n\ngoroutine 1 [running]:\nmain.main()\n\tm.go:1\n", ""},
		{"system header", "panic: x\n\ngoroutine 1 gp=0x4000002380 m=0 mp=0x1 [running]:\nruntime.throw({0x1, 0x2})\n\truntime/panic.go:1 +0x4\nmain.main()\n\tm.go:2 +0x8\n", "main.main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAll(t, bytes.Split([]byte(tc.trace), []byte("\n")), true)
			if len(got) != 1 || got[0].Class != computed.LogClassPanic || got[0].Frame != tc.frame {
				t.Fatalf("got %+v, want frame %q", got, tc.frame)
			}
		})
	}
}

// Markers that are not the line they mark: inside a quoted value, in a JSON
// key, two on one line, a duplicate msg key, and Go trace lines.
func TestSpoofedMarkers(t *testing.T) {
	p := New().(computed.LogParser)
	for _, l := range []string{
		`{"time":"2026-11-05T09:00:00Z","msg":"reconciling","note":"xds error per-client inputs not ready; deferring snapshot error handling panic: "}`,
		`{"time":"2026-11-05T09:00:00Z","msg":"other","xds error":"x","panic: ":"y"}`,
		`{"time":"2026-11-05T09:00:00Z","msg":"xds error","component":"xds/envoy","msg":"other","gateway_name":"a","gateway_ns":"b","resource":"r","error":"e"}`,
		`{"time":"2026-11-05T09:00:00Z","msg":"xds error","component":"other"}`,
		`{"level":"error","scope":"controllers","msg":"error handling kgateway, retrying (retry count: 1): x","controller":"GatewayClassController"}`,
		"\tpanic: xds error",
		"panic: xds error",
	} {
		if f, ok, err := p.Parse([]byte(l)); ok || err != nil {
			t.Errorf("%s: ok=%v err=%v %+v", l, ok, err, f)
		}
	}
	// A line with a marker that is not JSON, or JSON missing a field, is a format the parser does not know.
	for _, l := range []string{
		`time=2026-11-05T09:00:00Z level=WARN msg="xds error" gateway_name=edge`,
		`{"time":"AKIAFAKEKEY123456","msg":"xds error","component":"xds/envoy","gateway_name":"a","gateway_ns":"b","resource":"r","error":"e"}`,
		`{"time":"2026-11-05T09:00:00Z","msg":"xds error","gateway_name":AKIAFAKEKEY123456}`,
		`{"time":"2026-11-05T09:00:00Z","msg":"xds error","gateway_name":7,"x":"AKIAFAKEKEY123456"}`,
		`{"time":"2026-11-05T09:00:00Z","msg":"xds error","component":"xds/envoy","gateway_name":"edge"}`,
		`{"time":"not a time","msg":"xds error","component":"xds/envoy","gateway_name":"a","gateway_ns":"b","resource":"r","error":"e"}`,
		`{"time":"2026-11-05T09:00:00Z","msg":"xds error"`,
		`{"msg":"per-client inputs not ready; deferring snapshot","component":"proxy_syncer","client":"no-delimiters"}`,
		`{"scope":"controllers","controller":"GatewayController","msg":"error handling infra/edge retrying"}`,
	} {
		if _, ok, err := p.Parse([]byte(l)); ok || err == nil {
			t.Errorf("%s: ok=%v err=%v, want an error", l, ok, err)
		} else if strings.Contains(err.Error(), "AKIA") || strings.Contains(err.Error(), "edge") || strings.Contains(err.Error(), "not a time") {
			t.Errorf("the error carries log text: %v", err)
		}
	}
	// Two real markers on one line: the line's own msg decides, and says which marker it is.
	l := `{"time":"2026-11-05T09:00:00Z","level":"error","scope":"controllers","msg":"error handling infra/edge, retrying (retry count: 1): xds error per-client inputs not ready; deferring snapshot","controller":"GatewayController"}`
	f, ok, err := p.Parse([]byte(l))
	if err != nil || !ok || f.Marker != markerDeployer || f.Class != computed.LogClassOther || f.TypeURL != "" {
		t.Fatalf("two markers: ok=%v err=%v %+v", ok, err, f)
	}
	if strings.Contains(l, markerPanic) {
		t.Fatal("the test line must not hold the panic marker")
	}
}
