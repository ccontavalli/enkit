package enproxy

import (
	"bufio"
	"encoding/json"
	"fmt"
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

func PublicConfig(host, path, to string, tunnels ...string) Config {
	return Config{
		Mapping: []httpp.Mapping{
			{
				From: httpp.HostPath{
					Host: host,
					Path: path,
				},
				Auth: httpp.MappingPublic,
				To:   to,
			},
		},
		Tunnels: tunnels,
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

func TestInvalidConfig(t *testing.T) {
	var url string
	rng := rand.New(rand.NewSource(1))

	// Config file without any mappings.
	ep, err := New(rng, WithHttpStarter(Server(&sync.WaitGroup{}, &url)))
	assert.Regexp(t, "config file.*has no Mapping.*defined", err)
	assert.Nil(t, ep)

	config := Config{
		Mapping: []httpp.Mapping{
			httpp.Mapping{
				From: httpp.HostPath{
					Host: "test.lan",
					Path: "/",
				},
				To: "toast.lan"},
		},
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
	assert.True(t, len(events) >= 4, "%v", events)
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
		Mapping: []httpp.Mapping{
			{
				From: httpp.HostPath{
					Host: "TEST.LAN",
					Path: "/",
				},
				Auth: httpp.MappingPublic,
				To:   s1,
			},
			{
				From: httpp.HostPath{
					Host: "test.lan.",
					Path: "/",
				},
				Auth: httpp.MappingPublic,
				To:   s2,
			},
		},
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
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)
	s2, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s2")))
	assert.NoError(t, err)

	cookie := &oauth.CredentialsCookie{
		Identity: oauth.Identity{
			Id:           "id",
			Username:     "username",
			Organization: "organization",
		},
	}

	initial := PublicConfig("test.lan", "/", s1, "tcp|10.0.0.1:22")
	updated := PublicConfig("test.lan", "/", s2, "tcp|10.0.0.2:22")

	rng := rand.New(rand.NewSource(1))
	ep, err := New(rng,
		WithConfig(initial),
		WithAuthenticator(Allow(cookie)),
		WithNasshpMods(nasshp.WithSymmetricOptions(token.WithGeneratedSymmetricKey(0))),
	)
	assert.NoError(t, err)
	assert.NotNil(t, ep.nproxy)

	nproxy := ep.nproxy
	assert.Equal(t, utils.VerdictAllow, ep.whitelist.Allow("tcp", "10.0.0.1:22", cookie))
	assert.Equal(t, utils.VerdictDrop, ep.whitelist.Allow("tcp", "10.0.0.2:22", cookie))

	err = ep.ApplyConfigStruct(updated)
	assert.NoError(t, err)
	assert.Same(t, nproxy, ep.nproxy)
	assert.Equal(t, utils.VerdictDrop, ep.whitelist.Allow("tcp", "10.0.0.1:22", cookie))
	assert.Equal(t, utils.VerdictAllow, ep.whitelist.Allow("tcp", "10.0.0.2:22", cookie))
}

func TestApplyConfigStructReusesUnchangedProxyHandlers(t *testing.T) {
	s1, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s1")))
	assert.NoError(t, err)
	s2, err := ktest.Start(http.HandlerFunc(ktest.StringHandler("s2")))
	assert.NoError(t, err)

	initial := PublicConfig("test.lan", "/", s1, "*")
	updated := PublicConfig("test.lan", "/", s2, "*")

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
		Mapping: []httpp.Mapping{
			{
				From: httpp.HostPath{
					Host: "one.lan",
					Path: "/",
				},
				Auth: httpp.MappingPublic,
				To:   s1,
			},
			{
				From: httpp.HostPath{
					Host: "two.lan",
					Path: "/",
				},
				Auth: httpp.MappingPublic,
				To:   s1,
			},
		},
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
		Mapping: []httpp.Mapping{
			{
				Name: "one",
				From: httpp.HostPath{
					Host: "one.lan",
					Path: "/",
				},
				Auth: httpp.MappingPublic,
				To:   s1,
			},
			{
				Name: "two",
				From: httpp.HostPath{
					Host: "two.lan",
					Path: "/",
				},
				Auth: httpp.MappingPublic,
				To:   s1,
			},
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
		Mapping: []httpp.Mapping{
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

			// No wildcard match for now.
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
		Mapping: []httpp.Mapping{
			// A single file path on this host.
			httpp.Mapping{
				From: httpp.HostPath{
					Host: "test1.lan",
					Path: "/glad",
				},
				Auth: httpp.MappingPublic,
				To:   s1,
			},
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

func TestBandwidth(t *testing.T) {
	config := Config{
		Mapping: []httpp.Mapping{
			httpp.Mapping{
				From: httpp.HostPath{
					Host: "",
					Path: "/",
				},
				Auth: httpp.MappingPublic,
			},
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
