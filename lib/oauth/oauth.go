package oauth

// Simplifies the use of oauth2 with a net.http handler and specific oauth providers.
//
// # Introduction
//
// To use the library, you have to:
//
//   1) Create a new authentication object of your taste, using one of
//      New(), NewExtractor(), NewRedirector(), NewMultiOauth(), ...
//   2) Set up two urls in your http mux: one to start the login process (with LoginHandler,
//      PerformLogin, MakeLoginHandler), and one to end the login process, and finally
//      authenticate the user (with AuthHandler, PerformAuth, MakeAuthHandler), and Complete().
//
// In your other http handlers, you can then use WithCredentialsOrRedirect,
// WithCredentialsOrError or WithCredentials as a moral equivalent of a middleware to
// have the credentials of the user accessible from the context of your http handler with
// GetCredentials.
//
// Wrappers for grpc or the kassets library (Mapper) are also provided.
//
// # Examples
//
// Simple setup/example:
//
//     authenticator, err := New(..., WithSecrets(...), WithTargetURL("https://localhos:5433/auth"), ogoogle.Defaults())
//
//     [...]
//
//     http.HandleFunc("/auth", authenticator.AuthHandler())     // /auth will be the endpoint for oauth, store the cookie.
//     http.HandleFunc("/login", authenticator.LoginHandler())   // visiting /login will redirect to the oauth provider.
//
// More complex setup:
//
//     authenticator, err := New(..., WithSecrets(...), WithTargetURL("https://localhos:5433/auth"), ogoogle.Defaults())
//
//     [...]
//
//     http.HandleFunc("/", authenticator.MakeAuthHandler(authenticator.MakeLoginHandler(rootHandler, "")))
//
// Request authentication:
//
//    http.HandleFunc("/", authenticator.WithCredentials(rootHandler))
//
// or:
//
//    http.HandleFunc("/", authenticator.WithCredentialsOrRedirect(rootHandler, "/login"))
//
// From within your handler, you can use:
//
//    [...]
//    credentials := oauth.GetCredentials(r.Context())
//    if credentials == nil {
//        http.Error(w, "not authenticated", http.StatusInternalServerError)
//    } else {
//        log.Printf("email: %s", credentials.Identity.Email)
//    }
//
//
// # Authentication mechanisms
//
// The library currently allows to obtain and interact with authentication
// cookies by either using an [Extractor], [Redirector], or a full fledger [Authenticator].
//
// An [Extractor] is generally used on non-interactive backends (eg, a gRPC
// or JSON endpoint) that expects to receive an authentication cookie, and
// simply returns an error if no such cookie is found, or if the cookie is
// invalid from a cryptographic standpoint.
//
// A [Redirector] is an [Extractor] that is capable of generating http
// redirects and receiving the result of those redirects to complete
// the authentication process. This is commonly used if - for example -
// you deploy an authentication server in your environment which is
// the only configured endpoint for oauth2, but use additional web
// backends or microservices that need to support logging in your users.
// This is very typical of corp environments, where it is impractical
// to configure each internal web service or endpoint as an oauth target.
//
// An [Authenticator] is a full fledged oauth server: it redirects the user
// to the IdP of your choice to complete authentication after setting up some
// encrypted cookies and tokens, verifies the information received at the end
// of authentication, and then generates an enkit/lib/oauth specific cookie
// that [Redirector]s or [Extractor]s can process.
//
// Both [Redirector] and [Authenticator] implement the [IAuthenticator]
// interface. A backend should typically use the [IAuthenticator] interface
// and set it up via flags (or config structs) so the backend can either
// directly perform oAuth with google/github/... or rely on an authentication
// server using lib/oauth in your network.
