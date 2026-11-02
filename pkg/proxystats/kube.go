package proxystats

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"syscall"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/DuncanDoyle/kgateway-ui/pkg/cache"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/objects"
	"github.com/DuncanDoyle/kgateway-ui/pkg/process"
)

// StatsPort is the kgateway proxy's stats listener; Envoy's admin port is never read.
const StatsPort = 9091

// Kube is the Kubernetes Source: the running Pods of each Gateway's proxy
// Deployment, their stats listener's /stats, and Envoy's text format.
type Kube struct {
	// Client fetches over the Pod IP, in-cluster; nil means NewHTTPClient().
	// Redirects are never followed, whatever its own policy.
	Client *http.Client
	// Kube, when set, fetches through pods/proxy with the user's own rights
	// instead; build it with NewPodProxyClient so redirects are not followed.
	Kube kubernetes.Interface
	// Port is the stats listener port; zero means StatsPort.
	Port int32
}

// Targets implements Source: the running, non-terminating Pods of gw's proxy
// Deployment. Without the Deployment or the proxy Pod watch, as for a
// self-managed Gateway, they are not observed.
func (k Kube) Targets(in install.Resolved, gw objects.Ref, snap *cache.Snapshot) ([]Target, bool) {
	if snap == nil || !snap.Access[cache.ProxyPodsGVR].OK {
		return nil, false
	}
	d := process.ProxyDeployment(gw, snap.Deployments, in.ProxySelector)
	if d == nil {
		return nil, false
	}
	sel, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	// An empty selector would claim every Pod in the namespace.
	if err != nil || sel.Empty() {
		return nil, false
	}
	port := strconv.Itoa(int(k.port()))
	out := []Target{}
	for _, p := range snap.ProxyPods {
		if p.Namespace != d.Namespace || p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning || p.Status.PodIP == "" ||
			!sel.Matches(labels.Set(p.Labels)) || !ownedBy(p, d) {
			continue
		}
		out = append(out, Target{ID: string(p.UID), Name: p.Name, Gateway: gw, Addr: net.JoinHostPort(p.Status.PodIP, port)})
	}
	slices.SortFunc(out, func(a, b Target) int { return strings.Compare(a.Name, b.Name) })
	return out, true
}

// ownedBy reports whether p's controller is a ReplicaSet of d: kind, API
// group and the "<deployment>-<pod-template-hash>" name. The cache holds no
// ReplicaSets, so their UIDs cannot be matched.
func ownedBy(p *corev1.Pod, d *appsv1.Deployment) bool {
	o := metav1.GetControllerOf(p)
	hash := p.Labels[appsv1.DefaultDeploymentUniqueLabelKey]
	return o != nil && hash != "" && o.Kind == "ReplicaSet" && o.APIVersion == "apps/v1" && o.Name == d.Name+"-"+hash
}

func (k Kube) port() int32 {
	if k.Port == 0 {
		return StatsPort
	}
	return k.Port
}

// statsQuery is the /stats query: used stats only, by StatsFilter.
var statsQuery = "usedonly&filter=" + url.QueryEscape(StatsFilter)

// Fetch implements Source. It reads at most MaxBody + 1 bytes, and its errors
// are fixed text: the response body never reaches an error or a log.
func (k Kube) Fetch(ctx context.Context, t Target) ([]byte, error) {
	var body io.ReadCloser
	var err error
	if k.Kube != nil {
		// The Pod is in the Gateway's namespace (process.ProxyDeployment).
		rw := k.Kube.CoreV1().Pods(t.Gateway.Namespace).ProxyGet("http", t.Name, strconv.Itoa(int(k.port())), "/stats",
			map[string]string{"usedonly": "", "filter": StatsFilter})
		// A fake clientset without a proxy reactor returns no wrapper.
		if rw == nil {
			return nil, errors.New("pods/proxy unavailable")
		}
		body, err = rw.Stream(ctx)
	} else {
		body, err = k.get(ctx, "http://"+t.Addr+"/stats?"+statsQuery)
	}
	if err != nil {
		return nil, classify(ctx, err)
	}
	defer func() { _ = body.Close() }()
	b, err := io.ReadAll(io.LimitReader(body, MaxBody+1))
	if err != nil {
		return nil, classify(ctx, err)
	}
	return b, nil
}

// get issues one GET; a status other than 200 is an error with the code only.
func (k Kube) get(ctx context.Context, u string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c := defaultClient
	if k.Client != nil {
		cp := *k.Client
		cp.CheckRedirect = noRedirect
		c = &cp
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, statusError(resp.StatusCode)
	}
	return resp.Body, nil
}

// noRedirect stops at a 3xx, which then fails as a status error.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

var defaultClient = NewHTTPClient()

// NewHTTPClient is the in-cluster client: no proxy from the environment, no
// redirects; the collector bounds each request through its context.
func NewHTTPClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	return &http.Client{Transport: t, CheckRedirect: noRedirect}
}

// NewPodProxyClient is a clientset over cfg whose requests never follow a
// redirect, for Kube.Kube; a 3xx through pods/proxy fails as a status error.
func NewPodProxyClient(cfg *rest.Config) (kubernetes.Interface, error) {
	hc, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, err
	}
	hc.CheckRedirect = noRedirect
	return kubernetes.NewForConfigAndClient(cfg, hc)
}

type statusError int

func (s statusError) Error() string { return fmt.Sprintf("HTTP %d", int(s)) }

// classify maps err to fixed text.
func classify(ctx context.Context, err error) error {
	var s statusError
	var api apierrors.APIStatus
	switch {
	case ctx.Err() != nil:
		return errors.New("timed out")
	case errors.As(err, &s):
		return s
	case apierrors.IsForbidden(err):
		return errors.New("pods/proxy denied")
	case errors.As(err, &api):
		return statusError(int(api.Status().Code))
	case errors.Is(err, syscall.ECONNREFUSED):
		return errors.New("connection refused")
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return errors.New("timed out")
	}
	return errors.New("stats listener unreachable")
}

// Interpret implements Source with Envoy's text format.
func (Kube) Interpret(t Target, body []byte) (PodStats, error) {
	p, err := ParseEnvoy(body)
	p.UID, p.Name, p.Gateway = t.ID, t.Name, t.Gateway
	return p, err
}

var _ Source = Kube{}
