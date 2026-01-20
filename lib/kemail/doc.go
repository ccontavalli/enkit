// Package kemail provides helpers for reliably sending bulk email.
//
// The sender retries both dialing and message delivery, optionally shuffles the
// recipient list, and enforces a minimum wait between connection attempts.
//
// Typical usage:
//
//	flags := kemail.DefaultFlags()
//	flags.Register(kflags.CommandLine, "")
//	// parse flags
//
//	dialer, err := kemail.NewDialer(kemail.FromDialerFlags(dialerFlags))
//	if err != nil {
//		// handle error
//	}
//
//	err = kemail.Send(dialer, recipients, buildMessage, nil, kemail.FromFlags(flags))
//	if err != nil {
//		// handle error
//	}
package kemail
