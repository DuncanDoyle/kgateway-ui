package ctrlmetrics

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/clock"

	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

// Scraper fetches the /metrics payload of one controller pod.
type Scraper interface {
	Scrape(ctx context.Context, pod *corev1.Pod, port int32) (io.ReadCloser, error)
}

const (
	// maxBodyBytes caps one /metrics body; a larger body is an error, never parsed.
	maxBodyBytes = 8 << 20
	// maxPodTimeout caps the deadline of a single pod scrape.
	maxPodTimeout = 5 * time.Second
)

// defaultClient is used when HTTPScraper.Client is nil.
var defaultClient = &http.Client{Timeout: maxPodTimeout}

// HTTPScraper scrapes the pod IP directly (in-cluster mode). A nil Client
// uses one with a 5s timeout.
type HTTPScraper struct{ Client *http.Client }

// Scrape implements Scraper.
func (s HTTPScraper) Scrape(ctx context.Context, pod *corev1.Pod, port int32) (io.ReadCloser, error) {
	if pod.Status.PodIP == "" {
		return nil, fmt.Errorf("pod %s has no IP", pod.Name)
	}
	url := "http://" + net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(int(port))) + "/metrics"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/plain;version=0.0.4")
	client := s.Client
	if client == nil {
		client = defaultClient
	}
	resp, err := client.Do(req)
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

// SameContent reports whether r and o carry the same Err and Metrics.
// CollectedAt is not content: freshness is a deadline, not a change (D-019).
func (r Result) SameContent(o Result) bool {
	return r.Err == o.Err && r.Metrics.ManagedPresent == o.Metrics.ManagedPresent &&
		maps.EqualFunc(r.Metrics.Rejects, o.Metrics.Rejects, sameBits) && maps.EqualFunc(r.Metrics.Managed, o.Metrics.Managed, sameBits)
}

// sameBits compares samples by bit pattern, so NaN equals NaN, as the engine's
// metrics identity hashes them.
func sameBits(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }

// Source returns the latest Result per install ID.
type Source interface {
	Get(id string) (Result, bool)
}

// Collector periodically scrapes every install's ready controller pods.
type Collector struct {
	scraper Scraper
	// clock stamps results and drives Run's ticker.
	clock clock.WithTicker
	// maxBody is the largest accepted body; podTimeout bounds one pod scrape.
	maxBody    int64
	podTimeout time.Duration
	mu         sync.RWMutex
	results    map[string]Result
	// OnResult, if set, is notified after a stored result whose content differs
	// from the previous one for its install, or is its first. A result that
	// only renews CollectedAt does not notify; the engine's staleness deadline reads it.
	OnResult *trigger.Signal
}

// CollectorOption adjusts a Collector at construction.
type CollectorOption func(*Collector)

// WithClock makes the collector stamp results and tick with clk (tests).
func WithClock(clk clock.WithTicker) CollectorOption { return func(c *Collector) { c.clock = clk } }

// NewCollector returns a Collector using s and the real clock unless an option says otherwise.
func NewCollector(s Scraper, opts ...CollectorOption) *Collector {
	c := &Collector{scraper: s, clock: clock.RealClock{}, maxBody: maxBodyBytes, podTimeout: maxPodTimeout, results: map[string]Result{}}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Collect scrapes all ready pods of in and aggregates them. It fails if any
// ready pod cannot be scraped: a partial sum of rejects must not look like zero.
func (c *Collector) Collect(ctx context.Context, in install.Install) Result {
	var per []Metrics
	for _, p := range in.Pods {
		if !ready(p) {
			continue
		}
		m, err := c.scrapePod(ctx, p, in.MetricsPort)
		if err != nil {
			return Result{CollectedAt: c.clock.Now(), Err: fmt.Sprintf("pod %s: %v", p.Name, err)}
		}
		per = append(per, m)
	}
	if len(per) == 0 {
		return Result{CollectedAt: c.clock.Now(), Err: "no ready controller pod to scrape"}
	}
	return Result{Metrics: Aggregate(per), CollectedAt: c.clock.Now()}
}

// scrapePod scrapes and parses one pod under its own deadline and size cap.
func (c *Collector) scrapePod(ctx context.Context, p *corev1.Pod, port int32) (Metrics, error) {
	ctx, cancel := context.WithTimeout(ctx, c.podTimeout)
	defer cancel()
	body, err := c.scraper.Scrape(ctx, p, port)
	if err != nil {
		return Metrics{}, err
	}
	defer func() { _ = body.Close() }()
	// Read one byte past the cap to tell "exactly at the cap" from "too large".
	b, err := io.ReadAll(io.LimitReader(body, c.maxBody+1))
	if err != nil {
		return Metrics{}, err
	}
	if int64(len(b)) > c.maxBody {
		return Metrics{}, fmt.Errorf("metrics body exceeds %d bytes", c.maxBody)
	}
	m, err := Parse(bytes.NewReader(b))
	if err != nil {
		return Metrics{}, fmt.Errorf("parse: %v", err)
	}
	return m, nil
}

func ready(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// Run collects every interval until ctx is done and returns nil then. It
// returns an error if every <= 0. Installs are scraped concurrently, each pod
// under a deadline of min(every/2, 5s); a tick waits for all installs.
func (c *Collector) Run(ctx context.Context, every time.Duration, installs func() []install.Install) error {
	if every <= 0 {
		return errors.New("ctrlmetrics: collection interval must be positive")
	}
	c.podTimeout = min(every/2, maxPodTimeout)
	t := c.clock.NewTicker(every)
	defer t.Stop()
	for {
		var wg sync.WaitGroup
		for _, in := range installs() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := c.Collect(ctx, in)
				c.mu.Lock()
				prev, had := c.results[in.ID()]
				c.results[in.ID()] = r
				c.mu.Unlock()
				if !had || !prev.SameContent(r) {
					c.OnResult.Notify()
				}
			}()
		}
		wg.Wait()
		select {
		case <-ctx.Done():
			return nil
		case <-t.C():
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
