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

type RedisLeaderElection struct {
	cfg         RedisLeaderElectionConfig
	redisClient *redis.Client
	mutex       *redsync.Mutex

	leaderelection.LeaderElection
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
func New(ctx context.Context, cfg RedisLeaderElectionConfig) (leaderelection.LeaderElector, error) {
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
// Redis implementation will ignore RelinquishIntervalSpec. Redis distributed locking
// described here https://redis.io/docs/latest/develop/use/patterns/distributed-locks/,
// relies on TTL of the SET value. This crucially covers for the case where the instance
// holding the lock just vanishes. RelinquishIntervalSpec is used for cases where the user
// wants leadership handoff every day for e.g. Setting TTL to a day will result in a very
// long wait time before leadership handoff takes place in case where the leader vanishes.
func NewWithConn(ctx context.Context, client *redis.Client, cfg RedisLeaderElectionConfig) (leaderelection.LeaderElector, error) {
	intvl := cfg.RelinquishInterval
	if intvl == 0 {
		intvl = leaderelection.DefaultRelinquishInterval
	}

	// Empty out spec
	cfg.RelinquishIntervalSpec = ""

	rle := &RedisLeaderElection{
		redisClient: client,
		mutex:       redsync.New(goredis.NewPool(client)).NewMutex(fmt.Sprint(cfg.LockId), redsync.WithExpiry(intvl), redsync.WithFailFast(true)),
		cfg:         cfg,
	}

	// Initialize LeaderElectionRuntime and return
	rle.Init(cfg.LeaderElectionConfig)
	return rle, nil
}

func (rle *RedisLeaderElection) AcquireLeadership(ctx context.Context) (bool, error) {
	if rle.IsLeader() {
		return true, nil
	}

	err := rle.mutex.TryLockContext(ctx)
	if err != nil {
		if _, ok := err.(*redsync.ErrTaken); ok {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

func (rle *RedisLeaderElection) CheckLeadership(ctx context.Context) (bool, error) {
	if !rle.IsLeader() {
		return false, leaderelection.ErrInvalidState
	}

	lockUntil := rle.mutex.Until()
	if lockUntil.IsZero() || time.Now().After(lockUntil) {
		return false, nil
	}

	return true, nil
}

// Relinquish leadership. A follower calling this function will be returned an error.
func (rle *RedisLeaderElection) RelinquishLeadership(ctx context.Context) (bool, error) {
	if !rle.IsLeader() {
		return false, leaderelection.ErrInvalidState
	}

	unlocked, err := rle.mutex.UnlockContext(ctx)
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
func (rle *RedisLeaderElection) Run(ctx context.Context) error {
	rle.Elector = rle
	return rle.LeaderElection.Run(ctx)
}

// Close the election
func (rle *RedisLeaderElection) Close() {
	rle.LeaderElection.Close()
}
