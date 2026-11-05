package fnosfile

import "context"

// Paths adapts Client to the sysagent FnosPaths interface.
type Paths struct {
	Client *Client
}

// DisplayPath implements tools.FnosPaths.
func (p Paths) DisplayPath(ctx context.Context, path, language string) (string, error) {
	if p.Client == nil || !p.Client.Enabled() {
		return "", ErrDisabled
	}
	return p.Client.DisplayPath(ctx, path, language)
}

// ErrDisabled reports that the fnOS open API token is not available.
var ErrDisabled = errDisabled{}

type errDisabled struct{}

func (errDisabled) Error() string { return "fnos api token is not set" }
