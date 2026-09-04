package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
	"github.com/viggggil/go_oj_agent/services/user/internal/data"
)

const (
	envBootstrapDSN      = "USER_BOOTSTRAP_DSN"
	envBootstrapUsername = "USER_BOOTSTRAP_ADMIN_USERNAME"
	envBootstrapEmail    = "USER_BOOTSTRAP_ADMIN_EMAIL"
	envBootstrapPassword = "USER_BOOTSTRAP_ADMIN_PASSWORD"
)

func main() {
	logger := newJSONLogger(os.Stdout)
	errorLogger := newJSONLogger(os.Stderr)
	if err := run(context.Background(), logger); err != nil {
		errorLogger.Error("admin bootstrap failed", "error", err)
		os.Exit(1)
	}
	logger.Info("admin bootstrap finished")
}

func run(ctx context.Context, logger *slog.Logger) error {
	// bootstrap 只从环境变量读取敏感参数，避免把初始管理员密码写入命令行历史或配置文件。
	input, err := loadBootstrapInput()
	if err != nil {
		return err
	}

	cfg := conf.DefaultConfig()
	hasher := biz.NewBcryptPasswordHasher(cfg.Auth.Password.BcryptCost)
	passwordHash, err := hasher.Hash(input.password)
	if err != nil {
		return err
	}

	db, err := sql.Open("mysql", input.dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	store := data.NewStoreSet(db)
	admin, err := store.BootstrapAdmin(ctx, biz.User{
		Username:     biz.NormalizeUsername(input.username),
		Email:        biz.NormalizeEmail(input.email),
		PasswordHash: passwordHash,
		Status:       biz.UserStatusActive,
	})
	if err != nil {
		if errors.Is(err, biz.ErrAdminAlreadyExists) {
			logger.Info("admin bootstrap skipped", "reason", "admin already exists")
			return nil
		}
		return err
	}

	logger.Info("admin bootstrap created", "user_id", admin.ID, "username", admin.Username)
	return nil
}

type bootstrapInput struct {
	dsn      string
	username string
	email    string
	password string
}

func loadBootstrapInput() (bootstrapInput, error) {
	input := bootstrapInput{
		dsn:      strings.TrimSpace(os.Getenv(envBootstrapDSN)),
		username: strings.TrimSpace(os.Getenv(envBootstrapUsername)),
		email:    strings.TrimSpace(os.Getenv(envBootstrapEmail)),
		password: strings.TrimSpace(os.Getenv(envBootstrapPassword)),
	}
	if input.dsn == "" {
		return bootstrapInput{}, fmt.Errorf("%s is required", envBootstrapDSN)
	}
	if input.username == "" {
		return bootstrapInput{}, fmt.Errorf("%s is required", envBootstrapUsername)
	}
	if input.email == "" {
		return bootstrapInput{}, fmt.Errorf("%s is required", envBootstrapEmail)
	}
	if err := biz.DefaultPasswordPolicy().Validate(input.password); err != nil {
		return bootstrapInput{}, fmt.Errorf("%s is invalid: %w", envBootstrapPassword, err)
	}
	return input, nil
}

func newJSONLogger(output *os.File) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{}))
}
