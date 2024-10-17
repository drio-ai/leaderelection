package postgres

import (
	"context"
	"fmt"

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

	leaderelection.LeaderElectionConfig
}

type PostgresLeaderElection struct {
	cfg    PostgresLeaderElectionConfig
	dbConn *pgx.Conn

	leaderelection.LeaderElection
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
func New(ctx context.Context, cfg PostgresLeaderElectionConfig) (leaderelection.LeaderElector, error) {
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
func NewWithConn(ctx context.Context, conn *pgx.Conn, cfg PostgresLeaderElectionConfig) (leaderelection.LeaderElector, error) {
	err := prepareStatements(ctx, conn)
	if err != nil {
		return nil, err
	}

	if cfg.RelinquishInterval == 0 {
		cfg.RelinquishInterval = leaderelection.DefaultRelinquishInterval
	}

	ple := &PostgresLeaderElection{
		dbConn: conn,
		cfg:    cfg,
	}

	// Initialize LeaderElectionRuntime and return
	ple.Init(cfg.LeaderElectionConfig)
	return ple, nil
}

func (ple *PostgresLeaderElection) AcquireLeadership(ctx context.Context) (bool, error) {
	// Postgres allows us to call try lock even if we already hold the lock.
	// If we do, we must make as many unlock calls to relinquish the lock.
	// We don't want to keep track of this. Return if we are already the leader.
	if ple.IsLeader() {
		return true, nil
	}

	var isLeader bool
	err := ple.dbConn.QueryRow(ctx, lelock, ple.cfg.LockId).Scan(&isLeader)
	if err != nil {
		return false, err
	}

	return isLeader, nil
}

func (ple *PostgresLeaderElection) CheckLeadership(ctx context.Context) (bool, error) {
	if !ple.IsLeader() {
		return false, leaderelection.ErrInvalidState
	}

	var isLeader bool
	err := ple.dbConn.QueryRow(ctx, lecheck, ple.cfg.LockId).Scan(&isLeader)
	if err != nil {
		return false, err
	}

	return isLeader, nil
}

// Relinquish leadership. An error will be returned if caller is not the leader.
func (ple *PostgresLeaderElection) RelinquishLeadership(ctx context.Context) (bool, error) {
	if !ple.IsLeader() {
		return false, leaderelection.ErrInvalidState
	}

	var status bool
	err := ple.dbConn.QueryRow(ctx, leunlock, ple.cfg.LockId).Scan(&status)
	if err != nil {
		return false, err
	}

	// unlock query will return true or false in these cases
	// Returns true: If this connection holds the lock and it was successfully released.
	// Returns false: If this connection did not hold the lock.
	// In the latter case, we thought we were the leader but we were not. TODO: Log an error.
	return status, nil
}

// Run the election
func (ple *PostgresLeaderElection) Run(ctx context.Context) error {
	ple.Elector = ple
	return ple.LeaderElection.Run(ctx)
}

// Close the election
func (ple *PostgresLeaderElection) Close() {
	ple.LeaderElection.Close()
}
