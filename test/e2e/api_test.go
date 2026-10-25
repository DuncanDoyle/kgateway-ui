//go:build e2e

// Package e2e checks the deployed UI's API against a real kgateway install
// created by test/e2e/setup.sh.
package e2e

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DuncanDoyle/kgateway-ui/pkg/api"
	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/overview"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
)

var base = func() string {
	if u := os.Getenv("UI_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:18080"
}()

// get decodes path into v, retrying until it answers 200 or two minutes pass.
func get(t *testing.T, path string, v any) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	var last string // last error, or status and start of body, for the timeout message
	for {
		resp, err := http.Get(base + path)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer func() { _ = resp.Body.Close() }()
			if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
				t.Fatal(err)
			}
			return
		}
		if err != nil {
			last = err.Error()
		} else {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			last = fmt.Sprintf("status %d: %s", resp.StatusCode, body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s never succeeded: %s", path, last)
		}
		time.Sleep(2 * time.Second)
	}
}

// status returns the status code of one GET of path.
func status(t *testing.T, path string) int {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// has reports a finding with code on res; a non-empty msg must match too.
func has(h api.HealthResponse, code, res string, msg ...string) bool {
	return slices.ContainsFunc(h.Findings, func(f api.FindingDTO) bool {
		return f.Code == code && f.Resource != nil && f.Resource.String() == res && (len(msg) == 0 || f.Message == msg[0])
	})
}

// tpMissingTarget is the finding expected for tp-missing-target. Only kgateway
// main reports TargetNotFound; v2.3.x and v2.4.x write no status for a policy
// whose target is missing, so after the grace period it is "not yet processed" (finding 7).
func tpMissingTarget() (code, msg string) {
	if os.Getenv("KGW_MAIN") == "1" {
		return health.CodeConditionFalse, "Policy target not found"
	}
	return health.CodeRouteUnattached, "Not yet processed by the controller"
}

func secondInstall() bool { return os.Getenv("SECOND_INSTALL") == "1" }

func TestRealCluster(t *testing.T) {
	var meta api.MetaResponse
	get(t, "/api/v1/meta", &meta)
	if meta.Platform == "" || !slices.Contains(meta.Capabilities, platform.Scope) || !slices.Contains(meta.Capabilities, platform.Workloads) {
		t.Fatalf("meta: platform=%q capabilities=%v", meta.Platform, meta.Capabilities)
	}

	var insts []api.InstallSummary
	get(t, "/api/v1/installs", &insts)
	want := 1
	if secondInstall() {
		want = 2
	}
	if len(insts) != want {
		t.Fatalf("installs: want %d got %+v", want, insts)
	}
	// Installs are sorted by ID; the ID is opaque and only displayed as namespace/name (D-016).
	if insts[0].ID != "kgw-a.kgateway" || insts[0].DisplayName != "kgw-a/kgateway" {
		t.Fatalf("install identity: %+v", insts[0])
	}
	if secondInstall() && (insts[1].ID != "kgw-b.kgateway" || insts[1].DisplayName != "kgw-b/kgateway") {
		t.Fatalf("second install identity: %+v", insts[1])
	}
	for _, i := range insts {
		if i.Readiness.State != overview.ReadinessReady {
			t.Fatalf("install %s readiness: %+v", i.ID, i.Readiness)
		}
	}

	// Gateways edge and self; posts in the unlabelled blog namespace must not count. An in-scope
	// HTTPRoute is owned, so counted, when a parentRef names one of the install's Gateways or
	// ListenerSets (denied ones included), or once the settled controller wrote it an entry (design §4.2).
	routes := 0
	w := settledWorld(t)
	for _, r := range w.routes {
		parent := slices.ContainsFunc(r.Spec.ParentRefs, func(p kref) bool {
			id := p.resolve(r.Metadata.Namespace).Ref().String()
			k := deref(p.Kind, "Gateway")
			_, gw := w.gateways[id]
			_, set := w.sets[id]
			return k == "Gateway" && gw || k == "ListenerSet" && set
		})
		if parent || slices.ContainsFunc(r.Status.Parents, func(e kentry) bool { return e.ControllerName == ours }) {
			routes++
		}
	}
	eventually(t, func() error {
		var o api.OverviewResponse
		get(t, "/api/v1/installs/kgw-a.kgateway/overview", &o)
		counts := map[string]int{}
		for _, c := range o.Counts {
			counts[c.Kind] = c.Count
		}
		if counts["Gateway"] != 2 || counts["HTTPRoute"] != routes {
			return fmt.Errorf("counts: want Gateway 2, HTTPRoute %d; got %+v", routes, counts)
		}
		return nil
	})

	// An owned object is served by install ID; another install's object is not.
	var route map[string]any
	get(t, "/api/v1/installs/kgw-a.kgateway/resources/gateway.networking.k8s.io/HTTPRoute/shop/broken", &route)
	if md, _ := route["metadata"].(map[string]any); md["name"] != "broken" || md["namespace"] != "shop" {
		t.Fatalf("resource: metadata %+v", route["metadata"])
	}
	if secondInstall() {
		// Positive control: the same route serves app-b/edge under its own install.
		const gw = "/resources/gateway.networking.k8s.io/Gateway/app-b/edge"
		if code := status(t, "/api/v1/installs/kgw-b.kgateway"+gw); code != http.StatusOK {
			t.Fatalf("kgw-b's Gateway not served by kgw-b: status %d", code)
		}
		if code := status(t, "/api/v1/installs/kgw-a.kgateway"+gw); code != http.StatusNotFound {
			t.Fatalf("kgw-b's Gateway served by kgw-a: status %d", code)
		}
		var ob api.OverviewResponse
		get(t, "/api/v1/installs/kgw-b.kgateway/overview", &ob)
		countsB := map[string]int{}
		for _, c := range ob.Counts {
			countsB[c.Kind] = c.Count
		}
		if countsB["Gateway"] != 1 {
			t.Fatalf("kgw-b counts: %+v", countsB)
		}
	}

	// Findings settle once kgateway has written status and metrics were scraped.
	tpCode, tpMsg := tpMissingTarget()
	deadline := time.Now().Add(3 * time.Minute) // covers health.Grace for the policy
	for {
		var h api.HealthResponse
		get(t, "/api/v1/installs/kgw-a.kgateway/health", &h)
		ok := has(h, health.CodeConditionFalse, "shop/broken") && has(h, health.CodeNamespaceNotWatched, "blog/posts") &&
			has(h, tpCode, "shop/tp-missing-target", tpMsg) && has(h, health.CodeSelfManagedProxy, "infra/self") &&
			!has(h, health.CodeConditionFalse, "shop/good")
		xds := slices.ContainsFunc(h.Checks, func(c health.CheckResult) bool {
			return c.Name == "xDS rejects" && c.OK // in-cluster metrics scraping works
		})
		if ok && xds && h.State == health.StateError {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("health never settled: state=%s findings=%+v checks=%+v", h.State, h.Findings, h.Checks)
		}
		time.Sleep(3 * time.Second)
	}
}

// region Explorer (fixtures/explorer.yaml)

const (
	install = "/api/v1/installs/kgw-a.kgateway"
	gwGroup = "gateway.networking.k8s.io"
	ours    = "kgateway.dev/kgateway"
	// programmed is kgateway's route condition; 2.3 does not write it.
	programmed = "kgateway.dev/Programmed"
)

// scoped are install A's namespaces (team=a); blog is outside its scope.
var scoped = []string{"infra", "shop", "store", "deleg", "closed", "flip"}

// kubectl decodes `kubectl get … -o json` from the harness cluster into v. It
// always passes the harness kubeconfig, so the user's current context is never used.
func kubectl(t *testing.T, v any, args ...string) {
	t.Helper()
	out, err := kubectlRun(t, args...)
	if err != nil {
		t.Fatalf("kubectl %v: %v", args, err)
	}
	if err := json.Unmarshal(out, v); err != nil {
		t.Fatal(err)
	}
}

// kubectlRun runs kubectl against the harness cluster and returns its stdout.
func kubectlRun(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	dir := os.Getenv("E2E_DIR")
	if dir == "" {
		cluster := cmp.Or(os.Getenv("CLUSTER"), "kgwui-e2e")
		dir = filepath.Join(cmp.Or(os.Getenv("TMPDIR"), "/tmp"), cluster) // as setup.sh
	}
	kc := filepath.Join(dir, "kubeconfig")
	if _, err := os.Stat(kc); err != nil {
		t.Fatalf("harness kubeconfig: %v", err)
	}
	return exec.Command("kubectl", append([]string{"--kubeconfig", kc}, args...)...).Output()
}

// kcond is a condition as the controller wrote it; lastTransitionTime is left out.
type kcond struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Message            string `json:"message"`
	ObservedGeneration int64  `json:"observedGeneration"`
}

