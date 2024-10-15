package redis

import (
	"context"
	"crypto/tls"
	"errors"
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

	leaderelection.LeaderElectionConfig
}

type RedisLeaderElectionRuntime struct {
	cfg         RedisLeaderElectionConfig
	redisClient *redis.Client
	mutex       *redsync.Mutex

	leaderelection.LeaderElectionRuntime
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

	rler := &RedisLeaderElectionRuntime{
		redisClient: client,
		mutex:       redsync.New(goredis.NewPool(client)).NewMutex(fmt.Sprint(cfg.LockId), redsync.WithExpiry(intvl), redsync.WithFailFast(true)),
		cfg:         cfg,
	}

	// Initialize LeaderElectionRuntime and return
	rler.Init(cfg.LeaderElectionConfig)
	return rler, nil
}

func (rler *RedisLeaderElectionRuntime) AcquireLeadership(ctx context.Context) (bool, error) {
	if rler.IsLeader() {
		return true, nil
	}

	err := rler.mutex.TryLockContext(ctx)
	if err != nil {
		if _, ok := err.(*redsync.ErrTaken); ok {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

func (rler *RedisLeaderElectionRuntime) CheckLeadership(ctx context.Context) (bool, error) {
	if !rler.IsLeader() {
		return false, leaderelection.ErrInvalidState
	}

	lockUntil := rler.mutex.Until()
	if lockUntil.IsZero() || time.Now().After(lockUntil) {
		return false, nil
	}

	return true, nil
}

// Relinquish leadership. A follower calling this function will be returned an error.
func (rler *RedisLeaderElectionRuntime) RelinquishLeadership(ctx context.Context) (bool, error) {
	if !rler.IsLeader() {
		return false, leaderelection.ErrInvalidState
	}

	unlocked, err := rler.mutex.UnlockContext(ctx)
	if err != nil {
		if errors.Is(err, redsync.ErrLockAlreadyExpired) {
			return false, nil
		}

		if _, ok := err.(*redsync.ErrTaken); ok {
			return false, nil
		}

		return false, err
	}
	if !unlocked {
		return false, nil
	}

	return true, nil
}

// Run the election
func (rler *RedisLeaderElectionRuntime) Run(ctx context.Context) error {
	rler.Elector = rler
	return rler.LeaderElectionRuntime.Run(ctx)
}
