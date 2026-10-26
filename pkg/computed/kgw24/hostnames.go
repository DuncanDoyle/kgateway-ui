package kgw24

import "strings"

// intersect computes the effective hostnames of a route on a listener and
// whether the route attaches (rules §4.3). Output keeps first-seen order.
func intersect(listener string, route []string) (ok bool, eff []string) {
	if len(route) == 0 {
		if listener == "" {
			return true, nil
		}
		return true, []string{listener}
	}
	// No listener hostname: the route's hostnames as written, duplicates kept.
	if listener == "" {
		return true, append([]string(nil), route...)
	}
	seen := map[string]bool{}
	for _, r := range route {
		h, m := intersectOne(listener, r)
		if m && !seen[h] {
			seen[h] = true
			eff = append(eff, h)
		}
	}
	return len(eff) > 0, eff
}

// intersectOne intersects one listener hostname with one route hostname.
func intersectOne(l, r string) (string, bool) {
	if l == r {
		return l, true
	}
	lw, rw := strings.HasPrefix(l, "*."), strings.HasPrefix(r, "*.")
	switch {
	case lw && rw:
		// The longer suffix is the more specific wildcard.
		if strings.HasSuffix(r, l[1:]) {
			return r, true
		}
		if strings.HasSuffix(l, r[1:]) {
			return l, true
		}
	case lw:
		if strings.HasSuffix(r, l[1:]) {
			return r, true
		}
	case rw:
		if strings.HasSuffix(l, r[1:]) {
			return l, true
		}
	}
	return "", false
}
