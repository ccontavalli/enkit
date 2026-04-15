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
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/ccontavalli/enkit/lib/config"
	"github.com/ccontavalli/enkit/lib/config/factory"
	"github.com/ccontavalli/enkit/lib/config/marshal"
	"github.com/ccontavalli/enkit/lib/kflags"
	"github.com/ccontavalli/enkit/lib/khttp"
	"github.com/ccontavalli/enkit/lib/logger"
	"github.com/ccontavalli/enkit/lib/oauth"
	"github.com/ccontavalli/enkit/proxy/amux/amuxie"
	"github.com/ccontavalli/enkit/proxy/httpp"
	"github.com/ccontavalli/enkit/proxy/nasshp"
	"github.com/ccontavalli/enkit/proxy/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type ProxyModule struct {
	To        string
	Transform *httpp.Transform
}

type ProxyTarget struct {
	To        string
	Transform *httpp.Transform
}

type NasshModule struct {
	RelayHost string
}

type NasshTarget struct {
	RelayHost string
}

type Target struct {
	Proxy *ProxyTarget
	Nassh *NasshTarget
}

type Mapping struct {
	Name   string
	From   httpp.HostPath
	Auth   httpp.MappingAuth
	Module string
	Target Target
}

const defaultModuleName = "default"

// Config is the content of the proxy configuration file.
type Config struct {
	ProxyModules map[string]ProxyModule
	NasshModules map[string]NasshModule

	// Which URLs to map to which modules or targets.
	Mapping []Mapping
	// Extra domains for which to obtain a certificate.
	Domains []string
	// List of allowed tunnels.
	Tunnels []string
}

type MissingConfigPolicy string

const (
	MissingConfigAuto     MissingConfigPolicy = "auto"
	MissingConfigEmbedded MissingConfigPolicy = "embedded"
	MissingConfigError    MissingConfigPolicy = "error"
)

var errMissingConfig = errors.New("missing config")

func (m MissingConfigPolicy) Valid() bool {
	switch m {
	case MissingConfigAuto, MissingConfigEmbedded, MissingConfigError:
		return true
	default:
		return false
	}
}

func (t Target) kindCount() int {
	total := 0
	if t.Proxy != nil {
		total++
	}
	if t.Nassh != nil {
		total++
	}
	return total
}

func canonicalModuleName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return defaultModuleName
	}
	return name
}

func validateModuleName(kind, name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("%s module map cannot use an empty name; use %q", kind, defaultModuleName)
	}
	if trimmed != name {
		return fmt.Errorf("%s module %q must not have leading or trailing whitespace", kind, name)
	}
	return nil
}

func validateProxyModuleNames(modules map[string]ProxyModule) error {
	for name := range modules {
		if err := validateModuleName("proxy", name); err != nil {
			return err
		}
	}
	return nil
}

func validateNasshModuleNames(modules map[string]NasshModule) error {
	for name := range modules {
		if err := validateModuleName("nassh", name); err != nil {
			return err
		}
	}
	return nil
}

func resolveProxyModule(modules map[string]ProxyModule, name string) (ProxyModule, error) {
	name = canonicalModuleName(name)
	if name == defaultModuleName {
		if modules == nil {
			return ProxyModule{}, nil
		}
		return modules[defaultModuleName], nil
	}

	module, ok := modules[name]
	if !ok {
		return ProxyModule{}, fmt.Errorf("unknown proxy module %q", name)
	}
	return module, nil
}

func resolveNasshModule(modules map[string]NasshModule, name string) (NasshModule, error) {
	name = canonicalModuleName(name)
	if name == defaultModuleName {
		if modules == nil {
			return NasshModule{}, nil
		}
		return modules[defaultModuleName], nil
	}

	module, ok := modules[name]
	if !ok {
		return NasshModule{}, fmt.Errorf("unknown nassh module %q", name)
	}
	return module, nil
}

func resolveProxyMapping(module ProxyModule, mapping Mapping) (httpp.Mapping, error) {
	if mapping.Target.Proxy == nil {
		return httpp.Mapping{}, fmt.Errorf("proxy target is missing")
	}

	to := strings.TrimSpace(module.To)
	if targetTo := strings.TrimSpace(mapping.Target.Proxy.To); targetTo != "" {
		to = targetTo
	}
	if to == "" {
		return httpp.Mapping{}, fmt.Errorf("proxy target is missing a backend address")
	}

	transform := module.Transform
	if mapping.Target.Proxy.Transform != nil {
		transform = mapping.Target.Proxy.Transform
	}

	return httpp.Mapping{
		Name: mapping.Name,
		From: mapping.From,
		Auth: mapping.Auth,
		To:   to,

		Transform: transform,
	}, nil
}