type kref struct {
	Group       *string `json:"group"`
	Kind        *string `json:"kind"`
	Namespace   *string `json:"namespace"`
	Name        string  `json:"name"`
	SectionName *string `json:"sectionName"`
	Port        *int32  `json:"port"`
}

// resolve applies the Gateway API parentRef defaults.
func (r kref) resolve(ns string) objects.ParentRef {
	return objects.ParentRef{
		Group: deref(r.Group, gwGroup), Kind: deref(r.Kind, "Gateway"), Namespace: cmp.Or(deref(r.Namespace, ""), ns),
		Name: r.Name, SectionName: deref(r.SectionName, ""), Port: deref(r.Port, 0),
	}
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

type kentry struct {
	ParentRef      kref    `json:"parentRef"`
	ControllerName string  `json:"controllerName"`
	Conditions     []kcond `json:"conditions"`
}

// kobj is the part of a Gateway API or kgateway object the comparisons read.
type kobj struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name       string `json:"name"`
		Namespace  string `json:"namespace"`
		Generation int64  `json:"generation"`
	} `json:"metadata"`
	Spec struct {
		ParentRefs       []kref         `json:"parentRefs"`
		ParentRef        kref           `json:"parentRef"`
		AllowedListeners map[string]any `json:"allowedListeners"`
	} `json:"spec"`
	Status struct {
		Conditions []kcond `json:"conditions"`
		Listeners  []struct {
			Name           string  `json:"name"`
			AttachedRoutes int32   `json:"attachedRoutes"`
			Conditions     []kcond `json:"conditions"`
		} `json:"listeners"`
		AttachedListenerSets *int32   `json:"attachedListenerSets"`
		Parents              []kentry `json:"parents"`
	} `json:"status"`
}

func (o kobj) id() string { return o.Metadata.Namespace + "/" + o.Metadata.Name }

// world is one read of the fixture's kinds in install A's namespaces.
type world struct {
	gateways, sets, routes, backends map[string]kobj
}

func readWorld(t *testing.T) world {
	t.Helper()
	w := world{map[string]kobj{}, map[string]kobj{}, map[string]kobj{}, map[string]kobj{}}
	var list struct{ Items []kobj }
	kubectl(t, &list, "get", "gateways.gateway.networking.k8s.io,listenersets.gateway.networking.k8s.io,"+
		"httproutes.gateway.networking.k8s.io,backends.gateway.kgateway.dev", "-A", "-o", "json")
	for _, o := range list.Items {
		if !slices.Contains(scoped, o.Metadata.Namespace) {
			continue
		}
		m := map[string]map[string]kobj{"Gateway": w.gateways, "ListenerSet": w.sets, "HTTPRoute": w.routes, "Backend": w.backends}[o.Kind]
		m[o.id()] = o
	}
	return w
}

