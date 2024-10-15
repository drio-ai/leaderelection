package leaderelection

import (
	"context"
	"time"
)

// Initializes leader election runtime
func (le *LeaderElection) Init(cfg LeaderElectionConfig) {
	le.state = Bootstrap
	le.LeaderElectionConfig = cfg

	le.relinquishIntvl = cfg.RelinquishInterval
	if le.relinquishIntvl == 0 {
		le.relinquishIntvl = DefaultRelinquishInterval
	}
}

// Returns true if current state is Bootstrap
func (le *LeaderElection) IsBootstrap() bool {
	return le.GetState() == Bootstrap
}

// Returns true if current state is Leader
func (le *LeaderElection) IsLeader() bool {
	return le.GetState() == Leader
}

// Returns true if current state is Follower
func (le *LeaderElection) IsFollower() bool {
	return le.GetState() == Follower
}

// Return current state
func (le *LeaderElection) GetState() State {
	return le.state
}

func (le *LeaderElection) setState(state State) {
	if !le.IsLeader() && state == Leader {
		le.leaderAt = time.Now()
	}
	le.state = state
}

func (le *LeaderElection) shouldRelinquish() bool {
	if le.IsLeader() {
		return time.Since(le.leaderAt) >= le.relinquishIntvl
	}

	return false
}

func (le *LeaderElection) acquireLeadershipWrapper(ctx context.Context) error {
	isLeader, err := le.Elector.AcquireLeadership(ctx)
	if err != nil {
		return err
	}

	if isLeader {
		le.setState(Leader)
		return le.LeaderCallback(ctx)
	}

	le.setState(Follower)
	return le.FollowerCallback(ctx)
}

func (le *LeaderElection) checkLeadershipWrapper(ctx context.Context) error {
	if le.shouldRelinquish() {
		status, err := le.Elector.RelinquishLeadership(ctx)
		if err != nil {
			return err
		}

		// We are a follower if status is true
		if status {
			le.setState(Follower)
			return le.FollowerCallback(ctx)
		}

		// If status == false, we will leave the current state as is.
	}

	isLeader, err := le.Elector.CheckLeadership(ctx)
	if err != nil {
		return err
	}

	if isLeader {
		le.setState(Leader)
		return le.LeaderCallback(ctx)
	}

	le.setState(Follower)
	return le.FollowerCallback(ctx)
}

// Run the election. Will run until passed context is canceled or times out or deadline is exceeded.
func (le *LeaderElection) Run(ctx context.Context) error {
	if !le.IsBootstrap() {
		return ErrInvalidState
	}

	for {
		loopTs := time.Now()

		switch le.GetState() {
		case Bootstrap, Follower:
			err := le.acquireLeadershipWrapper(ctx)
			if err != nil {
				return err
			}

		case Leader:
			err := le.checkLeadershipWrapper(ctx)
			if err != nil {
				return err
			}
		}

		intvl := le.FollowerCheckInterval
		if le.IsLeader() {
			intvl = le.LeaderCheckInterval
		}

		intvl -= time.Since(loopTs)
		if intvl < 0 {
			intvl = 0
		}

		select {
		case <-ctx.Done():
			return context.Canceled

		case <-time.After(intvl):
		}
	}
}
