package leaderelection

import (
	"context"
	"errors"
	"time"
)

type State string

// State
const (
	Bootstrap State = "bootstrap"
	Follower  State = "follower"
	Leader    State = "leader"
)

var (
	ErrInvalidState = errors.New("invalid election state")
)

type Callback func(context.Context) error

type LeaderElectionConfig struct {
	// How often will leader check to make sure they are still the leader
	LeaderCheckInterval time.Duration

	// How often a follower will check to see if they can take over as a leader
	FollowerCheckInterval time.Duration

	// Callback once leadership has been acquired or staying on as a leader
	LeaderCallback Callback

	// Callback once switch to a follower or staying on as a follower
	FollowerCallback Callback
}

type LeaderElection interface {
	// Run will start the election and should typically be called inside a goroutine.
	Run(context.Context) error

	// Called by the leader. A follower calling this function will be returned an error.
	RelinquishLeadership(context.Context) error
}
