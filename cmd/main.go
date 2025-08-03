package main

import (
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"log"
	_ "test-task1/docs"
	handlers "test-task1/internal/service"
	"test-task1/internal/storage"
	"test-task1/models"
)

const (
	configPath = "config.yaml"
)

func setupRouter(storage *storage.Storage) *gin.Engine {
	r := gin.Default()

	currencyHandler := handlers.NewCurrencyHandler(storage)

	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// API endpoints
	api := r.Group("/currency")
	{
		api.POST("/add", currencyHandler.AddCurrency)
		api.POST("/remove", currencyHandler.RemoveCurrency)
		api.POST("/price", currencyHandler.GetPrice)
	}

	return r
}

func main() {
	cfg := models.MustLoad(configPath)

	db, err := storage.New(*cfg)
	if err != nil {
		panic(err)
	}
	defer db.Shutdown()

	r := setupRouter(db)
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("run error: %v", err)
	}
}
