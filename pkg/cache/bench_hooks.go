//go:build bench

package cache

import "k8s.io/apimachinery/pkg/runtime/schema"

// StopOptional stops c's optional watches, waits for them to return and drops the
// proxy Pod, Event and CRD stores, Pod UID index and Event histories, so a heap
// measurement can free them alone; the Service and EndpointSlice stores stay. No Snapshot may run during or after it.
func StopOptional(c *Cache) {
	if c.stopOptional != nil {
		c.stopOptional()
	}
	c.optional.Wait()
	// The first len(c.pods) registrations are on the controller Pod informers, which keep running.
	for i, reg := range c.uidRegs[:min(len(c.uidRegs), len(c.pods))] {
		if reg != nil {
			_ = c.pods[i].inf.RemoveEventHandler(reg)
		}
	}
	c.proxyPods, c.events, c.crds, c.evf, c.uids, c.uidRegs = nil, nil, nil, nil, nil, nil
}

// DropListed drops c's Service and EndpointSlice watches and the informers of
// gvrs, so a heap measurement can free their stores alone. Call it once c has stopped and Wait returned.
func DropListed(c *Cache, gvrs ...schema.GroupVersionResource) {
	for _, g := range gvrs {
		delete(c.dyn, g)
	}
	c.services, c.slices = nil, nil
}