// current reports whether every condition the controller wrote describes the
// object's generation. 2.3 writes observedGeneration 0 on Backends, which count as current.
func (w world) current() bool {
	ok := func(o kobj, cs []kcond) bool {
		return !slices.ContainsFunc(cs, func(c kcond) bool { return c.ObservedGeneration != 0 && c.ObservedGeneration != o.Metadata.Generation })
	}
	for _, m := range []map[string]kobj{w.gateways, w.sets, w.backends} {
		for _, o := range m {
			if !ok(o, o.Status.Conditions) {
				return false
			}
		}
	}
	for _, o := range w.routes {
		for _, e := range o.Status.Parents {
			if !ok(o, e.Conditions) {
				return false
			}
		}
	}
	return true
}

// settledWorld waits until the controller's status is current and two reads 5 s apart match.
func settledWorld(t *testing.T) world {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	var prev []byte
	for {
		w := readWorld(t)
		cur, _ := json.Marshal([]any{w.gateways, w.sets, w.routes, w.backends})
		if w.current() && string(cur) == string(prev) {
			return w
		}
		if time.Now().After(deadline) {
			t.Fatalf("controller status never settled: %s", cur)
		}
		prev = cur
		time.Sleep(5 * time.Second)
	}
}

// eventually retries check until it returns no error or two minutes pass:
// the UI publishes what the cache saw a moment ago.
func eventually(t *testing.T, check func() error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Second)
	}
}

func sameConds(ui []api.Condition, k []kcond) error {
	got := make([]kcond, len(ui))
	for i, c := range ui {
		got[i] = kcond{c.Type, c.Status, c.Reason, c.Message, c.ObservedGeneration}
	}
	if !slices.Equal(got, k) {
		return fmt.Errorf("conditions: API %+v, controller %+v", got, k)
	}
	return nil
}

func findCond(cs []kcond, typ string) *kcond {
	for i := range cs {
		if cs[i].Type == typ {
			return &cs[i]
		}
	}
	return nil
}

// ourEntry is route r's entry from our controller for parent p (after defaults), or nil.
func ourEntry(r kobj, p objects.ParentRef) *kentry {
	for i, e := range r.Status.Parents {
		if e.ControllerName == ours && e.ParentRef.resolve(r.Metadata.Namespace) == p {
			return &r.Status.Parents[i]
		}
	}
	return nil
}

// reportedEdge is design §4.2: an entry from our controller naming parent, and a
// spec parentRef equal to it or an empty parentRefs list.
func reportedEdge(r kobj, parent objects.Ref) bool {
	ns := r.Metadata.Namespace
	for _, e := range r.Status.Parents {
		p := e.ParentRef.resolve(ns)
		if e.ControllerName != ours || p.Ref() != parent {
			continue
		}
		if len(r.Spec.ParentRefs) == 0 || slices.ContainsFunc(r.Spec.ParentRefs, func(s kref) bool { return s.resolve(ns) == p }) {
			return true
		}
	}
	return false
}

// declared is design §4.2's declared candidate: a spec parentRef to parent without a reported edge.
func declared(r kobj, parent objects.Ref) bool {
	ns := r.Metadata.Namespace
	return !reportedEdge(r, parent) && slices.ContainsFunc(r.Spec.ParentRefs, func(s kref) bool { return s.resolve(ns).Ref() == parent })
}

func routeRef(id string) objects.Ref {
	ns, name, _ := strings.Cut(id, "/")
	return objects.Ref{Group: gwGroup, Kind: "HTTPRoute", Namespace: ns, Name: name}
}

func refParam(r objects.Ref) string {
	return url.QueryEscape(cmp.Or(r.Group, "core") + "/" + r.Kind + "/" + r.Namespace + "/" + r.Name)
}

// routeList returns every row of GET …/routes with query q (one page: the fixture is small).
func routeList(t *testing.T, q string) []api.RouteRow {
	t.Helper()
	var p api.Page[api.RouteRow]
	get(t, install+"/routes?limit=200&"+q, &p)
	if p.Next != "" {
		t.Fatalf("routes?%s: more than one page", q)
	}
	return p.Items
}

func rowIDs(rows []api.RouteRow) []string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Ref.Kind == "HTTPRoute" {
			ids = append(ids, r.Ref.String())
		}
	}
	slices.Sort(ids)
	return ids
}

func sortedKeys(m map[string]bool) []string {
	ids := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			ids = append(ids, k)
		}
	}
	slices.Sort(ids)
	return ids
}

// kgwMinor is the controller's minor version line ("2.3", "2.4"), read from its image tag.
func kgwMinor(t *testing.T) string {
	var d struct {
		Spec struct {
			Template struct {
				Spec struct{ Containers []struct{ Image string } }
			}
		}
	}
	kubectl(t, &d, "-n", "kgw-a", "get", "deploy/kgateway", "-o", "json")
	img := d.Spec.Template.Spec.Containers[0].Image
	_, tag, _ := strings.Cut(img[strings.LastIndex(img, "/")+1:], ":")
	parts := strings.SplitN(strings.TrimPrefix(tag, "v"), ".", 3)
	if len(parts) < 2 {
		t.Fatalf("controller image tag %q", img)
	}
	return parts[0] + "." + parts[1]
}

