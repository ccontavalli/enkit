package enproxy

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/factory"
	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/khttp/krequest"
	"github.com/ccontavalli/enkit/lib/khttp/ktest"
	"github.com/ccontavalli/enkit/lib/khttp/protocol"
	"github.com/ccontavalli/enkit/lib/knetwork/echo"
	"github.com/ccontavalli/enkit/lib/logger"
	"github.com/ccontavalli/enkit/lib/oauth"
	"github.com/ccontavalli/enkit/lib/token"
	"github.com/ccontavalli/enkit/proxy/httpp"
	"github.com/ccontavalli/enkit/proxy/nasshp"
	"github.com/ccontavalli/enkit/proxy/ptunnel"
	"github.com/ccontavalli/enkit/proxy/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Deny returns an authenticator that either denies a request, or returns a constant cookie.
// Every request received is logged in log.
func Deny(cookie *oauth.CredentialsCookie, urls []string, log *[]string) oauth.Authenticate {
	return func(w http.ResponseWriter, r *http.Request, rurl *url.URL) (*oauth.CredentialsCookie, error) {
		uri := *r.URL
		if uri.Host == "" {
			uri.Host = r.Host
		}
		suri := uri.String()

		if log != nil {
			(*log) = append(*log, suri)
		}

		for _, block := range urls {
			if strings.HasPrefix(suri, block) {
				http.Error(w, "Not authorized", http.StatusUnauthorized)
				return nil, nil
			}
		}

		return cookie, nil
	}
}

func Allow(cookie *oauth.CredentialsCookie) oauth.Authenticate {
	return func(w http.ResponseWriter, r *http.Request, rurl *url.URL) (*oauth.CredentialsCookie, error) {
		return cookie, nil
	}
}

// Server creates a Starter capable of binding an unused port and start an http server on it.
func Server(wg *sync.WaitGroup, url *string) Starter {
	wg.Add(1)
	return func(log logger.Printer, handler http.Handler, domains ...string) error {
		defer wg.Done()
		var err error
		*url, err = ktest.Start(handler)
		return err
	}
}

func GetWithHost(t *testing.T, target, host string) (*http.Response, string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, target, nil)
	if !assert.NoError(t, err) {
		return nil, ""
	}
	req.Host = host

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	if !assert.NoError(t, err) {
		return nil, ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if !assert.NoError(t, err) {
		return resp, ""
	}
	return resp, string(body)
}

func PublicConfig(host, path, to string, tunnels ...string) Config {
	return Config{
		Mapping: ProxyMappings(
			httpp.Mapping{
				From: httpp.HostPath{
					Host: host,
					Path: path,
				},
				Auth: httpp.MappingPublic,
				To:   to,
			},
		),
		Tunnels: tunnels,
	}
}

func ProxyMapping(mapping httpp.Mapping) Mapping {
	return Mapping{
		Name:   mapping.Name,
		From:   mapping.From,
		Auth:   mapping.Auth,
		Target: Target{Proxy: &ProxyTarget{To: mapping.To, Transform: mapping.Transform}},
	}
}

func NamedProxyMapping(module string, mapping httpp.Mapping) Mapping {
	result := ProxyMapping(mapping)
	result.Module = module
	return result
}

func ProxyMappings(mappings ...httpp.Mapping) []Mapping {
	converted := make([]Mapping, 0, len(mappings))
	for _, mapping := range mappings {
		converted = append(converted, ProxyMapping(mapping))
	}
	return converted
}

func NasshMapping(host string) Mapping {
	relayHost := host
	if relayHost == "" {
		relayHost = "nassh.test"
	}
	return Mapping{
		From: httpp.HostPath{
			Host: host,
			Path: "/",
		},
		Target: Target{
			Nassh: &NasshTarget{RelayHost: relayHost},
		},
	}
}

func singleProxyModule(t *testing.T, modules map[string]runtimeModule) *proxyRuntimeModule {
	proxies := []*proxyRuntimeModule{}
	for _, module := range modules {
		proxy, ok := module.(*proxyRuntimeModule)
		if ok {
			proxies = append(proxies, proxy)
		}
	}
	assert.Len(t, proxies, 1)
	for _, proxy := range proxies {
		return proxy
	}
	return nil
}

func proxyModuleForMapping(t *testing.T, modules map[string]runtimeModule, mapping Mapping) *proxyRuntimeModule {
	module, ok := modules["proxy:"+canonicalModuleName(mapping.Module)]
	assert.True(t, ok)

	proxy, ok := module.(*proxyRuntimeModule)
	assert.True(t, ok)
	return proxy
}

func countProxyModules(modules map[string]runtimeModule) int {
	total := 0
	for _, module := range modules {
		_, ok := module.(*proxyRuntimeModule)
		if ok {
			total++
		}
	}
	return total
}

func writeDefaultConfigForFlags(t *testing.T, flags *factory.Flags, cfg Config) {
	t.Helper()

	ws, err := factory.NewStore(rand.New(rand.NewSource(1)), factory.FromFlags(flags))
	if !assert.NoError(t, err) {
		return
	}
	defer ws.Close()

	parsed, err := config.ResolvePathWithinStore(config.StoreRoot{AppName: "enproxy"}, "enproxy")
	if !assert.NoError(t, err) {
		return
	}

	store, err := parsed.OpenStore(ws)
	if !assert.NoError(t, err) {
		return
	}
	defer store.Close()

	assert.NoError(t, parsed.Bind(store).Marshal(cfg))
}

func deleteDefaultConfigForFlags(t *testing.T, flags *factory.Flags) {
	t.Helper()

	ws, err := factory.NewStore(rand.New(rand.NewSource(1)), factory.FromFlags(flags))
	if !assert.NoError(t, err) {
		return
	}
	defer ws.Close()

	parsed, err := config.ResolvePathWithinStore(config.StoreRoot{AppName: "enproxy"}, "enproxy")
	if !assert.NoError(t, err) {
		return
	}

	store, err := parsed.OpenStore(ws)
	if !assert.NoError(t, err) {
		return
	}
	defer store.Close()

	assert.NoError(t, store.Delete(parsed.Descriptor))
}

