package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"thz-service/internal/cache"
	"thz-service/internal/config"
	"thz-service/internal/handlers"
	"thz-service/internal/metrics"
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

	var cacheSvc *cache.CacheService
	if cfg.EnableCache {
		cacheSvc, err = cache.NewCacheService(cfg.RedisURL, cfg.CacheTTL)
		if err != nil {
			log.Printf("WARNING: Failed to connect to Redis, cache disabled: %v", err)
			cacheSvc = nil
		} else {
			log.Println("Redis cache connected successfully")
		}
	}

	var metricsCollector *metrics.MetricsCollector
	if cfg.EnableMetrics {
		metricsCollector = metrics.NewMetricsCollector()
		log.Println("Prometheus metrics collector initialized")
	}

	producer, err := rabbitmq.NewProducer(cfg.RabbitMQURL, cfg.RabbitMQQueue, cfg.RabbitMQDiff)
	if err != nil {
		log.Fatalf("Failed to create RabbitMQ producer: %v", err)
	}
	defer producer.Close()
	log.Println("RabbitMQ producer connected")

	wsMgr := websocket.NewManager(cfg.WSBuffer)

	consumer, err := rabbitmq.NewResultConsumer(cfg.RabbitMQURL, cfg.RabbitMQResult, repo, wsMgr, metricsCollector)
	if err != nil {
		log.Fatalf("Failed to create RabbitMQ consumer: %v", err)
	}
	defer consumer.Stop()

	if err := consumer.Start(); err != nil {
		log.Fatalf("Failed to start result consumer: %v", err)
	}
	log.Println("RabbitMQ result consumer started")

	if metricsCollector != nil {
		consumer.StartQueueMonitor(producer, cfg.RabbitMQResult, 5*time.Second)
		log.Println("Queue monitor started")
	}

	diffConsumer, err := rabbitmq.NewDiffResultConsumer(cfg.RabbitMQURL, cfg.RabbitMQDiff+"_results", repo, wsMgr, metricsCollector)
	if err != nil {
		log.Printf("WARNING: Failed to create diff result consumer: %v", err)
	} else {
		if err := diffConsumer.Start(); err != nil {
			log.Printf("WARNING: Failed to start diff consumer: %v", err)
		} else {
			defer diffConsumer.Stop()
			log.Println("RabbitMQ diff result consumer started")
		}
	}

	h := handlers.NewHandler(repo, producer, cacheSvc, metricsCollector, cfg.EnableCache && cacheSvc != nil)

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
		api.POST("/analyses/batch-upload", h.BatchUpload)
		api.GET("/analyses", h.ListAnalyses)
		api.GET("/analyses/:id", h.GetAnalysis)
		api.GET("/analyses/:id/ws", wsMgr.HandleConnection)

		api.POST("/differential/compare", h.DifferentialCompare)
		api.GET("/differential", h.ListDifferentialComparisons)
		api.GET("/differential/:id", h.GetDifferentialComparison)

		api.GET("/cache/stats", h.GetCacheStats)
		api.GET("/metrics/summary", h.GetMetricsSummary)
	}

	if metricsCollector != nil {
		r.GET("/metrics", gin.WrapH(metricsCollector.Handler()))
		log.Printf("Prometheus metrics endpoint available at /metrics")
	}

	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	var metricsSrv *http.Server
	if cfg.EnableMetrics && metricsCollector != nil {
		metricsAddr := fmt.Sprintf(":%d", cfg.MetricsPort)
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", metricsCollector.Handler())
		metricsSrv = &http.Server{
			Addr:    metricsAddr,
			Handler: metricsMux,
		}
		go func() {
			log.Printf("Metrics server starting on %s", metricsAddr)
			if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("Metrics server error: %v", err)
			}
		}()
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

	if metricsSrv != nil {
		metricsSrv.Shutdown(ctx)
	}

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited gracefully")
}
