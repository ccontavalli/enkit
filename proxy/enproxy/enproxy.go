// Package enproxy provides a complete proxy implementation with support
// for HTTP, HTTP/2, and NASSH, with OAUTH authentication, all in a simple
// API to use.
//
// This package glues together the default go net/http/httputil ReverseProxy
// packaged in proxy/httpp and the SSH over HTTPs implementation in proxy/nasshp
// together witha frontend server implemented using net/http, packaged in
// lib/khttp.
//
// The simplest use of this library is via flags:
//
//	import (
//	    // Secure random numbers.
//	    "github.com/ccontavalli/enkit/lib/srand"
//	    "github.com/ccontavalli/enkit/lib/kflags"
//	    "flag"
//	)
//
//	flags := enproxy.DefaultFlags()
//	flags.Register(&kflags.GoFlagSet{FlagSet: flag.CommandLine})
//
//	// Parse flags after registering them!!
//	flag.Parse()
//
//	rng := rand.New(srand.Source)
//	proxy, err := enproxy.New(rng, enproxy.FromFlags(flags))
//	if err != nil {
//	  ...
//	}
//
//	proxy.Run()
//
// You can, of course, create a proxy manually with the desired options.
// In that case, you want to use `WithConfig` and other `With.*` modifiers
// to set all the desired options.
package enproxy

import (
	"context"
	"fmt"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/khttp"
	"github.com/ccontavalli/enkit/lib/logger"
	"github.com/ccontavalli/enkit/lib/oauth"
	"github.com/ccontavalli/enkit/proxy/amux"
	"github.com/ccontavalli/enkit/proxy/amux/amuxie"
	"github.com/ccontavalli/enkit/proxy/httpp"
	"github.com/ccontavalli/enkit/proxy/nasshp"
	"github.com/ccontavalli/enkit/proxy/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"math/rand"
	"net/http"
	"sync"
)

// Config is the content of the proxy configuration file.
type Config struct {
	// Which URLs to map to which other URLs.
	Mapping []httpp.Mapping
	// Extra domains for which to obtain a certificate.
	Domains []string
	// List of allowed tunnels.
	Tunnels []string
}

// Warnings represents a list of warnings.
type Warnings []string

// Add adds a new warning.
func (w *Warnings) Add(warning string) {
	(*w) = append(*w, warning)
}

// Print prints the list of warnings.
//
// For example:
//
//	warnings.Print(log.Printf)
//
// or:
//
//	warnings.Print(klogger.Warnf)
func (w *Warnings) Print(printer logger.Printer) {
	for _, warn := range *w {
		printer("%s", warn)
	}
}

// Parse verifies and indexes a loaded Config.
//
// Returns the parsed whitelist of tunnels allowed, followed by a list of warnings.
func (config *Config) Parse() (utils.PatternList, Warnings, error) {
	var warn Warnings

	if len(config.Mapping) <= 0 {
		return nil, warn, kflags.NewUsageErrorf("config file: has no Mapping(s) defined")
	}
	if len(config.Tunnels) <= 0 {
		warn.Add("config file: empty whitelist for tunnels - no tunnel will be allowed!")
	}
	wl, err := utils.NewPatternList(config.Tunnels)
	if err != nil {
		return nil, warn, kflags.NewUsageErrorf("config file: illegal patterns specified in tunnels: %s", err)
	}

	return wl, warn, nil
}

// Flags represents command line flags necessary to define a proxy.
type Flags struct {
	Http       *khttp.Flags
	Oauth      *oauth.RedirectorFlags
	Nassh      *nasshp.Flags
	Prometheus *khttp.Flags

	ConfigContent          []byte
	ConfigName             string
	DisabledAuthentication bool
}

// DefaultFlags returns the default flags.
//
// The default is generally a valid, working, one except for mandatory
// configuration parameters.
func DefaultFlags() *Flags {
	fl := &Flags{
		Http:       khttp.DefaultFlags(),
		Oauth:      oauth.DefaultRedirectorFlags(),
		Nassh:      nasshp.DefaultFlags(),
		Prometheus: khttp.DefaultFlags(),
	}

	// By default, disable the prometheus server.
	fl.Prometheus.HttpPort = 0
	return fl
}