func TestInvalidConfig(t *testing.T) {
	var url string
	rng := rand.New(rand.NewSource(1))

	// Config file without any mappings.
	ep, err := New(rng, WithHttpStarter(Server(&sync.WaitGroup{}, &url)))
	assert.Regexp(t, "config file.*has no Mapping.*defined", err)
	assert.Nil(t, ep)

	config := Config{
		Mapping: ProxyMappings(
			httpp.Mapping{
				From: httpp.HostPath{
					Host: "test.lan",
					Path: "/",
				},
				To: "toast.lan",
			},
		),
	}

	// One mapping is provided, now authentication is required.
	ep, err = New(rng, WithHttpStarter(Server(&sync.WaitGroup{}, &url)), WithConfig(config))
	assert.Regexp(t, "error in mapping entry 0", err)
	assert.Nil(t, ep)

	// Valid, but there is no tunnel configuration nor authentication, it should spew a few warnings.
	accumulator := logger.NewAccumulator()
	config.Mapping[0].Auth = httpp.MappingPublic
	ep, err = New(rng, WithHttpStarter(Server(&sync.WaitGroup{}, &url)), WithConfig(config), WithLogging(accumulator))
	assert.NoError(t, err)
	assert.NotNil(t, ep)

	events := accumulator.Retrieve()
	assert.True(t, len(events) >= 3, "%v", events)
}

func TestConfigRejectsEmptyModuleMapKeys(t *testing.T) {
	proxyConfig := Config{
		ProxyModules: map[string]ProxyModule{
			"": {To: "https://backend.example.com"},
		},
		Mapping: []Mapping{
			{
				From: httpp.HostPath{Host: "test.lan", Path: "/"},
				Target: Target{
					Proxy: &ProxyTarget{},
				},
			},
		},
	}
	_, _, err := (&proxyConfig).Parse()
	assert.Regexp(t, `proxy module map cannot use an empty name.*default`, err)

	nasshConfig := Config{
		NasshModules: map[string]NasshModule{
			"": {RelayHost: "nassh.test"},
		},
		Mapping: []Mapping{
			NasshMapping("nassh.test"),
		},
		Tunnels: []string{"*"},
	}
	_, _, err = (&nasshConfig).Parse()
	assert.Regexp(t, `nassh module map cannot use an empty name.*default`, err)
}

func TestInvalidConfigFileFailsStartup(t *testing.T) {
	var url string
	rng := rand.New(rand.NewSource(1))

	ep, err := New(rng,
		WithHttpStarter(Server(&sync.WaitGroup{}, &url)),
		WithConfigFile("config.json", []byte("{")),
	)
	assert.Regexp(t, "Invalid configuration file 'config.json'", err)
	assert.Nil(t, ep)
}

func TestSemanticallyInvalidConfigFileFailsStartup(t *testing.T) {
	var url string
	rng := rand.New(rand.NewSource(1))

	ep, err := New(rng,
		WithHttpStarter(Server(&sync.WaitGroup{}, &url)),
		WithConfigFile("config.json", []byte("{}")),
	)
	assert.Regexp(t, "config file.*has no Mapping.*defined", err)
	assert.Nil(t, ep)
}

func TestFromFlagsLoadsConfigFromStoreBinding(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)

	data, err := json.Marshal(PublicConfig("test.lan", "/", s1, "*"))
	assert.NoError(t, err)

	configPath := filepath.Join(t.TempDir(), "enproxy.json")
	err = os.WriteFile(configPath, data, 0600)
	assert.NoError(t, err)

	flags := DefaultFlags()
	flags.ConfigPath = configPath

	rng := rand.New(rand.NewSource(1))
	var fe string
	var wg sync.WaitGroup
	ep, err := New(rng, FromFlags(flags), WithHttpStarter(Server(&wg, &fe)))
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	body := ""
	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)
}

func TestFromFlagsLoadsDefaultConfigBindingWhenConfigOmitted(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)

	flags := DefaultFlags()
	flags.ConfigStore = factory.DefaultAppConfigFlags()
	flags.ConfigStore.Directory.Path = t.TempDir()
	writeDefaultConfigForFlags(t, flags.ConfigStore, PublicConfig("test.lan", "/", s1, "*"))

	rng := rand.New(rand.NewSource(1))
	var fe string
	var wg sync.WaitGroup
	ep, err := New(rng, FromFlags(flags), WithHttpStarter(Server(&wg, &fe)))
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	body := ""
	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)
}

func TestFromFlagsUsesEmbeddedDefaultWhenDefaultConfigMissing(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)

	data, err := json.Marshal(PublicConfig("test.lan", "/", s1, "*"))
	assert.NoError(t, err)

	flags := DefaultFlags()
	flags.ConfigPath = ""
	flags.ConfigStore = factory.DefaultAppConfigFlags()
	flags.ConfigStore.Directory.Path = t.TempDir()

	rng := rand.New(rand.NewSource(1))
	var fe string
	var wg sync.WaitGroup
	ep, err := New(
		rng,
		WithDefaultConfigFile("embedded-default.json", data),
		FromFlags(flags),
		WithHttpStarter(Server(&wg, &fe)),
	)
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	body := ""
	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)
}

func TestFromFlagsRejectsMissingDefaultConfigWithoutEmbeddedFallback(t *testing.T) {
	flags := DefaultFlags()
	flags.ConfigPath = ""
	flags.ConfigStore = factory.DefaultAppConfigFlags()
	flags.ConfigStore.Directory.Path = t.TempDir()

	rng := rand.New(rand.NewSource(1))
	ep, err := New(rng, FromFlags(flags))
	assert.Regexp(t, "Default configuration \"enproxy/enproxy\" does not exist", err)
	assert.Nil(t, ep)
}

func TestFromFlagsRejectsMissingConfigBinding(t *testing.T) {
	flags := DefaultFlags()
	flags.ConfigPath = filepath.Join(t.TempDir(), "missing.json")

	rng := rand.New(rand.NewSource(1))
	ep, err := New(rng, FromFlags(flags))
	assert.Regexp(t, "does not exist in the configured store", err)
	assert.Nil(t, ep)
}

func TestFromFlagsUsesEmbeddedDefaultWhenExplicitConfigMissingAndPolicyIsEmbedded(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)

	data, err := json.Marshal(PublicConfig("test.lan", "/", s1, "*"))
	assert.NoError(t, err)

	flags := DefaultFlags()
	flags.ConfigPath = filepath.Join(t.TempDir(), "missing.json")
	flags.ConfigMissing = MissingConfigEmbedded

	rng := rand.New(rand.NewSource(1))
	var fe string
	var wg sync.WaitGroup
	ep, err := New(
		rng,
		WithDefaultConfigFile("embedded-default.json", data),
		FromFlags(flags),
		WithHttpStarter(Server(&wg, &fe)),
	)
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	body := ""
	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)
}