// TestExplorerShowsControllerStatus checks that the explorer endpoints carry what
// the controller wrote (design §9.5, 2.2). Expected values come from the cluster,
// so the test holds on every kgateway version; fixture checks say when a version
// stops producing the case a fixture object exists for.
func TestExplorerShowsControllerStatus(t *testing.T) {
	w := settledWorld(t)
	minor := kgwMinor(t)
	t.Logf("kgateway %s", minor)
	for _, id := range slices.Sorted(maps.Keys(w.routes)) {
		b, _ := json.Marshal(w.routes[id].Status.Parents)
		t.Logf("route %s: %s", id, b)
	}
	for _, m := range []map[string]kobj{w.gateways, w.sets, w.backends} {
		for _, id := range slices.Sorted(maps.Keys(m)) {
			b, _ := json.Marshal(m[id].Status)
			t.Logf("%s %s: %s", m[id].Kind, id, b)
		}
	}

	t.Run("fixture", func(t *testing.T) {
		// Each object below exists for one case; if a controller stops producing it, the comparisons prove less.
		want := map[string]string{"closed/not-allowed": "NotAllowedByListeners", "shop/no-section": "NoMatchingParent", "store/wrong-host": "NoMatchingListenerHostname"}
		for id, reason := range want {
			r := w.routes[id]
			e := ourEntry(r, r.Spec.ParentRefs[0].resolve(r.Metadata.Namespace))
			if e == nil || findCond(e.Conditions, "Accepted") == nil || findCond(e.Conditions, "Accepted").Reason != reason {
				t.Errorf("%s: want Accepted reason %s, controller wrote %+v", id, reason, e)
			}
		}
		for _, id := range []string{"shop/denied", "infra/closed"} {
			if c := findCond(w.sets[id].Status.Conditions, "Accepted"); c == nil || c.Status != "False" || c.Reason != "NotAllowed" {
				t.Errorf("ListenerSet %s: want Accepted=False/NotAllowed, controller wrote %+v", id, c)
			}
		}
		if c := findCond(w.backends["shop/aws-be"].Status.Conditions, "Accepted"); c == nil || c.Status != "False" {
			t.Errorf("Backend shop/aws-be: want Accepted=False, controller wrote %+v", c)
		}
		store := routeRef("shop/store")
		for _, id := range []string{"deleg/c-forbidden", "shop/c-hdr"} {
			if reportedEdge(w.routes[id], store) {
				t.Errorf("%s: the controller reports an edge to shop/store", id)
			}
		}
		for _, id := range []string{"shop/c-name", "shop/c-label", "deleg/c-wild", "shop/c-host", "shop/c-ihost", "shop/c-inherit", "shop/c-cycle"} {
			if !reportedEdge(w.routes[id], store) {
				t.Errorf("%s: no reported edge to shop/store", id)
			}
		}
		for _, id := range []string{"shop/c-host", "shop/c-ihost"} {
			e := ourEntry(w.routes[id], objects.ParentRef{Group: gwGroup, Kind: "HTTPRoute", Namespace: "shop", Name: "store"})
			if e == nil || findCond(e.Conditions, "Accepted") == nil || findCond(e.Conditions, "Accepted").Status != "False" {
				t.Errorf("%s: want Accepted=False from shop/store, controller wrote %+v", id, e)
			}
		}
		if !declared(w.routes["deleg/c-forbidden"], routeRef("shop/c-name")) {
			t.Errorf("deleg/c-forbidden: not a declared candidate of shop/c-name")
		}
	})

	t.Run("gateways", func(t *testing.T) {
		eventually(t, func() error {
			var list api.Page[api.GatewayListRow]
			get(t, install+"/gateways?limit=200", &list)
			rows := map[string]api.GatewayListRow{}
			for _, r := range list.Items {
				rows[r.Ref.String()] = r
			}
			for id, g := range w.gateways {
				var sum int32
				for _, l := range g.Status.Listeners {
					sum += l.AttachedRoutes
				}
				if _, ok := rows[id]; !ok {
					return fmt.Errorf("Gateway %s: no row", id)
				}
				if rows[id].AttachedRoutes != sum {
					return fmt.Errorf("Gateway row %s: attachedRoutes %d, status sum %d", id, rows[id].AttachedRoutes, sum)
				}
				var d api.GatewayDetail
				get(t, install+"/gateways/"+id, &d)
				if err := sameConds(d.Conditions, g.Status.Conditions); err != nil {
					return fmt.Errorf("Gateway %s: %w", id, err)
				}
				if deref(d.AttachedListenerSets, -1) != deref(g.Status.AttachedListenerSets, -1) {
					return fmt.Errorf("Gateway %s: attachedListenerSets %v, status %v", id, d.AttachedListenerSets, g.Status.AttachedListenerSets)
				}
				if err := sameListeners(d.Listeners, g); err != nil {
					return fmt.Errorf("Gateway %s: %w", id, err)
				}
			}
			return nil
		})
	})

	t.Run("listenersets", func(t *testing.T) {
		eventually(t, func() error {
			var list api.Page[api.ListenerSetRow]
			get(t, install+"/listenersets?limit=200", &list)
			rows := map[string]api.ListenerSetRow{}
			for _, r := range list.Items {
				rows[r.Ref.String()] = r
			}
			for id, s := range w.sets {
				row, ok := rows[id]
				if !ok {
					return fmt.Errorf("ListenerSet %s: no row", id)
				}
				want := ""
				if c := findCond(s.Status.Conditions, "Accepted"); c != nil && c.Status != "True" {
					want = c.Reason
				}
				if row.AcceptedReason != want {
					return fmt.Errorf("ListenerSet row %s: acceptedReason %q, controller %q", id, row.AcceptedReason, want)
				}
				var d api.ListenerSetDetail
				get(t, install+"/listenersets/gateway.networking.k8s.io/ListenerSet/"+id, &d)
				if err := sameConds(d.Conditions, s.Status.Conditions); err != nil {
					return fmt.Errorf("ListenerSet %s: %w", id, err)
				}
				if err := sameListeners(d.Listeners, s); err != nil {
					return fmt.Errorf("ListenerSet %s: %w", id, err)
				}
				if want != "NotAllowed" {
					continue
				}
				parent := w.gateways[s.Spec.ParentRef.resolve(s.Metadata.Namespace).Ref().String()]
				al := d.ParentAllowedListeners
				// The UI reads no CRDs, so the hint shows whenever the field is absent (design §8.5).
				if al == nil || al.Set != (parent.Spec.AllowedListeners != nil) || al.CRDHint != (parent.Spec.AllowedListeners == nil) ||
					!reflect.DeepEqual(al.Value, parent.Spec.AllowedListeners) {
					return fmt.Errorf("ListenerSet %s: parentAllowedListeners %+v, parent spec %v", id, al, parent.Spec.AllowedListeners)
				}
			}
			return nil
		})
	})

	t.Run("routes", func(t *testing.T) {
		eventually(t, func() error {
			rows := map[string]api.RouteRow{}
			for _, r := range routeList(t, "") {
				rows[r.Ref.String()] = r
			}
			for id, r := range w.routes {
				var d api.RouteDetail
				get(t, install+"/routes/gateway.networking.k8s.io/HTTPRoute/"+id, &d)
				if len(d.Parents) != len(r.Status.Parents) {
					return fmt.Errorf("route %s: %d status entries, controller %d", id, len(d.Parents), len(r.Status.Parents))
				}
				for i, e := range r.Status.Parents {
					p := d.Parents[i]
					if p.ParentRef != e.ParentRef.resolve(r.Metadata.Namespace) || p.ControllerName != e.ControllerName {
						return fmt.Errorf("route %s entry %d: API %+v, controller %+v", id, i, p, e)
					}
					if err := sameConds(p.Conditions, e.Conditions); err != nil {
						return fmt.Errorf("route %s entry %d: %w", id, i, err)
					}
				}
				// A row's Gateway or ListenerSet parents carry the deciding condition as written.
				for _, s := range rows[id].Parents {
					if s.Attachment != "accepted" && s.Attachment != "not-accepted" {
						continue
					}
					e := ourEntry(r, objects.ParentRef{Group: s.Ref.Group, Kind: s.Ref.Kind, Namespace: s.Ref.Namespace, Name: s.Ref.Name, SectionName: s.SectionName, Port: s.Port})
					if e == nil {
						return fmt.Errorf("route row %s parent %s: %s, but the controller wrote no entry", id, s.Ref.Name, s.Attachment)
					}
					var want kcond
					for _, typ := range []string{"Accepted", "ResolvedRefs", programmed} {
						if c := findCond(e.Conditions, typ); c != nil && c.Status == "False" {
							want = *c
							break
						}
					}
					if s.ConditionType != want.Type || s.Reason != want.Reason || s.Message != want.Message {
						return fmt.Errorf("route row %s parent %s: %s/%s, controller %s/%s", id, s.Ref.Name, s.ConditionType, s.Reason, want.Type, want.Reason)
					}
				}
			}
			return nil
		})
	})

	t.Run("delegation", func(t *testing.T) {
		store := routeRef("shop/store")
		children, decl := map[string]bool{}, map[string]map[string]bool{}
		for id, r := range w.routes {
			children[id] = reportedEdge(r, store)
			for _, p := range []string{"shop/store", "shop/c-name"} {
				if decl[p] == nil {
					decl[p] = map[string]bool{}
				}
				decl[p][id] = declared(r, routeRef(p))
			}
		}
		// PF15: 2.3 writes no entry for a child ref without a namespace, so it stays a declared candidate.
		if minor == "2.3" {
			if !decl["shop/store"]["shop/c-nons"] {
				t.Errorf("2.3: shop/c-nons should be a declared candidate of shop/store")
			}
		} else if !children["shop/c-nons"] {
			t.Errorf("%s: shop/c-nons should be a reported child of shop/store", minor)
		}
		eventually(t, func() error {
			got := rowIDs(routeList(t, "parent="+refParam(store)))
			if want := sortedKeys(children); !slices.Equal(got, want) {
				return fmt.Errorf("child routes of shop/store: API %v, controller %v", got, want)
			}
			var d api.RouteDetail
			get(t, install+"/routes/gateway.networking.k8s.io/HTTPRoute/shop/store", &d)
			if d.ChildCount != len(sortedKeys(children)) {
				return fmt.Errorf("shop/store childCount %d, want %d", d.ChildCount, len(sortedKeys(children)))
			}
			for _, rule := range d.Rules {
				for _, b := range rule.BackendRefs {
					if b.Delegation == nil || b.Delegation.Mode != objects.DelegationName {
						continue
					}
					want := api.DelegationNotReported
					if children[b.Ref.String()] {
						want = api.DelegationReported
					}
					if b.Delegation.State != want {
						return fmt.Errorf("shop/store ref %s: state %s, want %s", b.Ref.String(), b.Delegation.State, want)
					}
				}
			}
			for parent, ids := range decl {
				got := rowIDs(routeList(t, "parent="+refParam(routeRef(parent))+"&attachment=declared"))
				if want := sortedKeys(ids); !slices.Equal(got, want) {
					return fmt.Errorf("declared under %s: API %v, controller %v", parent, got, want)
				}
			}
			var h api.HealthResponse
			get(t, install+"/health", &h)
			for _, ids := range decl {
				for _, id := range sortedKeys(ids) {
					if !has(h, health.CodeDelegatedNotAttached, id) {
						return fmt.Errorf("%s: no KGWUI-011", id)
					}
				}
			}
			// The up-chain of a reported child runs through its parent to the Gateway.
			get(t, install+"/routes/gateway.networking.k8s.io/HTTPRoute/shop/c-host", &d)
			want := []string{"infra/edge", "shop/store", "shop/c-host"}
			if !slices.ContainsFunc(d.Ancestors, func(p api.AncestorPathDTO) bool {
				var ids []string
				for _, s := range p.Steps {
					if s.Ref != nil {
						ids = append(ids, s.Ref.String())
					}
				}
				return slices.Equal(ids, want)
			}) {
				return fmt.Errorf("shop/c-host up-chain %+v, want %v", d.Ancestors, want)
			}
			return nil
		})
	})

	t.Run("backends", func(t *testing.T) {
		eventually(t, func() error {
			var d api.RouteDetail
			get(t, install+"/routes/gateway.networking.k8s.io/HTTPRoute/shop/backends", &d)
			for _, rule := range d.Rules {
				for _, b := range rule.BackendRefs {
					be, ok := w.backends[b.Ref.String()]
					if !ok || b.Backend == nil {
						return fmt.Errorf("shop/backends ref %s: backend %+v", b.Ref.String(), b.Backend)
					}
					want := findCond(be.Status.Conditions, "Accepted")
					var got []api.Condition
					if b.Backend.Accepted != nil {
						got = []api.Condition{*b.Backend.Accepted}
					}
					var k []kcond
					if want != nil {
						k = []kcond{*want}
					}
					if err := sameConds(got, k); err != nil {
						return fmt.Errorf("Backend %s: %w", b.Ref.String(), err)
					}
				}
			}
			return nil
		})
	})

	t.Run("mirror", func(t *testing.T) {
		eventually(t, func() error {
			rows := routeList(t, "backend="+refParam(objects.Ref{Kind: "Service", Namespace: "shop", Name: "shadow"}))
			if len(rows) != 1 || rows[0].Ref.String() != "shop/mirror" || !rows[0].Mirror {
				return fmt.Errorf("routes?backend=shop/shadow: %+v", rows)
			}
			for _, r := range routeList(t, "backend="+refParam(objects.Ref{Kind: "Service", Namespace: "shop", Name: "httpbin"})) {
				if r.Mirror {
					return fmt.Errorf("routes?backend=shop/httpbin: %s marked mirror", r.Ref.String())
				}
			}
			return nil
		})
	})

	t.Run("not-programmed", func(t *testing.T) {
		want := map[string]bool{}
		for id, r := range w.routes {
			for _, e := range r.Status.Parents {
				p, a := findCond(e.Conditions, programmed), findCond(e.Conditions, "Accepted")
				if e.ControllerName == ours && (p != nil && p.Status == "False" ||
					a != nil && a.Status == "False" && (a.Reason == "RouteRuleReplaced" || a.Reason == "RouteRuleDropped")) {
					want[id] = true
				}
			}
		}
		if !want["shop/replaced"] {
			t.Errorf("fixture: shop/replaced has no replaced rule")
		}
		eventually(t, func() error {
			if got := rowIDs(routeList(t, "state=not-programmed")); !slices.Equal(got, sortedKeys(want)) {
				return fmt.Errorf("state=not-programmed: API %v, controller %v", got, sortedKeys(want))
			}
			return nil
		})
	})
}

