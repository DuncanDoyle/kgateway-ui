package health

import (
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// computedCheck raises KGWUI-012 for each cross-check the engine matured into
// state disagrees (computed-rules §9.3). It is optional and always OK.
type computedCheck struct{}

// Name implements Check.
func (computedCheck) Name() string { return "computed views" }

// Run implements Check.
func (c computedCheck) Run(in Input) ([]Finding, CheckResult) {
	var fs []Finding
	if v := in.Computed; v != nil {
		n := 0
		for i := range v.Checks {
			if v.Checks[i].State == computed.StateDisagrees {
				n++
			}
		}
		fs = make([]Finding, 0, n)
		var owned map[objects.Ref]*unstructured.Unstructured
		if n > 0 {
			owned = ownedResources(in.Owned)
		}
		for i := range v.Checks {
			ch := &v.Checks[i]
			if ch.State != computed.StateDisagrees {
				continue
			}
			on, since := ch.On, ch.Since
			f := Finding{Code: CodeComputedDisagrees, Slug: "computed-view-disagrees", Severity: SeverityWarning,
				Install: in.Install.ID(), Resource: &on, Message: ComputedDisagreesMessage, Detail: disagreeDetail(*ch, v.RuleSet),
				Since: &since, Evidence: ch.Deps}
			// A caller who cannot view every dep gets the resource's own first-seen (design §7.4 rule 7).
			if u := ownedObject(in.Owned, owned, on); u != nil {
				own := in.firstSeen(u)
				f.ResourceSince = &own
			}
			fs = append(fs, f)
		}
	}
	return fs, ok(c.Name(), false, in.Now)
}

// ownedResources maps the owned Gateways, then ListenerSets, by ref; the first of duplicates wins.
func ownedResources(o *install.Owned) map[objects.Ref]*unstructured.Unstructured {
	if o == nil {
		return nil
	}
	m := make(map[objects.Ref]*unstructured.Unstructured, len(o.Gateways)+len(o.ListenerSets))
	for _, us := range [][]*unstructured.Unstructured{o.Gateways, o.ListenerSets} {
		for _, u := range us {
			if r := objects.RefOf(u); m[r] == nil {
				m[r] = u
			}
		}
	}
	return m
}

// ownedObject returns the owned Gateway or ListenerSet (from resources) or route ref, or nil.
func ownedObject(o *install.Owned, resources map[objects.Ref]*unstructured.Unstructured, ref objects.Ref) *unstructured.Unstructured {
	if o == nil {
		return nil
	}
	if u := resources[ref]; u != nil {
		return u
	}
	if r, ok := o.Route(ref); ok {
		return r.Obj
	}
	return nil
}

// disagreeDetail says what was computed and what the controller reports, and
// under which rule set (wording pass, Task 48).
func disagreeDetail(c computed.CheckResult, ruleSet string) string {
	var d string
	switch c.ID.Kind {
	case computed.KindListenerCount:
		d = fmt.Sprintf(countDisagrees, c.Listener, routes(c.Expected), c.Observed)
	default:
		parent := "`" + c.Key.Name + "`"
		if c.Key.SectionName != "" {
			parent += " (section `" + c.Key.SectionName + "`)"
		}
		d = fmt.Sprintf(parentDisagrees, parent, c.Expected, c.Observed)
	}
	d += fmt.Sprintf(ruleSetDetail, ruleSet)
	if c.Note != "" {
		d += " " + c.Note + "."
	}
	return d
}

// routes is "1 route" or "n routes"; a non-number is kept as is.
func routes(n string) string {
	switch v, err := strconv.Atoi(n); {
	case err != nil:
		return n
	case v == 1:
		return "1 route"
	}
	return n + " routes"
}
