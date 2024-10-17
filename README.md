# Leaderelection

Leaderelection is a module for electing a leader from multiple instances of a service. Electing a leader enables a design where certain actions like alerting, notifications etc. must only be executed by one instance in the group. A leader can choose to relinquish or go away and in either of these cases, one of the followers will take over as the leader.

## Installation

Install leaderelection using the go get command:

    $ go get github.com/drio-ai/leaderelection

An leader election can be run with either one of these implementations.

 * [Postgres advisory locks](https://pkg.go.dev/github.com/drio-ai/leaderelection@v0.1.1/postgres)
 * [Redis distributed locks](https://pkg.go.dev/github.com/drio-ai/leaderelection@v0.1.1/redis)

## Recommendations

### Relationship between RelinquishInterval and LeaderCheckInterval

LeaderCheckInterval must be lesser than and be a factor of RelinquishInterval. Complying with both of these recommendations is most likely to ensure that leadership handoff is clean as possible in a distributed environment.

### RelinquishInterval and Redis

Redis distributed locks described at this link https://redis.io/docs/latest/develop/use/patterns/distributed-locks/ uses TTL in the SET values to handle among other things, a leader that has vanished. In such cases, the new leader will be elected once the TTL (RelinquishInterval) expires. Setting RelinquishInterval to a high value will delay the election of a new leader. It is recommended to set RelinquishInterval to a reasonable value depending on your environment.

### Relationship between RelinquishInterval and RelinquishIntervalSpec

If RelinquishIntervalSpec is configured, RelinquishInterval will be ignored. This is true except for the Redis implementation. It is expected that RelinquishIntervalSpec will be used in cases like e.g. leadership handoff is scheduled every day at midnight. This duration must also be set as the TTL in case of Redis to make sure leadership handoff does not happen before this schedule. This will not help in the case where the leader vanishes and as a result, Redis implementation will ignore RelinquishIntervalSpec and only work with RelinquishInterval.

### Vanishing Leader and Postgres

Postgres implementation uses session advisory locking to run the election. In case a leader vanishes, Postgres will have to detect this scenario before the lock is released as a result of cleaning up the associated session. The sooner Postgres detects a leader is gone, the sooner it will cleanup and a new leader will be elected. There are multiple ways to do this and it is left to the user to figure out a strategy that best fits their environment.

### PgBouncer and Postgres advisory locks

It is recommended to establish direct connection to Postgres instead of going through PgBouncer. Getting PgBouncer to work with session advisory locks is tricky.


## Documentation

- [Reference](https://godoc.org/github.com/drio-ai/leaderelection)

## Usage

### Leader election using Postgres advisory locking.

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/drio-ai/leader-election/leaderelection"
	"github.com/drio-ai/leader-election/leaderelection/postgres"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	wg := &sync.WaitGroup{}

	numRoutines := 1
	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func(ctx context.Context, id int) {
			defer wg.Done()
			task(ctx, id)
		}(ctx, i+1)
	}

	<-sig
	cancel()
	wg.Wait()
}

const (
	lockId = 10203040
)

type taskInfo struct {
	ple   leaderelection.LeaderElector
	state leaderelection.State
	ts    time.Time
	id    int
}

func (t *taskInfo) isLeader(ctx context.Context) error {
	fmt.Printf("%d: leader\n", t.id)
	if t.state != leaderelection.Leader {
		t.ts = time.Now()
	}
	t.state = leaderelection.Leader

	return nil
}

func (t *taskInfo) isFollower(_ context.Context) error {
	fmt.Printf("%d: follower\n", t.id)
	if t.state != leaderelection.Follower {
		t.ts = time.Now()
	}
	t.state = leaderelection.Follower

	return nil
}

func task(ctx context.Context, id int) {
	t := &taskInfo{
		state: leaderelection.Bootstrap,
		id:    id,
	}

	cfg := postgres.PostgresLeaderElectionConfig{
		Host:     "postgres",
		Port:     5432,
		Secure:   "allow",
		User:     "user",
		Password: "****************",
		Database: "db",
		LeaderElectionConfig: leaderelection.LeaderElectionConfig{
			LockId:                 lockId,
			RelinquishInterval:     30 * time.Second,
			RelinquishIntervalSpec: "@every 60s", // This will be honored and RelinquishInterval above will be ignored
			LeaderCheckInterval:    5 * time.Second,
			FollowerCheckInterval:  5 * time.Second,

			LeaderCallback:   t.isLeader,
			FollowerCallback: t.isFollower,
		},
	}

	ple, err := postgres.New(ctx, cfg)
	if err != nil {
		fmt.Println(err)
		return
	}
	t.ple = ple

	err = ple.Run(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
}
```


### Leader election using Redis distributed locking


```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/drio-ai/leader-election/leaderelection"
	"github.com/drio-ai/leader-election/leaderelection/redis"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	wg := &sync.WaitGroup{}

	numRoutines := 1
	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func(ctx context.Context, id int) {
			defer wg.Done()
			task(ctx, id)
		}(ctx, i+1)
	}

	<-sig
	cancel()
	wg.Wait()
}

const (
	lockId = 10203040
)

type taskInfo struct {
	rle   leaderelection.LeaderElector
	state leaderelection.State
	ts    time.Time
	id    int
}

func (t *taskInfo) isLeader(ctx context.Context) error {
	fmt.Printf("%d: leader\n", t.id)
	if t.state != leaderelection.Leader {
		t.ts = time.Now()
	}
	t.state = leaderelection.Leader

	return nil
}

func (t *taskInfo) isFollower(_ context.Context) error {
	fmt.Printf("%d: follower\n", t.id)
	if t.state != leaderelection.Follower {
		t.ts = time.Now()
	}
	t.state = leaderelection.Follower

	return nil
}

func task(ctx context.Context, id int) {
	t := &taskInfo{
		state: leaderelection.Bootstrap,
		id:    id,
	}

	cfg := redis.RedisLeaderElectionConfig{
		Host:               "cache",
		Port:               6379,
		Secure:             false,
		InsecureSkipVerify: false,
		Password:           "****************",
		LeaderElectionConfig: leaderelection.LeaderElectionConfig{
			LockId:                 lockId,
			RelinquishInterval:     30 * time.Second,
			RelinquishIntervalSpec: "@every 60s", // This will be ignored and RelinquishInterval above will be honored
			LeaderCheckInterval:    5 * time.Second,
			FollowerCheckInterval:  5 * time.Second,

			LeaderCallback:   t.isLeader,
			FollowerCallback: t.isFollower,
		},
	}

	rle, err := redis.New(ctx, cfg)
	if err != nil {
		fmt.Println(err)
		return
	}
	t.rle = rle

	err = rle.Run(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
}
```

## Contributing

Contributions are welcome.

## License

leaderelection is available under the [Apache License, Version 2.0](https://www.apache.org/licenses/LICENSE-2.0).