// Register register the flags necessary to configure enproxy.
func (fl *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	fl.Http.Register(set, prefix)
	fl.Oauth.Register(set, prefix)
	fl.Nassh.Register(set, prefix)
	fl.Prometheus.Register(set, prefix+"prometheus-")

	set.ByteFileVar(&fl.ConfigContent, prefix+"config", fl.ConfigName, "Default config file location.", kflags.WithFilename(&fl.ConfigName))
	set.BoolVar(&fl.DisabledAuthentication, prefix+"without-authentication", false, "allow tunneling even without authentication")

	return fl
}

// Starter is a function capable of starting a web server.
//
// Requires providing a logger, an http.Handler (typically some form of mux), and
// a list of domains for which an https certificate is necessary.
type Starter func(log logger.Printer, handler http.Handler, domains ...string) error

// StarterFromFlags creates a starter from kserver.Flags.
func StarterFromFlags(flags *khttp.Flags) Starter {
	if flags.HttpPort == 0 && flags.HttpsPort == 0 {
		return nil
	}

	return func(log logger.Printer, handler http.Handler, domains ...string) error {
		return khttp.Run(handler, khttp.WithLogger(log), khttp.FromFlags(flags, domains...))
	}
}

type Options struct {
	log logger.Logger

	proxy   Starter
	metrics Starter

	gatherer prometheus.Gatherer
	register prometheus.Registerer

	configFileContent []byte
	configFileName    string
	config            *Config

	pmods []httpp.Modifier
	nmods []nasshp.Modifier

	authenticate               oauth.Authenticate
	withoutNasshAuthentication bool
}

type Modifier func(opt *Options) error
type Modifiers []Modifier

func (mods Modifiers) Apply(o *Options) error {
	for _, m := range mods {
		if err := m(o); err != nil {
			return err
		}
	}
	return nil
}

func WithConfig(config Config) Modifier {
	return func(op *Options) error {
		copy := config
		op.config = &copy
		op.configFileContent = nil
		op.configFileName = ""
		return nil
	}
}

func WithConfigFile(name string, data []byte) Modifier {
	return func(op *Options) error {
		op.config = nil
		op.configFileName = name
		op.configFileContent = append([]byte{}, data...)
		return nil
	}
}

func WithDisabledNasshAuthentication(disabled bool) Modifier {
	return func(op *Options) error {
		op.withoutNasshAuthentication = disabled
		return nil
	}
}

func WithAuthenticator(auth oauth.Authenticate) Modifier {
	return func(op *Options) error {
		op.authenticate = auth
		return nil
	}
}

func WithHttpStarter(starter Starter) Modifier {
	return func(op *Options) error {
		op.proxy = starter
		return nil
	}
}

func WithMetricsStarter(starter Starter) Modifier {
	return func(op *Options) error {
		op.metrics = starter
		return nil
	}
}

func WithHttpFlags(flags *khttp.Flags) Modifier {
	return func(op *Options) error {
		return WithHttpStarter(StarterFromFlags(flags))(op)
	}
}

func WithMetricsFlags(flags *khttp.Flags) Modifier {
	return func(op *Options) error {
		return WithMetricsStarter(StarterFromFlags(flags))(op)
	}
}

func WithPrometheus(gatherer prometheus.Gatherer, register prometheus.Registerer) Modifier {
	return func(op *Options) error {
		op.gatherer = gatherer
		op.register = register
		return nil
	}
}

func WithProxyMods(pmods ...httpp.Modifier) Modifier {
	return func(op *Options) error {
		op.pmods = append(op.pmods, pmods...)
		return nil
	}
}

func WithNasshpMods(nmods ...nasshp.Modifier) Modifier {
	return func(op *Options) error {
		op.nmods = append(op.nmods, nmods...)
		return nil
	}
}

func WithOauthRedirector(rflags *oauth.RedirectorFlags) Modifier {
	return func(op *Options) error {
		redirector, err := oauth.NewRedirector(oauth.WithRedirectorFlags(rflags))
		if err != nil {
			return err
		}
		if err := WithAuthenticator(redirector.Authenticate)(op); err != nil {
			return err
		}

		pmods := []httpp.Modifier{
			httpp.WithStripCookie([]string{redirector.CredentialsCookieName()}),
		}
		return WithProxyMods(pmods...)(op)
	}

}