func TestFromFlagsRejectsMissingDefaultConfigWhenPolicyIsError(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)

	data, err := json.Marshal(PublicConfig("test.lan", "/", s1, "*"))
	assert.NoError(t, err)

	flags := DefaultFlags()
	flags.ConfigPath = ""
	flags.ConfigStore = factory.DefaultAppConfigFlags()
	flags.ConfigStore.Directory.Path = t.TempDir()
	flags.ConfigMissing = MissingConfigError

	rng := rand.New(rand.NewSource(1))
	ep, err := New(rng, WithDefaultConfigFile("embedded-default.json", data), FromFlags(flags))
	assert.Regexp(t, "Default configuration \"enproxy/enproxy\" does not exist", err)
	assert.Nil(t, ep)
}

func TestReloadConfigReloadsFromBoundStore(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)
	s2, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s2")))
	assert.NoError(t, err)

	flags := DefaultFlags()
	flags.ConfigStore = factory.DefaultAppConfigFlags()
	flags.ConfigStore.Directory.Path = t.TempDir()
	writeDefaultConfigForFlags(t, flags.ConfigStore, PublicConfig("test.lan", "/", s1, "*"))

	rng := rand.New(rand.NewSource(1))
	var fe string
	var wg sync.WaitGroup
	ep, err := New(rng, FromFlags(flags), WithHttpStarter(Server(&wg, &fe)))
	assert.NoError(t, err)
	defer ep.Close()

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	body := ""
	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)

	writeDefaultConfigForFlags(t, flags.ConfigStore, PublicConfig("test.lan", "/", s2, "*"))

	err = ep.ReloadConfig()
	assert.NoError(t, err)

	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s2", body)
}

func TestReloadConfigMissingStoreEntryIsRuntimeError(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)

	flags := DefaultFlags()
	flags.ConfigStore = factory.DefaultAppConfigFlags()
	flags.ConfigStore.Directory.Path = t.TempDir()
	writeDefaultConfigForFlags(t, flags.ConfigStore, PublicConfig("test.lan", "/", s1, "*"))

	rng := rand.New(rand.NewSource(1))
	ep, err := New(rng, FromFlags(flags))
	assert.NoError(t, err)
	defer ep.Close()

	deleteDefaultConfigForFlags(t, flags.ConfigStore)

	err = ep.ReloadConfig()
	assert.Error(t, err)
	var usageErr *kflags.UsageError
	assert.False(t, errors.As(err, &usageErr))
	assert.Regexp(t, `configuration does not exist in the configured store`, err.Error())
}

func TestInvalidConfigDoesNotPollutePrometheusRegistry(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)

	reg := prometheus.NewRegistry()
	rng := rand.New(rand.NewSource(1))

	ep, err := New(rng,
		WithConfigFile("config.json", []byte("{}")),
		WithMetricsStarter(Server(&sync.WaitGroup{}, new(string))),
		WithDisabledNasshAuthentication(true),
		WithNasshpMods(nasshp.WithSymmetricOptions(token.WithGeneratedSymmetricKey(0))),
		WithPrometheus(reg, reg),
	)
	assert.Regexp(t, "config file.*has no Mapping.*defined", err)
	assert.Nil(t, ep)

	ep, err = New(rand.New(rand.NewSource(2)),
		WithConfig(PublicConfig("test.lan", "/", s1, "*")),
		WithMetricsStarter(Server(&sync.WaitGroup{}, new(string))),
		WithDisabledNasshAuthentication(true),
		WithNasshpMods(nasshp.WithSymmetricOptions(token.WithGeneratedSymmetricKey(0))),
		WithPrometheus(reg, reg),
	)
	assert.NoError(t, err)
	assert.NotNil(t, ep)
}

func TestApplyConfigStructSwapsActiveRoutes(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)
	s2, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s2")))
	assert.NoError(t, err)

	initial := PublicConfig("test.lan", "/", s1, "*")
	updated := PublicConfig("test.lan", "/", s2, "*")

	rng := rand.New(rand.NewSource(1))
	var fe string
	var wg sync.WaitGroup
	ep, err := New(rng, WithHttpStarter(Server(&wg, &fe)), WithConfig(initial))
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	body := ""
	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)

	err = ep.ApplyConfigStruct(updated)
	assert.NoError(t, err)

	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s2", body)
}

