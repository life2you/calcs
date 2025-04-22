package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/life2you_mini/calcs/internal/config"
	"github.com/life2you_mini/calcs/internal/services"
)

var (
	configFile = flag.String("config", "config/config.yaml", "配置文件路径")
)

func main() {
	// 解析命令行参数
	flag.Parse()

	// 初始化日志
	logger, err := initLogger()
	if err != nil {
		fmt.Printf("初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// 加载配置
	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		logger.Fatal("加载配置失败", zap.Error(err))
	}
	logger.Info("加载配置成功", zap.String("配置文件", *configFile))

	// 创建上下文，用于处理信号
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 设置信号处理
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	// 创建服务
	service, err := services.NewcalcsService(ctx, cfg, logger)
	if err != nil {
		logger.Fatal("创建服务失败", zap.Error(err))
	}

	// 启动服务
	service.Start()
	logger.Info("服务已启动")

	// 等待终止信号
	sig := <-signalChan
	logger.Info("接收到信号，准备关闭服务", zap.String("signal", sig.String()))

	// 创建关闭超时上下文
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// 停止服务
	if err := service.Stop(shutdownCtx); err != nil {
		logger.Error("服务关闭失败", zap.Error(err))
		os.Exit(1)
	}

	logger.Info("服务已优雅关闭")
}

// 初始化日志
func initLogger() (*zap.Logger, error) {
	// 使用开发环境配置，输出更易读的格式
	config := zap.NewDevelopmentConfig()
	// 可以选择性地保留或修改时间格式，开发配置默认使用不同的格式
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder        // 保留 ISO8601 时间格式
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder // 使用带颜色的级别显示
	return config.Build()
}
