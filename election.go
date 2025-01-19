package leaderelection

import (
	"context"
	"time"

	"github.com/robfig/cron/v3"
)

// Initializes leader election
func (le *LeaderElection) Init(cfg LeaderElectionConfig) {
	le.state = Bootstrap
	le.LeaderElectionConfig = cfg

	le.relinquishIntvl = cfg.RelinquishInterval
	if le.relinquishIntvl == 0 {
		le.relinquishIntvl = DefaultRelinquishInterval
	}

	// If Fails is 0, set it to default
	if le.Fails == 0 {
		le.Fails = DefaultFailsCount
	}

	le.initRelinquishJob()
}

// Stop leader election
func (le *LeaderElection) Close() {
	le.closeRelinquishJob()
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

func (le *LeaderElection) initRelinquishJob() {
	if len(le.RelinquishIntervalSpec) > 0 {
		le.csCh = make(chan bool, 1)
		le.cS = cron.New()
		le.cS.Start()
	}
}

func (le *LeaderElection) closeRelinquishJob() {
	if len(le.RelinquishIntervalSpec) > 0 {
		ctx := le.cS.Stop()
		<-ctx.Done()
	}
}

func (le *LeaderElection) startRelinquishJob() error {
	if len(le.RelinquishIntervalSpec) > 0 {
		id, err := le.cS.AddFunc(le.RelinquishIntervalSpec, func() { le.csCh <- true })
		if err != nil {
			return err
		}
		le.relinquishJobId = id
	}

	return nil
}

func (le *LeaderElection) stopRelinquishJob() error {
	if len(le.RelinquishIntervalSpec) > 0 {
		if le.relinquishJobId != 0 {
			le.cS.Remove(le.relinquishJobId)
			le.relinquishJobId = 0
		}
	}

	return nil
}

func (le *LeaderElection) shouldRelinquish() bool {
	if le.IsLeader() {
		if len(le.RelinquishIntervalSpec) > 0 {
			select {
			case relinquish := <-le.csCh:
				return relinquish
			default:
			}
		} else {
			return time.Since(le.leaderAt) >= le.relinquishIntvl
		}
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
		err := le.startRelinquishJob()
		if err != nil {
			return err
		}

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
			err := le.stopRelinquishJob()
			if err != nil {
				return err
			}

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
	err = le.stopRelinquishJob()
	if err != nil {
		return err
	}
	return le.FollowerCallback(ctx)
}

// Run the election. Will run until passed context is canceled or times out or deadline is exceeded.
func (le *LeaderElection) Run(ctx context.Context) error {
	if !le.IsBootstrap() {
		return ErrInvalidState
	}

	var err error
	for {
		loopTs := time.Now()

		switch le.GetState() {
		case Bootstrap, Follower:
			err = le.acquireLeadershipWrapper(ctx)

		case Leader:
			err = le.checkLeadershipWrapper(ctx)
		}

		// Check to see if we are within the threshold in case of an error
		// We will wait for successive failures before giving up. This is
		// susceptible to the case where there are repeated intermittent
		// failures that causes some requests to pass and other fail.
		// TODO: Check for failures within a time interval.
		if err != nil {
			le.fails++
			if le.fails >= le.Fails {
				return err
			}
		} else {
			// Reset fails count
			le.fails = 0
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
