// Package install discovers kgateway controller installs, resolves the
// namespace scope each one watches, and assigns resources to installs.
package install

import (
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
)

// Install is one kgateway controller deployment and what the UI knows about it.
type Install struct {
	Namespace string
	Name      string
	// CustomID, when set, is the install ID; empty means "<namespace>.<name>".
	// It must pass ValidateID, or Discover leaves the install out (D-016).
	CustomID string
	// DisplayName names the install in the UI; empty means "namespace/name".
	DisplayName     string
	Version         string
	Edition         string
	ControllerNames []string
	// Container is the name of the controller container (the source of the env-based settings).
	Container   string
	Deployment  *appsv1.Deployment
	Pods        []*corev1.Pod
	MetricsPort int32
	// ProxySelector selects this distribution's gateway proxy Deployments; Discover
	// fills it from the detector when Detect leaves it nil.
	ProxySelector labels.Selector
}

// ID returns the opaque install ID: CustomID, else "<namespace>.<name>".
// Namespace names hold no dots, so the Kubernetes ID is unique; clients must
// not rely on its structure (D-016).
func (i Install) ID() string {
	if i.CustomID != "" {
		return i.CustomID
	}
	return i.Namespace + "." + i.Name
}

// Display returns DisplayName, else "namespace/name".
func (i Install) Display() string {
	if i.DisplayName != "" {
		return i.DisplayName
	}
	return i.Namespace + "/" + i.Name
}

// Detector recognises the controller Deployments of one distribution.
// Detectors are tried in order; the first that matches wins.
type Detector interface {
	Name() string
	// ControllerSelector selects controller Deployments; the cache watches only these.
	ControllerSelector() labels.Selector
	// ControllerPodSelector selects the controller Pods the cache watches. It may
	// be broader than the Pod labels of one chart version: Discover attributes
	// Pods to an install through the Deployment's spec.selector.
	ControllerPodSelector() labels.Selector
	// ProxySelector selects the gateway proxy Deployments of this distribution.
	ProxySelector() labels.Selector
	// Detect returns the Install for d, or false if d is not this distribution's controller.
	Detect(d *appsv1.Deployment) (Install, bool)
}

// Discover returns one Install per controller Deployment in snap, sorted by ID.
// An install whose ID fails ValidateID is left out and reported in errs.
func Discover(snap *cache.Snapshot, detectors []Detector) (out []Install, errs []error) {
	for _, d := range snap.Deployments {
		for _, det := range detectors {
			if !det.ControllerSelector().Matches(labels.Set(d.Labels)) {
				continue
			}
			in, ok := det.Detect(d)
			if !ok {
				continue
			}
			if err := ValidateID(in.ID()); err != nil {
				errs = append(errs, fmt.Errorf("detector %s, Deployment %s/%s: %w", det.Name(), d.Namespace, d.Name, err))
				break
			}
			// Checks rely on both; a detector may leave them unset.
			if in.Deployment == nil {
				in.Deployment = d
			}
			if in.ProxySelector == nil {
				in.ProxySelector = det.ProxySelector()
			}
			in.Pods = podsOf(d, snap.Pods)
			out = append(out, in)
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out, errs
}

func podsOf(d *appsv1.Deployment, pods []*corev1.Pod) []*corev1.Pod {
	sel, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return nil
	}
	var out []*corev1.Pod
	for _, p := range pods {
		if p.Namespace == d.Namespace && sel.Matches(labels.Set(p.Labels)) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
