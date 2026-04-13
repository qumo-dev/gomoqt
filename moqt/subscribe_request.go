package moqt

import (
	"fmt"
)

// SubscribeRequest represents parameters for one subscribe operation.
type SubscribeRequest struct {
	// BroadcastPath is the path of the broadcast to subscribe to.
	// It must be a non-empty string and follow the path format defined in the spec.
	BroadcastPath BroadcastPath

	// TrackName is the name of the track to subscribe to.
	// It can be an empty string if the track name is not specified.
	TrackName TrackName

	// Config holds wire-level subscribe parameters.
	// If nil, a zero-value config is used.
	Config *SubscribeConfig
}

// NewSubscribeRequest returns a subscribe request initialized with the given values.
func NewSubscribeRequest(path BroadcastPath, name TrackName, config *SubscribeConfig) (*SubscribeRequest, error) {
	if !isValidPath(path) {
		return nil, fmt.Errorf("invalid broadcast path: %q", path)
	}

	req := &SubscribeRequest{
		BroadcastPath: path,
		TrackName:     name,
		Config:        config,
	}
	return req.normalized(), nil
}

func (r *SubscribeRequest) Clone() *SubscribeRequest {
	if r == nil {
		return nil
	}
	clone := *r
	return &clone
}

func (r *SubscribeRequest) normalized() *SubscribeRequest {
	if r == nil {
		return nil
	}

	config := r.Config
	if config == nil {
		config = &SubscribeConfig{}
	}

	return &SubscribeRequest{
		BroadcastPath: r.BroadcastPath,
		TrackName:     r.TrackName,
		Config:        config,
	}
}