func WithLogging(logger logger.Logger) Modifier {
	return func(op *Options) error {
		op.log = logger
		return nil
	}
}

func FromFlags(flags *Flags) Modifier {
	return func(op *Options) error {
		if len(flags.ConfigContent) <= 0 {
			return kflags.NewUsageErrorf("Config file is empty, or no config file specified. Check the --config flag.")
		}

		if flags.Oauth.AuthURL != "" && !flags.DisabledAuthentication {
			if err := WithOauthRedirector(flags.Oauth)(op); err != nil {
				return err
			}
		}

		if err := WithNasshpMods(nasshp.FromFlags(flags.Nassh))(op); err != nil {
			return err
		}

		if err := WithHttpFlags(flags.Http)(op); err != nil {
			return err
		}
		if err := WithMetricsFlags(flags.Prometheus)(op); err != nil {
			return err
		}

		return WithConfigFile(flags.ConfigName, flags.ConfigContent)(op)
	}
}

type Enproxy struct {
	log logger.Logger

	applyMu sync.Mutex

	handler      *khttp.ReplaceableHandler
	domains      []string
	proxyStarted bool

	nproxy    *nasshp.NasshProxy
	whitelist *utils.ReplaceableWhitelist
	modules   map[string]runtimeModule

	register prometheus.Registerer
	gatherer prometheus.Gatherer

	proxy   Starter
	metrics Starter

	pmods []httpp.Modifier
}

func New(rng *rand.Rand, mods ...Modifier) (*Enproxy, error) {
	op := &Options{
		log:   &logger.DefaultLogger{Printer: log.Printf},
		proxy: StarterFromFlags(khttp.DefaultFlags()),
	}
	if err := Modifiers(mods).Apply(op); err != nil {
		return nil, err
	}

	ep := &Enproxy{
		log: op.log,
		handler: khttp.NewReplaceableHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "configuration not loaded", http.StatusServiceUnavailable)
		})),
		whitelist: utils.NewReplaceableWhitelist(),
		modules:   map[string]runtimeModule{},
		proxy:     op.proxy,
		metrics:   op.metrics,
		gatherer:  op.gatherer,
		register:  op.register,
		pmods:     append([]httpp.Modifier{httpp.WithLogging(op.log), httpp.WithAuthenticator(op.authenticate)}, op.pmods...),
	}

	var (
		err    error
		nproxy *nasshp.NasshProxy
	)
	if op.authenticate == nil && !op.withoutNasshAuthentication {
		op.log.Warnf("ssh gateway disabled as no authentication was configured")
	} else {
		authenticate := op.authenticate
		if op.withoutNasshAuthentication {
			op.log.Errorf("Watch out! The proxy is being started without authentication! SSH tunneling will rely entirely on a filmsy whitelist")
			authenticate = nil
		}

		nproxy, err = nasshp.New(rng, authenticate, append([]nasshp.Modifier{nasshp.WithFilter(ep.whitelist.Allow), nasshp.WithLogging(op.log)}, op.nmods...)...)
		if err != nil {
			return nil, err
		}
	}
	ep.nproxy = nproxy

	if op.metrics != nil {
		if op.gatherer == nil || op.register == nil {
			ep.gatherer = prometheus.DefaultGatherer
			ep.register = prometheus.DefaultRegisterer
		}
	}

	switch {
	case op.config != nil:
		if err := ep.ApplyConfigStruct(*op.config); err != nil {
			return nil, err
		}
	case len(op.configFileContent) > 0:
		if err := ep.ApplyConfigFile(op.configFileName, op.configFileContent); err != nil {
			return nil, err
		}
	default:
		if err := ep.ApplyConfigStruct(Config{}); err != nil {
			return nil, err
		}
	}

	if ep.metrics != nil && nproxy != nil {
		if err := nproxy.ExportMetrics(ep.register); err != nil {
			return nil, err
		}
	}

	return ep, nil
}

