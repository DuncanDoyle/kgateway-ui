package cache

import (
	"context"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/metadata"

	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
)

// CRDsGVR is the Access key of the Gateway API CRD metadata watch.
var CRDsGVR = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}

// CRDsKind is the kind a check declares in its InputSet to read Snapshot.CRDs.
var CRDsKind = objects.GroupKind{Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition"}

// Annotations the Gateway API bundle sets on each of its CRDs.
const (
	BundleVersionAnnotation = "gateway.networking.k8s.io/bundle-version"
	ChannelAnnotation       = "gateway.networking.k8s.io/channel"
)

// gatewayAPIGroups are the API groups whose CRDs the cache keeps.
var gatewayAPIGroups = []string{"gateway.networking.k8s.io", "gateway.networking.x-k8s.io"}

// CRDMeta is the metadata of one Gateway API CRD.
type CRDMeta struct {
	Name, Group, BundleVersion, Channel string
}

// crdGroup is the group part of a CRD name, "<plural>.<group>".
func crdGroup(name string) string {
	_, g, _ := strings.Cut(name, ".")
	return g
}

func gatewayCRD(name string) bool { return slices.Contains(gatewayAPIGroups, crdGroup(name)) }

// stripCRD keeps the name, identity and the two bundle annotations.
func stripCRD(o any) (any, error) {
	m, ok := o.(*metav1.PartialObjectMetadata)
	if !ok {
		return o, nil
	}
	out := &metav1.PartialObjectMetadata{TypeMeta: m.TypeMeta,
		ObjectMeta: metav1.ObjectMeta{Name: m.Name, UID: m.UID, ResourceVersion: m.ResourceVersion}}
	for _, k := range []string{BundleVersionAnnotation, ChannelAnnotation} {
		if v, ok := m.Annotations[k]; ok {
			if out.Annotations == nil {
				out.Annotations = map[string]string{}
			}
			out.Annotations[k] = v
		}
	}
	return out, nil
}

func crdMeta(m *metav1.PartialObjectMetadata) CRDMeta {
	return CRDMeta{Name: m.Name, Group: crdGroup(m.Name), BundleVersion: m.Annotations[BundleVersionAnnotation], Channel: m.Annotations[ChannelAnnotation]}
}

// crdInformer watches CRD metadata and keeps only the Gateway API groups.
// The API server cannot select CRDs by group, so the filter runs client-side.
func (c *Cache) crdInformer(md metadata.Interface) tracked {
	ri := md.Resource(CRDsGVR)
	t := c.informer(md, &metav1.PartialObjectMetadata{},
		func(ctx context.Context, o metav1.ListOptions) (runtime.Object, error) {
			l, err := ri.List(ctx, o)
			if err != nil {
				return nil, err
			}
			l.Items = slices.DeleteFunc(l.Items, func(m metav1.PartialObjectMetadata) bool { return !gatewayCRD(m.Name) })
			return l, nil
		},
		func(ctx context.Context, o metav1.ListOptions) (watch.Interface, error) {
			w, err := ri.Watch(ctx, o)
			if err != nil {
				return nil, err
			}
			return newFilterWatch(w, func(e watch.Event) []watch.Event {
				if m, ok := e.Object.(*metav1.PartialObjectMetadata); ok && e.Type != watch.Bookmark && e.Type != watch.Error && !gatewayCRD(m.Name) {
					return nil
				}
				return []watch.Event{e}
			}), nil
		})
	_ = t.inf.SetTransform(stripCRD) // fails only on a started informer
	return t
}
