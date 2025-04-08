package log

import (
	"log/slog"
	"os"
	"sync"
)

var (
	logger *slog.Logger // 全局日志实例
	once   sync.Once    // 确保单例初始化
)

func init() {
	// 默认初始化日志实例
	once.Do(func() {
		handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo, // 默认级别为 Info
		})
		logger = slog.New(handler)
		slog.SetDefault(logger)
	})
}

// Init 初始化全局日志实例
func Init(level string, jsonFormat bool) {
	once.Do(func() {
		var slogLevel slog.Level
		switch level {
		case "debug":
			slogLevel = slog.LevelDebug
		case "info":
			slogLevel = slog.LevelInfo
		case "warn":
			slogLevel = slog.LevelWarn
		case "error":
			slogLevel = slog.LevelError
		default:
			slogLevel = slog.LevelInfo
		}

		var handler slog.Handler
		if jsonFormat {
			handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel})
		} else {
			handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel})
		}

		logger = slog.New(handler)
		slog.SetDefault(logger)
	})
}

// Replace 替换全局日志实例
func Replace(newLogger *slog.Logger) {
	logger = newLogger
	slog.SetDefault(logger)
}

// Debug 记录调试日志
func Debug(msg string, args ...any) {
	logger.Debug(msg, args...)
}

// Info 记录信息日志
func Info(msg string, args ...any) {
	logger.Info(msg, args...)
}

// Warn 记录警告日志
func Warn(msg string, args ...any) {
	logger.Warn(msg, args...)
}

// Error 记录错误日志
func Error(msg string, args ...any) {
	logger.Error(msg, args...)
}

// Fatal 记录致命错误并退出
func Fatal(msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}
