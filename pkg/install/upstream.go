package install

import (
	"cmp"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/DuncanDoyle/snellius/pkg/kinds"
)

// UpstreamControllerName is the GatewayClass controllerName of upstream kgateway,
// used by both the "kgateway" and "kgateway-waypoint" classes.
const UpstreamControllerName = "kgateway.dev/kgateway"

// UpstreamDetector recognises upstream kgateway controllers: Helm labels
// kgateway=kgateway and app.kubernetes.io/component=controller, and an image
// whose repository name is "kgateway" (mirrors keep that name).
type UpstreamDetector struct{}

// Name implements Detector.
func (UpstreamDetector) Name() string { return "kgateway" }

// ControllerSelector implements Detector.
func (UpstreamDetector) ControllerSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"kgateway": "kgateway", "app.kubernetes.io/component": "controller"})
}

// ControllerPodSelector implements Detector. Controller Pods carry the
// component label only from chart v2.3.0 on, so the watch uses kgateway=kgateway.
func (UpstreamDetector) ControllerPodSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"kgateway": "kgateway"})
}

// ProxySelector implements Detector.
func (UpstreamDetector) ProxySelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"app.kubernetes.io/managed-by": "kgateway", "app.kubernetes.io/component": "proxy"})
}

// ProxyPodSelector implements ProxyPodDetector with the proxy Pod template's
// labels; the Deployment's managed-by label is not on the Pods.
func (UpstreamDetector) ProxyPodSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"kgateway": "kube-gateway", "app.kubernetes.io/component": "proxy"})
}

// Detect implements Detector.
func (UpstreamDetector) Detect(d *appsv1.Deployment) (Install, bool) {
	for _, c := range d.Spec.Template.Spec.Containers {
		repo, tag := splitImage(c.Image)
		if repo != "kgateway" {
			continue
		}
		in := Install{Namespace: d.Namespace, Name: d.Name, Edition: "kgateway", Container: c.Name,
			ControllerNames: []string{UpstreamControllerName}, Deployment: d, MetricsPort: 9092,
			ProxySelector: UpstreamDetector{}.ProxySelector()}
		in.Version = d.Labels["app.kubernetes.io/version"]
		if in.Version == "" {
			in.Version = strings.TrimPrefix(tag, "v")
		}
		for _, p := range c.Ports {
			if p.Name == "metrics" {
				in.MetricsPort = p.ContainerPort
			}
		}
		return in, true
	}
	return Install{}, false
}

// splitImage returns the last path segment of the repository and the tag
// ("" for digest-only references). "reg:5000/a/kgateway:v1" → ("kgateway", "v1").
func splitImage(image string) (repo, tag string) {
	if i := strings.Index(image, "@"); i >= 0 {
		image = image[:i]
	}
	last := image[strings.LastIndex(image, "/")+1:]
	if i := strings.Index(last, ":"); i >= 0 {
		return last[:i], last[i+1:]
	}
	return last, ""
}

// KgatewayVersion returns the kgateway version the controller runs, or ""
// when unknown: ReportedVersion when the controller reports one, else Version.
// Other distributions number their own releases (D-018 §3), so they report
// BaseVersion, which their detector may leave empty.
func (i Install) KgatewayVersion() string {
	if i.Edition != "kgateway" {
		return i.BaseVersion
	}
	return cmp.Or(i.ReportedVersion, i.Version)
}

// UpstreamExperimentalOnly is kgateway's experimental-only classification of
// served kind k for inst, from the table in pkg/kinds at inst's KgatewayVersion.
func UpstreamExperimentalOnly(k kinds.Served, inst Install) bool {
	return k.ExperimentalOnly(inst.KgatewayVersion())
}

// ControllerVersion is the version the UI shows for the controller: for
// kgateway, KgatewayVersion; a downstream build shows its own Version.
func (i Install) ControllerVersion() string {
	if i.Edition != "kgateway" {
		return i.Version
	}
	return i.KgatewayVersion()
}
