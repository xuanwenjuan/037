package config

import (
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerPort     int
	DBHost         string
	DBPort         int
	DBUser         string
	DBPassword     string
	DBName         string
	RabbitMQURL    string
	RabbitMQQueue  string
	RabbitMQResult string
	RabbitMQDiff   string
	WSBuffer       int
	RedisURL       string
	CacheTTL       time.Duration
	MetricsPort    int
	EnableCache    bool
	EnableMetrics  bool
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	serverPort, _ := strconv.Atoi(getEnv("SERVER_PORT", "8080"))
	dbPort, _ := strconv.Atoi(getEnv("DB_PORT", "5432"))
	wsBuffer, _ := strconv.Atoi(getEnv("WS_UPGRADE_BUFFER", "1024"))
	metricsPort, _ := strconv.Atoi(getEnv("METRICS_PORT", "9090"))
	cacheTTLSeconds, _ := strconv.Atoi(getEnv("CACHE_TTL_SECONDS", "86400"))
	enableCache, _ := strconv.ParseBool(getEnv("ENABLE_CACHE", "true"))
	enableMetrics, _ := strconv.ParseBool(getEnv("ENABLE_METRICS", "true"))

	return &Config{
		ServerPort:     serverPort,
		DBHost:         getEnv("DB_HOST", "localhost"),
		DBPort:         dbPort,
		DBUser:         getEnv("DB_USER", "thz_user"),
		DBPassword:     getEnv("DB_PASSWORD", "thz_password"),
		DBName:         getEnv("DB_NAME", "thz_db"),
		RabbitMQURL:    getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		RabbitMQQueue:  getEnv("RABBITMQ_QUEUE", "thz_tasks"),
		RabbitMQResult: getEnv("RABBITMQ_RESULT_QUEUE", "thz_results"),
		RabbitMQDiff:   getEnv("RABBITMQ_DIFF_QUEUE", "thz_diff_tasks"),
		WSBuffer:       wsBuffer,
		RedisURL:       getEnv("REDIS_URL", "redis://localhost:6379/0"),
		CacheTTL:       time.Duration(cacheTTLSeconds) * time.Second,
		MetricsPort:    metricsPort,
		EnableCache:    enableCache,
		EnableMetrics:  enableMetrics,
	}, nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
