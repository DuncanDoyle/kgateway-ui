package overview

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/health"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/platform"
)

// A zero Info is Kubernetes, the same default health applies.
func TestBuildTreatsZeroPlatformAsKubernetes(t *testing.T) {
	two := int32(2)
	res := install.Resolved{Install: install.Install{
		Namespace: "kgw", Name: "kgateway",
		Deployment: &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &two}, Status: appsv1.DeploymentStatus{ReadyReplicas: 2}},
	}}
	snap := &cache.Snapshot{}
	zero := Build(res, nil, snap, nil, health.Report{}, platform.Info{}).Header
	k8s := Build(res, nil, snap, nil, health.Report{}, platform.Kubernetes()).Header
	if zero.Readiness.State != ReadinessReady || zero.Readiness.Ready == nil || zero.Readiness.Desired == nil || zero.Scope == nil {
		t.Fatalf("zero platform must behave as Kubernetes: %+v", zero)
	}
	if zero.Readiness.State != k8s.Readiness.State || (zero.Scope == nil) != (k8s.Scope == nil) {
		t.Fatalf("zero %+v differs from Kubernetes %+v", zero, k8s)
	}
}
