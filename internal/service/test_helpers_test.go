package service

import (
	"go.uber.org/zap"
)

// testLogger 创建测试用的 logger
func testLogger() *zap.SugaredLogger {
	logger, _ := zap.NewDevelopment()
	return logger.Sugar()
}
