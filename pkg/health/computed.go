package health

import (
	"fmt"
	"strconv"

	"github.com/DuncanDoyle/kgateway-ui/pkg/computed"
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
		for i := range v.Checks {
			ch := &v.Checks[i]
			if ch.State != computed.StateDisagrees {
				continue
			}
			on, since := ch.On, ch.Since
			fs = append(fs, Finding{Code: CodeComputedDisagrees, Slug: "computed-view-disagrees", Severity: SeverityWarning,
				Install: in.Install.ID(), Resource: &on, Message: ComputedDisagreesMessage, Detail: disagreeDetail(*ch, v.RuleSet),
				Since: &since, Evidence: ch.Deps})
		}
	}
	return fs, ok(c.Name(), false, in.Now)
}

// disagreeDetail says what was computed and what the controller reports, and
// under which rule set (draft, Task 48).
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
