package testutil

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

var (
	postgresOnce     sync.Once
	postgresPool     *dockertest.Pool
	postgresResource *dockertest.Resource
	postgresErr      error
	postgresDSN      string
)

func NewPostgresURL(t *testing.T) string {
	t.Helper()

	postgresOnce.Do(func() {
		if dsn := strings.TrimSpace(os.Getenv("ENGRAM_TEST_DATABASE_URL")); dsn != "" {
			postgresDSN = dsn
			return
		}

		postgresPool, postgresErr = dockertest.NewPool("")
		if postgresErr != nil {
			return
		}

		postgresResource, postgresErr = postgresPool.RunWithOptions(&dockertest.RunOptions{
			Repository: "postgres",
			Tag:        "16-alpine",
			Env: []string{
				"POSTGRES_USER=engram",
				"POSTGRES_PASSWORD=engram",
				"POSTGRES_DB=engram",
			},
		}, func(hostConfig *docker.HostConfig) {
			hostConfig.AutoRemove = true
			hostConfig.RestartPolicy = docker.RestartPolicy{Name: "no"}
		})
		if postgresErr != nil {
			return
		}
		postgresResource.Expire(600)

		base := fmt.Sprintf("postgres://engram:engram@127.0.0.1:%s/engram?sslmode=disable", postgresResource.GetPort("5432/tcp"))
		postgresErr = postgresPool.Retry(func() error {
			db, err := sql.Open("postgres", base)
			if err != nil {
				return err
			}
			defer db.Close()
			return db.Ping()
		})
		if postgresErr != nil {
			return
		}
		postgresDSN = base
	})

	if postgresErr != nil {
		t.Fatalf("start postgres test database: %v", postgresErr)
	}

	if strings.TrimSpace(os.Getenv("ENGRAM_TEST_DATABASE_URL")) != "" {
		return postgresDSN
	}

	dbName := "engram_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	adminDB, err := sql.Open("postgres", postgresDSN)
	if err != nil {
		t.Fatalf("open postgres admin connection: %v", err)
	}
	defer adminDB.Close()

	if _, err := adminDB.Exec(`CREATE DATABASE ` + dbName); err != nil {
		t.Fatalf("create test database %s: %v", dbName, err)
	}

	t.Cleanup(func() {
		cleanupDB, err := sql.Open("postgres", postgresDSN)
		if err != nil {
			return
		}
		defer cleanupDB.Close()
		_, _ = cleanupDB.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, dbName)
		_, _ = cleanupDB.Exec(`DROP DATABASE IF EXISTS ` + dbName)
	})

	parsed, err := url.Parse(postgresDSN)
	if err != nil {
		t.Fatalf("parse postgres dsn: %v", err)
	}
	parsed.Path = "/" + dbName
	return parsed.String()
}
