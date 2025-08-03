package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"log"
	"strconv"
	"strings"
	"sync"
	"test-task1/models"
	kraken "test-task1/pkg/kraken-api"
	"time"
)

const (
	migrationPath = "file://migrations"
	cacheTTL      = 10 * time.Minute
	//errorCacheTTL       = 1 * time.Minute
	priceUpdateInterval = 5 * time.Second
	dataRetention       = 4 * time.Hour
	maxTokenCount       = 100
)

type Storage struct {
	DB          *sql.DB
	Redis       *redis.Client
	ActiveCoins map[string]chan struct{}
	Shutdwn     chan struct{}
	wg          sync.WaitGroup
	mutex       sync.RWMutex
}

func initRedis(config models.Config) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     config.RDBConf.RedisAddress,
		Password: config.RDBConf.RedisPassword,
		DB:       config.RDBConf.RedisDB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := rdb.ConfigSet(ctx, "maxmemory", "100mb").Result(); err != nil {
		log.Printf("Warning: failed to set Redis maxmemory: %v", err)
	}
	if _, err := rdb.ConfigSet(ctx, "maxmemory-policy", "allkeys-lru").Result(); err != nil {
		return nil, fmt.Errorf("failed to configure Redis LRU: %v", err)
	}

	if _, err := rdb.Ping(ctx).Result(); err != nil {
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
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		c.DBConf.Host, c.DBConf.Port, c.DBConf.User, c.DBConf.Password, c.DBConf.DBName)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", op, err)
	}

	if err = waitForDB(db, 5, 1*time.Second); err != nil {
		return nil, fmt.Errorf("%s: %v", op, err)
	}

	rdb, err := initRedis(c)
	if err != nil {
		return nil, fmt.Errorf("%s (initRedis): %v", op, err)
	}

	s := &Storage{
		DB:          db,
		Redis:       rdb,
		ActiveCoins: make(map[string]chan struct{}),
		Shutdwn:     make(chan struct{}),
	}

	if err = runMigrations(db); err != nil {
		return nil, fmt.Errorf("failed to make migrations: %v", err)
	}

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

// AddCurrency adds cryptocurrency to tracking list and starts data collection.
// If currency is already tracked, does nothing.
// Parameters:
// - coin: cryptocurrency symbol (e.g. "BTC")
func (s *Storage) AddCurrency(coin string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, exists := s.ActiveCoins[coin]; exists {
		return
	}

	stopChan := make(chan struct{})
	s.ActiveCoins[coin] = stopChan

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.startCollecting(coin, stopChan)
	}()
}

// startCollecting launches the periodic collection of data on the price of cryptocurrencies.
// Data is collected every 15 seconds via the Kraken API and stored in the database.
// Works until a stop signal is received via stopChan.
// Parameters:
// - coin: the symbolic code of the cryptocurrency
// - stopChan: the channel for receiving the stop signal
func (s *Storage) startCollecting(coin string, stopChan <-chan struct{}) {
	ticker := time.NewTicker(priceUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			price, err := kraken.GetPrice(coin)
			if err != nil {
				log.Printf("Failed to get price for %s: %v", coin, err)
				continue
			}

			timestamp := time.Now().Unix()
			log.Printf("%s: %f, %d", coin, price, timestamp)
			s.SaveCurrency(coin, price, timestamp)

			s.UpdateCache(coin, price, timestamp)

		case <-stopChan:
			return
		case <-s.Shutdwn:
			return
		}
	}
}

// updateCache updates Redis cache with new price data and cleans expired entries.
// Parameters:
// - coin: cryptocurrency symbol
// - price: current price
// - timestamp: Unix timestamp of price
func (s *Storage) UpdateCache(coin string, price float64, timestamp int64) {
	ctx := context.Background()
	key := fmt.Sprintf("token:%s", coin)

	pipe := s.Redis.Pipeline()
	pipe.ZAdd(ctx, key, &redis.Z{
		Score:  float64(timestamp),
		Member: fmt.Sprintf("%d:%f", timestamp, price),
	})

	pipe.ZRemRangeByScore(ctx, key, "0", strconv.FormatInt(time.Now().Add(-dataRetention).Unix(), 10))

	pipe.Expire(ctx, key, cacheTTL)
	pipe.ZAdd(ctx, "token:lru", &redis.Z{
		Score:  float64(time.Now().Unix()),
		Member: coin,
	})

	if count, err := pipe.ZCard(ctx, "token:lru").Result(); err == nil && count > maxTokenCount {
		pipe.ZPopMin(ctx, "token:lru", 1)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("Cache update failed for %s: %v", coin, err)
	}
}