// sameListeners compares each listener's status block with the controller's.
func sameListeners(ls []api.ListenerDTO, o kobj) error {
	for _, k := range o.Status.Listeners {
		i := slices.IndexFunc(ls, func(l api.ListenerDTO) bool { return l.Name == k.Name })
		if i < 0 || ls[i].Status == nil {
			return fmt.Errorf("listener %s: no status block", k.Name)
		}
		if ls[i].Status.AttachedRoutes != k.AttachedRoutes {
			return fmt.Errorf("listener %s: attachedRoutes %d, controller %d", k.Name, ls[i].Status.AttachedRoutes, k.AttachedRoutes)
		}
		if err := sameConds(ls[i].Status.Conditions, k.Conditions); err != nil {
			return fmt.Errorf("listener %s: %w", k.Name, err)
		}
	}
	return nil
}

// region View A (2.3)

// accepted reports whether the controller accepted ListenerSet s.
func accepted(s kobj) bool {
	c := findCond(s.Status.Conditions, "Accepted")
	return c != nil && c.Status == "True"
}

// sameCounts checks every listener with a status block: view A's count equals
// attachedRoutes and the check holds count-agrees (rules §8.1).
func sameCounts(ls []api.ListenerDTO, o kobj) error {
	for _, k := range o.Status.Listeners {
		i := slices.IndexFunc(ls, func(l api.ListenerDTO) bool { return l.Name == k.Name })
		if i < 0 {
			return fmt.Errorf("listener %s: not served", k.Name)
		}
		l := ls[i]
		if l.Computed == nil || l.ComputedRoutes == nil {
			return fmt.Errorf("listener %s: no computed count (computed %+v)", k.Name, l.Computed)
		}
		if *l.ComputedRoutes != int(k.AttachedRoutes) {
			return fmt.Errorf("listener %s: computed %d, controller attachedRoutes %d", k.Name, *l.ComputedRoutes, k.AttachedRoutes)
		}
		if l.Computed.By != api.ComputedBy || l.Computed.State != string(computed.StateChecked) ||
			!slices.Contains(l.Computed.Coverage, string(computed.CoverageCount)) {
			return fmt.Errorf("listener %s: computed %+v, want checked with %s", k.Name, *l.Computed, computed.CoverageCount)
		}
	}
	return nil
}

