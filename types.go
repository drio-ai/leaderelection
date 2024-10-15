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

const (
	DefaultRelinquishInterval time.Duration = 300 * time.Second
)

var (
	ErrInvalidState = errors.New("invalid election state")
)

type LeaderElection interface {
	// Called by a follower or bootstrap to acquire leadership. A leader calling this will be a no-op
	// bool return value must be true if leadership acquisition was successful and false otherwise.
	AcquireLeadership(context.Context) (bool, error)

	// Called by the leader to make sure they are still the leader. Will return an error if not leader.
	// bool return value must be true if we are still the leader and false otherwise.
	CheckLeadership(context.Context) (bool, error)

	// Called by the leader to relinquish leadership. Will return an error if not leader.
	// bool return value must be true if relinquish attempt was successful and false otherwise.
	RelinquishLeadership(context.Context) (bool, error)

	// Run the election. Will run until passed context is canceled or times out or deadline exceeds
	Run(context.Context) error
}

type Callback func(context.Context) error

type LeaderElectionConfig struct {
	// Lock identifier
	LockId uint

	// Duration after which the leader will relinquish
	RelinquishInterval time.Duration

	// How often will leader check to make sure they are still the leader
	LeaderCheckInterval time.Duration

	// How often a follower will check to see if they can take over as a leader
	FollowerCheckInterval time.Duration

	// Callback once leadership has been acquired or staying on as a leader
	LeaderCallback Callback

	// Callback once switch to a follower or staying on as a follower
	FollowerCallback Callback
}

type LeaderElectionRuntime struct {
	state           State
	relinquishIntvl time.Duration
	leaderAt        time.Time
	Elector         LeaderElection

	LeaderElectionConfig
}