func (s *Storage) GetFromCache(ctx context.Context, key string, timestamp int64) (float64, error) {

	members, err := s.Redis.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: strconv.FormatInt(timestamp-300, 10),
		Max: strconv.FormatInt(timestamp+300, 10),
	}).Result()

	if err != nil || len(members) == 0 {
		return 0, errors.New("no cached data")
	}

	parts := splitMember(members[0])
	return strconv.ParseFloat(parts[1], 64)
}

func (s *Storage) getFromDB(coin string, timestamp int64) (float64, int64, error) {
	var price float64
	var dbTimestamp int64
	err := s.DB.QueryRow(`
		SELECT price, timestamp 
		FROM currencies 
		WHERE coin = $1 
		ORDER BY ABS(timestamp - $2) 
		LIMIT 1`,
		coin, timestamp,
	).Scan(&price, &dbTimestamp)

	return price, dbTimestamp, err
}

func splitMember(member string) []string {
	return strings.Split(member, ":")
}

// SaveCurrency saves data on the price of cryptocurrencies to the database.
// In case of a saving error, logs the error, but does not interrupt execution.
// Parameters:
// - coin: the symbolic code of the cryptocurrency
// - price: the current price
// - timestamp: a timestamp in Unix format
func (s *Storage) SaveCurrency(coin string, price float64, timestamp int64) {
	_, err := s.DB.Exec(
		"INSERT INTO currencies (coin, price, timestamp) VALUES ($1, $2, $3)",
		coin, price, timestamp,
	)
	if err != nil {
		log.Printf("Failed to save currency: %v", err)
	}
}

// GetPrice returns the price of the cryptocurrency at the specified time.
// First it checks the cache in Redis, if not, it searches the database for the nearest value.
// The found value is cached in Redis for 10 minutes.
// Parameters:
// - coin: the symbolic code of the cryptocurrency
// - timestamp: a timestamp in Unix format
// Returns:
// - price: the price of the cryptocurrency
// - error: error if the price could not be found
func (s *Storage) GetPrice(coin string, timestamp int64) (float64, error) {
	ctx := context.Background()
	key := fmt.Sprintf("token:%s", coin)
	t1 := time.Now().UnixNano()
	if result, err := s.GetFromCache(ctx, key, timestamp); err == nil {
		fmt.Printf("Get from cache, time (ns): %d", time.Now().UnixNano()-t1)
		return result, nil
	}

	price, dbTimestamp, err := s.getFromDB(coin, timestamp)
	if err != nil {
		return 0, err
	}

	s.Redis.ZAdd(ctx, "token:lru", &redis.Z{
		Score:  float64(time.Now().Unix()),
		Member: coin,
	})

	if abs(timestamp-dbTimestamp) <= 300 {
		s.UpdateCache(coin, price, dbTimestamp)
	}

	fmt.Printf("Get from PostgresQL, time (ns): %d", time.Now().UnixNano()-t1)
	return price, nil
}

// Shutdown gracefully stops all background operations.
func (s *Storage) Shutdown() {
	close(s.Shutdwn)
	s.wg.Wait()

	if err := s.DB.Close(); err != nil {
		log.Printf("Error closing database: %v", err)
	}

	if err := s.Redis.Close(); err != nil {
		log.Printf("Error closing Redis: %v", err)
	}
}

// RemoveCurrency stops tracking cryptocurrency and removes from active list.
// Parameters:
// - coin: cryptocurrency symbol to remove
func (s *Storage) RemoveCurrency(coin string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if stopChan, exists := s.ActiveCoins[coin]; exists {
		close(stopChan)
		delete(s.ActiveCoins, coin)
		ctx := context.Background()
		s.Redis.ZRem(ctx, "token:lru", coin)
		s.Redis.Del(ctx, fmt.Sprintf("token:%s", coin))
	}
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
