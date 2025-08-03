package main

import (
	"context"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	_ "test-task1/docs"
	handlers "test-task1/internal/service"
	"test-task1/internal/storage"
	"test-task1/models"
	"time"
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
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer db.Shutdown()

	r := setupRouter(db)
	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	go func() {
		log.Printf("Server starting on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited properly")
}