func TestApplyConfigStructReconcilesMixedRouteChanges(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)
	s2, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s2")))
	assert.NoError(t, err)
	s3, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s3")))
	assert.NoError(t, err)
	s4, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s4")))
	assert.NoError(t, err)

	unchangedInitial := httpp.Mapping{
		From: httpp.HostPath{
			Host: "test.lan",
			Path: "/unchanged",
		},
		Auth: httpp.MappingPublic,
		To:   s1,
	}
	changedInitial := httpp.Mapping{
		From: httpp.HostPath{
			Host: "test.lan",
			Path: "/changed",
		},
		Auth: httpp.MappingPublic,
		To:   s2,
	}
	removedInitial := httpp.Mapping{
		From: httpp.HostPath{
			Host: "test.lan",
			Path: "/removed",
		},
		Auth: httpp.MappingPublic,
		To:   s3,
	}
	changedUpdated := changedInitial
	changedUpdated.To = s3
	addedUpdated := httpp.Mapping{
		From: httpp.HostPath{
			Host: "test.lan",
			Path: "/added",
		},
		Auth: httpp.MappingPublic,
		To:   s4,
	}

	initial := Config{
		ProxyModules: map[string]ProxyModule{
			"unchanged": {},
			"changed":   {},
			"removed":   {},
		},
		Mapping: []Mapping{
			NamedProxyMapping("unchanged", unchangedInitial),
			NamedProxyMapping("changed", changedInitial),
			NamedProxyMapping("removed", removedInitial),
		},
		Tunnels: []string{"*"},
	}
	updated := Config{
		ProxyModules: map[string]ProxyModule{
			"unchanged": {},
			"changed":   {},
			"added":     {},
		},
		Mapping: []Mapping{
			NamedProxyMapping("unchanged", unchangedInitial),
			NamedProxyMapping("changed", changedUpdated),
			NamedProxyMapping("added", addedUpdated),
		},
		Tunnels: []string{"*"},
	}

	rng := rand.New(rand.NewSource(1))
	var fe string
	var wg sync.WaitGroup
	ep, err := New(rng, WithHttpStarter(Server(&wg, &fe)), WithConfig(initial))
	assert.NoError(t, err)

	unchangedBefore := proxyModuleForMapping(t, ep.modules, NamedProxyMapping("unchanged", unchangedInitial))
	changedBefore := proxyModuleForMapping(t, ep.modules, NamedProxyMapping("changed", changedInitial))
	removedBefore := proxyModuleForMapping(t, ep.modules, NamedProxyMapping("removed", removedInitial))

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	body := ""
	err = protocol.Get(fe+"unchanged", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)
	err = protocol.Get(fe+"changed", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s2", body)
	err = protocol.Get(fe+"removed", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s3", body)

	err = ep.ApplyConfigStruct(updated)
	assert.NoError(t, err)

	unchangedAfter := proxyModuleForMapping(t, ep.modules, NamedProxyMapping("unchanged", unchangedInitial))
	changedAfter := proxyModuleForMapping(t, ep.modules, NamedProxyMapping("changed", changedUpdated))
	addedAfter := proxyModuleForMapping(t, ep.modules, NamedProxyMapping("added", addedUpdated))
	assert.Same(t, unchangedBefore, unchangedAfter)
	assert.Same(t, changedBefore, changedAfter)
	assert.NotNil(t, addedAfter)

	_, found := ep.modules["proxy:removed"]
	assert.False(t, found)
	assert.NotNil(t, removedBefore)

	err = protocol.Get(fe+"unchanged", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)

	err = protocol.Get(fe+"changed", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s3", body)

	var herr *protocol.HTTPError
	err = protocol.Get(fe+"removed", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.ErrorAs(t, err, &herr)
	assert.Equal(t, http.StatusNotFound, herr.Resp.StatusCode)

	err = protocol.Get(fe+"added", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s4", body)
}

func TestApplyConfigStructRejectsDomainChangesAfterStart(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)

	initial := PublicConfig("test1.lan", "/", s1, "*")
	updated := PublicConfig("test2.lan", "/", s1, "*")

	rng := rand.New(rand.NewSource(1))
	var fe string
	var wg sync.WaitGroup
	ep, err := New(rng, WithHttpStarter(Server(&wg, &fe)), WithConfig(initial))
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	err = ep.ApplyConfigStruct(updated)
	assert.Regexp(t, "cannot apply config changing listener domains after proxy start", err)

	body := ""
	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test1.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)

	var herr *protocol.HTTPError
	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test2.lan")))
	assert.ErrorAs(t, err, &herr)
	assert.Equal(t, http.StatusNotFound, herr.Resp.StatusCode)
}

func TestApplyConfigStructRejectsInvalidUpdates(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)

	initial := PublicConfig("test.lan", "/", s1, "*")

	rng := rand.New(rand.NewSource(1))
	var fe string
	var wg sync.WaitGroup
	ep, err := New(rng, WithHttpStarter(Server(&wg, &fe)), WithConfig(initial))
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	body := ""
	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)

	err = ep.ApplyConfigStruct(Config{})
	assert.Regexp(t, "config file.*has no Mapping.*defined", err)

	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)
}

func TestApplyConfigStructNormalizesHostNames(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)

	config := PublicConfig(" TeSt.Lan. ", "/", s1, "*")

	rng := rand.New(rand.NewSource(1))
	var fe string
	var wg sync.WaitGroup
	ep, err := New(rng, WithHttpStarter(Server(&wg, &fe)), WithConfig(config))
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	body := ""
	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)

	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("TEST.LAN")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)

	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan.")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)

	assert.Equal(t, []string{"test.lan"}, ep.domains)
}

func TestApplyConfigStructRejectsNormalizedHostCollisions(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)
	s2, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s2")))
	assert.NoError(t, err)

	config := Config{
		Mapping: ProxyMappings(
			httpp.Mapping{
				From: httpp.HostPath{
					Host: "TEST.LAN",
					Path: "/",
				},
				Auth: httpp.MappingPublic,
				To:   s1,
			},
			httpp.Mapping{
				From: httpp.HostPath{
					Host: "test.lan.",
					Path: "/",
				},
				Auth: httpp.MappingPublic,
				To:   s2,
			},
		),
		Tunnels: []string{"*"},
	}

	rng := rand.New(rand.NewSource(1))
	ep, err := New(rng, WithConfig(config))
	assert.Nil(t, ep)
	assert.Regexp(t, "duplicate route", err)
}

func TestApplyConfigFileSwapsActiveRoutes(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)
	s2, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s2")))
	assert.NoError(t, err)

	initial := PublicConfig("test.lan", "/", s1, "*")
	updated := PublicConfig("test.lan", "/", s2, "*")
	updatedJSON, err := json.Marshal(updated)
	assert.NoError(t, err)

	rng := rand.New(rand.NewSource(1))
	var fe string
	var wg sync.WaitGroup
	ep, err := New(rng, WithHttpStarter(Server(&wg, &fe)), WithConfig(initial))
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	body := ""
	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)

	err = ep.ApplyConfigFile("config.json", updatedJSON)
	assert.NoError(t, err)

	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s2", body)
}

func TestApplyConfigStructReusesNasshProxy(t *testing.T) {
	cookie := &oauth.CredentialsCookie{
		Identity: oauth.Identity{
			Id:           "id",
			Username:     "username",
			Organization: "organization",
		},
	}

	initial := Config{
		Mapping: []Mapping{
			NasshMapping(""),
		},
		Tunnels: []string{"tcp|10.0.0.1:22"},
	}
	updated := Config{
		Mapping: []Mapping{
			NasshMapping(""),
		},
		Tunnels: []string{"tcp|10.0.0.2:22"},
	}

	rng := rand.New(rand.NewSource(1))
	ep, err := New(rng,
		WithConfig(initial),
		WithAuthenticator(Allow(cookie)),
		WithNasshpMods(nasshp.WithSymmetricOptions(token.WithGeneratedSymmetricKey(0))),
	)
	assert.NoError(t, err)
	nasshModule, ok := ep.modules["nassh:default"]
	assert.True(t, ok)
	nproxy, ok := nasshModule.(*nasshRuntimeModule)
	assert.True(t, ok)

	assert.Equal(t, utils.VerdictAllow, ep.whitelist.Allow("tcp", "10.0.0.1:22", cookie))
	assert.Equal(t, utils.VerdictDrop, ep.whitelist.Allow("tcp", "10.0.0.2:22", cookie))

	err = ep.ApplyConfigStruct(updated)
	assert.NoError(t, err)
	nasshUpdated, ok := ep.modules["nassh:default"]
	assert.True(t, ok)
	assert.Same(t, nproxy, nasshUpdated)
	assert.Equal(t, utils.VerdictDrop, ep.whitelist.Allow("tcp", "10.0.0.1:22", cookie))
	assert.Equal(t, utils.VerdictAllow, ep.whitelist.Allow("tcp", "10.0.0.2:22", cookie))
}

