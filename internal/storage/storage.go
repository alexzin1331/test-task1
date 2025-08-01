package storage

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"log"
	"test-task1/models"
	"time"
)

const (
	migrationPath = "file://migrations"
)

const (
	cacheLimit = 1000
)

type Storage struct {
	db    *sql.DB
	redis *redis.Client
}

func initRedis(config models.Config) (*redis.Client, error) {
	var rdb *redis.Client
	rdb = redis.NewClient(&redis.Options{
		Addr:     config.RDBConf.RedisAddress,
		Password: config.RDBConf.RedisPassword,
		DB:       config.RDBConf.RedisDB,
	})
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %v", err)
	}
	return rdb, nil
}

// run migrations for PostgreSQL
func runMigrations(db *sql.DB) error {
	const op = "storage.migrations"
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("%s: %v", op, err)
	}
	//init new Migrate struct
	m, err := migrate.NewWithDatabaseInstance(
		migrationPath,
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("%s: %v", op, err)
	}

	//run migrations (up)
	if err = m.Up(); err != nil {
		if err != migrate.ErrNoChange {
			return fmt.Errorf("%s: %v", op, err)
		}
		log.Println("No migrations to apply.")
	} else {
		log.Println("Database migrations applied successfully.")
	}
	return nil
}

// New create new storage with Redis and Postgres
func New(c models.Config) (*Storage, error) {
	const op = "storage.connection"
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", c.DBConf.Host, c.DBConf.Port, c.DBConf.User, c.DBConf.Password, c.DBConf.DBName)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", op, err)
	}
	//attempting to reconnect to the database.
	if err = waitForDB(db, 5, 1*time.Second); err != nil {
		return nil, fmt.Errorf("%s: %v", op, err)
	}
	//test connection
	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("%s: %v", op, err)
	}
	log.Println("Connection is ready")
	rdb, err := initRedis(c)
	if err != nil {
		return nil, fmt.Errorf("%s (initRedis): %v", op, err)
	}
	s := &Storage{
		db:    db,
		redis: rdb,
	}

	//create tables in PostgreSQL
	if err = runMigrations(db); err != nil {
		return &Storage{}, fmt.Errorf("failed to make migrations: %v", err)
	}
	log.Printf("\nmigraitions is success\n")

	return s, nil
}

// waitForDB attempts to reconnect to the database.
// This is necessary because when running in Docker,
// the server might try to connect before the database is fully initialized.
func waitForDB(db *sql.DB, attempts int, delay time.Duration) error {
	for i := 0; i < attempts; i++ {
		err := db.Ping()
		if err == nil {
			return nil
		}
		log.Printf("Waiting for DB... attempt %d/%d: %v", i+1, attempts, err)
		time.Sleep(delay)
	}
	return fmt.Errorf("database is not reachable after %d attempts", attempts)
}