func resolveNasshRelayHost(defaultRelayHost string, module NasshModule, mapping Mapping) string {
	if mapping.Target.Nassh != nil {
		if relay := strings.TrimSpace(mapping.Target.Nassh.RelayHost); relay != "" {
			return relay
		}
	}
	if relay := strings.TrimSpace(module.RelayHost); relay != "" {
		return relay
	}
	if relay := strings.TrimSpace(mapping.From.Host); relay != "" {
		return relay
	}
	if relay := strings.TrimSpace(defaultRelayHost); relay != "" {
		return relay
	}
	return ""
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
	if err := validateProxyModuleNames(config.ProxyModules); err != nil {
		return nil, warn, kflags.NewUsageErrorf("config file: %w", err)
	}
	if err := validateNasshModuleNames(config.NasshModules); err != nil {
		return nil, warn, kflags.NewUsageErrorf("config file: %w", err)
	}

	hasNassh := false
	for ix, mapping := range config.Mapping {
		if mapping.Target.kindCount() != 1 {
			return nil, warn, kflags.NewUsageErrorf("config file: mapping entry %d must define exactly one target kind", ix)
		}

		switch {
		case mapping.Target.Proxy != nil:
			module, err := resolveProxyModule(config.ProxyModules, mapping.Module)
			if err != nil {
				return nil, warn, kflags.NewUsageErrorf("config file: mapping entry %d - %w", ix, err)
			}
			if _, err := resolveProxyMapping(module, mapping); err != nil {
				return nil, warn, kflags.NewUsageErrorf("config file: mapping entry %d - %w", ix, err)
			}

		case mapping.Target.Nassh != nil:
			hasNassh = true
			if _, err := resolveNasshModule(config.NasshModules, mapping.Module); err != nil {
				return nil, warn, kflags.NewUsageErrorf("config file: mapping entry %d - %w", ix, err)
			}

			path := strings.TrimSpace(mapping.From.Path)
			if path == "" {
				path = "/"
			}
			if path != "/" {
				return nil, warn, kflags.NewUsageErrorf("config file: mapping entry %d - nassh targets must be mounted on /", ix)
			}

		}
	}

	if hasNassh && len(config.Tunnels) <= 0 {
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
	// ConfigStore controls the backend used to resolve and read --config.
	ConfigStore *factory.Flags

	// ConfigPath identifies the config entry to read from ConfigStore.
	ConfigPath string
	// ConfigMissing controls what happens when the selected config is missing.
	ConfigMissing          MissingConfigPolicy
	DisabledAuthentication bool
}

// DefaultFlags returns the default flags.
//
// The default is generally a valid, working, one except for mandatory
// configuration parameters.
func DefaultFlags() *Flags {
	fl := &Flags{
		Http:          khttp.DefaultFlags(),
		Oauth:         oauth.DefaultRedirectorFlags(),
		Nassh:         nasshp.DefaultFlags(),
		Prometheus:    khttp.DefaultFlags(),
		ConfigStore:   factory.DefaultAppConfigFlags(),
		ConfigMissing: MissingConfigAuto,
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
	fl.ConfigStore.Register(set, prefix)

	set.StringVar(&fl.ConfigPath, prefix+"config", fl.ConfigPath,
		"Path of the proxy config entry to load from the configured config store. Directory-backed stores accept filesystem paths; other backends typically use app/ns/key.")
	set.StringVar((*string)(&fl.ConfigMissing), prefix+"config-missing", string(fl.ConfigMissing), "What to do when the selected config is missing: auto, embedded, or error.")
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
	rng *rand.Rand
	log logger.Logger

	proxy   Starter
	metrics Starter

	gatherer prometheus.Gatherer
	register prometheus.Registerer

	configWorkspace config.StoreWorkspace
	configStore     config.Store
	configBinding   config.Binding
	configMissing   MissingConfigPolicy
	defaultConfig   *Config
	config          *Config

	pmods []httpp.Modifier
	nmods []nasshp.Modifier

	authenticate               oauth.Authenticate
	withoutNasshAuthentication bool
}

// Modifier updates enproxy construction options.
//
// Modifiers are applied in order, and later modifiers win. In particular,
// config source modifiers such as FromFlags, WithConfig, WithConfigFile, and
// WithConfigStore intentionally override earlier config sources.
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

func (op *Options) closeConfigStore() error {
	store := op.configStore
	workspace := op.configWorkspace
	op.configBinding = nil
	op.configStore = nil
	op.configWorkspace = nil

	var errs []error
	if store != nil {
		errs = append(errs, store.Close())
	}
	if workspace != nil {
		errs = append(errs, workspace.Close())
	}
	return errors.Join(errs...)
}

func WithConfig(config Config) Modifier {
	return func(op *Options) error {
		if err := op.closeConfigStore(); err != nil {
			return err
		}
		op.config = &config
		return nil
	}
}

// WithConfigFile parses the provided config immediately and overrides any
// earlier config source modifier.
func WithConfigFile(name string, data []byte) Modifier {
	return func(op *Options) error {
		if err := op.closeConfigStore(); err != nil {
			return err
		}
		var parsed Config
		if err := marshal.UnmarshalDefault(name, data, marshal.Json, &parsed); err != nil {
			return kflags.NewUsageErrorf("Invalid configuration file '%s': %w", name, err)
		}
		op.config = &parsed
		return nil
	}
}

func withConfigStore(workspace config.StoreWorkspace, store config.Store, binding config.Binding, explicit bool) Modifier {
	return func(op *Options) error {
		if err := op.closeConfigStore(); err != nil {
			return err
		}
		op.configWorkspace = workspace
		op.configStore = store
		op.configBinding = binding

		var loaded Config
		err := binding.Unmarshal(&loaded)
		if err == nil {
			op.config = &loaded
			return nil
		}
		if !os.IsNotExist(err) {
			_ = op.closeConfigStore()
			return fmt.Errorf("failed to load configuration from the configured store: %w", err)
		}

		missingErr := kflags.NewUsageErrorf("Default configuration %q does not exist in the configured store", defaultConfigTargetLabel)
		if explicit {
			missingErr = kflags.NewUsageErrorf("Configuration does not exist in the configured store")
		}

		switch op.configMissing {
		case MissingConfigAuto:
			if explicit {
				_ = op.closeConfigStore()
				return missingErr
			}
		case MissingConfigError:
			_ = op.closeConfigStore()
			return missingErr
		case MissingConfigEmbedded:
			// fall through
		default:
			if !op.configMissing.Valid() {
				_ = op.closeConfigStore()
				return kflags.NewUsageErrorf("Invalid value for --config-missing: %q", op.configMissing)
			}
		}

		if op.defaultConfig == nil {
			_ = op.closeConfigStore()
			return missingErr
		}
		op.config = op.defaultConfig
		return nil
	}
}

// WithConfigStore installs an explicitly selected config store binding and
// loads it immediately.
//
// Order matters: later config source modifiers override earlier ones. Apply
// WithDefaultConfigFile before WithConfigStore if you want the embedded config
// to be used as the missing-config fallback when the missing-config policy
// allows it.
func WithConfigStore(workspace config.StoreWorkspace, store config.Store, binding config.Binding) Modifier {
	return withConfigStore(workspace, store, binding, true)
}

// WithDefaultConfigStore installs the implicit default config store binding and
// loads it immediately.
//
// Order matters: later config source modifiers override earlier ones. Apply
// WithDefaultConfigFile before WithDefaultConfigStore if you want the embedded
// config to be used as the missing-config fallback when the missing-config
// policy allows it.
func WithDefaultConfigStore(workspace config.StoreWorkspace, store config.Store, binding config.Binding) Modifier {
	return withConfigStore(workspace, store, binding, false)
}

// WithDefaultConfigFile provides an embedded fallback config used when the
// selected config is missing and the missing-config policy allows it.
//
// Order matters: apply this before WithConfigStore or FromFlags if you want it
// to affect their eager config loading.
func WithDefaultConfigFile(name string, data []byte) Modifier {
	return func(op *Options) error {
		var parsed Config
		if err := marshal.UnmarshalDefault(name, data, marshal.Json, &parsed); err != nil {
			return kflags.NewUsageErrorf("Invalid configuration file '%s': %w", name, err)
		}
		op.defaultConfig = &parsed
		return nil
	}
}

func WithConfigMissing(policy MissingConfigPolicy) Modifier {
	return func(op *Options) error {
		if !policy.Valid() {
			return kflags.NewUsageErrorf("Invalid value for --config-missing: %q", policy)
		}
		op.configMissing = policy
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

// FromFlags applies the current CLI configuration to enproxy.
//
// Like every other modifier, order matters: later modifiers may override the
// config source, HTTP starter, metrics starter, or authentication configured
// here. FromFlags is intentionally a thin wrapper around the corresponding
// With* modifiers. It also loads the selected config store binding
// immediately, so apply WithDefaultConfigFile before FromFlags if you want
// embedded fallback to participate in missing-config handling.
func FromFlags(flags *Flags) Modifier {
	return func(op *Options) error {
		mods := Modifiers{
			WithConfigMissing(flags.ConfigMissing),
			WithDisabledNasshAuthentication(flags.DisabledAuthentication),
			WithNasshpMods(nasshp.FromFlags(flags.Nassh)),
			WithHttpFlags(flags.Http),
			WithMetricsFlags(flags.Prometheus),
		}
		if flags.Oauth.AuthURL != "" && !flags.DisabledAuthentication {
			mods = append(mods, WithOauthRedirector(flags.Oauth))
		}
		if err := mods.Apply(op); err != nil {
			return err
		}

		path := strings.TrimSpace(flags.ConfigPath)
		storeFlags := flags.ConfigStore
		if path != "" && storeFlags.Directory.Path == "" {
			flagsClone := *storeFlags
			dirClone := *storeFlags.Directory
			dirClone.Path = "/"
			flagsClone.Directory = &dirClone
			storeFlags = &flagsClone
		}

		workspace, err := factory.NewStore(op.rng, factory.FromFlags(storeFlags))
		if err != nil {
			return err
		}

		parsed := config.ParsedPath{}
		explicit := false
		if path != "" {
			parsed, err = config.ResolvePathNative(workspace, path)
			explicit = true
		} else {
			parsed, err = config.ResolvePathWithinStore(config.StoreRoot{AppName: "enproxy"}, "enproxy")
		}
		if err != nil {
			_ = workspace.Close()
			return err
		}

		store, err := parsed.OpenStore(workspace)
		if err != nil {
			_ = workspace.Close()
			target := defaultConfigTargetLabel
			if path != "" {
				target = path
			}
			return fmt.Errorf("failed to open config namespace for %q: %w", target, err)
		}

		if explicit {
			return WithConfigStore(workspace, store, parsed.Bind(store))(op)
		}
		return WithDefaultConfigStore(workspace, store, parsed.Bind(store))(op)
	}
}

type Enproxy struct {
	rng *rand.Rand

	log logger.Logger

	applyMu sync.Mutex

	handler      *khttp.ReplaceableHandler
	domains      []string
	proxyStarted bool

	whitelist     *utils.ReplaceableWhitelist
	modules       map[string]runtimeModule
	moduleMetrics *moduleMetricsManager

	register prometheus.Registerer
	gatherer prometheus.Gatherer

	configWorkspace config.StoreWorkspace
	configStore     config.Store
	configBinding   config.Binding

	proxy   Starter
	metrics Starter

	pmods                      []httpp.Modifier
	nmods                      []nasshp.Modifier
	defaultNasshRelayHost      string
	authenticate               oauth.Authenticate
	withoutNasshAuthentication bool
}

// New constructs an Enproxy by applying modifiers in order.
//
// Modifier order is part of the API. Callers are expected to choose a coherent
// sequence, and later modifiers override earlier ones.
func New(rng *rand.Rand, mods ...Modifier) (*Enproxy, error) {
	op := &Options{
		rng:           rng,
		log:           &logger.DefaultLogger{Printer: log.Printf},
		proxy:         StarterFromFlags(khttp.DefaultFlags()),
		configMissing: MissingConfigAuto,
	}
	if err := Modifiers(mods).Apply(op); err != nil {
		return nil, err
	}

	ep := &Enproxy{
		rng: rng,
		log: op.log,
		handler: khttp.NewReplaceableHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "configuration not loaded", http.StatusServiceUnavailable)
		})),
		whitelist:                  utils.NewReplaceableWhitelist(),
		modules:                    map[string]runtimeModule{},
		proxy:                      op.proxy,
		metrics:                    op.metrics,
		gatherer:                   op.gatherer,
		register:                   op.register,
		pmods:                      append([]httpp.Modifier{httpp.WithLogging(op.log), httpp.WithAuthenticator(op.authenticate)}, op.pmods...),
		authenticate:               op.authenticate,
		withoutNasshAuthentication: op.withoutNasshAuthentication,
	}
	ep.nmods = append([]nasshp.Modifier{nasshp.WithFilter(ep.whitelist.Allow), nasshp.WithLogging(op.log)}, op.nmods...)
	defaultRelayHost, err := nasshp.RelayHostFromModifiers(rng, ep.nmods...)
	if err != nil {
		return nil, err
	}
	ep.defaultNasshRelayHost = defaultRelayHost
	if op.authenticate == nil && !op.withoutNasshAuthentication {
		op.log.Warnf("ssh gateway disabled as no authentication was configured")
	}
	if op.withoutNasshAuthentication {
		op.log.Errorf("Watch out! The proxy is being started without authentication! SSH tunneling will rely entirely on a filmsy whitelist")
		ep.authenticate = nil
	}

	cleanupConfig := true
	defer func() {
		if cleanupConfig {
			_ = ep.closeConfigStore()
		}
	}()

	if op.metrics != nil {
		if op.gatherer == nil || op.register == nil {
			ep.gatherer = prometheus.DefaultGatherer
			ep.register = prometheus.DefaultRegisterer
		}
	}
	ep.moduleMetrics = newModuleMetricsManager(ep.register)

	ep.configWorkspace = op.configWorkspace
	ep.configStore = op.configStore
	ep.configBinding = op.configBinding

	config := Config{}
	if op.config != nil {
		config = *op.config
	}
	if err := ep.ApplyConfigStruct(config); err != nil {
		return nil, err
	}

	cleanupConfig = false
	return ep, nil
}

