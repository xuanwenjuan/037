package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"thz-service/internal/config"
	"thz-service/internal/handlers"
	"thz-service/internal/rabbitmq"
	"thz-service/internal/repository"
	"thz-service/internal/websocket"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	repo, err := repository.New(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	log.Println("Database connected successfully")

	producer, err := rabbitmq.NewProducer(cfg.RabbitMQURL, cfg.RabbitMQQueue)
	if err != nil {
		log.Fatalf("Failed to create RabbitMQ producer: %v", err)
	}
	defer producer.Close()
	log.Println("RabbitMQ producer connected")

	wsMgr := websocket.NewManager(cfg.WSBuffer)

	consumer, err := rabbitmq.NewResultConsumer(cfg.RabbitMQURL, cfg.RabbitMQResult, repo, wsMgr)
	if err != nil {
		log.Fatalf("Failed to create RabbitMQ consumer: %v", err)
	}
	defer consumer.Stop()

	if err := consumer.Start(); err != nil {
		log.Fatalf("Failed to start result consumer: %v", err)
	}
	log.Println("RabbitMQ result consumer started")

	h := handlers.NewHandler(repo, producer)

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	api := r.Group("/api/v1")
	{
		api.GET("/health", h.HealthCheck)
		api.POST("/analyses/upload", h.UploadWaveform)
		api.GET("/analyses", h.ListAnalyses)
		api.GET("/analyses/:id", h.GetAnalysis)
		api.GET("/analyses/:id/ws", wsMgr.HandleConnection)
	}

	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		log.Printf("Server starting on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
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

	log.Println("Server exited gracefully")
}
