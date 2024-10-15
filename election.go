package leaderelection

import (
	"context"
	"time"
)

// Initializes leader election runtime
func (ler *LeaderElectionRuntime) Init(cfg LeaderElectionConfig) {
	ler.state = Bootstrap
	ler.LeaderElectionConfig = cfg

	ler.relinquishIntvl = cfg.RelinquishInterval
	if ler.relinquishIntvl == 0 {
		ler.relinquishIntvl = DefaultRelinquishInterval
	}
}

// Returns true if current state is Bootstrap
func (ler *LeaderElectionRuntime) IsBootstrap() bool {
	return ler.GetState() == Bootstrap
}

// Returns true if current state is Leader
func (ler *LeaderElectionRuntime) IsLeader() bool {
	return ler.GetState() == Leader
}

// Returns true if current state is Follower
func (ler *LeaderElectionRuntime) IsFollower() bool {
	return ler.GetState() == Follower
}

// Return current state
func (ler *LeaderElectionRuntime) GetState() State {
	return ler.state
}

func (ler *LeaderElectionRuntime) setState(state State) {
	if !ler.IsLeader() && state == Leader {
		ler.leaderAt = time.Now()
	}
	ler.state = state
}

func (ler *LeaderElectionRuntime) shouldRelinquish() bool {
	if ler.IsLeader() {
		return time.Since(ler.leaderAt) >= ler.relinquishIntvl
	}

	return false
}

func (ler *LeaderElectionRuntime) acquireLeadershipWrapper(ctx context.Context) error {
	isLeader, err := ler.Elector.AcquireLeadership(ctx)
	if err != nil {
		return err
	}

	if isLeader {
		ler.setState(Leader)
		return ler.LeaderCallback(ctx)
	}

	ler.setState(Follower)
	return ler.FollowerCallback(ctx)
}

func (ler *LeaderElectionRuntime) checkLeadershipWrapper(ctx context.Context) error {
	if ler.shouldRelinquish() {
		status, err := ler.Elector.RelinquishLeadership(ctx)
		if err != nil {
			return err
		}

		// We are a follower if status is true
		if status {
			ler.setState(Follower)
			return ler.FollowerCallback(ctx)
		}

		// If status == false, we will leave the current state as is.
	}

	isLeader, err := ler.Elector.CheckLeadership(ctx)
	if err != nil {
		return err
	}

	if isLeader {
		ler.setState(Leader)
		return ler.LeaderCallback(ctx)
	}

	ler.setState(Follower)
	return ler.FollowerCallback(ctx)
}

// Run the election. Will run until passed context is canceled or times out or deadline is exceeded.
func (ler *LeaderElectionRuntime) Run(ctx context.Context) error {
	if !ler.IsBootstrap() {
		return ErrInvalidState
	}

	for {
		loopTs := time.Now()

		switch ler.GetState() {
		case Bootstrap, Follower:
			err := ler.acquireLeadershipWrapper(ctx)
			if err != nil {
				return err
			}

		case Leader:
			err := ler.checkLeadershipWrapper(ctx)
			if err != nil {
				return err
			}
		}

		intvl := ler.FollowerCheckInterval
		if ler.IsLeader() {
			intvl = ler.LeaderCheckInterval
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