func normalizeDomains(domains []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, domain := range domains {
		normalized := normalizeDomain(domain)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func normalizeDomain(domain string) string {
	return utils.NormalizeHost(khttp.LooselyGetHost(domain))
}

const defaultConfigTargetLabel = "enproxy/enproxy"

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
		if route.Mapping.Target.kindCount() > 0 {
			domains = append(domains, route.Mapping.From.Host)
		}
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

func (ep *Enproxy) closeConfigStore() error {
	store := ep.configStore
	workspace := ep.configWorkspace
	ep.configBinding = nil
	ep.configStore = nil
	ep.configWorkspace = nil

	var errs []error
	if store != nil {
		errs = append(errs, store.Close())
	}
	if workspace != nil {
		errs = append(errs, workspace.Close())
	}
	return errors.Join(errs...)
}

// ReloadConfig reloads the active config from the configured store binding.
func (ep *Enproxy) ReloadConfig() error {
	var loaded Config
	err := ep.configBinding.Unmarshal(&loaded)
	if err == nil {
		return ep.ApplyConfigStruct(loaded)
	}
	if os.IsNotExist(err) {
		return fmt.Errorf("configuration does not exist in the configured store")
	}
	return fmt.Errorf("failed to load configuration from the configured store: %w", err)
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

	desired, err := compileDesiredState(builder, ep, config, wl, warns)
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
	bindings := []httpp.Binding{}
	seenBindings := map[string]int{}

	for _, route := range desired.Routes {
		module := modules[route.ModuleID]
		moduleBindings, err := module.BindingsForMapping(route.Mapping)
		if err != nil {
			return fmt.Errorf("error in mapping entry %d - %w", route.Index, err)
		}
		for _, binding := range moduleBindings {
			bkey := binding.Host + "\x00" + binding.Path
			if previous, found := seenBindings[bkey]; found {
				return fmt.Errorf(
					"error in mapping entry %d - duplicate route %q on host %q already defined by mapping entry %d",
					route.Index, binding.Path, binding.Host, previous,
				)
			}
			seenBindings[bkey] = route.Index
			bindings = append(bindings, binding)
		}
	}
	httpp.InstallBindings(mux, ep.log, bindings)

	activations := []moduleActivation{}
	for _, desiredModule := range desired.Modules {
		activation, err := modules[desiredModule.ID()].PrepareActivate()
		if err != nil {
			for i := len(activations) - 1; i >= 0; i-- {
				activations[i].Rollback()
			}
			return err
		}
		activations = append(activations, activation)
	}

	metricsActivation := ep.moduleMetrics.Prepare(modules)
	if err := metricsActivation.Commit(); err != nil {
		for i := len(activations) - 1; i >= 0; i-- {
			activations[i].Rollback()
		}
		return err
	}

	for _, activation := range activations {
		activation.Commit()
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

func (ep *Enproxy) Close() error {
	ep.applyMu.Lock()
	defer ep.applyMu.Unlock()

	var errs []error
	ep.moduleMetrics.Close()
	for _, module := range ep.modules {
		errs = append(errs, module.Close())
	}
	ep.modules = map[string]runtimeModule{}
	errs = append(errs, ep.closeConfigStore())
	return errors.Join(errs...)
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
	return ep.RunProxy()
}
