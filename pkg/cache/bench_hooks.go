//go:build bench

package cache

// StopOptional stops c's proxy Pod, Event and CRD watches, waits for them to
// return and drops their stores, Pod UID index and Event histories, so a heap
// measurement can free them alone. No Snapshot may run during or after it.
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
