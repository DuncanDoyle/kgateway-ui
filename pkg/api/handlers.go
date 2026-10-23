package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/DuncanDoyle/kgateway-ui/pkg/engine"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/kinds"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/overview"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
	"github.com/DuncanDoyle/kgateway-ui/pkg/version"
)

// ModelSource provides the current publication and the platform it is computed for.
type ModelSource interface {
	Current() *engine.Published
	Platform() platform.Info
}

// NewHandler returns the HTTP handler for the API, the probes and the SPA in ui.
// Every response with resources, findings or counts goes through authz; a nil
// authz means AllowAll (D-013).
func NewHandler(src ModelSource, ui fs.FS, authz Authorizer) http.Handler {
	if authz == nil {
		authz = AllowAll{}
	}
	h := &handler{src: src, authz: authz}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.HandleFunc("GET /api/v1/meta", h.meta)
	mux.HandleFunc("GET /api/v1/installs", h.installs)
	mux.HandleFunc("GET /api/v1/installs/{id}/overview", h.overview)
	mux.HandleFunc("GET /api/v1/installs/{id}/health", h.health)
	mux.HandleFunc("GET /api/v1/installs/{id}/routes", h.routes)
	mux.HandleFunc("GET /api/v1/installs/{id}/gateways", h.gateways)
	mux.HandleFunc("GET /api/v1/installs/{id}/listenersets", h.listenerSets)
	mux.HandleFunc("GET /api/v1/installs/{id}/resources/{group}/{kind}/{rns}/{rname}", h.resource)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) { writeErr(w, http.StatusNotFound, "not found") })
	mux.Handle("/", spa(ui))
	return mux
}

type handler struct {
	src   ModelSource
	authz Authorizer
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}

// current returns the published model, or nil before the first publication.
func (h *handler) current() *engine.Model {
	if p := h.src.Current(); p != nil {
		return p.Model
	}
	return nil
}

// load returns the current publication, or writes 503 if its model is not
// ready. A request calls it once and serves everything from that one value.
func (h *handler) load(w http.ResponseWriter) *engine.Published {
	p := h.src.Current()
	if p == nil || !synced(p.Model) {
		writeErr(w, http.StatusServiceUnavailable, "connecting to cluster: cache not synced")
		return nil
	}
	return p
}

// synced reports whether m exists and was computed over a synced cache.
func synced(m *engine.Model) bool { return m != nil && m.Snapshot != nil && m.Snapshot.Synced }