func normalizeDomains(domains []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, domain := range domains {
		normalized := utils.NormalizeHost(domain)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func sameDomains(one, two []string) bool {
	left := normalizeDomains(one)
	right := normalizeDomains(two)
	if len(left) != len(right) {
		return false
	}

	seen := map[string]bool{}
	for _, domain := range left {
		seen[domain] = true
	}
	for _, domain := range right {
		if !seen[domain] {
			return false
		}
	}
	return true
}

func collectDomains(desired *desiredState, modules map[string]runtimeModule) []string {
	domains := append([]string{}, desired.Domains...)
	for _, route := range desired.Routes {
		domains = append(domains, route.Host)
	}
	for _, desiredModule := range desired.Modules {
		module := modules[desiredModule.ID()]
		domains = append(domains, module.Domains()...)
	}
	return normalizeDomains(domains)
}

func (ep *Enproxy) ApplyConfigFile(name string, data []byte) error {
	var config Config
	if err := marshal.UnmarshalDefault(name, data, marshal.Json, &config); err != nil {
		return kflags.NewUsageErrorf("Invalid configuration file '%s': %w", name, err)
	}
	return ep.ApplyConfigStruct(config)
}

func (ep *Enproxy) ApplyConfigStruct(config Config) error {
	wl, warns, err := config.Parse()
	if err != nil {
		return err
	}

	builder, err := httpp.NewBuilder(ep.pmods...)
	if err != nil {
		return err
	}

	desired, err := compileDesiredState(builder, ep.nproxy, ep.whitelist, config, wl, warns)
	if err != nil {
		return err
	}

	ep.applyMu.Lock()
	defer ep.applyMu.Unlock()

	modules, stale, err := reconcileModules(desired.Modules, ep.modules)
	if err != nil {
		return err
	}

	domains := collectDomains(desired, modules)
	if ep.proxyStarted && !sameDomains(ep.domains, domains) {
		return fmt.Errorf("cannot apply config changing listener domains after proxy start; restart required")
	}

	mux := amuxie.New()
	installer := newInstallContext(ep.log, amux.Mux(mux))
	seenBindings := map[string]int{}

	for _, route := range desired.Routes {
		module, ok := modules[route.ModuleID].(*proxyRuntimeModule)
		if !ok {
			return fmt.Errorf("module %s is not a proxy module", route.ModuleID)
		}
		for _, binding := range module.BindingsForHost(route.Host) {
			bkey := binding.Host + "\x00" + binding.Path
			if previous, found := seenBindings[bkey]; found {
				return fmt.Errorf(
					"error in mapping entry %d - duplicate route %q on host %q already defined by mapping entry %d",
					route.Index, binding.Path, binding.Host, previous,
				)
			}
			seenBindings[bkey] = route.Index
			installer.InstallBinding(binding)
		}
	}

	for _, desiredModule := range desired.Modules {
		module := modules[desiredModule.ID()]
		if err := module.Install(installer); err != nil {
			return err
		}
	}

	for _, desiredModule := range desired.Modules {
		if err := modules[desiredModule.ID()].Activate(); err != nil {
			return err
		}
	}

	desired.Warnings.Print(ep.log.Warnf)
	ep.modules = modules
	ep.domains = domains
	ep.handler.Swap(mux)
	for _, module := range stale {
		if err := module.Close(); err != nil {
			ep.log.Warnf("failed closing stale module %s: %v", module.ID(), err)
		}
	}
	return nil
}

func (ep *Enproxy) RunMetrics() error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(ep.gatherer, promhttp.HandlerOpts{}))
	return ep.metrics(ep.log.Infof, mux)
}

func (ep *Enproxy) RunProxy() error {
	ep.applyMu.Lock()
	domains := append([]string{}, ep.domains...)
	ep.proxyStarted = true
	ep.applyMu.Unlock()

	err := ep.proxy(ep.log.Infof, &khttp.Dumper{Real: ep.handler, Log: log.Printf}, domains...)
	if err != nil {
		ep.applyMu.Lock()
		ep.proxyStarted = false
		ep.applyMu.Unlock()
	}
	return err
}

func (ep *Enproxy) Run() error {
	if ep.metrics != nil {
		go ep.RunMetrics()
	}
	if ep.nproxy != nil {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go ep.nproxy.Run(ctx)
	}
	return ep.RunProxy()
}
