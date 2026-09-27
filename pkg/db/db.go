// Package db 构造可复用的 PostgreSQL database/sql 连接池。
//
// 只依赖一个朴素的 Config 结构体而不是进程内部的 config proto，
// 使多个服务能共用同一份实现，pkg 不反向依赖 app。
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/eagle-go/eagle/pkg/healthx"

	// 注册 pgx 的 database/sql 驱动。
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Config 是连接池参数。零值字段会落到下面的默认值。
type Config struct {
	DSN             string
	MaxConns        int32
	MaxIdleConns    int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// New 建立连接池并做一次连通性探测。
// 返回的 cleanup 需由应用装配层在退出时执行。
func New(ctx context.Context, cfg Config) (*sql.DB, func(), error) {
	sqlDB, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres: %w", err)
	}

	if cfg.MaxConns > 0 {
		sqlDB.SetMaxOpenConns(int(cfg.MaxConns))
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(int(cfg.MaxIdleConns))
	}
	if cfg.MaxConnLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.MaxConnLifetime)
	}
	if cfg.MaxConnIdleTime > 0 {
		sqlDB.SetConnMaxIdleTime(cfg.MaxConnIdleTime)
	}

	// 启动即失败优于跑起来之后第一个请求才发现连不上库
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("ping postgres: %w", err)
	}

	return sqlDB, func() { _ = sqlDB.Close() }, nil
}

// NewMonitored 建立连接池并把 PostgreSQL 就绪状态注册到进程健康检查。
func NewMonitored(ctx context.Context, cfg Config, health *healthx.Registry) (*sql.DB, func(), error) {
	sqlDB, closeDB, err := New(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	unregister := func() {}
	if health != nil {
		unregister = health.Register("postgres", func(ctx context.Context) error {
			return sqlDB.PingContext(ctx)
		})
	}
	cleanup := func() {
		unregister()
		slog.Info("closing database")
		closeDB()
	}
	return sqlDB, cleanup, nil
}
