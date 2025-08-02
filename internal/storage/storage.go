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

// AddCurrency adds cryptocurrency to tracking list and starts data collection.
// If currency is already tracked, does nothing.
// Parameters:
// - coin: cryptocurrency symbol (e.g. "BTC")
func (s *Storage) AddCurrency(coin string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, exists := s.activeCoins[coin]; exists {
		return
	}

	stopChan := make(chan struct{})
	s.activeCoins[coin] = stopChan

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
			s.SaveCurrency(coin, price, timestamp)

			s.updateCache(coin, price, timestamp)

		case <-stopChan:
			return
		case <-s.shutdown:
			return
		}
	}
}

// updateCache updates Redis cache with new price data and cleans expired entries.
// Parameters:
// - coin: cryptocurrency symbol
// - price: current price
// - timestamp: Unix timestamp of price
func (s *Storage) updateCache(coin string, price float64, timestamp int64) {
	ctx := context.Background()
	key := coin

	if err := s.redis.HSet(ctx, key, fmt.Sprintf("%d", timestamp), price).Err(); err != nil {
		log.Printf("Failed to update cache for %s: %v", coin, err)
	}

	expirationTime := time.Now().Add(-dataRetention).Unix()
	//get all values
	fields, err := s.redis.HKeys(ctx, key).Result()
	if err != nil {
		log.Printf("Failed to get cache keys for %s: %v", coin, err)
		return
	}

	//iterate for all values and if this timestamp less than now()-4_hour then delete (delete old lines)
	for _, field := range fields {
		ts, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			continue
		}

		if ts < expirationTime {
			if err := s.redis.HDel(ctx, key, field).Err(); err != nil {
				log.Printf("Failed to delete expired cache for %s: %v", coin, err)
			}
		}
	}
	//update for lru
	s.redis.Expire(ctx, key, cacheTTL)
}

// SaveCurrency saves data on the price of cryptocurrencies to the database.
// In case of a saving error, logs the error, but does not interrupt execution.
// Parameters:
// - coin: the symbolic code of the cryptocurrency
// - price: the current price
// - timestamp: a timestamp in Unix format
func (s *Storage) SaveCurrency(coin string, price float64, timestamp int64) {
	_, err := s.db.Exec(
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
	const op = "storage.GetPrice"
	ctx := context.Background()
	key := coin
	//if token cached then take it from redis
	Cached := s.isTokenCached(coin)
	if Cached {
		price, err := s.redis.HGet(ctx, key, fmt.Sprintf("%d", timestamp)).Float64()
		if err == nil {
			s.redis.Expire(ctx, key, cacheTTL)
			return price, nil
		}

		if nearestPrice, err := s.findNearestCachedPrice(ctx, key, timestamp); err == nil {
			return nearestPrice, nil
		}
	}

	query := `
		SELECT price, timestamp, ABS(timestamp - $2) AS diff
		FROM currencies
		WHERE coin = $1
		ORDER BY diff
		LIMIT 1
	`
	var result float64
	var foundTimestamp int64
	err := s.db.QueryRow(query, coin, timestamp).Scan(&result, &foundTimestamp, nil)
	if err != nil {
		return 0, fmt.Errorf("%s: %v", op, err)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	//delete old lines from redis
	if !Cached {
		if keys, err := s.redis.Keys(ctx, "*").Result(); err == nil && len(keys) >= maxTokenCount {
			var oldestKey string
			minTTL := time.Duration(1<<63 - 1)

			for _, k := range keys {
				if ttl, err := s.redis.TTL(ctx, k).Result(); err == nil && ttl < minTTL {
					minTTL = ttl
					oldestKey = k
				}
			}

			if oldestKey != "" {
				s.redis.Del(ctx, oldestKey)
			}
		}

		s.redis.HSet(ctx, key, fmt.Sprintf("%d", foundTimestamp), result)
		s.redis.Expire(ctx, key, cacheTTL)
	} else {
		s.redis.HSet(ctx, key, fmt.Sprintf("%d", foundTimestamp), result)
	}

	return result, nil
}

func (s *Storage) isTokenCached(coin string) bool {
	ctx := context.Background()
	exists, err := s.redis.Exists(ctx, coin).Result()
	return err == nil && exists == 1
}

// findNearestCachedPrice finds price closest to requested timestamp in cache.
// Parameters:
// - ctx: context
// - key: Redis key (cryptocurrency symbol)
// - timestamp: target Unix timestamp
// Returns:
// - float64: nearest price found
// - error: if no cached data available
func (s *Storage) findNearestCachedPrice(ctx context.Context, key string, timestamp int64) (float64, error) {
	allData, err := s.redis.HGetAll(ctx, key).Result()
	if err != nil {
		return 0, err
	}

	var nearestPrice float64
	minDiff := int64(1<<63 - 1)

	for tsStr, priceStr := range allData {
		cacheTime, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			continue
		}
		diff := abs(cacheTime - timestamp)
		if diff < minDiff {
			minDiff = diff
			nearestPrice, _ = strconv.ParseFloat(priceStr, 64)
		}
	}

	if minDiff == int64(1<<63-1) {
		return 0, fmt.Errorf("no cached data found")
	}

	return nearestPrice, nil
}

// Shutdown gracefully stops all background operations.
func (s *Storage) Shutdown() {
	close(s.shutdown)
	s.wg.Wait()

	if err := s.db.Close(); err != nil {
		log.Printf("Error closing database: %v", err)
	}

	if err := s.redis.Close(); err != nil {
		log.Printf("Error closing Redis: %v", err)
	}
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