// TestComputedAgreesWithController checks view A against the controller on a
// settled cluster (design §9.5, 2.3): every listener count equals attachedRoutes,
// every parentRef outcome is status-compatible, and nothing raises KGWUI-012.
func TestComputedAgreesWithController(t *testing.T) {
	w := settledWorld(t)
	t.Logf("kgateway %s", kgwMinor(t))

	t.Run("fixture", func(t *testing.T) {
		// The cases the comparisons exist for; if a controller stops producing one, they prove less.
		q := w.routes["shop/quirk"]
		e := ourEntry(q, q.Spec.ParentRefs[0].resolve("shop"))
		if e == nil || findCond(e.Conditions, "Accepted") == nil || findCond(e.Conditions, "Accepted").Reason != "NoMatchingParent" {
			t.Errorf("shop/quirk: want Accepted reason NoMatchingParent, controller wrote %+v", e)
		}
		// Its two refs resolve alike, so ourEntry cannot tell their entries apart; count them instead.
		two, rt := 0, w.routes["infra/two-refs"]
		for _, e := range rt.Status.Parents {
			if c := findCond(e.Conditions, "Accepted"); e.ControllerName == ours && c != nil && c.Status == "True" {
				two++
			}
		}
		if two != 2 {
			t.Errorf("infra/two-refs: want two Accepted=True entries, controller wrote %+v", rt.Status.Parents)
		}
		if accepted(w.sets["shop/denied"]) || !accepted(w.sets["infra/extra"]) {
			t.Errorf("ListenerSets: want shop/denied denied and infra/extra accepted")
		}
		if _, ok := w.routes["flip/flip"]; !ok {
			t.Errorf("flip/flip: not read")
		}
	})

	t.Run("counts", func(t *testing.T) {
		eventually(t, func() error {
			for id, g := range w.gateways {
				var d api.GatewayDetail
				get(t, install+"/gateways/"+id, &d)
				if err := sameCounts(d.Listeners, g); err != nil {
					return fmt.Errorf("Gateway %s: %w", id, err)
				}
			}
			for id, s := range w.sets {
				var d api.ListenerSetDetail
				get(t, install+"/listenersets/gateway.networking.k8s.io/ListenerSet/"+id, &d)
				if accepted(s) {
					if err := sameCounts(d.Listeners, s); err != nil {
						return fmt.Errorf("ListenerSet %s: %w", id, err)
					}
					continue
				}
				// View A never evaluates a denied ListenerSet (rules §3.1).
				for _, l := range d.Listeners {
					if l.ComputedRoutes != nil {
						return fmt.Errorf("denied ListenerSet %s listener %s: computed count %d", id, l.Name, *l.ComputedRoutes)
					}
				}
			}
			return nil
		})
	})

	t.Run("outcomes", func(t *testing.T) {
		eventually(t, func() error {
			checked := 0
			for _, r := range routeList(t, "") {
				id := r.Ref.String()
				if !slices.Contains(scoped, r.Ref.Namespace) {
					continue // an unwatched route is never evaluated
				}
				for _, p := range r.Parents {
					evaluated := p.Ref.Kind == "Gateway" || p.Ref.Kind == "ListenerSet" && accepted(w.sets[p.Ref.String()])
					if p.Computed == nil {
						if evaluated {
							return fmt.Errorf("route %s parent %s: no computed outcome", id, p.Ref.String())
						}
						continue
					}
					c := p.Computed.Computed
					if !evaluated || c.By != api.ComputedBy || c.State != string(computed.StateChecked) ||
						!slices.Equal(c.Coverage, []string{string(computed.CoverageStatus)}) {
						return fmt.Errorf("route %s parent %s: computed %+v, want checked with %s", id, p.Ref.String(), c, computed.CoverageStatus)
					}
					checked++
				}
			}
			if checked == 0 {
				return fmt.Errorf("no computed parent outcome")
			}

			var d api.RouteDetail
			get(t, install+"/routes/gateway.networking.k8s.io/HTTPRoute/shop/quirk", &d)
			if len(d.Attachments) != 1 || d.Attachments[0].Result.Outcome != string(computed.OutcomeNoParent) {
				return fmt.Errorf("shop/quirk: attachments %+v, want %s", d.Attachments, computed.OutcomeNoParent)
			}
			// Both refs of two-refs land on edge's http listener, so the route counts twice there.
			get(t, install+"/routes/gateway.networking.k8s.io/HTTPRoute/infra/two-refs", &d)
			on := 0
			for _, a := range d.Attachments {
				for _, l := range a.Result.Listeners {
					if l.Resource.String() == "infra/edge" && l.Listener == "http" {
						on++
					}
				}
			}
			if on != 2 {
				return fmt.Errorf("infra/two-refs: %d memberships on infra/edge http, want 2: %+v", on, d.Attachments)
			}
			rows := rowIDs(routeList(t, "parent="+refParam(objects.Ref{Group: gwGroup, Kind: "Gateway", Namespace: "infra", Name: "edge"})+"&listener=http"))
			if !slices.Contains(rows, "infra/two-refs") {
				return fmt.Errorf("listener=http of infra/edge: %v lacks infra/two-refs", rows)
			}
			return nil
		})
	})

	t.Run("health", func(t *testing.T) {
		var h api.HealthResponse
		get(t, install+"/health", &h)
		for _, f := range h.Findings {
			if f.Code == health.CodeComputedDisagrees {
				t.Errorf("KGWUI-012 on %v: %s", f.Resource, f.Detail)
			}
		}
		if !slices.ContainsFunc(h.Checks, func(c health.CheckResult) bool { return c.Name == "computed views" && c.OK }) {
			t.Errorf("no OK computed views check: %+v", h.Checks)
		}
	})
}

