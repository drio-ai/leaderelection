package leaderelection

import (
	"context"
	"errors"
	"time"
)

type State string

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
	LeaderCheckInterval   time.Duration
	FollowerCheckInterval time.Duration

	LeaderCallback   Callback
	FollowerCallback Callback
}

type LeaderElection interface {
	Run(context.Context) error
	RelinquishLeadership(context.Context) error
}
