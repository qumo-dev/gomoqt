package moqt

import (
	"errors"
	"fmt"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
)

const (
	// defaultMaxRedirects is how many redirects in a row Session.Subscribe
	// follows for one subscription when no CheckRedirect is set.
	defaultMaxRedirects = 2
	// maxRedirects bounds the redirects followed for one subscription even
	// when CheckRedirect allows more, so that publishers redirecting to each
	// other cannot loop.
	maxRedirects = 10
)

// CheckRedirectFunc decides whether Session.Subscribe follows a redirect to
// another track on the same session, as http.Client.CheckRedirect does for
// HTTP. to is the next redirect; via holds the redirects already followed for
// this subscription, oldest first, as a copy. Returning nil follows it.
// Returning ErrStopRedirect makes Subscribe return a *RedirectError for to
// without following it; any other error is returned joined with one. A
// redirect to another session is never followed and does not reach it.
//
// Set it on Dialer, Server, WebTransportHandler or WebSocketHandler. If nil,
// Subscribe follows two redirects in a row and returns ErrTooManyRedirects on
// the third. Whatever it returns, Subscribe stops after ten.
type CheckRedirectFunc func(to Redirect, via []Redirect) error

var (
	// ErrRedirectNotSupported is returned by TrackWriter.Redirect when the
	// subscriber did not advertise that it follows redirects. The subscription
	// is left as it was, so the publisher may serve it or refuse it instead.
	ErrRedirectNotSupported = errors.New("moqt: peer does not follow redirects")

	// ErrTooManyRedirects is returned by Session.Subscribe, joined with a
	// *RedirectError, when a subscription was redirected more than it follows.
	ErrTooManyRedirects = errors.New("moqt: too many redirects")

	// ErrStopRedirect, returned by a CheckRedirectFunc, makes
	// Session.Subscribe return the redirect as a *RedirectError instead of
	// following it.
	ErrStopRedirect = errors.New("moqt: redirect not followed")

	// ErrClientRedirectURI is returned by TrackWriter.Redirect on a client for
	// a redirect to another session, which only a server may send, and by a
	// server's Session.Subscribe that received one.
	ErrClientRedirectURI = errors.New("moqt: only a server may redirect to another URI")

	errSubscriptionAnswered = errors.New("moqt: subscription already answered")
	errTrackWriterStopped   = errors.New("moqt: track writer closed or redirected")
)

// Redirect is where a publisher sends a subscriber instead of serving it: to
// another track, on this session or on another one.
type Redirect struct {
	// URI is the session to subscribe on, such as "moqt://relay-b.example/".
	// Empty means the current session. Only a server may set it.
	URI string

	// BroadcastPath and TrackName name the track to subscribe to. An empty
	// TrackName in TrackWriter.Redirect keeps the subscriber's track name, so
	// a whole broadcast can be redirected with one rule.
	BroadcastPath BroadcastPath
	TrackName     TrackName
}

// RedirectError is returned by Session.Subscribe when the publisher redirected
// the subscription somewhere the session does not go by itself: to another
// session, which the caller dials, or past the redirects it follows.
type RedirectError struct {
	Redirect Redirect
}

// Error describes where the subscription was redirected.
func (err *RedirectError) Error() string {
	where := string(err.Redirect.BroadcastPath) + " " + string(err.Redirect.TrackName)
	if err.Redirect.URI != "" {
		where = err.Redirect.URI + " " + where
	}
	return "moqt: subscription redirected to " + where
}

func (to Redirect) message() message.SubscribeRedirectMessage {
	return message.SubscribeRedirectMessage{
		ConnectURI:    to.URI,
		BroadcastPath: string(to.BroadcastPath),
		TrackName:     string(to.TrackName),
	}
}

func redirectFromMessage(m *message.SubscribeRedirectMessage) Redirect {
	return Redirect{
		URI:           m.ConnectURI,
		BroadcastPath: BroadcastPath(m.BroadcastPath),
		TrackName:     TrackName(m.TrackName),
	}
}

func (to Redirect) validate() error {
	if !isValidPath(to.BroadcastPath) {
		return fmt.Errorf("moqt: invalid redirect path: %q", to.BroadcastPath)
	}
	return nil
}
