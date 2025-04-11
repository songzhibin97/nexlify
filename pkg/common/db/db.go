package db

import (
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/songzhibin97/nexlify/pkg/common/config"
	"github.com/songzhibin97/nexlify/pkg/common/log"
)

type DB struct {
	*sqlx.DB
	Type string
}

func NewDB(cfg *config.DBConfig) (*DB, error) {
	var driverName string
	switch cfg.Type {
	case "mysql":
		driverName = "mysql"
	case "postgres":
		driverName = "postgres"
	default:
		log.Fatal("Unsupported database type", "type", cfg.Type)
	}

	db, err := sqlx.Open(driverName, cfg.DSN)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)

	if err := db.Ping(); err != nil {
		return nil, err
	}

	if err := initSchema(db, cfg.Type); err != nil {
		return nil, err
	}

	log.Info("Database connected", "type", cfg.Type)
	return &DB{DB: db, Type: cfg.Type}, nil
}

func initSchema(db *sqlx.DB, dbType string) error {
	var agentsSchema, tasksSchema string

	switch dbType {
	case "mysql":
		agentsSchema = `
		CREATE TABLE IF NOT EXISTS agents (
			agent_id VARCHAR(255) PRIMARY KEY,
			hostname VARCHAR(255),
			ip VARCHAR(45),
			version VARCHAR(50),
			api_token TEXT,
			created_at BIGINT,
			updated_at BIGINT DEFAULT NULL,
			last_heartbeat BIGINT DEFAULT NULL,
			status VARCHAR(50) DEFAULT 'ONLINE',
			supported_task_types TEXT,
			heartbeat_failures INT DEFAULT 0  
		)`
		tasksSchema = `
		CREATE TABLE IF NOT EXISTS tasks (
			task_id VARCHAR(255) PRIMARY KEY,
			task_type VARCHAR(50),
			parameters TEXT,
			status VARCHAR(50),
			progress INT,
			message TEXT,
			result TEXT,
			created_at BIGINT,
			updated_at BIGINT,
			scheduled_at BIGINT DEFAULT NULL,
			priority INT DEFAULT 5,
			timeout_seconds INT DEFAULT 0,
			retry_count INT DEFAULT 0,
			max_retries INT DEFAULT 0,
			assigned_agent_id VARCHAR(255) DEFAULT NULL,
			version INT DEFAULT 0
		)`
	case "postgres":
		agentsSchema = `
		CREATE TABLE IF NOT EXISTS agents (
			agent_id VARCHAR(255) PRIMARY KEY,
			hostname VARCHAR(255),
			ip VARCHAR(45),
			version VARCHAR(50),
			api_token TEXT,
			created_at BIGINT,
			updated_at BIGINT DEFAULT NULL,
			last_heartbeat BIGINT DEFAULT NULL,
			status VARCHAR(50) DEFAULT 'ONLINE',
			supported_task_types TEXT,
			heartbeat_failures INTEGER DEFAULT 0  
		)`
		tasksSchema = `
		CREATE TABLE IF NOT EXISTS tasks (
			task_id VARCHAR(255) PRIMARY KEY,
			task_type VARCHAR(50),
			parameters TEXT,
			status VARCHAR(50),
			progress INTEGER,
			message TEXT,
			result TEXT,
			created_at BIGINT,
			updated_at BIGINT,
			scheduled_at BIGINT DEFAULT NULL,
			priority INTEGER DEFAULT 5,
			timeout_seconds INTEGER DEFAULT 0,
			retry_count INTEGER DEFAULT 0,
			max_retries INTEGER DEFAULT 0,
			assigned_agent_id VARCHAR(255) DEFAULT NULL,
			version INTEGER DEFAULT 0
		)`
	}

	if _, err := db.Exec(agentsSchema); err != nil {
		log.Error("Failed to create agents table", "type", dbType, "error", err)
		return err
	}

	if _, err := db.Exec(tasksSchema); err != nil {
		log.Error("Failed to create tasks table", "type", dbType, "error", err)
		return err
	}

	log.Info("Database schema initialized", "type", dbType)
	return nil
}
