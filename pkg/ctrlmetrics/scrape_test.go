package ctrlmetrics

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
	"github.com/DuncanDoyle/kgateway-ui/pkg/trigger"
)

type fakeScraper map[string]string // pod name → body; missing → error

func (f fakeScraper) Scrape(_ context.Context, p *corev1.Pod, _ int32) (io.ReadCloser, error) {
	b, ok := f[p.Name]
	if !ok {
		return nil, errors.New("connection refused")
	}
	return io.NopCloser(strings.NewReader(b)), nil
}

func inst(pods ...*corev1.Pod) install.Install {
	return install.Install{Namespace: "kgw", Name: "kgateway", Pods: pods, MetricsPort: 9092}
}

func TestCollectSumsOverReadyReplicas(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "kgateway:v2.4.7", 2, 2)
	p1, p2 := testutil.ControllerPod(d, "a", true), testutil.ControllerPod(d, "b", true)
	c := NewCollector(fakeScraper{"a": sample, "b": sample})
	r := c.Collect(context.Background(), inst(p1, p2))
	if r.Err != "" || r.Metrics.Rejects[GatewayKey{"infra", "edge"}] != 4 {
		t.Fatalf("got %+v", r)
	}
}

func TestCollectFailsWhenAnyReadyReplicaFails(t *testing.T) {
	// A missing replica may hold the rejects: an incomplete sum must not look like "no rejects".
	d := testutil.ControllerDeployment("kgw", "kgateway", "kgateway:v2.4.7", 2, 2)
	r := NewCollector(fakeScraper{"a": sample}).Collect(context.Background(), inst(testutil.ControllerPod(d, "a", true), testutil.ControllerPod(d, "b", true)))
	if r.Err == "" {
		t.Fatal("partial scrape must be an error")
	}
}

func TestCollectNoReadyPodsIsError(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "kgateway:v2.4.7", 1, 0)
	if r := NewCollector(fakeScraper{}).Collect(context.Background(), inst(testutil.ControllerPod(d, "a", false))); r.Err == "" {
		t.Fatal("no ready controller pod must be an error")
	}
}

// A result keeps its CollectedAt, so recomputing the model cannot make it look fresh.
func TestResultStaleness(t *testing.T) {
	r := Result{CollectedAt: testutil.Now}
	if r.Stale(testutil.Now.Add(30*time.Second), 30*time.Second) {
		t.Fatal("exactly maxAge old is not stale")
	}
	if !r.Stale(testutil.Now.Add(31*time.Second), 30*time.Second) {
		t.Fatal("older than maxAge must be stale")
	}
	if r.Stale(testutil.Now.Add(time.Hour), 0) {
		t.Fatal("a zero maxAge never makes a result stale")
	}
}

func podAt(t *testing.T, srv *httptest.Server) (*corev1.Pod, int32) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	p := &corev1.Pod{}
	p.Name, p.Status.PodIP = "a", host
	p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	return p, int32(port)
}

func collectOne(t *testing.T, c *Collector, p *corev1.Pod, port int32) Result {
	t.Helper()
	return c.Collect(context.Background(), install.Install{Namespace: "kgw", Name: "kgateway", Pods: []*corev1.Pod{p}, MetricsPort: port})
}

func TestHTTPScraperSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, sample) }))
	defer srv.Close()
	p, port := podAt(t, srv)
	if r := collectOne(t, NewCollector(HTTPScraper{}), p, port); r.Err != "" || r.Metrics.Rejects[GatewayKey{"infra", "edge"}] != 2 {
		t.Fatalf("got %+v", r)
	}
}

func TestHTTPScraperHungServerFailsWithinPodDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	p, port := podAt(t, srv)
	c := NewCollector(HTTPScraper{Client: &http.Client{}}) // no client timeout: the pod deadline must bound it
	c.podTimeout = 100 * time.Millisecond
	start := time.Now()
	r := collectOne(t, c, p, port)
	if r.Err == "" || time.Since(start) > 2*time.Second {
		t.Fatalf("want error within the deadline, got %+v after %v", r, time.Since(start))
	}
}

func TestOversizedBodyIsErrorNotParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, sample)
		_, _ = io.WriteString(w, strings.Repeat("# padding\n", 100))
	}))
	defer srv.Close()
	p, port := podAt(t, srv)
	c := NewCollector(HTTPScraper{})
	c.maxBody = int64(len(sample)) + 10
	if r := collectOne(t, c, p, port); r.Err == "" || len(r.Metrics.Rejects) != 0 {
		t.Fatalf("oversized body must be an error with no metrics: %+v", r)
	}
	c.maxBody = int64(len(sample)) + 100*10 // exactly at the cap is accepted
	if r := collectOne(t, c, p, port); r.Err != "" {
		t.Fatalf("body at the cap must be accepted: %+v", r)
	}
}

func TestNon200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusServiceUnavailable) }))
	defer srv.Close()
	p, port := podAt(t, srv)
	if r := collectOne(t, NewCollector(HTTPScraper{}), p, port); r.Err == "" {
		t.Fatal("non-200 must be an error")
	}
}

