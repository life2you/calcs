package logger

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger 封装zap日志器
type Logger struct {
	*zap.Logger
}

// NewLogger 创建新的日志记录器
func NewLogger(logDir string, level string) (*Logger, error) {
	// 确保日志目录存在
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}

	// 解析日志级别
	var logLevel zapcore.Level
	if err := logLevel.UnmarshalText([]byte(level)); err != nil {
		logLevel = zapcore.InfoLevel
	}

	// 创建编码器配置
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// 创建核心组件 - 控制台输出
	consoleCore := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.AddSync(os.Stdout),
		logLevel,
	)

	// 创建核心组件 - 文件输出
	logFilePath := filepath.Join(logDir, "funding_bot.log")
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	fileCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(logFile),
		logLevel,
	)

	// 创建核心组件 - 错误文件输出
	errorLogFilePath := filepath.Join(logDir, "funding_bot_error.log")
	errorLogFile, err := os.OpenFile(errorLogFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	errorFileCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(errorLogFile),
		zapcore.ErrorLevel,
	)

	// 合并核心组件
	core := zapcore.NewTee(consoleCore, fileCore, errorFileCore)

	// 创建日志记录器
	zapLogger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	defer zapLogger.Sync()

	return &Logger{zapLogger}, nil
}

// With 添加固定字段到logger
func (l *Logger) With(fields ...zap.Field) *Logger {
	return &Logger{l.Logger.With(fields...)}
}

// Named 添加子logger名称
func (l *Logger) Named(name string) *Logger {
	return &Logger{l.Logger.Named(name)}
}
