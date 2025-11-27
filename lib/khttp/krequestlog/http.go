package krequestlog

import (
	"net/http"
	"time"

	"github.com/ccontavalli/enkit/lib/khttp"
)

// NewHandler returns a new http.Handler that logs requests.
func NewHandler(next http.Handler, mods ...Modifier) http.Handler {
	opts := NewOptions(mods...)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		path := r.URL.Path
		method := r.Method
		origin := khttp.ClientOrigin(r)

		if opts.LogStart {
			opts.Printer("HTTP START origin=%s method=%s path=%s origin=%s", origin, method, path)
		}

		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)

		if opts.LogEnd {
			duration := time.Since(start)
			status := sw.status
			if status == 0 {
				status = 200
			}

			if opts.LogFormat == "apache" {
				// minimal apache combined style
				opts.Printer("%s - - [%s] \"%s %s %s\" %d %d \"%s\" \"%s\" %v",
					origin,
					start.Format("02/Jan/2006:15:04:05 -0700"),
					method, r.URL.RequestURI(), r.Proto,
					status, sw.length,
					r.Referer(), r.UserAgent(),
					duration,
				)
			} else {
				opts.Printer("HTTP END origin=%s method=%s path=%s status=%d size=%d duration=%v", origin, method, path, status, sw.length, duration)
			}
		}
	})
}
