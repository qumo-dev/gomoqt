package moqt

import (
	"errors"
	"fmt"

	"github.com/qumo-dev/gomoqt/moqt/internal/message"
)

// maxSubscribeRedirects is how many redirects Session.Subscribe follows for
// one subscription, without Config.CheckRedirect, before it gives up, so that
// two publishers redirecting to each other cannot loop.
const maxSubscribeRedirects = 2

var (
	// ErrRedirectNotSupported is returned by TrackWriter.Redirect when the
	// subscriber did not advertise that it follows redirects. The subscription
	// is left as it was, so the publisher may serve it or refuse it instead.
	ErrRedirectNotSupported = errors.New("moqt: peer does not follow redirects")

	// ErrTooManyRedirects is returned by Session.Subscribe, joined with a
	// *RedirectError, when a subscription was redirected more than it follows.
	ErrTooManyRedirects = errors.New("moqt: too many redirects")

	// ErrStopRedirect, returned by Config.CheckRedirect, makes
	// Session.Subscribe return the redirect as a *RedirectError instead of
	// following it.
	ErrStopRedirect = errors.New("moqt: redirect not followed")

	errSubscriptionAnswered = errors.New("moqt: subscription already answered")
	errClientRedirectURI    = errors.New("moqt: only a server may redirect to another URI")
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
