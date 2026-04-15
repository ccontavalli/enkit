package nasshp

import (
	"github.com/ccontavalli/enkit/proxy/utils"
	"github.com/prometheus/client_golang/prometheus"
)

type AllowErrors struct {
	InvalidCookie     utils.Counter
	InvalidHostFormat utils.Counter
	InvalidHostName   utils.Counter
	Unauthorized      utils.Counter
}

type ProxyErrors struct {
	CookieInvalidParameters utils.Counter
	CookieInvalidAuth       utils.Counter

	ProxyInvalidAuth     utils.Counter
	ProxyInvalidHostPort utils.Counter
	ProxyCouldNotEncrypt utils.Counter
	ProxyAllow           AllowErrors

	ConnectInvalidSID utils.Counter
	ConnectInvalidAck utils.Counter
	ConnectInvalidPos utils.Counter
	ConnectAllow      AllowErrors

	SshFailedUpgrade utils.Counter
	SshResumeNoSID   utils.Counter
	SshCreateExists  utils.Counter
	SshDialFailed    utils.Counter
}

type BrowserWindowCounters struct {
	BrowserWindowReset    utils.Counter
	BrowserWindowResumed  utils.Counter
	BrowserWindowStarted  utils.Counter
	BrowserWindowOrphaned utils.Counter
	BrowserWindowStopped  utils.Counter
	BrowserWindowReplaced utils.Counter
	BrowserWindowClosed   utils.Counter
}

type ReadWriterCounters struct {
	BrowserWindowCounters

	BrowserWriterStarted utils.Counter
	BrowserWriterStopped utils.Counter
	BrowserWriterError   utils.Counter

	BrowserReaderStarted utils.Counter
	BrowserReaderStopped utils.Counter
	BrowserReaderError   utils.Counter

	BrowserBytesRead  utils.Counter
	BackendBytesWrite utils.Counter

	BackendBytesRead  utils.Counter
	BrowserBytesWrite utils.Counter
}

type ExpireCounters struct {
	ExpireRuns     utils.Counter
	ExpireDuration utils.Counter

	ExpireAboveOrphanThresholdRuns  utils.Counter
	ExpireAboveOrphanThresholdTotal utils.Counter
	ExpireAboveOrphanThresholdFound utils.Counter

	ExpireRaced utils.Counter

	ExpireOrphanClosed   utils.Counter
	ExpireRuthlessClosed utils.Counter
	ExpireLifetimeTotal  utils.Counter

	ExpireYoungest utils.Counter
}

type ProxyCounters struct {
	ReadWriterCounters

	SshProxyStarted utils.Counter
	SshProxyStopped utils.Counter
}

type SessionCounters struct {
	Resumed utils.Counter
	Invalid utils.Counter
	Created utils.Counter

	Orphaned utils.Counter
	Deleted  utils.Counter
}

type MetricsCollector struct {
	collector prometheus.Collector
}

func NewMetricsCollector(proxy *NasshProxy) *MetricsCollector {
	var metrics utils.CounterMetrics
	if proxy != nil {
		proxy.RegisterMetrics(&metrics)
	}
	return &MetricsCollector{collector: metrics.Collector()}
}

func (mc *MetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	collector := mc.collector
	if collector != nil {
		collector.Describe(ch)
	}
}

func (mc *MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	collector := mc.collector
	if collector != nil {
		collector.Collect(ch)
	}
}

