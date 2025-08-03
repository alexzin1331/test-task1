package storage_test

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"test-task1/internal/storage"
)

// Test adding new currency to tracking
func TestAddCurrency(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	rdb := redis.NewClient(&redis.Options{})
	mockStorage := &storage.Storage{
		DB:          db,
		Redis:       rdb,
		ActiveCoins: make(map[string]chan struct{}),
		Shutdwn:     make(chan struct{}),
	}
	// Add currency and verify it's tracked
	mockStorage.AddCurrency("BTC")

	_, exists := mockStorage.ActiveCoins["BTC"]
	require.True(t, exists, "BTC should be in ActiveCoins")

	// Cleanup
	mockStorage.RemoveCurrency("BTC")
}

// Test price retrieval from database
func TestRemoveCurrency(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Test successful price fetch
	rdb := redis.NewClient(&redis.Options{})
	stopChan := make(chan struct{})
	mockStorage := &storage.Storage{
		DB:          db,
		Redis:       rdb,
		ActiveCoins: map[string]chan struct{}{"ETH": stopChan},
		Shutdwn:     make(chan struct{}),
	}

	mockStorage.RemoveCurrency("ETH")

	_, exists := mockStorage.ActiveCoins["ETH"]
	assert.False(t, exists, "ETH should be removed from ActiveCoins")
}

func TestGetPrice(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	rdb := redis.NewClient(&redis.Options{})
	mockStorage := &storage.Storage{
		DB:    db,
		Redis: rdb,
	}

	// Test successful price fetch
	t.Run("success from db", func(t *testing.T) {
		testTime := time.Now().Unix()
		expectedPrice := 50000.0
		expectedTimestamp := testTime

		mock.ExpectQuery(`
			SELECT price, timestamp 
			FROM currencies 
			WHERE coin = $1 
			ORDER BY ABS(timestamp - $2) 
			LIMIT 1`).
			WithArgs("BTC", testTime).
			WillReturnRows(sqlmock.NewRows([]string{"price", "timestamp"}).
				AddRow(expectedPrice, expectedTimestamp)) // Full query omitted for brevity

		price, err := mockStorage.GetPrice("BTC", testTime)
		assert.NoError(t, err)
		assert.Equal(t, expectedPrice, price)
	})

	// Test not found case
	t.Run("not found", func(t *testing.T) {
		testTime := time.Now().Unix()
		mock.ExpectQuery(`
			SELECT price, timestamp 
			FROM currencies 
			WHERE coin = $1 
			ORDER BY ABS(timestamp - $2) 
			LIMIT 1`).
			WithArgs("UNKNOWN", testTime).
			WillReturnError(sql.ErrNoRows)

		_, err := mockStorage.GetPrice("UNKNOWN", testTime)
		assert.Error(t, err)
	})
}

func TestSaveCurrency(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	rdb := redis.NewClient(&redis.Options{})
	mockStorage := &storage.Storage{
		DB:    db,
		Redis: rdb,
	}

	testTime := time.Now().Unix()
	testPrice := 50000.0

	mock.ExpectExec("INSERT INTO currencies (coin, price, timestamp) VALUES ($1, $2, $3)").
		WithArgs("BTC", testPrice, testTime).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mockStorage.SaveCurrency("BTC", testPrice, testTime)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestShutdown(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectClose()

	rdb := redis.NewClient(&redis.Options{})
	mockStorage := &storage.Storage{
		DB:          db,
		Redis:       rdb,
		ActiveCoins: make(map[string]chan struct{}),
		Shutdwn:     make(chan struct{}),
	}

	mockStorage.ActiveCoins["ETH"] = make(chan struct{})
	mockStorage.ActiveCoins["BTC"] = make(chan struct{})

	mockStorage.Shutdown()

	assert.Error(t, db.Ping(), "DB connection should be closed")
	_, err = rdb.Ping(context.Background()).Result()
	assert.Error(t, err, "Redis connection should be closed")
}

func TestCacheOperations(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	rdb := redis.NewClient(&redis.Options{})
	mockStorage := &storage.Storage{
		DB:    db,
		Redis: rdb,
	}

	ctx := context.Background()
	testTime := time.Now().Unix()
	testPrice := 50000.0
	coin := "BTC"

	mockStorage.UpdateCache(coin, testPrice, testTime)

	member := fmt.Sprintf("%d:%f", testTime, testPrice)
	key := fmt.Sprintf("token:%s", coin)
	results, err := rdb.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: strconv.FormatInt(testTime-1, 10),
		Max: strconv.FormatInt(testTime+1, 10),
	}).Result()
	assert.NoError(t, err)
	assert.Contains(t, results, member)

	price, err := mockStorage.GetFromCache(ctx, key, testTime)
	assert.NoError(t, err)
	assert.Equal(t, testPrice, price)
}
