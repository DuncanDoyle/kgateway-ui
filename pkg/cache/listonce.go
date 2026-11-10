package cache

import (
	"context"
	"errors"
	"slices"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	toolscache "k8s.io/client-go/tools/cache"
)

// Snapshot modes: from a Cache's informers, or from ListOnce's LISTs.
const (
	ModeWatch = "watch"
	ModeList  = "list"
)

// ListOnce fills the snapshot a synced Cache over cfg would give from one LIST
// per input, and never watches. cfg.Changes is ignored.
//
// It returns an error only for a Config without clients or a cancelled ctx. A
// LIST that fails shows in Access: a 403 is denied, anything else failed and
// holds back Synced when the input is required.
func ListOnce(ctx context.Context, cfg Config) (*Snapshot, error) {
	if cfg.Dynamic == nil || cfg.Kube == nil {
		return nil, errors.New("cache: ListOnce needs a dynamic and a typed client")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg.Changes = nil
	c := New(cfg)
	c.listed = true
	inputs := c.all()
	for gvr, t := range c.dyn {
		if c.listOnly[gvr] {
			inputs = append(inputs, t)
		}
	}
	inputs = append(inputs, c.proxyPods...)
	for _, t := range []*tracked{c.crds, c.services, c.slices} {
		if t != nil {
			inputs = append(inputs, *t)
		}
	}
	var wg sync.WaitGroup
	for _, t := range inputs {
		wg.Go(func() { t.listOnce(ctx) })
	}
	wg.Wait()
	// Events go last, so the listed Events find their Pods, as in Start.
	if c.events != nil {
		for _, t := range append(slices.Clone(c.pods), c.proxyPods...) {
			for _, o := range t.inf.GetStore().List() {
				c.uids.add(o, 1)
			}
		}
		c.events.listOnce(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s := c.Snapshot()
	s.Mode = ModeList
	return s, nil
}

// listOnce fills t's store from one call of its instrumented List, which
// records a failure, through its transform as the informer would.
func (t tracked) listOnce(ctx context.Context) {
	l, err := t.list(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	if err := replace(t.inf.GetStore(), l, t.strip); err != nil {
		t.progress.failure(err)
		return
	}
	t.progress.mu.Lock()
	t.progress.listed = true
	t.progress.mu.Unlock()
}

// replace sets store to the items of list l, each passed through strip when set.
func replace(store toolscache.Store, l runtime.Object, strip toolscache.TransformFunc) error {
	items, err := meta.ExtractListWithAlloc(l)
	if err != nil {
		return err
	}
	objs := make([]any, len(items))
	for i, o := range items {
		objs[i] = o
		if strip != nil {
			if objs[i], err = strip(o); err != nil {
				return err
			}
		}
	}
	rv := ""
	if m, err := meta.ListAccessor(l); err == nil {
		rv = m.GetResourceVersion()
	}
	return store.Replace(objs, rv)
}
