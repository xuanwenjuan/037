package config

import (
	"os"
	"strconv"

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
	WSBuffer       int
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	serverPort, _ := strconv.Atoi(getEnv("SERVER_PORT", "8080"))
	dbPort, _ := strconv.Atoi(getEnv("DB_PORT", "5432"))
	wsBuffer, _ := strconv.Atoi(getEnv("WS_UPGRADE_BUFFER", "1024"))

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
		WSBuffer:       wsBuffer,
	}, nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
