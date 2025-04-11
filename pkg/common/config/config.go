package config

import (
	"fmt"

	"github.com/songzhibin97/nexlify/pkg/common/log"
	"github.com/spf13/viper"
)

type ServerConfig struct {
	Port         string        `mapstructure:"port"`
	HTTPPort     string        `mapstructure:"http_port"`
	Database     DBConfig      `mapstructure:"database"`
	LoggingLevel string        `mapstructure:"level"`
	Agent        AgentSettings `mapstructure:"agent"` // New
	Task         TaskSettings  `mapstructure:"task"`  // New
}

type AgentConfig struct {
	ServerAddr        string        `mapstructure:"server_addr"`
	HeartbeatInterval int           `mapstructure:"heartbeat_interval"`
	LoggingLevel      string        `mapstructure:"level"`
	AgentID           string        `mapstructure:"agent_id"`
	Timeouts          AgentTimeouts `mapstructure:"timeouts"`
}

type DBConfig struct {
	Type            string `mapstructure:"type"`
	DSN             string `mapstructure:"dsn"`
	MaxOpenConns    int    `mapstructure:"max_open_conns"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetime int    `mapstructure:"conn_max_lifetime"` // (seconds)
}

type AgentSettings struct {
	ActiveTimeout        int `mapstructure:"active_timeout"` // (seconds)
	CheckInterval        int `mapstructure:"check_interval"` // (seconds)
	MaxHeartbeatFailures int `mapstructure:"max_heartbeat_failures"`
}

type TaskSettings struct {
	DefaultTimeout   int `mapstructure:"default_timeout"`   // (seconds)
	InitialBackoff   int `mapstructure:"initial_backoff"`   // (seconds)
	MaxBackoff       int `mapstructure:"max_backoff"`       // (seconds)
	LogSuppression   int `mapstructure:"log_suppression"`   // (seconds)
	ScheduleInterval int `mapstructure:"schedule_interval"` // (seconds)
	DefaultPriority  int `mapstructure:"default_priority"`
	MinPriority      int `mapstructure:"min_priority"`
	MaxPriority      int `mapstructure:"max_priority"`
}

type AgentTimeouts struct {
	RegisterTimeout  int `mapstructure:"register_timeout"`  // (seconds)
	HeartbeatTimeout int `mapstructure:"heartbeat_timeout"` // (seconds)
}

func LoadServerConfig() (*ServerConfig, error) {
	viper.SetConfigName("server")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("config/")
	viper.AddConfigPath(".")

	viper.AutomaticEnv()
	viper.SetEnvPrefix("NEXLIFY")
	// Existing bindings
	viper.BindEnv("server.port", "NEXLIFY_SERVER_PORT")
	viper.BindEnv("server.http_port", "NEXLIFY_SERVER_HTTP_PORT")
	viper.BindEnv("server.database.type", "NEXLIFY_DB_TYPE")
	viper.BindEnv("server.database.dsn", "NEXLIFY_DB_DSN")
	viper.BindEnv("logging.level", "NEXLIFY_LOGGING_LEVEL")
	// New bindings for database
	viper.BindEnv("server.database.max_open_conns", "NEXLIFY_DB_MAX_OPEN_CONNS")
	viper.BindEnv("server.database.max_idle_conns", "NEXLIFY_DB_MAX_IDLE_CONNS")
	viper.BindEnv("server.database.conn_max_lifetime", "NEXLIFY_DB_CONN_MAX_LIFETIME")
	// New bindings for agent settings
	viper.BindEnv("server.agent.active_timeout", "NEXLIFY_AGENT_ACTIVE_TIMEOUT")
	viper.BindEnv("server.agent.check_interval", "NEXLIFY_AGENT_CHECK_INTERVAL")
	viper.BindEnv("server.agent.max_heartbeat_failures", "NEXLIFY_AGENT_MAX_HEARTBEAT_FAILURES")
	// New bindings for task settings
	viper.BindEnv("server.task.default_timeout", "NEXLIFY_TASK_DEFAULT_TIMEOUT")
	viper.BindEnv("server.task.initial_backoff", "NEXLIFY_TASK_INITIAL_BACKOFF")
	viper.BindEnv("server.task.max_backoff", "NEXLIFY_TASK_MAX_BACKOFF")
	viper.BindEnv("server.task.log_suppression", "NEXLIFY_TASK_LOG_SUPPRESSION")
	viper.BindEnv("server.task.schedule_interval", "NEXLIFY_TASK_SCHEDULE_INTERVAL")
	viper.BindEnv("server.task.default_priority", "NEXLIFY_TASK_DEFAULT_PRIORITY")
	viper.BindEnv("server.task.min_priority", "NEXLIFY_TASK_MIN_PRIORITY")
	viper.BindEnv("server.task.max_priority", "NEXLIFY_TASK_MAX_PRIORITY")

	// Set defaults for new fields
	viper.SetDefault("server.database.max_open_conns", 50)
	viper.SetDefault("server.database.max_idle_conns", 10)
	viper.SetDefault("server.database.conn_max_lifetime", 3600) // 1 hour in seconds
	viper.SetDefault("server.agent.active_timeout", 60)         // 60 seconds
	viper.SetDefault("server.agent.check_interval", 30)         // 30 seconds
	viper.SetDefault("server.agent.max_heartbeat_failures", 3)
	viper.SetDefault("server.task.default_timeout", 30)  // 30 seconds
	viper.SetDefault("server.task.initial_backoff", 1)   // 1 second
	viper.SetDefault("server.task.max_backoff", 10)      // 10 seconds
	viper.SetDefault("server.task.log_suppression", 5)   // 5 seconds
	viper.SetDefault("server.task.schedule_interval", 1) // 1 second
	viper.SetDefault("server.task.default_priority", 5)
	viper.SetDefault("server.task.min_priority", 1)
	viper.SetDefault("server.task.max_priority", 10)

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read server config: %v", err)
	}

	var cfg ServerConfig
	if err := viper.UnmarshalKey("server", &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server config: %v", err)
	}
	if err := viper.UnmarshalKey("logging.level", &cfg.LoggingLevel); err != nil {
		return nil, fmt.Errorf("failed to unmarshal logging config: %v", err)
	}

	log.Init(cfg.LoggingLevel, false)
	log.Info("Loaded server config",
		"port", cfg.Port,
		"http_port", cfg.HTTPPort,
		"db_type", cfg.Database.Type,
		"db_max_open_conns", cfg.Database.MaxOpenConns,
		"db_max_idle_conns", cfg.Database.MaxIdleConns,
		"db_conn_max_lifetime", cfg.Database.ConnMaxLifetime,
		"agent_active_timeout", cfg.Agent.ActiveTimeout,
		"agent_check_interval", cfg.Agent.CheckInterval,
		"agent_max_heartbeat_failures", cfg.Agent.MaxHeartbeatFailures,
		"task_default_timeout", cfg.Task.DefaultTimeout,
		"task_initial_backoff", cfg.Task.InitialBackoff,
		"task_max_backoff", cfg.Task.MaxBackoff,
		"task_log_suppression", cfg.Task.LogSuppression,
		"task_schedule_interval", cfg.Task.ScheduleInterval,
		"task_default_priority", cfg.Task.DefaultPriority,
		"task_min_priority", cfg.Task.MinPriority,
		"task_max_priority", cfg.Task.MaxPriority,
	)
	return &cfg, nil
}

func LoadAgentConfig() (*AgentConfig, error) {
	viper.SetConfigName("agent")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("config/")
	viper.AddConfigPath(".")

	viper.AutomaticEnv()
	viper.SetEnvPrefix("NEXLIFY")
	viper.BindEnv("agent.server_addr", "NEXLIFY_SERVER_ADDR")
	viper.BindEnv("agent.heartbeat_interval", "NEXLIFY_HEARTBEAT_INTERVAL")
	viper.BindEnv("agent.timeouts.register_timeout", "NEXLIFY_AGENT_REGISTER_TIMEOUT")
	viper.BindEnv("agent.timeouts.heartbeat_timeout", "NEXLIFY_AGENT_HEARTBEAT_TIMEOUT")
	viper.BindEnv("logging.level", "NEXLIFY_LOGGING_LEVEL")

	viper.SetDefault("agent.heartbeat_interval", 30)
	viper.SetDefault("agent.timeouts.register_timeout", 5)
	viper.SetDefault("agent.timeouts.heartbeat_timeout", 5)

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read agent config: %v", err)
	}

	var cfg AgentConfig
	if err := viper.UnmarshalKey("agent", &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent config: %v", err)
	}
	if err := viper.UnmarshalKey("logging.level", &cfg.LoggingLevel); err != nil {
		return nil, fmt.Errorf("failed to unmarshal logging config: %v", err)
	}

	log.Init(cfg.LoggingLevel, false)
	log.Info("Loaded agent config",
		"server_addr", cfg.ServerAddr,
		"agent_id", cfg.AgentID,
		"heartbeat_interval", cfg.HeartbeatInterval,
		"register_timeout", cfg.Timeouts.RegisterTimeout,
		"heartbeat_timeout", cfg.Timeouts.HeartbeatTimeout,
	)
	return &cfg, nil
}