func TestHTTPScraperEmptyPodIPIsError(t *testing.T) {
	if _, err := (HTTPScraper{}).Scrape(context.Background(), &corev1.Pod{}, 9092); err == nil {
		t.Fatal("empty PodIP must be an error")
	}
}

func TestRunRejectsNonPositiveInterval(t *testing.T) {
	if err := NewCollector(fakeScraper{}).Run(context.Background(), 0, nil); err == nil {
		t.Fatal("want error for a zero interval")
	}
}

// Run scrapes installs concurrently, so one hung install does not starve the
// other, while Get is called concurrently (run with -race).
func TestRunConcurrentWithGet(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "kgateway:v2.4.7", 1, 1)
	good := install.Install{Namespace: "kgw", Name: "good", Pods: testutil.PodList(testutil.ControllerPod(d, "a", true)), MetricsPort: 9092}
	hung := install.Install{Namespace: "kgw", Name: "hung", Pods: testutil.PodList(testutil.ControllerPod(d, "h", true)), MetricsPort: 9092}
	c := NewCollector(blockingScraper{"a": sample})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		done <- c.Run(ctx, 40*time.Millisecond, func() []install.Install { return []install.Install{hung, good} })
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if r, ok := c.Get("kgw.good"); ok && r.Err == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("good install never collected")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := testutil.Recv(t, done, "Run to return after cancel"); err != nil {
		t.Fatal(err)
	}
}

// blockingScraper serves known pods and blocks until the context ends for others.
type blockingScraper map[string]string

func (b blockingScraper) Scrape(ctx context.Context, p *corev1.Pod, _ int32) (io.ReadCloser, error) {
	if body, ok := b[p.Name]; ok {
		return io.NopCloser(strings.NewReader(body)), nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

// CollectedAt keeps time.Now's monotonic reading for the staleness check.
func TestCollectedAtKeepsMonotonicReading(t *testing.T) {
	d := testutil.ControllerDeployment("kgw", "kgateway", "img", 1, 1)
	if at := NewCollector(fakeScraper{}).Collect(context.Background(), inst(testutil.ControllerPod(d, "a", false))).CollectedAt; !strings.Contains(at.String(), " m=") {
		t.Fatalf("CollectedAt lost its monotonic reading: %s", at)
	}
}

// stored is one result's notify decision, from WithResultHook.
type stored struct {
	r        Result
	notified bool
}

// runHooked runs a collector of body every 10 s on a fake clock with max age
// maxAge; each result's decision arrives on the returned channel.
func runHooked(t *testing.T, body *atomic.Pointer[string], maxAge time.Duration) (*Collector, *clocktesting.FakeClock, <-chan stored) {
	t.Helper()
	d := testutil.ControllerDeployment("kgw", "kgateway", "kgateway:v2.4.7", 1, 1)
	in := install.Install{Namespace: "kgw", Name: "kgateway", Pods: testutil.PodList(testutil.ControllerPod(d, "a", true)), MetricsPort: 9092}
	acks := make(chan stored, 64)
	clk := testutil.FakeClock()
	c := NewCollector(scraperFunc(func() string { return *body.Load() }), WithClock(clk), WithMaxAge(maxAge),
		WithResultHook(func(_ string, r Result, notified bool) { acks <- stored{r, notified} }))
	c.OnResult = trigger.New()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, 10*time.Second, func() []install.Install { return []install.Install{in} }) }()
	t.Cleanup(func() {
		cancel()
		if err := testutil.Recv(t, done, "Run to return after cancel"); err != nil {
			t.Error(err)
		}
	})
	return c, clk, acks
}

// ack receives the next decision and checks it was collected at at.
func ack(t *testing.T, acks <-chan stored, at time.Time) stored {
	t.Helper()
	s := testutil.Recv(t, acks, "a result decision")
	if !s.r.CollectedAt.Equal(at) {
		t.Fatalf("result collected at %v, want %v", s.r.CollectedAt, at)
	}
	return s
}

// The first result notifies; a scrape with unchanged content does not; a
// content change does (D-019).
func TestCollectorNotifiesOnContentChange(t *testing.T) {
	var body atomic.Pointer[string]
	body.Store(new(sample))
	c, clk, acks := runHooked(t, &body, time.Hour)
	if s := ack(t, acks, clk.Now()); !s.notified {
		t.Fatal("the first result must notify")
	}
	testutil.Recv(t, c.OnResult.C(), "the first notification")
	for range 2 {
		clk.Step(10 * time.Second)
		if s := ack(t, acks, clk.Now()); s.notified {
			t.Fatal("a scrape that only renews CollectedAt must not notify")
		}
	}
	body.Store(new(strings.Replace(sample, "} 3\n", "} 4\n", 1)))
	clk.Step(10 * time.Second)
	if s := ack(t, acks, clk.Now()); !s.notified {
		t.Fatal("changed content must notify")
	}
	testutil.Recv(t, c.OnResult.C(), "the notification for changed content")
}

