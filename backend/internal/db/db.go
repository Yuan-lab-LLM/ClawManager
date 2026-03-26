package db

import (
	"fmt"
	"log"

	"clawreef/internal/config"
	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
)

// Session holds the database session
var Session db.Session

// Initialize initializes the database connection
func Initialize(cfg config.DatabaseConfig) (db.Session, error) {
	hostPort := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	settings := mysql.ConnectionURL{
		Host:     hostPort,
		User:     cfg.User,
		Password: cfg.Password,
		Database: cfg.Database,
		Options: map[string]string{
			"charset":   "utf8mb4",
			"collation": "utf8mb4_unicode_ci",
			"parseTime": "true",
		},
	}

	session, err := mysql.Open(settings)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	if _, err := session.SQL().Exec("SET NAMES utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("failed to configure database connection charset: %w", err)
	}

	Session = session
	log.Println("Database connected successfully")
	return session, nil
}

// Close closes the database connection
func Close() error {
	if Session != nil {
		return Session.Close()
	}
	return nil
}

// GetSession returns the current database session
func GetSession() db.Session {
	return Session
}