func (h *handler) readyz(w http.ResponseWriter, _ *http.Request) {
	m := h.current()
	if !synced(m) {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	// An install with an invalid ID is a detector bug; it must not pass silently (D-016).
	if len(m.Rejected) > 0 {
		writeErr(w, http.StatusServiceUnavailable, "install rejected: "+strings.Join(m.Rejected, "; "))
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *handler) meta(w http.ResponseWriter, _ *http.Request) {
	m, plat := h.current(), h.src.Platform()
	resp := MetaResponse{Version: version.Version, Platform: plat.Name, Capabilities: append([]string{}, plat.Capabilities...), Kinds: []kinds.Served{}}
	if m != nil {
		resp.Pinned, resp.Synced = m.Pinned, synced(m)
		for _, k := range m.Served {
			if !isAddedAfterV1(k) {
				resp.Kinds = append(resp.Kinds, k)
			}
		}
	}
	// Deployments are linkable only where the platform runs workloads.
	if plat.Has(platform.Workloads) {
		resp.Kinds = append(resp.Kinds, deploymentKind)
	}
	if m != nil {
		for _, k := range m.Served {
			if isAddedAfterV1(k) {
				resp.Kinds = append(resp.Kinds, k)
			}
		}
	}
	writeJSON(w, resp)
}

// visibleHealth returns the install's report as the caller of r may see it
// (D-013). A finding on a denied resource keeps its code, severity and ref but
// is restricted: message, detail and since are emptied. State and counts still
// cover every finding, so partial visibility never makes an install look
// healthier, and the findings keep the order health returned.
func (h *handler) visibleHealth(r *http.Request, i *engine.InstallModel) health.Report {
	if allowsAll(h.authz) {
		return i.Health
	}
	rep := i.Health
	rep.Findings = make([]health.Finding, 0, len(i.Health.Findings))
	for _, f := range i.Health.Findings {
		if f.Resource != nil && !h.authz.Authorize(r, *f.Resource) {
			f.Message, f.Detail, f.Since, f.Restricted = "", "", nil, true
		}
		rep.Findings = append(rep.Findings, f)
	}
	return rep
}

// visibleOverview rebuilds the overview over the owned objects the caller of r
// may see: denied objects leave the count tiles and the gateway rows.
func (h *handler) visibleOverview(r *http.Request, m *engine.Model, i *engine.InstallModel, rep health.Report) overview.Overview {
	if allowsAll(h.authz) {
		return i.Overview
	}
	can := func(u *unstructured.Unstructured) bool { return h.authz.Authorize(r, objects.RefOf(u)) }
	keep := func(us []*unstructured.Unstructured) []*unstructured.Unstructured {
		out := []*unstructured.Unstructured{}
		for _, u := range us {
			if can(u) {
				out = append(out, u)
			}
		}
		return out
	}
	keepObjs := func(os []install.OwnedObject) []install.OwnedObject {
		out := []install.OwnedObject{}
		for _, o := range os {
			if can(o.Obj) {
				out = append(out, o)
			}
		}
		return out
	}
	o := i.Owned
	owned := &install.Owned{GatewayClasses: keep(o.GatewayClasses), Gateways: keep(o.Gateways), ListenerSets: keep(o.ListenerSets),
		Routes: keepObjs(o.Routes), Policies: keepObjs(o.Policies), Unattached: o.Unattached, Backends: keep(o.Backends)}
	return overview.Build(i.Resolved, owned, m.Snapshot, m.Served, rep, h.src.Platform())
}

func (h *handler) installs(w http.ResponseWriter, r *http.Request) {
	p := h.load(w)
	if p == nil {
		return
	}
	m := p.Model
	// A zero confirmation time is omitted, so clients fall back to computedAt.
	var confirmed *time.Time
	if !p.ConfirmedAt.IsZero() {
		at := wireTime(p.ConfirmedAt)
		confirmed = &at
	}
	out := []InstallSummary{}
	for k := range m.Installs {
		i := &m.Installs[k]
		hd, rep := i.Overview.Header, h.visibleHealth(r, i)
		out = append(out, InstallSummary{ID: hd.ID, DisplayName: hd.DisplayName, Namespace: hd.Namespace, Name: hd.Name, Version: hd.Version,
			Edition: hd.Edition, ControllerNames: hd.ControllerNames, Readiness: hd.Readiness, Scope: hd.Scope,
			Health: HealthSummary{State: rep.State, Counts: rep.Counts}, ComputedAt: wireTime(m.ComputedAt), ConfirmedAt: confirmed})
	}
	writeJSON(w, out)
}

// find loads the publication and the install named by the request, or writes 503 or 404.
func (h *handler) find(w http.ResponseWriter, r *http.Request) (*engine.Published, *engine.InstallModel) {
	p := h.load(w)
	if p == nil {
		return nil, nil
	}
	id := r.PathValue("id")
	for i := range p.Model.Installs {
		if p.Model.Installs[i].Resolved.ID() == id {
			return p, &p.Model.Installs[i]
		}
	}
	writeErr(w, http.StatusNotFound, "install "+id+" not found")
	return nil, nil
}

func (h *handler) overview(w http.ResponseWriter, r *http.Request) {
	if p, i := h.find(w, r); i != nil {
		ov := h.visibleOverview(r, p.Model, i, h.visibleHealth(r, i))
		rows, l := make([]OverviewGatewayRow, 0, len(ov.Gateways)), h.linker(r, p.Model, i)
		for _, g := range ov.Gateways {
			rows = append(rows, OverviewGatewayRow{GatewayRow: g, Link: l.link(g.Ref)})
		}
		writeJSON(w, OverviewResponse{Overview: ov, Gateways: rows, CollectedAt: wireTime(p.ConfirmedAt)})
	}
}

// wireTime is t as the API emits it: UTC. Internal clocks keep their zone and
// monotonic reading, which grace and staleness durations rely on.
func wireTime(t time.Time) time.Time { return t.UTC() }

// wireHealth is rep with every time in wire form and collectedAt set to the
// confirmation time (design §3.1). It copies the slices, since rep may share
// them with the model; the stored report is unchanged.
func wireHealth(rep health.Report, confirmedAt time.Time) health.Report {
	rep.CollectedAt = wireTime(confirmedAt)
	rep.Findings = slices.Clone(rep.Findings)
	for i, f := range rep.Findings {
		if f.Since != nil {
			at := wireTime(*f.Since)
			rep.Findings[i].Since = &at
		}
	}
	rep.Checks = slices.Clone(rep.Checks)
	for i := range rep.Checks {
		rep.Checks[i].CollectedAt = wireTime(rep.Checks[i].CollectedAt)
	}
	return rep
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if p, i := h.find(w, r); i != nil {
		writeJSON(w, h.healthResponse(r, p.Model, i, p.ConfirmedAt))
	}
}

// healthResponse is the caller's report in wire form: each finding's resource
// carries its link, and a visible finding whose detail quotes an object the
// caller may not view has that detail replaced (design §7.4 rule 7).
func (h *handler) healthResponse(r *http.Request, m *engine.Model, i *engine.InstallModel, confirmedAt time.Time) HealthResponse {
	rep := wireHealth(h.visibleHealth(r, i), confirmedAt)
	out := HealthResponse{Report: rep, Findings: make([]FindingDTO, 0, len(rep.Findings))}
	out.Report.Findings = nil // shadowed by out.Findings
	l := h.linker(r, m, i)
	for _, f := range rep.Findings {
		d := FindingDTO{Finding: f}
		if f.Resource != nil {
			lr := l.linked(*f.Resource)
			d.Resource = &lr
		}
		if !f.Restricted && !h.allVisible(r, f.Evidence) {
			d.Detail, d.EvidenceRestricted = restrictedEvidence, true
		}
		out.Findings = append(out.Findings, d)
	}
	return out
}

// resource serves one object the install admits (design §7.1) when the
// caller may view it and it exists: its link is not none. Only
// metadata.managedFields is stripped (D-013). Secrets are never watched, so
// they are never reachable. "-" stands for the empty group or namespace.
func (h *handler) resource(w http.ResponseWriter, r *http.Request) {
	p, i := h.find(w, r)
	if i == nil {
		return
	}
	dash := func(s string) string {
		if s == "-" {
			return ""
		}
		return s
	}
	want := objects.Ref{Group: dash(r.PathValue("group")), Kind: r.PathValue("kind"), Namespace: dash(r.PathValue("rns")), Name: r.PathValue("rname")}
	if h.linker(r, p.Model, i).link(want) != LinkNone {
		if obj := object(p.Model, want); obj != nil {
			// The body is the raw object, so the collection time travels in a header (D-017 §6).
			w.Header().Set("Collected-At", wireTime(p.ConfirmedAt).Format(time.RFC3339Nano))
			writeJSON(w, obj)
			return
		}
	}
	// Denied, absent and unadmitted objects get the same answer.
	writeErr(w, http.StatusNotFound, "resource not found in this install's scope")
}

// object returns the stored object ref names as a JSON object, or nil.
func object(m *engine.Model, ref objects.Ref) map[string]any {
	switch u, d := stored(m, ref); {
	case u != nil:
		return clean(u.Object)
	case d != nil:
		return deploymentObject(d)
	}
	return nil
}

// stored returns the object ref names in the snapshot: an object of a served
// kind, or a Deployment. Both are nil when it is absent or never served.
func stored(m *engine.Model, ref objects.Ref) (*unstructured.Unstructured, *appsv1.Deployment) {
	if ref.Group == deploymentKind.Group && ref.Kind == deploymentKind.Kind {
		for _, d := range m.Snapshot.Deployments {
			if d.Namespace == ref.Namespace && d.Name == ref.Name {
				return nil, d
			}
		}
		return nil, nil
	}
	for _, k := range m.Served {
		if k.Group != ref.Group || k.Kind != ref.Kind {
			continue
		}
		// The cache sorts each kind by "namespace/name".
		objs, key := m.Snapshot.Objects[k.GVR()], ref.Namespace+"/"+ref.Name
		if j, ok := slices.BinarySearchFunc(objs, key, func(u *unstructured.Unstructured, key string) int {
			return strings.Compare(u.GetNamespace()+"/"+u.GetName(), key)
		}); ok {
			return objs[j], nil
		}
	}
	return nil, nil
}

// clean returns a copy of obj without metadata.managedFields.
func clean(obj map[string]any) map[string]any {
	c := (&unstructured.Unstructured{Object: obj}).DeepCopy()
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	return c.Object
}

// deploymentObject returns d as served by the resource endpoint. Typed
// informers drop apiVersion and kind, so they are set here.
func deploymentObject(d *appsv1.Deployment) map[string]any {
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(d.DeepCopy())
	if err != nil {
		return nil
	}
	obj["apiVersion"], obj["kind"] = "apps/v1", "Deployment"
	return clean(obj)
}

// spa serves static files and falls back to index.html for client-side
// routes; directories are never listed.
func spa(ui fs.FS) http.Handler {
	files := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if st, err := fs.Stat(ui, p); err == nil && !st.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(ui, "index.html")
		if err != nil {
			http.Error(w, "UI not built", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})
}
