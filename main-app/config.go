package main

import (
	"fmt"
	"os"
)

type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

func LoadDBConfig() DBConfig {
	return DBConfig{
		Host:     getEnv("PGHOST", "postgres"), // Изменено с localhost на postgres
		Port:     getEnv("PGPORT", "5432"),
		User:     getEnv("PGUSER", "postgres"),
		Password: getEnv("PGPASSWORD", "example"),
		Database: getEnv("PGDATABASE", "appdb"),
	}
}

func (c DBConfig) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.User,
		c.Password,
		c.Host,
		c.Port,
		c.Database,
	)
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
