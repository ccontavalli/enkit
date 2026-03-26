package httpp

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/ccontavalli/enkit/lib/khttp"
	"github.com/ccontavalli/enkit/lib/logger"
	"github.com/ccontavalli/enkit/proxy/amux"
	"github.com/ccontavalli/enkit/proxy/utils"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func NewProxy(fromurl, tourl string, transform *Transform) (*httputil.ReverseProxy, error) {
	to, err := url.Parse(tourl)
	if err != nil {
		return nil, err
	}
	if transform == nil {
		transform = &Transform{}
	}

	if err := transform.Compile(fromurl, tourl); err != nil {
		return nil, err
	}

	toQuery := to.RawQuery
	director := func(req *http.Request) {
		req.URL.Scheme = to.Scheme
		req.URL.Host = to.Host
		req.URL.RawQuery = khttp.JoinURLQuery(toQuery, req.URL.RawQuery)

		transform.Apply(req)

		req.URL.Path = khttp.JoinPreserve(to.Path, req.URL.Path)
		req.URL.RawPath = ""
	}

	proxy := &httputil.ReverseProxy{Director: director}
	proxy.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	return proxy, nil
}

// Binding is one concrete mux registration.
//
// A single logical Mapping can expand into multiple bindings, for example when
// a trailing-slash route also needs a "*" catch-all registration. Bindings keep
// "where this handler is mounted" separate from "which handler instance should
// serve it", so route installation can happen independently from handler
// creation and handler reuse.
type Binding struct {
	Host      string
	Path      string
	To        string
	Transform *Transform
	Handler   http.Handler
}

// Compiled is the result of turning configuration mappings into a routing
// installation plan.
//
// It contains:
//   - the Bindings to register on a mux for this generation
//   - the Domains implied by those bindings
//   - the Handlers keyed by module signature, so unchanged handler instances can
//     be reused across config applies
//
// The split is needed because handler identity and route mounting are not the
// same thing. A single handler instance may be mounted on multiple hosts, and a
// route move should not force a new handler when the handler-producing
// configuration is unchanged.
type Compiled struct {
	Domains  []string
	Bindings []Binding
	Handlers map[string]http.Handler
}

type ProxyCreator func(m Mapping) (http.Handler, error)

func bindingKey(binding Binding) string {
	return binding.Host + "\x00" + binding.Path
}

// BindingsForMapping expands one logical Mapping into the concrete mux
// registrations needed to serve it, and returns any hostnames that should be
// considered part of the active domain set.
func BindingsForMapping(mapping Mapping, handler http.Handler) ([]Binding, []string) {
	host := utils.NormalizeHost(mapping.From.Host)
	path := mapping.From.Path
	if path == "" {
		path = "/"
	}

	bindings := []Binding{{
		Host:      host,
		Path:      path,
		To:        mapping.To,
		Transform: mapping.Transform,
		Handler:   handler,
	}}
	if strings.HasSuffix(path, "/") {
		bindings = append(bindings, Binding{
			Host:      host,
			Path:      path + "*",
			To:        mapping.To,
			Transform: mapping.Transform,
			Handler:   handler,
		})
	}

	domains := []string{}
	if host != "" {
		domains = append(domains, host)
	}
	return bindings, domains
}

// InstallBindings attaches a compiled routing plan to a mux.
//
// This is intentionally separate from CompileBindings so callers can build a
// fresh mux from an already-compiled plan, or compile first and decide later
// whether to make that plan live.
func InstallBindings(mux amux.Mux, log logger.Logger, bindings []Binding) {
	if log == nil {
		log = logger.Nil
	}

	add := func(binding Binding, mux amux.Mux) {
		t := "default transforms"
		if binding.Transform != nil {
			t = fmt.Sprintf("%+v", binding.Transform)
		}
		log.Infof("Mapping: %s%s to %s (%+v)", binding.Host, binding.Path, binding.To, t)
		mux.Handle(binding.Path, binding.Handler)
	}

	hosts := map[string]amux.Mux{
		"": mux,
	}
	for _, binding := range bindings {
		hmux, found := hosts[binding.Host]
		if !found {
			hmux = mux.Host(binding.Host)
			hosts[binding.Host] = hmux
		}
		add(binding, hmux)
	}
}

// CompileBindings turns mappings into a routing plan and a set of reusable
// handler instances.
//
// The reuse map is keyed by ModuleKey and lets callers carry handler instances
// across config generations. The resulting Compiled value can then be installed
// onto a mux separately, which is what makes reload-safe rebuild-and-swap
// possible.
func CompileBindings(mappings []Mapping, reuse map[string]http.Handler, creator ProxyCreator) (*Compiled, error) {
	compiled := &Compiled{
		Domains:  []string{},
		Bindings: []Binding{},
		Handlers: map[string]http.Handler{},
	}

	seenDomains := map[string]bool{}
	seenBindings := map[string]int{}
	for ix, mapping := range mappings {
		key, err := ModuleKey(mapping)
		if err != nil {
			return nil, fmt.Errorf("error in mapping entry %d - %w", ix, err)
		}

		handler := compiled.Handlers[key]
		if handler == nil {
			handler = reuse[key]
		}
		if handler == nil {
			handler, err = creator(mapping)
			if err != nil {
				return nil, fmt.Errorf("error in mapping entry %d - %w", ix, err)
			}
		}
		compiled.Handlers[key] = handler

		bindings, domains := BindingsForMapping(mapping, handler)
		for _, binding := range bindings {
			bkey := bindingKey(binding)
			if previous, found := seenBindings[bkey]; found {
				return nil, fmt.Errorf(
					"error in mapping entry %d - duplicate route %q on host %q already defined by mapping entry %d",
					ix, binding.Path, binding.Host, previous,
				)
			}
			seenBindings[bkey] = ix
		}
		compiled.Bindings = append(compiled.Bindings, bindings...)

		for _, domain := range domains {
			if !seenDomains[domain] {
				compiled.Domains = append(compiled.Domains, domain)
				seenDomains[domain] = true
			}
		}
	}

	return compiled, nil
}

func canonicalTransformForKey(transform *Transform) *Transform {
	normalized := cloneTransform(transform)
	if len(normalized.UrlRegex) == 0 {
		normalized.UrlRegex = nil
	}
	if len(normalized.StripCookie) == 0 {
		normalized.StripCookie = nil
	}
	if len(normalized.MapRequestHeaders) == 0 {
		normalized.MapRequestHeaders = nil
	}
	for ix := range normalized.MapRequestHeadersByGroup {
		if len(normalized.MapRequestHeadersByGroup[ix].GroupMapping) == 0 {
			normalized.MapRequestHeadersByGroup[ix].GroupMapping = nil
		}
	}
	if len(normalized.MapRequestHeadersByGroup) == 0 {
		normalized.MapRequestHeadersByGroup = nil
	}
	return normalized
}

func ModuleKey(mapping Mapping) (string, error) {
	normalized := mapping
	normalized.Name = strings.TrimSpace(normalized.Name)
	normalized.From.Host = ""
	if normalized.From.Path == "" {
		normalized.From.Path = "/"
	}
	normalized.Transform = canonicalTransformForKey(normalized.Transform)
	key, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(key), nil
}

func MappingKey(mapping Mapping) (string, error) {
	return ModuleKey(mapping)
}
