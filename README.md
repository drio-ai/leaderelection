# Leaderelection

Leaderelection is a module for electing a leader from multiple instances of a service. Electing a leader enables a design where certain actions like alerting, notifications etc. must only be executed by one instance in the group. A leader can choose to relinquish or go away and in either of these cases, one of the followers will take over as the leader.

## Installation

Install leaderelection using the go get command:

    $ go get github.com/drio-ai/leaderelection

An leader election can be run with either one of these implementations.

 * [Postgres advisory locks](https://pkg.go.dev/github.com/drio-ai/leaderelection@v0.1.1/postgres)
 * [Redis distributed locks](https://pkg.go.dev/github.com/drio-ai/leaderelection@v0.1.1/redis)

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
			LockId:                lockId,
			RelinquishInterval:    30 * time.Second,
			LeaderCheckInterval:   5 * time.Second,
			FollowerCheckInterval: 5 * time.Second,

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
			LockId:                lockId,
			RelinquishInterval:    30 * time.Second,
			LeaderCheckInterval:   5 * time.Second,
			FollowerCheckInterval: 5 * time.Second,

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