// uiArgs is the UI container's args as installed by setup.sh.
func uiArgs(t *testing.T) []string {
	var d struct {
		Spec struct {
			Template struct {
				Spec struct{ Containers []struct{ Args []string } }
			}
		}
	}
	kubectl(t, &d, "-n", "kgateway-ui", "get", "deploy/ui-kgateway-ui", "-o", "json")
	return d.Spec.Template.Spec.Containers[0].Args
}

// controllerPods is the number of install A's controller pods, terminating ones included.
func controllerPods(t *testing.T) int {
	var d struct {
		Spec struct {
			Selector struct{ MatchLabels map[string]string }
		}
	}
	kubectl(t, &d, "-n", "kgw-a", "get", "deploy/kgateway", "-o", "json")
	sel := make([]string, 0, len(d.Spec.Selector.MatchLabels))
	for k, v := range d.Spec.Selector.MatchLabels {
		sel = append(sel, k+"="+v)
	}
	var pods struct{ Items []any }
	kubectl(t, &pods, "-n", "kgw-a", "get", "pods", "-l", strings.Join(sel, ","), "-o", "json")
	return len(pods.Items)
}

// must runs kubectl and fails the test on error.
func must(t *testing.T, args ...string) {
	t.Helper()
	if out, err := kubectlRun(t, args...); err != nil {
		t.Fatalf("kubectl %v: %v: %s", args, err, out)
	}
}

