package api

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
)

// maxTrailRefs is the most refs one GET …/refs resolves: the breadcrumb trail's limit (design §17.6).
const maxTrailRefs = 6

// The parts of a ref parameter, as the SPA's trail validates them.
var (
	trailGroup     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
	trailKind      = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,62}$`)
	trailNamespace = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	trailName      = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
)

// TrailRefs is GET …/refs: each requested ref with its link, in request order.
type TrailRefs struct {
	Install string      `json:"install"`
	Refs    []LinkedRef `json:"refs"`
}

// parseTrailRefs reads 1–maxTrailRefs repeated ref=group/Kind/namespace/name
// parameters, "core" for the empty group and "-" for no namespace, of kinds served.
func parseTrailRefs(r *http.Request, served func(objects.GroupKind) bool) ([]objects.Ref, error) {
	vals, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, badParam("malformed query: %v", err)
	}
	for name := range vals {
		if name != "ref" {
			return nil, badParam("unknown parameter %q", name)
		}
	}
	vs := vals["ref"]
	if len(vs) == 0 || len(vs) > maxTrailRefs {
		return nil, badParam("ref: want 1–%d", maxTrailRefs)
	}
	out := make([]objects.Ref, 0, len(vs))
	for _, v := range vs {
		s := strings.Split(v, "/")
		if len(s) != 4 {
			return nil, badParam("ref %q: want group/Kind/namespace/name", v)
		}
		ref := objects.Ref{Group: s[0], Kind: s[1], Namespace: s[2], Name: s[3]}
		if ref.Group == coreGroup {
			ref.Group = ""
		} else if len(ref.Group) > 253 || !trailGroup.MatchString(ref.Group) {
			return nil, badParam("ref %q: bad group", v)
		}
		if ref.Namespace == "-" {
			ref.Namespace = ""
		} else if len(ref.Namespace) > 63 || !trailNamespace.MatchString(ref.Namespace) {
			return nil, badParam("ref %q: bad namespace", v)
		}
		if !trailKind.MatchString(ref.Kind) || len(ref.Name) > 253 || !trailName.MatchString(ref.Name) {
			return nil, badParam("ref %q: bad kind or name", v)
		}
		if !served(objects.GroupKind{Group: ref.Group, Kind: ref.Kind}) {
			return nil, badParam("ref %q: kind not served", v)
		}
		out = append(out, ref)
	}
	return out, nil
}

// refs labels the breadcrumb trail's objects as every LinkedRef is labelled:
// hidden and absent refs both get none (D-013).
func (h *handler) refs(w http.ResponseWriter, r *http.Request) {
	p, i := h.find(w, r)
	if i == nil {
		return
	}
	workloads := h.src.Platform().Has(platform.Workloads)
	served := func(gk objects.GroupKind) bool {
		if workloads && gk.Group == deploymentKind.Group && gk.Kind == deploymentKind.Kind {
			return true
		}
		for _, k := range p.Model.Served {
			if k.Group == gk.Group && k.Kind == gk.Kind {
				return true
			}
		}
		return false
	}
	want, err := parseTrailRefs(r, served)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	l := linkerFor(h.view(r), p.Model, i)
	out := TrailRefs{Install: i.Resolved.ID(), Refs: make([]LinkedRef, 0, len(want))}
	for _, ref := range want {
		out.Refs = append(out.Refs, l.linked(ref))
	}
	writeJSON(w, out)
}