// Codex 1: a result that follows a stale one notifies even with equal content.
func TestCollectorNotifiesAfterStaleResult(t *testing.T) {
	var body atomic.Pointer[string]
	body.Store(new(sample))
	_, clk, acks := runHooked(t, &body, 15*time.Second)
	ack(t, acks, clk.Now())
	clk.Step(10 * time.Second) // 10 s after the last: fresh
	if s := ack(t, acks, clk.Now()); s.notified {
		t.Fatal("a result after a fresh one with equal content must not notify")
	}
	clk.Step(20 * time.Second) // one delayed tick: 20 s > 15 s after the last
	if s := ack(t, acks, clk.Now()); !s.notified {
		t.Fatal("a result after a stale one must notify")
	}
}

// scraperFunc serves the body f returns.
type scraperFunc func() string

func (f scraperFunc) Scrape(context.Context, *corev1.Pod, int32) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f())), nil
}

func TestSameContentIgnoresCollectedAt(t *testing.T) {
	m, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	a := Result{Metrics: m, CollectedAt: testutil.Now}
	b := a
	b.CollectedAt = a.CollectedAt.Add(time.Minute)
	if !a.SameContent(b) {
		t.Fatal("CollectedAt is not content")
	}
	b.Err = "pod a: timeout"
	if a.SameContent(b) {
		t.Fatal("Err is content")
	}
	m2, _ := Parse(strings.NewReader(strings.Replace(sample, "} 3\n", "} 4\n", 1)))
	if a.SameContent(Result{Metrics: m2, CollectedAt: a.CollectedAt}) {
		t.Fatal("Managed values are content")
	}
	// NaN equals NaN by bits, as in the engine's identity; else every scrape would notify.
	nan := Result{Metrics: Metrics{Rejects: map[GatewayKey]float64{{"infra", "edge"}: math.NaN()}}}
	if !nan.SameContent(nan) {
		t.Fatal("a NaN sample must equal itself")
	}
}

func TestSameContentSeesPerPodChange(t *testing.T) {
	mk := func(d float64) Result {
		return Result{Metrics: Metrics{PerPod: []PodMetrics{{UID: "u", Name: "p", Dropped: d, DroppedPresent: true}}}}
	}
	if !mk(0).SameContent(mk(0)) || mk(0).SameContent(mk(1)) {
		t.Fatal("a changed per-Pod value is a content change")
	}
}

// Neither scraper follows a redirect: the target sees no request.
func TestScrapersNeverFollowRedirects(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "kgateway_build_info 1\n")
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.RedirectHandler(target.URL+"/metrics", http.StatusFound))
	t.Cleanup(redirect.Close)
	host, port, _ := net.SplitHostPort(redirect.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	pod := &corev1.Pod{}
	pod.Namespace, pod.Name, pod.Status.PodIP = "kgw", "kgateway-0", host
	kc, err := kubernetes.NewForConfig(&rest.Config{Host: redirect.URL})
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]Scraper{"default client": HTTPScraper{}, "supplied client": HTTPScraper{Client: &http.Client{}}, "pods/proxy": PodProxyScraper{Kube: kc}} {
		if _, err := s.Scrape(context.Background(), pod, int32(p)); err == nil || !strings.Contains(err.Error(), "302") {
			t.Errorf("%s: want a 302 error, got %v", name, err)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("the redirect target saw %d requests", n)
	}
	if tr, ok := NewHTTPClient().Transport.(*http.Transport); !ok || tr.Proxy != nil {
		t.Fatal("Pod-IP scrapes never go through an environment proxy")
	}
}

// pods/proxy errors are closed unread and never retried: an oversized 500
// and a 429 with Retry-After each fail once, without the body in the error.
func TestPodProxyScrapeErrorsAreBoundedAndNotRetried(t *testing.T) {
	for _, tc := range []struct {
		code int
		hdr  string
	}{{http.StatusInternalServerError, ""}, {http.StatusTooManyRequests, "1"}} {
		t.Run(strconv.Itoa(tc.code), func(t *testing.T) {
			var hits atomic.Int32
			var path string
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				path = r.URL.Path
				if tc.hdr != "" {
					w.Header().Set("Retry-After", tc.hdr)
				}
				w.WriteHeader(tc.code)
				chunk := []byte(strings.Repeat("secret ", 1<<14))
				for range 128 { // far over maxBodyBytes
					if _, err := w.Write(chunk); err != nil {
						return
					}
				}
			}))
			t.Cleanup(api.Close)
			kc, err := kubernetes.NewForConfig(&rest.Config{Host: api.URL})
			if err != nil {
				t.Fatal(err)
			}
			pod := &corev1.Pod{}
			pod.Namespace, pod.Name = "kgw", "kgateway-0"
			_, err = PodProxyScraper{Kube: kc}.Scrape(context.Background(), pod, 9092)
			if err == nil || !strings.Contains(err.Error(), strconv.Itoa(tc.code)) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("want a %d error without the body, got %v", tc.code, err)
			}
			if n := hits.Load(); n != 1 {
				t.Fatalf("%d requests, want 1 (no retry)", n)
			}
			if path != "/api/v1/namespaces/kgw/pods/http:kgateway-0:9092/proxy/metrics" {
				t.Fatalf("path %s", path)
			}
		})
	}
}
