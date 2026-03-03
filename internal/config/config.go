package config

import (
	"fmt"
	"os"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	ServerPort string `env:"SERVER_PORT" envDefault:"8081"`

	Minio struct {
		Endpoint  string `env:"ENDPOINT" envDefault:"localhost:9000"`
		AccessKey string `env:"ACCESS_KEY,required" envDefault:"minioadmin"`
		SecretKey string `env:"SECRET_KEY,required" envDefault:"minioadmin"`
		Bucket    string `env:"BUCKET,required" envDefault:"files"`
		UseSSL    bool   `env:"USE_SSL" envDefault:"false"`
	} `envPrefix:"MINIO_"`

	RabbitMQ struct {
		URL string `env:"URL" envDefault:"amqp://guest:guest@localhost:5672/"`
	} `envPrefix:"RABBITMQ_"`

	OpenAI struct {
		APIKey  string `env:"API_KEY,required" envDefault:""`
		BaseURL string `env:"BASE_URL,required" envDefault:"https://api.openai.com/v1/"`
	} `envPrefix:"OPENAI_"`
}

func Load() (*Config, error) {
	if _, err := os.Stat("./.env"); err == nil {
		if err := godotenv.Load(".env"); err != nil {
			return nil, fmt.Errorf("error loading .env file: %w", err)
		}
	}

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("error loading config: %w", err)
	}

	return cfg, nil
}