func (np *NasshProxy) RegisterMetrics(metrics utils.MetricRegistry) {
	errors := &np.errors
	counters := &np.counters
	sessions := &np.sessions
	expires := &np.expires
	const helpError = "Number of times the request to the url resulted in the specified error"
	const helpBrowser = "Number of times a goroutine of the specified type was started/stopped/errored out"

	metrics.Counter(prometheus.NewDesc("nasshp_pool_gets", "Number of buffers retrieved from the pool", nil, nil), &np.pool.gets)
	metrics.Counter(prometheus.NewDesc("nasshp_pool_puts", "Number of buffers returned to the pool", nil, nil), &np.pool.puts)
	metrics.Counter(prometheus.NewDesc("nasshp_pool_news", "Number of buffers created for the pool", nil, nil), &np.pool.news)

	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/cookie", "error": "invalid parameters", "type": "bad client"}), &errors.CookieInvalidParameters)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/cookie", "error": "invalid auth", "type": "unauthorized"}), &errors.CookieInvalidAuth)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/proxy", "error": "invalid auth", "type": "unauthorized"}), &errors.ProxyInvalidAuth)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/proxy", "error": "invalid host/port", "type": "bad client"}), &errors.ProxyInvalidHostPort)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/proxy", "error": "could not encrypt", "type": "internal"}), &errors.ProxyCouldNotEncrypt)

	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/proxy", "error": "invalid cookie", "type": "auth"}), &errors.ProxyAllow.InvalidCookie)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/proxy", "error": "invalid host split", "type": "bad client"}), &errors.ProxyAllow.InvalidHostFormat)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/proxy", "error": "invalid host name", "type": "dns"}), &errors.ProxyAllow.InvalidHostName)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/proxy", "error": "unauthorized user", "type": "auth"}), &errors.ProxyAllow.Unauthorized)

	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/connect", "error": "invalid sid", "type": "bad client"}), &errors.ConnectInvalidSID)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/connect", "error": "invalid ack", "type": "bad client"}), &errors.ConnectInvalidAck)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/connect", "error": "invalid pos", "type": "bad client"}), &errors.ConnectInvalidPos)

	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/connect", "error": "invalid cookie", "type": "auth"}), &errors.ConnectAllow.InvalidCookie)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/connect", "error": "invalid host split", "type": "bad client"}), &errors.ConnectAllow.InvalidHostFormat)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/connect", "error": "invalid host name", "type": "dns"}), &errors.ConnectAllow.InvalidHostName)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/connect", "error": "unauthorized user", "type": "auth"}), &errors.ConnectAllow.Unauthorized)

	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/connect", "error": "failed upgrade", "type": "bad client"}), &errors.SshFailedUpgrade)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/connect", "error": "failed resume", "type": "bad client"}), &errors.SshResumeNoSID)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/connect", "error": "create existing", "type": "bad client"}), &errors.SshCreateExists)
	metrics.Counter(prometheus.NewDesc("nasshp_url_errors", helpError, nil, prometheus.Labels{"url": "/connect", "error": "dial failed", "type": "endpoint"}), &errors.SshDialFailed)

	metrics.Counter(prometheus.NewDesc("nasshp_browser", helpBrowser, nil, prometheus.Labels{"type": "writer", "action": "started"}), &counters.BrowserWriterStarted)
	metrics.Counter(prometheus.NewDesc("nasshp_browser", helpBrowser, nil, prometheus.Labels{"type": "writer", "action": "stopped"}), &counters.BrowserWriterStopped)
	metrics.Counter(prometheus.NewDesc("nasshp_browser", helpBrowser, nil, prometheus.Labels{"type": "writer", "action": "error"}), &counters.BrowserWriterError)

	metrics.Counter(prometheus.NewDesc("nasshp_browser", helpBrowser, nil, prometheus.Labels{"type": "reader", "action": "started"}), &counters.BrowserReaderStarted)
	metrics.Counter(prometheus.NewDesc("nasshp_browser", helpBrowser, nil, prometheus.Labels{"type": "reader", "action": "stopped"}), &counters.BrowserReaderStopped)
	metrics.Counter(prometheus.NewDesc("nasshp_browser", helpBrowser, nil, prometheus.Labels{"type": "reader", "action": "error"}), &counters.BrowserReaderError)

	metrics.Counter(prometheus.NewDesc("nasshp_browser_read", "Total amount of bytes read from the browser (this includes rack/wack)", nil, nil), &counters.BrowserBytesRead)
	metrics.Counter(prometheus.NewDesc("nasshp_browser_write", "Total amount of bytes written to the browser (this includes rack/wack)", nil, nil), &counters.BrowserBytesWrite)
	metrics.Counter(prometheus.NewDesc("nasshp_backend_write", "Total amount of bytes written to the backend", nil, nil), &counters.BackendBytesWrite)
	metrics.Counter(prometheus.NewDesc("nasshp_backend_read", "Total amount of bytes read from the backend", nil, nil), &counters.BackendBytesRead)

	metrics.Counter(prometheus.NewDesc("nasshp_browser", helpBrowser, nil, prometheus.Labels{"type": "proxy", "action": "started"}), &counters.SshProxyStarted)
	metrics.Counter(prometheus.NewDesc("nasshp_browser", helpBrowser, nil, prometheus.Labels{"type": "proxy", "action": "stopped"}), &counters.SshProxyStopped)

	metrics.Counter(prometheus.NewDesc("nasshp_sessions_resumed", "Number of times SIDs were found in the sessions table already", nil, nil), &sessions.Resumed)
	metrics.Counter(prometheus.NewDesc("nasshp_sessions_invalid", "Number of times the state of a SID was found, but invalid - file a BUG!", nil, nil), &sessions.Invalid)
	metrics.Counter(prometheus.NewDesc("nasshp_sessions_created", "Number of times SIDs were not found in the session table, causing a new session to be created", nil, nil), &sessions.Created)
	metrics.Counter(prometheus.NewDesc("nasshp_sessions_orphaned", "Number of times SIDs were left in the session table for the browser to reconnect", nil, nil), &sessions.Orphaned)
	metrics.Counter(prometheus.NewDesc("nasshp_sessions_deleted", "Number of times SIDs were deleted from the session table as the connection terminated", nil, nil), &sessions.Deleted)

	metrics.Counter(prometheus.NewDesc("nasshp_window_reset", "Number of times a browser window rack/wack was reset to 0 (should be rare)", nil, nil), &counters.BrowserWindowReset)
	metrics.Counter(prometheus.NewDesc("nasshp_window_resume", "Number of times a browser window was reused with rack/wack recovery", nil, nil), &counters.BrowserWindowResumed)
	metrics.Counter(prometheus.NewDesc("nasshp_window_started", "Number of times a browser window was newly started, with 0 rack/wack", nil, nil), &counters.BrowserWindowStarted)
	metrics.Counter(prometheus.NewDesc("nasshp_window_orphaned", "Number of times a browser error resulted in orphaning a window", nil, nil), &counters.BrowserWindowOrphaned)
	metrics.Counter(prometheus.NewDesc("nasshp_window_stopped", "Number of times an application asked to close the window (generally, a backend error)", nil, nil), &counters.BrowserWindowStopped)
	metrics.Counter(prometheus.NewDesc("nasshp_window_replaced", "Number of times an active browser window was replaced by another (should be rare)", nil, nil), &counters.BrowserWindowReplaced)
	metrics.Counter(prometheus.NewDesc("nasshp_window_closed", "Number of times an active browser window had to be closed (every time a stop is asked, if the window was still active)", nil, nil), &counters.BrowserWindowClosed)

	metrics.Counter(prometheus.NewDesc("nasshp_expire_runs", "Number of times the expiration goroutine was run", nil, nil), &expires.ExpireRuns)
	metrics.Counter(prometheus.NewDesc("nasshp_expire_durations", "Total time spent to implement session expirations (nanoseconds)", nil, nil), &expires.ExpireDuration)
	metrics.Counter(prometheus.NewDesc("nasshp_expire_above_orphan_threshold_runs", "Number of times the expiration goroutine found sessions above the orphan threshold", nil, nil), &expires.ExpireAboveOrphanThresholdRuns)
	metrics.Counter(prometheus.NewDesc("nasshp_expire_above_orphan_threshold_total", "Total number of sessions found across all runs when expire was run", nil, nil), &expires.ExpireAboveOrphanThresholdTotal)
	metrics.Counter(prometheus.NewDesc("nasshp_expire_above_orphan_threshold_found", "Total number of orphaned sessions found across all runs", nil, nil), &expires.ExpireAboveOrphanThresholdFound)
	metrics.Counter(prometheus.NewDesc("nasshp_expire_raced", "Total number of times an orphaned session became unorphaned while expire was in progress", nil, nil), &expires.ExpireRaced)
	metrics.Counter(prometheus.NewDesc("nasshp_expire_orphan_closed", "Number of sessions the expire goroutine gently closed", nil, nil), &expires.ExpireOrphanClosed)
	metrics.Counter(prometheus.NewDesc("nasshp_expire_ruthless_closed", "Number of sessions the expire goroutine had to close ruthlessly", nil, nil), &expires.ExpireRuthlessClosed)
	metrics.Counter(prometheus.NewDesc("nasshp_expire_lifetime_total", "Total number of seconds expired sessions were orphaned for", nil, nil), &expires.ExpireLifetimeTotal)
	metrics.Counter(prometheus.NewDesc("nasshp_expire_youngest", "Epoch in nanoseconds of the most recent session expired", nil, nil), &expires.ExpireYoungest)
}