func TestOmittedNasshModuleUsesDefaultIdentity(t *testing.T) {
	config := Config{
		NasshModules: map[string]NasshModule{
			"default": {},
		},
		Mapping: []Mapping{
			NasshMapping("nassh.test"),
		},
		Tunnels: []string{"*"},
	}

	rng := rand.New(rand.NewSource(1))
	ep, err := New(
		rng,
		WithConfig(config),
		WithDisabledNasshAuthentication(true),
		WithNasshpMods(nasshp.WithSymmetricOptions(token.WithGeneratedSymmetricKey(0))),
	)
	assert.NoError(t, err)
	assert.Contains(t, ep.modules, "nassh:default")
	assert.NotContains(t, ep.modules, "nassh:")
}

func TestNasshMappingHostBindsAndDefinesRelayHost(t *testing.T) {
	flags := DefaultFlags()
	flags.Nassh.RelayHost = "relay.test:8443"

	config := Config{
		Mapping: []Mapping{
			{
				From: httpp.HostPath{
					Host: "frontend.test",
					Path: "/",
				},
				Target: Target{Nassh: &NasshTarget{}},
			},
		},
		Tunnels: []string{"*"},
	}

	rng := rand.New(rand.NewSource(1))
	var fe string
	var wg sync.WaitGroup
	ep, err := New(
		rng,
		WithHttpStarter(Server(&wg, &fe)),
		WithConfig(config),
		WithDisabledNasshAuthentication(true),
		WithNasshpMods(nasshp.FromFlags(flags.Nassh)),
	)
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	resp, _ := GetWithHost(t, fe+"cookie?ext=test-ext&path=html/nassh.html", "frontend.test")
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Location"), "#nasshp-enkit@frontend.test")

	resp, _ = GetWithHost(t, fe+"cookie?ext=test-ext&path=html/nassh.html", "relay.test:8443")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestNasshRelayHostFromConfigOverridesGlobalDefault(t *testing.T) {
	config := Config{
		NasshModules: map[string]NasshModule{
			"module": {RelayHost: "module.test:8443"},
		},
		Mapping: []Mapping{
			{
				Module: "module",
				From: httpp.HostPath{
					Host: "frontend.test",
					Path: "/",
				},
				Target: Target{Nassh: &NasshTarget{}},
			},
		},
		Tunnels: []string{"*"},
	}

	rng := rand.New(rand.NewSource(1))
	var fe string
	var wg sync.WaitGroup
	ep, err := New(
		rng,
		WithHttpStarter(Server(&wg, &fe)),
		WithConfig(config),
		WithDisabledNasshAuthentication(true),
		WithNasshpMods(
			nasshp.WithRelayHost("global.test:443"),
			nasshp.WithSymmetricOptions(token.WithGeneratedSymmetricKey(0)),
		),
	)
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	resp, _ := GetWithHost(t, fe+"cookie?ext=test-ext&path=html/nassh.html", "frontend.test")
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Location"), "#nasshp-enkit@module.test:8443")

	resp, _ = GetWithHost(t, fe+"cookie?ext=test-ext&path=html/nassh.html", "module.test:8443")
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Location"), "#nasshp-enkit@module.test:8443")

	resp, _ = GetWithHost(t, fe+"cookie?ext=test-ext&path=html/nassh.html", "global.test:443")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestApplyConfigStructCollectsNasshDomains(t *testing.T) {
	config := Config{
		NasshModules: map[string]NasshModule{
			"module": {RelayHost: "relay.test:8443"},
		},
		Mapping: []Mapping{
			{
				Module: "module",
				From: httpp.HostPath{
					Host: "frontend.test",
					Path: "/",
				},
				Target: Target{Nassh: &NasshTarget{}},
			},
		},
		Tunnels: []string{"*"},
	}

	rng := rand.New(rand.NewSource(1))
	ep, err := New(
		rng,
		WithConfig(config),
		WithDisabledNasshAuthentication(true),
		WithNasshpMods(nasshp.WithSymmetricOptions(token.WithGeneratedSymmetricKey(0))),
	)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"frontend.test", "relay.test"}, ep.domains)
}

func TestApplyConfigStructReusesUnchangedProxyHandlers(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)
	s2, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s2")))
	assert.NoError(t, err)

	initial := Config{
		ProxyModules: map[string]ProxyModule{
			"default": {To: s1},
		},
		Mapping: []Mapping{
			{
				From: httpp.HostPath{Host: "test.lan", Path: "/"},
				Auth: httpp.MappingPublic,
				Target: Target{
					Proxy: &ProxyTarget{},
				},
			},
		},
		Tunnels: []string{"*"},
	}
	updated := Config{
		ProxyModules: map[string]ProxyModule{
			"default": {To: s2},
		},
		Mapping: initial.Mapping,
		Tunnels: []string{"*"},
	}

	rng := rand.New(rand.NewSource(1))
	ep, err := New(rng, WithConfig(initial))
	assert.NoError(t, err)

	reused := singleProxyModule(t, ep.modules)

	err = ep.ApplyConfigStruct(initial)
	assert.NoError(t, err)
	assert.Same(t, reused, singleProxyModule(t, ep.modules))

	err = ep.ApplyConfigStruct(updated)
	assert.NoError(t, err)
	assert.NotSame(t, reused, singleProxyModule(t, ep.modules))
}

func TestApplyConfigStructSharesModulesAcrossHosts(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)

	config := Config{
		Mapping: ProxyMappings(
			httpp.Mapping{
				From: httpp.HostPath{
					Host: "one.lan",
					Path: "/",
				},
				Auth: httpp.MappingPublic,
				To:   s1,
			},
			httpp.Mapping{
				From: httpp.HostPath{
					Host: "two.lan",
					Path: "/",
				},
				Auth: httpp.MappingPublic,
				To:   s1,
			},
		),
		Tunnels: []string{"*"},
	}

	rng := rand.New(rand.NewSource(1))
	ep, err := New(rng, WithConfig(config))
	assert.NoError(t, err)
	assert.Equal(t, 1, countProxyModules(ep.modules))
}

