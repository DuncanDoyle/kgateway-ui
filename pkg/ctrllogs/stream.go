package ctrllogs

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
)

// Kube is the Kubernetes Source: the install's controller Pods, read through
// pods/log with the UI's own Role (design §8.9).
type Kube struct{ Client kubernetes.Interface }

// Targets implements Source: the install's controller Pods that have started.
func (Kube) Targets(in install.Resolved) []Target {
	out := make([]Target, 0, len(in.Pods))
	for _, p := range in.Pods {
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		t := Target{ID: string(p.UID), Namespace: p.Namespace, Name: p.Name, Container: in.Container}
		for _, cs := range p.Status.ContainerStatuses {
			if cs.Name == in.Container || (in.Container == "" && len(p.Spec.Containers) == 1) {
				t.Restarts = cs.RestartCount
			}
		}
		out = append(out, t)
	}
	return out
}

// Follow implements Source.
func (k Kube) Follow(ctx context.Context, t Target, since time.Duration) (io.ReadCloser, error) {
	s := int64(math.Ceil(since.Seconds()))
	return k.open(ctx, t, &corev1.PodLogOptions{Container: t.Container, Follow: true, SinceSeconds: &s})
}

// Previous implements Source.
func (k Kube) Previous(ctx context.Context, t Target) (io.ReadCloser, error) {
	tail := int64(PreviousTailLines)
	return k.open(ctx, t, &corev1.PodLogOptions{Container: t.Container, Previous: true, TailLines: &tail})
}

func (k Kube) open(ctx context.Context, t Target, o *corev1.PodLogOptions) (io.ReadCloser, error) {
	rc, err := k.Client.CoreV1().Pods(t.Namespace).GetLogs(t.Name, o).Stream(ctx)
	if err != nil {
		return nil, classify(err)
	}
	return rc, nil
}

// classify wraps a log request error as ErrDenied or ErrNotReady; its text is
// the API status reason only, never a response body.
func classify(err error) error {
	switch {
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return fmt.Errorf("%w (%s)", ErrDenied, apierrors.ReasonForError(err))
	case apierrors.IsBadRequest(err) || apierrors.IsNotFound(err):
		return fmt.Errorf("%w (%s)", ErrNotReady, apierrors.ReasonForError(err))
	}
	return fmt.Errorf("controller logs: %s", apierrors.ReasonForError(err))
}
