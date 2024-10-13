package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	leaderelection "github.com/drio-ai/leaderelection"
)

// Postgres connection parameters and lock id
type PostgresLeaderElectionConfig struct {
	Host     string
	Port     uint16
	Secure   string
	User     string
	Password string
	Database string
	LockId   uint

	leaderelection.LeaderElectionConfig
}

type PostgresLeaderElection struct {
	cfg             PostgresLeaderElectionConfig
	dbConn          *pgx.Conn
	state           leaderelection.State
	relinquishIntvl time.Duration
	ts              time.Time
	leaderAt        time.Time
}

const (
	lelock   = "lelock"
	leunlock = "leunlock"
	lecheck  = "lecheck"
)

var statements = map[string]string{
	lelock:   "select pg_try_advisory_lock($1)",
	leunlock: "select pg_advisory_unlock($1)",
	lecheck:  "select granted from pg_locks where pid = pg_backend_pid() and locktype = 'advisory' and objid = $1",
}

func prepareStatements(ctx context.Context, conn *pgx.Conn) error {
	for name, statement := range statements {
		_, err := conn.Prepare(ctx, name, statement)
		if err != nil {
			return err
		}
	}

	return nil
}

func setupDBConn(ctx context.Context, cfg PostgresLeaderElectionConfig) (*pgx.Conn, error) {
	dbUrl := "postgres://" + cfg.User + ":" + cfg.Password + "@" + cfg.Host + ":" + fmt.Sprint(cfg.Port) + "/" + cfg.Database + "?sslmode=" + cfg.Secure
	config, err := pgx.ParseConfig(dbUrl)
	if err != nil {
		return nil, err
	}

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	err = prepareStatements(ctx, conn)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// Initialize leader election with postgres DB parameters and lock id
func New(ctx context.Context, cfg PostgresLeaderElectionConfig) (leaderelection.LeaderElection, error) {
	conn, err := setupDBConn(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return NewWithConn(ctx, conn, cfg)
}

// Initialize leader election with postgres DB connection and lock id.
// Caller must have connected to postgres and gives us a connection to work with.
// DB parameters can be skipped in this case.
// Please note, this DB connection will dedicated for leader election only.
func NewWithConn(ctx context.Context, conn *pgx.Conn, cfg PostgresLeaderElectionConfig) (leaderelection.LeaderElection, error) {
	err := prepareStatements(ctx, conn)
	if err != nil {
		return nil, err
	}

	intvl := cfg.RelinquishInterval
	if intvl == 0 {
		intvl = leaderelection.DefaultRelinquishInterval
	}

	return &PostgresLeaderElection{
		dbConn:          conn,
		cfg:             cfg,
		state:           leaderelection.Bootstrap,
		relinquishIntvl: intvl,
	}, nil
}

func (ple *PostgresLeaderElection) shouldRelinquish() bool {
	if ple.state == leaderelection.Leader {
		return time.Now().Sub(ple.leaderAt) >= ple.relinquishIntvl
	}

	return false
}

func (ple *PostgresLeaderElection) setState(state leaderelection.State) {
	ple.state = state
	ple.ts = time.Now()
}

func (ple *PostgresLeaderElection) acquireLeadership(ctx context.Context) error {
	// Postgres allows us to call try lock even if we already hold the lock.
	// If we do, we must make as many unlock calls to relinquish the lock.
	// We don't want to keep track of this. Return if we are already the leader.
	if ple.state == leaderelection.Leader {
		return nil
	}

	var isLeader bool
	err := ple.dbConn.QueryRow(ctx, lelock, ple.cfg.LockId).Scan(&isLeader)
	if err != nil {
		return err
	}

	if isLeader {
		ple.leaderAt = time.Now()
		ple.setState(leaderelection.Leader)
		return ple.cfg.LeaderCallback(ctx)
	}

	ple.setState(leaderelection.Follower)
	return ple.cfg.FollowerCallback(ctx)
}

// Relinquish leadership. A follower calling this function will be returned an error.
func (ple *PostgresLeaderElection) RelinquishLeadership(ctx context.Context) error {
	if ple.state != leaderelection.Leader {
		return leaderelection.ErrInvalidState
	}

	var status bool
	err := ple.dbConn.QueryRow(ctx, leunlock, ple.cfg.LockId).Scan(&status)
	if err != nil {
		return err
	}

	// unlock query will return true or false in these cases
	// Returns true: If this connection holds the lock and it was successfully released.
	// Returns false: If this connection did not hold the lock.
	// In the latter case, we thought we were the leader but we were not. TODO: Log an error.
	// Set state to follower and issue callback.
	ple.setState(leaderelection.Follower)
	return ple.cfg.FollowerCallback(ctx)
}

func (ple *PostgresLeaderElection) checkLeadership(ctx context.Context) error {
	if ple.state != leaderelection.Leader {
		return leaderelection.ErrInvalidState
	}

	var isLeader bool
	err := ple.dbConn.QueryRow(ctx, lecheck, ple.cfg.LockId).Scan(&isLeader)
	if err != nil {
		return err
	}

	if isLeader {
		if ple.shouldRelinquish() {
			return ple.RelinquishLeadership(ctx)
		}

		ple.setState(leaderelection.Leader)
		return ple.cfg.LeaderCallback(ctx)
	}

	ple.setState(leaderelection.Follower)
	return ple.cfg.FollowerCallback(ctx)
}

// Run the election. Will run until passed context is canceled or times out or deadline is exceeded.
func (ple *PostgresLeaderElection) Run(ctx context.Context) error {
	if ple.state != leaderelection.Bootstrap {
		return leaderelection.ErrInvalidState
	}

	for {
		ple.ts = time.Now()

		switch ple.state {
		case leaderelection.Bootstrap, leaderelection.Follower:
			err := ple.acquireLeadership(ctx)
			if err != nil {
				return err
			}

		case leaderelection.Leader:
			err := ple.checkLeadership(ctx)
			if err != nil {
				return err
			}
		}

		intvl := ple.cfg.FollowerCheckInterval
		if ple.state == leaderelection.Leader {
			intvl = ple.cfg.LeaderCheckInterval
		}

		intvl -= time.Now().Sub(ple.ts)
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
