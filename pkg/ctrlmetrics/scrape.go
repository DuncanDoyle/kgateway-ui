package ctrlmetrics

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
)

// Scraper fetches the /metrics payload of one controller pod.
type Scraper interface {
	Scrape(ctx context.Context, pod *corev1.Pod, port int32) (io.ReadCloser, error)
}

// HTTPScraper scrapes the pod IP directly (in-cluster mode).
type HTTPScraper struct{ Client *http.Client }

// Scrape implements Scraper.
func (s HTTPScraper) Scrape(ctx context.Context, pod *corev1.Pod, port int32) (io.ReadCloser, error) {
	url := "http://" + net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(int(port))) + "/metrics"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/plain;version=0.0.4")
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// PodProxyScraper scrapes through the API server's pods/proxy subresource
// (local mode; uses the user's kubeconfig rights).
type PodProxyScraper struct{ Kube kubernetes.Interface }

// Scrape implements Scraper.
func (s PodProxyScraper) Scrape(ctx context.Context, pod *corev1.Pod, port int32) (io.ReadCloser, error) {
	return s.Kube.CoreV1().Pods(pod.Namespace).ProxyGet("http", pod.Name, strconv.Itoa(int(port)), "/metrics", nil).Stream(ctx)
}

// Result is the outcome of collecting one install's metrics.
type Result struct {
	Metrics     Metrics
	CollectedAt time.Time
	// Err is non-empty when the metrics could not be collected completely.
	Err string
}

// Stale reports whether r was collected more than maxAge before now. A zero
// maxAge never makes a result stale. Consumers use 3 × the metrics interval,
// so a stalled collector cannot keep an old zero-reject result green.
func (r Result) Stale(now time.Time, maxAge time.Duration) bool {
	return maxAge > 0 && now.Sub(r.CollectedAt) > maxAge
}

// Source returns the latest Result per install ID.
type Source interface {
	Get(id string) (Result, bool)
}

// Collector periodically scrapes every install's ready controller pods.
type Collector struct {
	scraper Scraper
	now     func() time.Time
	mu      sync.RWMutex
	results map[string]Result
}

// NewCollector returns a Collector using s.
func NewCollector(s Scraper) *Collector {
	return &Collector{scraper: s, now: time.Now, results: map[string]Result{}}
}

// Collect scrapes all ready pods of in and aggregates them. It fails if any
// ready pod cannot be scraped: a partial sum of rejects must not look like zero.
func (c *Collector) Collect(ctx context.Context, in install.Install) Result {
	var per []Metrics
	for _, p := range in.Pods {
		if !ready(p) {
			continue
		}
		body, err := c.scraper.Scrape(ctx, p, in.MetricsPort)
		if err != nil {
			return Result{CollectedAt: c.now(), Err: fmt.Sprintf("pod %s: %v", p.Name, err)}
		}
		m, err := Parse(body)
		_ = body.Close()
		if err != nil {
			return Result{CollectedAt: c.now(), Err: fmt.Sprintf("pod %s: parse: %v", p.Name, err)}
		}
		per = append(per, m)
	}
	if len(per) == 0 {
		return Result{CollectedAt: c.now(), Err: "no ready controller pod to scrape"}
	}
	return Result{Metrics: Aggregate(per), CollectedAt: c.now()}
}

func ready(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// Run collects every interval until ctx is done.
func (c *Collector) Run(ctx context.Context, every time.Duration, installs func() []install.Install) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		for _, in := range installs() {
			sctx, cancel := context.WithTimeout(ctx, every)
			r := c.Collect(sctx, in)
			cancel()
			c.mu.Lock()
			c.results[in.ID()] = r
			c.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Get implements Source.
func (c *Collector) Get(id string) (Result, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.results[id]
	return r, ok
}