func TestApplyConfigStructSeparatesNamedModules(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)

	config := Config{
		ProxyModules: map[string]ProxyModule{
			"one": {},
			"two": {},
		},
		Mapping: []Mapping{
			NamedProxyMapping("one", httpp.Mapping{
				Name: "one",
				From: httpp.HostPath{
					Host: "one.lan",
					Path: "/",
				},
				Auth: httpp.MappingPublic,
				To:   s1,
			}),
			NamedProxyMapping("two", httpp.Mapping{
				Name: "two",
				From: httpp.HostPath{
					Host: "two.lan",
					Path: "/",
				},
				Auth: httpp.MappingPublic,
				To:   s1,
			}),
		},
		Tunnels: []string{"*"},
	}

	rng := rand.New(rand.NewSource(1))
	ep, err := New(rng, WithConfig(config))
	assert.NoError(t, err)
	assert.Equal(t, 2, countProxyModules(ep.modules))
}

func TestSimpleHTTP(t *testing.T) {
	// Create a few http servers to use as backends.
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.Nil(t, err)
	s2, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s2")))
	assert.Nil(t, err)
	s3, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s3")))
	assert.Nil(t, err)
	s4, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s4")))
	assert.Nil(t, err)

	// Frontend proxy config.
	config := Config{
		Mapping: append(
			ProxyMappings(
				// A single file path on this host.
				httpp.Mapping{
					From: httpp.HostPath{
						Host: "test1.lan",
						Path: "/glad",
					},
					Auth: httpp.MappingPublic,
					To:   s1,
				},

				// Multiple overlapping paths on test2.
				httpp.Mapping{
					From: httpp.HostPath{
						Host: "test2.lan",
						Path: "/",
					},
					Auth: httpp.MappingPublic,
					To:   s2,
				},

				// ... this one is private (but a directory).
				httpp.Mapping{
					From: httpp.HostPath{
						Host: "test2.lan",
						Path: "/oppose/",
					},
					To: s3,
				},

				// ... this one is also private - but access will be denied.
				httpp.Mapping{
					From: httpp.HostPath{
						Host: "test2.lan",
						Path: "/deny/",
					},
					To: s3,
				},

				// ... this one is a prefix of /oppose and public.
				httpp.Mapping{
					From: httpp.HostPath{
						Host: "test2.lan",
						Path: "/opp/",
					},
					Auth: httpp.MappingPublic,
					To:   s4,
				},
			),
			NasshMapping(""),
		),

		// Allow any tunnel.
		Tunnels: []string{"*"},
	}

	cookie := &oauth.CredentialsCookie{
		Identity: oauth.Identity{
			Id:           "id",
			Username:     "username",
			Organization: "organization",
		},
	}

	rng := rand.New(rand.NewSource(1))

	var fe string
	var metrics string
	var wg sync.WaitGroup
	reg := prometheus.NewRegistry()
	accessLog := []string{}
	accumulator := logger.NewAccumulator()
	ep, err := New(rng, WithHttpStarter(Server(&wg, &fe)), WithConfig(config), WithMetricsStarter(Server(&wg, &metrics)),
		WithLogging(accumulator), WithAuthenticator(Deny(cookie, []string{"//test2.lan/deny"}, &accessLog)),
		WithNasshpMods(nasshp.WithSymmetricOptions(token.WithGeneratedSymmetricKey(0))),
		WithPrometheus(reg, reg))
	assert.NoError(t, err)
	assert.NotNil(t, ep)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	var herr *protocol.HTTPError
	body := ""
	metrics += "/metrics"

	// The root fe for test1.lan is not mapped anywhere, should return an error.
	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test1.lan")))
	assert.ErrorAs(t, err, &herr)
	assert.Equal(t, http.StatusNotFound, herr.Resp.StatusCode)

	// /glad for test1.lan is mapped to s1, let's check that.
	err = protocol.Get(fe+"glad", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test1.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s1", body)
	// /glad should be an exact match, so /gladder should not match.
	err = protocol.Get(fe+"gladder", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test1.lan")))
	assert.ErrorAs(t, err, &herr)
	assert.Equal(t, http.StatusNotFound, herr.Resp.StatusCode)
	// /glad/glod should also not work, as /glad was not configured as a path prefix (not /glad/).
	err = protocol.Get(fe+"glad/glod", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test1.lan")))
	assert.ErrorAs(t, err, &herr)
	assert.Equal(t, http.StatusNotFound, herr.Resp.StatusCode)

	// Let's try a single request to test2.lan root.
	err = protocol.Get(fe, protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test2.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s2", body)
	// test2.lan maps all prefixes to s2, as it has a default path. Let's test it.
	err = protocol.Get(fe+"darwin/was/right", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test2.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s2", body)

	// Before making any private request, let's ensure no private request was made so far.
	assert.Equal(t, 0, len(accessLog))

	// Private request, should be allowed, but checked with the authenticator.
	// Note that this verifies both that the map works correctly, and that authentication is enforced.
	err = protocol.Get(fe+"oppose", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test2.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s3", body)
	assert.Equal(t, "//test2.lan/oppose", accessLog[len(accessLog)-1])
	// Same for subdirectories.
	err = protocol.Get(fe+"oppose/censorship", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test2.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s3", body)
	assert.Equal(t, "//test2.lan/oppose/censorship", accessLog[len(accessLog)-1])

	// Let's see what happens if the authentication layer denies a request.
	err = protocol.Get(fe+"deny/oppression", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test2.lan")))
	assert.ErrorAs(t, err, &herr)
	assert.Equal(t, http.StatusUnauthorized, herr.Resp.StatusCode)
	assert.Equal(t, "//test2.lan/deny/oppression", accessLog[len(accessLog)-1])

	// /opp is a prefix of /oppose, but should still work as expected.
	err = protocol.Get(fe+"opp", protocol.Read(protocol.String(&body)), protocol.WithRequestOptions(krequest.SetHost("test2.lan")))
	assert.NoError(t, err)
	assert.Equal(t, "s4", body)

	// Start an echo server to use as a tunnel backend.
	e, err := echo.New("127.0.0.1:0")
	assert.NoError(t, err)
	assert.NotNil(t, e)

	echoaddress, err := e.Address()
	assert.NoError(t, err)

	defer e.Close()
	go e.Run()

	proxy, err := url.Parse(fe)
	assert.NoError(t, err)

	// Try a tunnel connection.
	pool := nasshp.NewBufferPool(8192)
	tlog := logger.NewAccumulator()
	tunnel, err := ptunnel.NewTunnel(pool, ptunnel.WithLogger(tlog))
	assert.NoError(t, err)
	assert.NotNil(t, tunnel)

	defer tunnel.Close()
	go tunnel.KeepConnected(proxy, echoaddress.IP.String(), uint16(echoaddress.Port))

	send, write := io.Pipe()
	go tunnel.Send(send)

	read, receive := io.Pipe()
	go tunnel.Receive(receive)

	quote := "You never change things by fighting the existing reality. To change something, build a new model that makes the existing model obsolete.\n"
	l, err := write.Write([]byte(quote))
	assert.NoError(t, err)
	assert.Equal(t, len(quote), l)

	rback, err := bufio.NewReader(read).ReadString('\n')
	assert.NoError(t, err)
	assert.Equal(t, quote, rback)

	assert.Nil(t, tlog.Retrieve())

	// This is for defense in depth: check that the test actually connected to the echo server VIA THE PROXY.
	// We do so by verifying that there is a log entry reporting the connection.
	// TODO: once we have better metrics and introspection, do something smarter.
	events := accumulator.Retrieve()
	assert.True(t, len(events) > 1)
	assert.Regexp(t, "- connects "+echoaddress.String(), events[len(events)-1].Message)

	err = protocol.Get(metrics, protocol.Read(protocol.String(&body)))
	assert.NoError(t, err)
	lines := strings.Split(body, "\n")
	// Surely there are more than 10 metrics...
	assert.True(t, len(lines) > 10)
	// Check that all metrics are expected...
	assert.Regexp(t, "(?m)^(#|nasshp_)", body, "%s", body)
}

// Generate metrics through a prometheus PedanticRegistry, so that it will
// report errors like conflicting metric names, incorrect representations, and such.
func TestPedanticMetrics(t *testing.T) {
	// Create a few http servers to use as backends.
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.Nil(t, err)

	// Simple proxy config.
	config := Config{
		Mapping: append(
			ProxyMappings(
				httpp.Mapping{
					From: httpp.HostPath{
						Host: "test1.lan",
						Path: "/glad",
					},
					Auth: httpp.MappingPublic,
					To:   s1,
				},
			),
			NasshMapping(""),
		),

		// Allow any tunnel.
		Tunnels: []string{"*"},
	}

	cookie := &oauth.CredentialsCookie{
		Identity: oauth.Identity{
			Id:           "id",
			Username:     "username",
			Organization: "organization",
		},
	}

	var proxy string
	var metrics string
	var wg sync.WaitGroup
	rng := rand.New(rand.NewSource(1))

	// Ensures that no other variables are registered (eg, golang defaults) and...
	// errors out in case there are inconsistencies in the declared variables.
	reg := prometheus.NewPedanticRegistry()
	accumulator := logger.NewAccumulator()
	ep, err := New(rng, WithHttpStarter(Server(&wg, &proxy)), WithMetricsStarter(Server(&wg, &metrics)),
		WithConfig(config), WithLogging(accumulator), WithAuthenticator(Deny(cookie, nil, nil)),
		WithNasshpMods(nasshp.WithSymmetricOptions(token.WithGeneratedSymmetricKey(0))),
		WithPrometheus(reg, reg))
	assert.NoError(t, err)
	assert.NotNil(t, ep)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	metrics += "/metrics"
	body := ""

	// The root fe for test1.lan is not mapped anywhere, should return an error.
	err = protocol.Get(metrics, protocol.Read(protocol.String(&body)))
	assert.NoError(t, err, "%s - %r", err, accumulator.Retrieve())
	lines := strings.Split(body, "\n")
	// Surely there are more than 10 metrics...
	assert.True(t, len(lines) > 10)
	// Check that all metrics are expected...
	assert.Regexp(t, "(?m)^(#|nasshp_)", body, "%s", body)
}

func TestMetricsExportEveryNasshModule(t *testing.T) {
	config := Config{
		NasshModules: map[string]NasshModule{
			"alpha": {RelayHost: "alpha.test"},
			"beta":  {RelayHost: "beta.test:8443"},
		},
		Mapping: []Mapping{
			{
				Module: "alpha",
				From: httpp.HostPath{
					Host: "frontend-alpha.test",
					Path: "/",
				},
				Target: Target{Nassh: &NasshTarget{}},
			},
			{
				Module: "beta",
				From: httpp.HostPath{
					Host: "frontend-beta.test",
					Path: "/",
				},
				Target: Target{Nassh: &NasshTarget{}},
			},
		},
		Tunnels: []string{"*"},
	}

	var proxy string
	var metrics string
	var wg sync.WaitGroup
	rng := rand.New(rand.NewSource(1))
	reg := prometheus.NewPedanticRegistry()
	ep, err := New(
		rng,
		WithHttpStarter(Server(&wg, &proxy)),
		WithMetricsStarter(Server(&wg, &metrics)),
		WithConfig(config),
		WithDisabledNasshAuthentication(true),
		WithNasshpMods(nasshp.WithSymmetricOptions(token.WithGeneratedSymmetricKey(0))),
		WithPrometheus(reg, reg),
	)
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	body := ""
	err = protocol.Get(metrics+"/metrics", protocol.Read(protocol.String(&body)))
	assert.NoError(t, err)
	assert.Contains(t, body, `nasshp_pool_gets{module="alpha"} 0`)
	assert.Contains(t, body, `nasshp_pool_gets{module="beta"} 0`)
	assert.NotContains(t, body, "nasshp_pool_gets 0")
}

func TestRejectedNasshReloadDoesNotSwapMetrics(t *testing.T) {
	initial := Config{
		NasshModules: map[string]NasshModule{
			"default": {RelayHost: "nassh.test"},
		},
		Mapping: []Mapping{
			{
				From: httpp.HostPath{
					Host: "nassh.test",
					Path: "/",
				},
				Target: Target{Nassh: &NasshTarget{}},
			},
		},
		Tunnels: []string{"*"},
	}
	updated := Config{
		NasshModules: map[string]NasshModule{
			"default": {RelayHost: "other.test"},
		},
		Mapping: []Mapping{
			{
				From: httpp.HostPath{
					Host: "other.test",
					Path: "/",
				},
				Target: Target{Nassh: &NasshTarget{}},
			},
		},
		Tunnels: []string{"*"},
	}

	var proxy string
	var metrics string
	var wg sync.WaitGroup
	rng := rand.New(rand.NewSource(1))
	reg := prometheus.NewPedanticRegistry()
	ep, err := New(
		rng,
		WithHttpStarter(Server(&wg, &proxy)),
		WithMetricsStarter(Server(&wg, &metrics)),
		WithConfig(initial),
		WithDisabledNasshAuthentication(true),
		WithNasshpMods(nasshp.WithSymmetricOptions(token.WithGeneratedSymmetricKey(0))),
		WithPrometheus(reg, reg),
	)
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	resp, _ := GetWithHost(t, proxy+"cookie", "nassh.test")
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	body := ""
	err = protocol.Get(metrics+"/metrics", protocol.Read(protocol.String(&body)))
	assert.NoError(t, err)
	assert.Contains(t, body, `nasshp_url_errors{error="invalid parameters",module="default",type="bad client",url="/cookie"} 1`)

	err = ep.ApplyConfigStruct(updated)
	assert.Regexp(t, "cannot apply config changing listener domains after proxy start", err)

	body = ""
	err = protocol.Get(metrics+"/metrics", protocol.Read(protocol.String(&body)))
	assert.NoError(t, err)
	assert.Contains(t, body, `nasshp_url_errors{error="invalid parameters",module="default",type="bad client",url="/cookie"} 1`)
}

func TestRemovedNasshModuleUnregistersMetrics(t *testing.T) {
	backend, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("ok")))
	assert.NoError(t, err)

	proxyMapping := ProxyMapping(httpp.Mapping{
		From: httpp.HostPath{
			Host: "proxy.test",
			Path: "/",
		},
		Auth: httpp.MappingPublic,
		To:   backend,
	})

	initial := Config{
		NasshModules: map[string]NasshModule{
			"old": {RelayHost: "nassh.test"},
		},
		Mapping: []Mapping{
			{
				Module: "old",
				From: httpp.HostPath{
					Host: "nassh.test",
					Path: "/",
				},
				Target: Target{Nassh: &NasshTarget{}},
			},
			proxyMapping,
		},
		Tunnels: []string{"*"},
	}
	updated := Config{
		Domains: []string{"nassh.test"},
		Mapping: []Mapping{proxyMapping},
		Tunnels: []string{"*"},
	}

	var proxy string
	var metrics string
	var wg sync.WaitGroup
	rng := rand.New(rand.NewSource(1))
	reg := prometheus.NewPedanticRegistry()
	ep, err := New(
		rng,
		WithHttpStarter(Server(&wg, &proxy)),
		WithMetricsStarter(Server(&wg, &metrics)),
		WithConfig(initial),
		WithDisabledNasshAuthentication(true),
		WithNasshpMods(nasshp.WithSymmetricOptions(token.WithGeneratedSymmetricKey(0))),
		WithPrometheus(reg, reg),
	)
	assert.NoError(t, err)

	err = ep.Run()
	assert.NoError(t, err)
	wg.Wait()

	body := ""
	err = protocol.Get(metrics+"/metrics", protocol.Read(protocol.String(&body)))
	assert.NoError(t, err)
	assert.Contains(t, body, `nasshp_pool_gets{module="old"} 0`)

	err = ep.ApplyConfigStruct(updated)
	assert.NoError(t, err)

	body = ""
	err = protocol.Get(metrics+"/metrics", protocol.Read(protocol.String(&body)))
	assert.NoError(t, err)
	assert.NotContains(t, body, `module="old"`)
	assert.NotContains(t, body, "nasshp_pool_gets")
}

func TestBandwidth(t *testing.T) {
	config := Config{
		Mapping: []Mapping{
			NasshMapping(""),
		},

		// Allow any tunnel.
		Tunnels: []string{"*"},
	}

	cookie := &oauth.CredentialsCookie{
		Identity: oauth.Identity{
			Id:           "id",
			Username:     "username",
			Organization: "organization",
		},
	}

	var fe string
	rng := rand.New(rand.NewSource(1))

	ep, err := New(rng, WithHttpStarter(Server(&sync.WaitGroup{}, &fe)), WithConfig(config),
		WithLogging(logger.DefaultLogger{Printer: log.Printf}), WithAuthenticator(Allow(cookie)),
		WithNasshpMods(nasshp.WithSymmetricOptions(token.WithGeneratedSymmetricKey(0))))
	assert.NoError(t, err)
	assert.NotNil(t, ep)

	ep.Run()

	// Start an echo server to use as a tunnel backend.
	e, err := echo.New("127.0.0.1:0")
	assert.NoError(t, err)
	assert.NotNil(t, e)

	echoaddress, err := e.Address()
	assert.NoError(t, err)

	defer e.Close()
	go e.Run()

	proxy, err := url.Parse(fe)
	assert.NoError(t, err)

	// Try a tunnel connection.
	pool := nasshp.NewBufferPool(8192)
	tlog := logger.NewAccumulator()
	tunnel, err := ptunnel.NewTunnel(pool, ptunnel.WithLogger(tlog))
	assert.NoError(t, err)
	assert.NotNil(t, tunnel)

	defer tunnel.Close()
	go tunnel.KeepConnected(proxy, echoaddress.IP.String(), uint16(echoaddress.Port))

	send, write := io.Pipe()
	go func() {
		tunnel.Send(send)
		tunnel.Close()
		send.Close()
	}()

	read, receive := io.Pipe()
	go func() {
		tunnel.Receive(receive)
		tunnel.Close()
		receive.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	const kTotalBytes = 1000 * 1048576
	quote := "You never change things by fighting the existing reality. To change something, build a new model that makes the existing model obsolete.\n"

	start := time.Now()

	var total_write uint64
	go func() {
		defer wg.Done()

		for count := 0; atomic.LoadUint64(&total_write) < kTotalBytes; count++ {
			l, err := write.Write([]byte(fmt.Sprintf("%09d %s", count, quote)))
			assert.NoError(t, err)
			assert.Equal(t, len(quote)+10, l)
			atomic.AddUint64(&total_write, uint64(l))
		}
		write.Close()
	}()

	var total_read uint64
	go func() {
		defer wg.Done()

		reader := bufio.NewReader(read)
		for count := 0; atomic.LoadUint64(&total_read) < kTotalBytes; count++ {
			rback, err := reader.ReadString('\n')
			if err == io.EOF {
				break
			}
			assert.NoError(t, err)
			assert.Equal(t, fmt.Sprintf("%09d %s", count, quote), rback, "incorrect at offset %d - count %d", atomic.LoadUint64(&total_read), count)
			atomic.AddUint64(&total_read, uint64(len(rback)))
		}
		read.Close()
	}()

	go func() {
		for {
			time.Sleep(1 * time.Second)
			log.Printf("Progress: read %d - write %d", atomic.LoadUint64(&total_read), atomic.LoadUint64(&total_write))
		}
	}()
	wg.Wait()

	done := time.Now()
	logs := tlog.Retrieve()
	assert.True(t, len(logs) <= 0, "more than one log entry: %v", logs)

	delta := done.Sub(start)
	rate := (kTotalBytes / delta.Seconds()) / 1024
	assert.True(t, rate >= 10, "total run time: %s - rate %f KBps", delta, rate)
	log.Printf("total run time: %s - rate %f KBps", delta, rate)
}
