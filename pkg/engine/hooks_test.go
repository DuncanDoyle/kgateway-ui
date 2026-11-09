package engine

import (
	"slices"
	"sync"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/DuncanDoyle/snellius/internal/testutil"
	"github.com/DuncanDoyle/snellius/pkg/cache"
	"github.com/DuncanDoyle/snellius/pkg/health"
	"github.com/DuncanDoyle/snellius/pkg/install"
	"github.com/DuncanDoyle/snellius/pkg/kgateway"
	"github.com/DuncanDoyle/snellius/pkg/kinds"
	"github.com/DuncanDoyle/snellius/pkg/objects"
	"github.com/DuncanDoyle/snellius/pkg/process"
	"github.com/DuncanDoyle/snellius/pkg/proxymode"
	"github.com/DuncanDoyle/snellius/pkg/scopeverify"
)

const secondController = "acme.example.com/gateway"

// secondPackDetector claims the controller Deployments in namespace acme for a second pack, "acme".
type secondPackDetector struct{}

func (secondPackDetector) Name() string                           { return "acme" }
func (secondPackDetector) ControllerSelector() labels.Selector    { return labels.Everything() }
func (secondPackDetector) ControllerPodSelector() labels.Selector { return labels.Everything() }
func (secondPackDetector) ProxySelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{"app": "acme-proxy"})
}
func (secondPackDetector) Detect(d *appsv1.Deployment) (install.Install, bool) {
	if d.Namespace != "acme" {
		return install.Install{}, false
	}
	return install.Install{Namespace: d.Namespace, Name: d.Name, Edition: "acme", ControllerNames: []string{secondController}, Impl: "acme"}, true
}

// calls records which hook saw which install, as "hook install".
type calls struct {
	mu  sync.Mutex
	got []string
}

func (c *calls) add(hook, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Contains(c.got, hook+" "+id) {
		c.got = append(c.got, hook+" "+id)
	}
}

type spyVerifier struct {
	name string
	c    *calls
}

func (v spyVerifier) Name() string { return v.name }

func (v spyVerifier) Verify(in scopeverify.Input) scopeverify.Result {
	v.c.add(v.name+" verifier", in.Install.ID())
	return scopeverify.Result{Available: true}
}

type spyProcess struct {
	name string
	c    *calls
}

func (s spyProcess) Controller(in install.Resolved, snap *cache.Snapshot) process.Set {
	s.c.add(s.name+" process", in.ID())
	return process.Kube{}.Controller(in, snap)
}

func (s spyProcess) Proxies(in install.Resolved, gw objects.Ref, snap *cache.Snapshot) process.Set {
	s.c.add(s.name+" process", in.ID())
	return process.Kube{}.Proxies(in, gw, snap)
}

type spyMode struct {
	name string
	mode proxymode.Mode
	c    *calls
}

func (m spyMode) Resolve(gw *unstructured.Unstructured, _ *cache.Snapshot, _ []kinds.Served) proxymode.Mode {
	m.c.add(m.name+" proxy mode", gw.GetNamespace()+"/"+gw.GetName())
	return m.mode
}

// Each install gets its own pack's scope verifier, process source and proxy
// mode; a class-only install gets none of them: no verification and empty
// process sets. Config's registry-wide values are not read (Phase 5 design §4.2).
func TestHooksAreTheInstallsPack(t *testing.T) {
	ctl := testutil.ControllerDeployment("acme", "acme-ctl", "example.com/acme:v1", 1, 1)
	snap := twoControllers(t)
	acmeObjs := testutil.ObjectsByGVR(t, testutil.GatewayClass("acme", secondController, true),
		testutil.Gateway("shop", "acme-gw", "acme", nil, testutil.Listener{Name: "http", Conds: []metav1.Condition{
			testutil.Cond("Accepted", metav1.ConditionTrue, "Accepted"), testutil.Cond("Programmed", metav1.ConditionTrue, "Programmed")}}))
	for gvr, objs := range acmeObjs {
		snap.Objects[gvr] = append(snap.Objects[gvr], objs...)
	}
	snap.Deployments = append(snap.Deployments, ctl)
	snap.Pods = append(snap.Pods, testutil.ControllerPod(ctl, "acme-0", true))

	c := &calls{}
	cfg := classOnlyConfig(snap, true)
	cfg.Detectors = append(cfg.Detectors, secondPackDetector{})
	cfg.Verifier, cfg.Process, cfg.ProxyMode = spyVerifier{"config", c}, spyProcess{"config", c}, spyMode{"config", proxymode.Managed, c}
	cfg.HooksFor = func(i install.Install) Hooks {
		switch i.Impl {
		case kgateway.Name:
			h := packHooks(i)
			h.Verifier, h.Process, h.ProxyMode = spyVerifier{"kgateway", c}, spyProcess{"kgateway", c}, spyMode{"kgateway", proxymode.Managed, c}
			return h
		case "acme":
			return Hooks{Verifier: spyVerifier{"acme", c}, Process: spyProcess{"acme", c}, ProxyMode: spyMode{"acme", proxymode.SelfManaged, c}}
		}
		return Hooks{}
	}
	m := New(cfg).Compute()
	if len(m.Installs) != 3 {
		t.Fatalf("%d installs, want kgateway, acme and a class-only one", len(m.Installs))
	}
	slices.Sort(c.got)
	want := []string{"acme process acme.acme-ctl", "acme proxy mode shop/acme-gw", "acme verifier acme.acme-ctl",
		"kgateway process kgw.kgateway", "kgateway verifier kgw.kgateway"}
	if !slices.Equal(c.got, want) {
		t.Errorf("hook calls:\n%q\nwant:\n%q", c.got, want)
	}
	for _, im := range m.Installs {
		switch im.Resolved.ID() {
		case "acme.acme-ctl":
			if !hasFinding(im.Health, health.CodeSelfManagedProxy, "shop/acme-gw", "") {
				t.Errorf("acme: no KGWUI-010 from its own proxy mode: %+v", im.Health.Findings)
			}
		case "example.com~2Fgateway":
			p := im.Process
			if p == nil || p.Controller.Observed || p.Controller.Desired != nil || len(p.Proxies) != 1 {
				t.Fatalf("class-only process data %+v, want one Gateway and nothing observed", p)
			}
			for gw, s := range p.Proxies {
				if s.Observed || s.Desired != nil || len(s.Processes) != 0 {
					t.Errorf("class-only proxies of %v: %+v, want not applicable", gw, s)
				}
			}
		}
	}
}
