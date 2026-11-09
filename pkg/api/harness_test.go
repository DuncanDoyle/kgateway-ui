package api

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/health"
)

// slowProxyPods delays the first list of the proxy Pod watch, as a loaded
// machine can, so the required watches sync well before it.
type slowProxyPods struct{ kubernetes.Interface }

func (k slowProxyPods) CoreV1() corev1client.CoreV1Interface { return slowCore{k.Interface.CoreV1()} }

// IsWatchListSemanticsUnSupported keeps the fake's answer, so informers list.
func (slowProxyPods) IsWatchListSemanticsUnSupported() bool { return true }

type slowCore struct{ corev1client.CoreV1Interface }

func (c slowCore) Pods(ns string) corev1client.PodInterface {
	return slowPods{c.CoreV1Interface.Pods(ns)}
}

type slowPods struct{ corev1client.PodInterface }

func (p slowPods) List(ctx context.Context, o metav1.ListOptions) (*corev1.PodList, error) {
	if strings.Contains(o.LabelSelector, "kube-gateway") {
		time.Sleep(300 * time.Millisecond)
	}
	return p.PodInterface.List(ctx, o)
}

// The harness computes its first model only once the optional watches it
// started have read, so a finding first seen at UI start keeps that since
// however slowly the proxy Pod watch syncs (the version-skew golden).
func TestHarnessWaitsForOptionalWatches(t *testing.T) {
	h := newHarness(t, setup{proxyPods: true, wrapKube: func(k kubernetes.Interface) kubernetes.Interface { return slowProxyPods{k} }},
		skewWorld("v2.3.6", testutil.Old.Time)...)
	rep := h.at(t, testutil.Now)
	want := testutil.Now.Add(-health.Grace - time.Second)
	for _, f := range rep.Findings {
		if f.Code == health.CodeVersionSkew {
			if f.Since == nil || !f.Since.Equal(want) {
				t.Fatalf("022 since %v, want %v: the first compute ran before the proxy Pods were read", f.Since, want)
			}
			return
		}
	}
	t.Fatal("no KGWUI-022: the model was computed before the proxy Pods were read")
}