// routeCheck is view A's check state of route id's only parentRef.
func routeCheck(t *testing.T, id string) string {
	var d api.RouteDetail
	get(t, install+"/routes/gateway.networking.k8s.io/HTTPRoute/"+id, &d)
	if len(d.Attachments) != 1 {
		t.Fatalf("%s: attachments %+v", id, d.Attachments)
	}
	return d.Attachments[0].Result.Computed.State
}

// disagreement is the KGWUI-012 finding on the object kind/id in h, or nil.
func disagreement(h api.HealthResponse, kind, id string) *api.FindingDTO {
	for i, f := range h.Findings {
		if f.Code == health.CodeComputedDisagrees && f.Resource != nil && f.Resource.Kind == kind && f.Resource.String() == id {
			return &h.Findings[i]
		}
	}
	return nil
}

// TestForcedDisagreementRaisesKGWUI012 skews the cluster while the controller is
// stopped (design §9.5, 2.3): a Namespace relabel flips flip/flip's computed
// outcome and edge's https count, the status stays as written, and KGWUI-012
// matures at the scheduled deadline and clears once the controller catches up.
// The exact instant is Task 42's fake-clock test; this checks the window.
func TestForcedDisagreementRaisesKGWUI012(t *testing.T) {
	const pollInterval = "--poll-interval=10m"
	if !slices.Contains(uiArgs(t), pollInterval) {
		t.Skip("needs the UI installed with " + pollInterval + " (UI_EXTRA_ARGS=" + pollInterval +
			"), so only the scheduled deadline, never the safety timer, can mature the check")
	}
	settledWorld(t)
	const route = "flip/flip"
	eventually(t, func() error {
		if s := routeCheck(t, route); s != string(computed.StateChecked) {
			return fmt.Errorf("%s: check %s before the skew, want checked", route, s)
		}
		return nil
	})

	t.Cleanup(func() {
		for _, args := range [][]string{
			{"label", "namespace", "flip", "edge-https=allowed", "--overwrite"},
			{"-n", "kgw-a", "scale", "deploy/kgateway", "--replicas=1"},
			{"-n", "kgw-a", "rollout", "status", "deploy/kgateway", "--timeout=180s"},
		} {
			if out, err := kubectlRun(t, args...); err != nil {
				t.Errorf("cleanup: kubectl %v: %v: %s", args, err, out)
			}
		}
		// Later tests and Playwright expect the agreeing world back.
		settledWorld(t)
		eventually(t, func() error {
			if s := routeCheck(t, route); s != string(computed.StateChecked) {
				return fmt.Errorf("cleanup: %s check %s, want checked", route, s)
			}
			return nil
		})
	})

	must(t, "-n", "kgw-a", "scale", "deploy/kgateway", "--replicas=0")
	eventually(t, func() error {
		if n := controllerPods(t); n != 0 {
			return fmt.Errorf("%d controller pods left", n)
		}
		return nil
	})
	must(t, "label", "namespace", "flip", "edge-https-")

	// The check waits out its persistence window as pending.
	deadline := time.Now().Add(time.Minute)
	for routeCheck(t, route) != string(computed.StatePending) {
		if time.Now().After(deadline) {
			t.Fatalf("%s: check never pending after the relabel", route)
		}
		time.Sleep(time.Second)
	}

	// Each finding is checked on the first health response that carries it.
	// collectedAt is the UI's clock, as is since, so no clock skew enters.
	type subject struct{ kind, id string }
	subjects := []subject{{"HTTPRoute", route}, {"Gateway", "infra/edge"}}
	seen := map[subject]bool{}
	deadline = time.Now().Add(2 * time.Minute)
	for len(seen) < len(subjects) {
		var h api.HealthResponse
		get(t, install+"/health", &h)
		for _, s := range subjects {
			f := disagreement(h, s.kind, s.id)
			if f == nil || seen[s] {
				continue
			}
			seen[s] = true
			if f.Since == nil || f.Severity != health.SeverityWarning {
				t.Fatalf("KGWUI-012 on %s %s: severity %s, since %v; want a warning with since", s.kind, s.id, f.Severity, f.Since)
			}
			early, late := f.Since.Add(30*time.Second), f.Since.Add(90*time.Second)
			if h.CollectedAt.Before(early) || h.CollectedAt.After(late) {
				t.Errorf("KGWUI-012 on %s %s at %s: want within [since+30s, since+90s] = [%s, %s]",
					s.kind, s.id, h.CollectedAt.Format(time.RFC3339Nano), early.Format(time.RFC3339Nano), late.Format(time.RFC3339Nano))
			}
			t.Logf("KGWUI-012 on %s %s: since %s, first seen in a model of %s", s.kind, s.id,
				f.Since.Format(time.RFC3339Nano), h.CollectedAt.Format(time.RFC3339Nano))
		}
		if time.Now().After(deadline) {
			t.Fatalf("KGWUI-012 never raised on every subject: seen %v, findings %+v", seen, h.Findings)
		}
		time.Sleep(time.Second)
	}

	// The controller writes the skewed world's status, which now agrees.
	must(t, "-n", "kgw-a", "scale", "deploy/kgateway", "--replicas=1")
	deadline = time.Now().Add(2 * time.Minute)
	for {
		var h api.HealthResponse
		get(t, install+"/health", &h)
		left := 0
		for _, f := range h.Findings {
			if f.Code == health.CodeComputedDisagrees {
				left++
			}
		}
		if left == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("KGWUI-012 did not clear within 2 min of scaling the controller back: %+v", h.Findings)
		}
		time.Sleep(time.Second)
	}
}
