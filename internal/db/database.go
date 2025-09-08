package db

import (
	"fmt"
	"gpt-load/internal/types"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func NewDB(configManager types.ConfigManager) (*gorm.DB, error) {
	// -------------------------------------------------
	// 1️⃣ 读取配置
	// -------------------------------------------------
	dbConfig := configManager.GetDatabaseConfig()
	dsn := dbConfig.DSN
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_DSN is not configured")
	}

	// -------------------------------------------------
	// 2️⃣ 日志初始化（保持原有业务逻辑）
	// -------------------------------------------------
	var newLogger logger.Interface
	if configManager.GetLogConfig().Level == "debug" {
		newLogger = logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
			logger.Config{
				SlowThreshold:             time.Second,
				LogLevel:                  logger.Info,
				IgnoreRecordNotFoundError: true,
				Colorful:                  true,
			},
		)
	}

	// -------------------------------------------------
	// 3️⃣ 数据库 Dialector 选型
	// -------------------------------------------------
	var dialector gorm.Dialector

	switch {
	// ---------- PostgreSQL ----------
	case strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://"):
		dialector = postgres.New(postgres.Config{
			DSN:                  dsn,
			PreferSimpleProtocol: true,
		})

	// ---------- MySQL ----------
	case strings.Contains(dsn, "@tcp"):
		if !strings.Contains(dsn, "parseTime") {
			if strings.Contains(dsn, "?") {
				dsn += "&parseTime=true"
			} else {
				dsn += "?parseTime=true"
			}
		}
		dialector = mysql.Open(dsn)

	// ---------- SQLite ----------
	default:
		// ----- 3.1️⃣ 让 SQLite 的文件落在可写目录 ----------
		// 如果用户只给了文件名（如 "gpt.db"）或相对路径，我们把它拼到
		// 环境变量 DB_PATH（默认 /tmp/data）下面。若已经是绝对路径
		// 则直接使用。
		dbPathEnv := os.Getenv("DB_PATH")
		if dbPathEnv == "" {
			dbPathEnv = "/tmp/data"
		}

		// 如果 dsn 已经是一个目录+文件名，直接使用 dirname；
		// 否则把它当作文件名，拼到 DB_PATH。
		var sqliteFile string
		if filepath.IsAbs(dsn) {
			// 绝对路径 → 直接使用
			sqliteFile = dsn
		} else {
			// 相对路径或仅文件名 → 强制放到 DB_PATH 下
			// 若 dsn 包含目录（e.g. "data/gpt.db"），我们保留目录层级
			// 但根目录一定是 DB_PATH
			relDir := filepath.Dir(dsn)
			if relDir == "." {
				// 只给了文件名
				sqliteFile = filepath.Join(dbPathEnv, dsn)
			} else {
				// 给了子目录，先确保子目录在 DB_PATH 中
				sqliteFile = filepath.Join(dbPathEnv, relDir, filepath.Base(dsn))
			}
		}

		// ----- 3.2️⃣ 确保目录可写 ----------
		if err := os.MkdirAll(filepath.Dir(sqliteFile), 0755); err != nil {
			return nil, fmt.Errorf("failed to create SQLite directory %s: %w", filepath.Dir(sqliteFile), err)
		}

		// 最终的 DSN：sqlite 文件 + busy‑timeout 参数
		dialector = sqlite.Open(sqliteFile + "?_busy_timeout=5000")
	}

	// -------------------------------------------------
	// 4️⃣ 打开数据库
	// -------------------------------------------------
	var err error
	DB, err = gorm.Open(dialector, &gorm.Config{
		Logger:      newLogger,
		PrepareStmt: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// -------------------------------------------------
	// 5️⃣ 统一的连接池设置
	// -------------------------------------------------
	sqlDB, err := DB.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}
	sqlDB.SetMaxIdleConns(50)
	sqlDB.SetMaxOpenConns(500)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return DB, nil
}
