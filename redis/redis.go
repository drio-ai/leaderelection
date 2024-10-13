package redis

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
	"github.com/redis/go-redis/v9"

	leaderelection "github.com/drio-ai/leaderelection"
)

// Redis client parameters and lock id
type RedisLeaderElectionConfig struct {
	Host               string
	Port               uint16
	Secure             bool
	InsecureSkipVerify bool
	Password           string
	LockId             uint

	leaderelection.LeaderElectionConfig
}

type RedisLeaderElection struct {
	cfg         RedisLeaderElectionConfig
	redisClient *redis.Client
	mutex       *redsync.Mutex
	state       leaderelection.State
	ts          time.Time
}

func setupRedisClient(_ context.Context, cfg RedisLeaderElectionConfig) (*redis.Client, error) {
	opts := &redis.Options{
		Addr:     cfg.Host + ":" + fmt.Sprint(cfg.Port),
		Password: cfg.Password,
	}

	if cfg.Secure {
		opts.TLSConfig = &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify}
	}

	return redis.NewClient(opts), nil
}

// Initialize leader election with redis parameters and lock id
func New(ctx context.Context, cfg RedisLeaderElectionConfig) (leaderelection.LeaderElection, error) {
	client, err := setupRedisClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return NewWithConn(ctx, client, cfg)
}

// Initialize leader election with redis client and lock id.
// Caller must have connected to redis and gives us a client to work with.
// Redis parameters can be skipped in this case.
// Please note, this Redis client will be dedicated for leader election only.
func NewWithConn(ctx context.Context, client *redis.Client, cfg RedisLeaderElectionConfig) (leaderelection.LeaderElection, error) {
	intvl := cfg.RelinquishInterval
	if intvl == 0 {
		intvl = leaderelection.DefaultRelinquishInterval
	}

	return &RedisLeaderElection{
		redisClient: client,
		mutex:       redsync.New(goredis.NewPool(client)).NewMutex(fmt.Sprint(cfg.LockId), redsync.WithExpiry(intvl)),
		cfg:         cfg,
		state:       leaderelection.Bootstrap,
	}, nil
}

func (rle *RedisLeaderElection) setState(state leaderelection.State) {
	rle.state = state
	rle.ts = time.Now()
}

func (rle *RedisLeaderElection) acquireLeadership(ctx context.Context) error {
	if rle.state == leaderelection.Leader {
		return nil
	}

	err := rle.mutex.TryLockContext(ctx)
	if err != nil {
		if _, ok := err.(*redsync.ErrTaken); !ok {
			return err
		}
	}

	if err == nil {
		rle.setState(leaderelection.Leader)
		return rle.cfg.LeaderCallback(ctx)
	}

	rle.setState(leaderelection.Follower)
	return rle.cfg.FollowerCallback(ctx)
}

// Relinquish leadership. A follower calling this function will be returned an error.
func (rle *RedisLeaderElection) RelinquishLeadership(ctx context.Context) error {
	if rle.state != leaderelection.Leader {
		return leaderelection.ErrInvalidState
	}

	unlocked, err := rle.mutex.UnlockContext(ctx)
	if err != nil {
		return err
	}

	if !unlocked {
		rle.setState(leaderelection.Leader)
		return rle.cfg.LeaderCallback(ctx)
	}

	rle.setState(leaderelection.Follower)
	return rle.cfg.FollowerCallback(ctx)
}

func (rle *RedisLeaderElection) checkLeadership(ctx context.Context) error {
	if rle.state != leaderelection.Leader {
		return leaderelection.ErrInvalidState
	}

	lockUntil := rle.mutex.Until()
	if lockUntil.IsZero() || time.Now().After(lockUntil) {
		rle.setState(leaderelection.Follower)
		return rle.cfg.FollowerCallback(ctx)
	}

	rle.setState(leaderelection.Leader)
	return rle.cfg.LeaderCallback(ctx)
}

// Run the election. Will run until passed context is canceled or times out or deadline is exceeded.
func (rle *RedisLeaderElection) Run(ctx context.Context) error {
	if rle.state != leaderelection.Bootstrap {
		return leaderelection.ErrInvalidState
	}

	for {
		rle.ts = time.Now()

		switch rle.state {
		case leaderelection.Bootstrap, leaderelection.Follower:
			err := rle.acquireLeadership(ctx)
			if err != nil {
				return err
			}

		case leaderelection.Leader:
			err := rle.checkLeadership(ctx)
			if err != nil {
				return err
			}
		}

		intvl := rle.cfg.FollowerCheckInterval
		if rle.state == leaderelection.Leader {
			intvl = rle.cfg.LeaderCheckInterval
		}

		intvl -= time.Now().Sub(rle.ts)
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
