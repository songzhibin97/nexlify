package config

import (
	"fmt"

	"github.com/songzhibin97/nexlify/pkg/common/log"
	"github.com/spf13/viper"
)

type ServerConfig struct {
	Port         string   `mapstructure:"port"`
	HTTPPort     string   `mapstructure:"http_port"`
	Database     DBConfig `mapstructure:"database"`
	LoggingLevel string   `mapstructure:"level"`
}

type AgentConfig struct {
	ServerAddr        string   `mapstructure:"server_addr"`
	HeartbeatInterval int      `mapstructure:"heartbeat_interval"`
	Database          DBConfig `mapstructure:"database"`
	LoggingLevel      string   `mapstructure:"level"`
	AgentID           string   `mapstructure:"agent_id"` // 新增字段
}

type DBConfig struct {
	Type string `mapstructure:"type"`
	DSN  string `mapstructure:"dsn"`
}

func LoadServerConfig() (*ServerConfig, error) {
	viper.SetConfigName("server")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("config/")
	viper.AddConfigPath(".")

	viper.AutomaticEnv()
	viper.SetEnvPrefix("NEXLIFY")
	viper.BindEnv("server.port", "NEXLIFY_SERVER_PORT")
	viper.BindEnv("server.http_port", "NEXLIFY_SERVER_HTTP_PORT")
	viper.BindEnv("server.database.type", "NEXLIFY_DB_TYPE")
	viper.BindEnv("server.database.dsn", "NEXLIFY_DB_DSN")
	viper.BindEnv("logging.level", "NEXLIFY_LOGGING_LEVEL")

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
	log.Info("Loaded server config", "port", cfg.Port, "http_port", cfg.HTTPPort, "db_type", cfg.Database.Type)
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
	viper.BindEnv("agent.database.type", "NEXLIFY_DB_TYPE")
	viper.BindEnv("agent.database.dsn", "NEXLIFY_DB_DSN")
	viper.BindEnv("logging.level", "NEXLIFY_LOGGING_LEVEL")

	// 设置默认值
	viper.SetDefault("agent.heartbeat_interval", 30)

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
	log.Info("Loaded agent config", "server_addr", cfg.ServerAddr, "db_type", cfg.Database.Type, "agent_id", cfg.AgentID)
	return &cfg, nil
}
