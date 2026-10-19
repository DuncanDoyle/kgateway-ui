package ctrlmetrics

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/DuncanDoyle/kgateway-ui/internal/testutil"
	"github.com/DuncanDoyle/kgateway-ui/pkg/install"
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
		if r, ok := c.Get("kgw/good"); ok && r.Err == "" {
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
