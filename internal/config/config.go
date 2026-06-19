package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	EncryptionKey  string
	Listen         ListenConfig
	Database       DatabaseConfig
	Auth           AuthConfig
	FrontendDir    string
	EditorBaseURL  string
	WebhookBaseURL string
	BinaryData     BinaryDataConfig
	Execution      ExecutionConfig
	Instance       InstanceConfig
	Scheduler      SchedulerConfig
}

type ListenConfig struct {
	Host     string
	Port     int
	Protocol string
	Path     string
}

type DatabaseConfig struct {
	Type        string
	SQLitePath  string
	PostgresDSN string
}

type AuthConfig struct {
	CookieSecure   bool
	CookieSameSite string
	JWTHeader      string
	JWTPrefix      string
	JWTDuration    time.Duration
	SetupEmail     string
	SetupPassword  string
	SetupFirstName string
	SetupLastName  string
}

type BinaryDataConfig struct {
	Mode              string
	Path              string
	S3Bucket          string
	S3Region          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3Endpoint        string
	S3ForcePathStyle  bool
	S3KeyPrefix       string
	S3UseSSL          bool
}

type ExecutionConfig struct {
	SaveOnError                 string
	SaveOnSuccess               string
	SaveManual                  bool
	SaveProgress                bool
	TimeoutSeconds              int
	MaxTimeoutSeconds           int
	ConcurrencyLimit            int
	ConcurrencyPerWorkflowLimit int
	ConcurrencyQueueSize        int
	ConcurrencyAcquireTimeout   time.Duration
	DispatcherMode              string
	DispatcherRedisAddr         string
	DispatcherRedisPassword     string
	DispatcherRedisDB           int
	DispatcherNATSURL           string
	DispatcherStream            string
	DispatcherConsumer          string
}

type InstanceConfig struct {
	ID       string
	LogLevel string
	Timezone string
	Locale   string
}

type SchedulerConfig struct {
	LeaderEnabled       bool
	LeaderRedisAddr     string
	LeaderRedisPassword string
	LeaderRedisDB       int
	LeaderKey           string
	LeaderTTL           time.Duration
}

func Load() (Config, error) {
	encryptionKey := strings.TrimSpace(os.Getenv("N8N_ENCRYPTION_KEY"))
	if encryptionKey == "" {
		return Config{}, fmt.Errorf("N8N_ENCRYPTION_KEY is required")
	}

	frontendDir := firstNonEmpty(
		os.Getenv("N8N_TURBO_FRONTEND_DIR"),
		os.Getenv("UI_PATH"),
		defaultFrontendDir(),
	)
	schedulerRedisAddr := schedulerRedisAddrFromEnv()
	dispatcherRedisAddr := dispatcherRedisAddrFromEnv()

	cfg := Config{
		EncryptionKey: encryptionKey,
		Listen: ListenConfig{
			Host:     firstNonEmpty(os.Getenv("N8N_HOST"), "127.0.0.1"),
			Port:     envInt("N8N_PORT", 5678),
			Protocol: firstNonEmpty(os.Getenv("N8N_PROTOCOL"), "http"),
			Path:     firstNonEmpty(os.Getenv("N8N_PATH"), "/"),
		},
		Database: DatabaseConfig{
			Type:        strings.ToLower(firstNonEmpty(os.Getenv("DB_TYPE"), "sqlite")),
			SQLitePath:  filepath.Clean(firstNonEmpty(os.Getenv("DB_SQLITE_DATABASE"), filepath.Join(".", "data", "database.sqlite"))),
			PostgresDSN: postgresDSNFromEnv(),
		},
		Auth: AuthConfig{
			CookieSecure:   envBool("N8N_AUTH_COOKIE_SECURE", false),
			CookieSameSite: firstNonEmpty(os.Getenv("N8N_AUTH_COOKIE_SAMESITE"), "lax"),
			JWTHeader:      firstNonEmpty(os.Getenv("JWT_AUTH_HEADER"), "authorization"),
			JWTPrefix:      firstNonEmpty(os.Getenv("JWT_AUTH_HEADER_VALUE_PREFIX"), "Bearer"),
			JWTDuration:    time.Duration(envInt("N8N_JWT_DURATION_HOURS", 168)) * time.Hour,
			SetupEmail:     firstNonEmpty(os.Getenv("N8N_SETUP_EMAIL"), "owner@n8n.local"),
			SetupPassword:  firstNonEmpty(os.Getenv("N8N_SETUP_PASSWORD"), "n8n-turbo"),
			SetupFirstName: firstNonEmpty(os.Getenv("N8N_SETUP_FIRST_NAME"), "Owner"),
			SetupLastName:  firstNonEmpty(os.Getenv("N8N_SETUP_LAST_NAME"), "User"),
		},
		FrontendDir:    frontendDir,
		EditorBaseURL:  firstNonEmpty(os.Getenv("N8N_EDITOR_BASE_URL"), ""),
		WebhookBaseURL: firstNonEmpty(os.Getenv("WEBHOOK_URL"), ""),
		BinaryData: BinaryDataConfig{
			Mode:              strings.ToLower(firstNonEmpty(os.Getenv("N8N_DEFAULT_BINARY_DATA_MODE"), "filesystem")),
			Path:              filepath.Clean(firstNonEmpty(os.Getenv("N8N_TURBO_BINARY_DATA_PATH"), filepath.Join(".", "storage", "binary"))),
			S3Bucket:          firstNonEmpty(os.Getenv("N8N_TURBO_BINARY_DATA_S3_BUCKET"), os.Getenv("N8N_BINARY_DATA_S3_BUCKET_NAME")),
			S3Region:          firstNonEmpty(os.Getenv("N8N_TURBO_BINARY_DATA_S3_REGION"), os.Getenv("N8N_BINARY_DATA_S3_BUCKET_REGION"), "us-east-1"),
			S3AccessKeyID:     firstNonEmpty(os.Getenv("N8N_TURBO_BINARY_DATA_S3_ACCESS_KEY_ID"), os.Getenv("N8N_BINARY_DATA_S3_ACCESS_KEY")),
			S3SecretAccessKey: firstNonEmpty(os.Getenv("N8N_TURBO_BINARY_DATA_S3_SECRET_ACCESS_KEY"), os.Getenv("N8N_BINARY_DATA_S3_SECRET_KEY")),
			S3Endpoint:        firstNonEmpty(os.Getenv("N8N_TURBO_BINARY_DATA_S3_ENDPOINT"), os.Getenv("N8N_BINARY_DATA_S3_ENDPOINT")),
			S3ForcePathStyle:  envBool("N8N_TURBO_BINARY_DATA_S3_FORCE_PATH_STYLE", envBool("N8N_BINARY_DATA_S3_FORCE_PATH_STYLE", false)),
			S3KeyPrefix:       firstNonEmpty(os.Getenv("N8N_TURBO_BINARY_DATA_S3_KEY_PREFIX"), os.Getenv("N8N_BINARY_DATA_S3_KEY_PREFIX"), "n8n-binary"),
			S3UseSSL:          envBool("N8N_TURBO_BINARY_DATA_S3_USE_SSL", envBool("N8N_BINARY_DATA_S3_USE_SSL", true)),
		},
		Execution: ExecutionConfig{
			SaveOnError:                 firstNonEmpty(os.Getenv("N8N_EXECUTIONS_DATA_SAVE_ON_ERROR"), "all"),
			SaveOnSuccess:               firstNonEmpty(os.Getenv("N8N_EXECUTIONS_DATA_SAVE_ON_SUCCESS"), "all"),
			SaveManual:                  envBool("N8N_EXECUTIONS_DATA_SAVE_MANUAL_EXECUTIONS", true),
			SaveProgress:                envBool("N8N_EXECUTIONS_DATA_SAVE_ON_PROGRESS", false),
			TimeoutSeconds:              envInt("EXECUTIONS_TIMEOUT", -1),
			MaxTimeoutSeconds:           envInt("EXECUTIONS_TIMEOUT_MAX", 3600),
			ConcurrencyLimit:            envInt("N8N_CONCURRENCY_PRODUCTION_LIMIT", 0),
			ConcurrencyPerWorkflowLimit: envInt("N8N_TURBO_CONCURRENCY_PER_WORKFLOW_LIMIT", 0),
			ConcurrencyQueueSize:        envInt("N8N_TURBO_CONCURRENCY_QUEUE_SIZE", 1000),
			ConcurrencyAcquireTimeout:   time.Duration(envInt("N8N_TURBO_CONCURRENCY_ACQUIRE_TIMEOUT_SECONDS", 300)) * time.Second,
			DispatcherMode:              strings.ToLower(firstNonEmpty(os.Getenv("N8N_TURBO_EXECUTION_DISPATCHER"), "local")),
			DispatcherRedisAddr:         dispatcherRedisAddr,
			DispatcherRedisPassword:     firstNonEmpty(os.Getenv("N8N_TURBO_EXECUTION_REDIS_PASSWORD"), os.Getenv("QUEUE_BULL_REDIS_PASSWORD")),
			DispatcherRedisDB:           envIntFirst([]string{"N8N_TURBO_EXECUTION_REDIS_DB", "QUEUE_BULL_REDIS_DB"}, 0),
			DispatcherNATSURL:           firstNonEmpty(os.Getenv("N8N_TURBO_EXECUTION_NATS_URL"), "nats://127.0.0.1:4222"),
			DispatcherStream:            firstNonEmpty(os.Getenv("N8N_TURBO_EXECUTION_STREAM"), "n8n:executions"),
			DispatcherConsumer:          firstNonEmpty(os.Getenv("N8N_TURBO_EXECUTION_CONSUMER"), "n8n-turbo-worker"),
		},
		Instance: InstanceConfig{
			ID:       firstNonEmpty(os.Getenv("N8N_INSTANCE_ID"), "n8n-turbo-local"),
			LogLevel: firstNonEmpty(os.Getenv("N8N_LOG_LEVEL"), "info"),
			Timezone: firstNonEmpty(os.Getenv("GENERIC_TIMEZONE"), "UTC"),
			Locale:   firstNonEmpty(os.Getenv("N8N_DEFAULT_LOCALE"), "en"),
		},
		Scheduler: SchedulerConfig{
			LeaderEnabled:       envBool("N8N_TURBO_SCHEDULER_LEADER_ENABLED", schedulerRedisAddr != ""),
			LeaderRedisAddr:     schedulerRedisAddr,
			LeaderRedisPassword: firstNonEmpty(os.Getenv("N8N_TURBO_SCHEDULER_LEADER_REDIS_PASSWORD"), os.Getenv("QUEUE_BULL_REDIS_PASSWORD")),
			LeaderRedisDB:       envIntFirst([]string{"N8N_TURBO_SCHEDULER_LEADER_REDIS_DB", "QUEUE_BULL_REDIS_DB"}, 0),
			LeaderKey:           firstNonEmpty(os.Getenv("N8N_TURBO_SCHEDULER_LEADER_KEY"), "n8n-turbo:scheduler:leader"),
			LeaderTTL:           time.Duration(envInt("N8N_TURBO_SCHEDULER_LEADER_TTL_SECONDS", 30)) * time.Second,
		},
	}

	switch cfg.Database.Type {
	case "sqlite":
	case "postgres", "postgresdb":
		if strings.TrimSpace(cfg.Database.PostgresDSN) == "" {
			return Config{}, fmt.Errorf("postgres DB_TYPE requires DB_POSTGRESDB_CONNECTION_URL or DB_POSTGRESDB_HOST")
		}
	default:
		return Config{}, fmt.Errorf("unsupported DB_TYPE %q", cfg.Database.Type)
	}
	if cfg.Scheduler.LeaderEnabled && strings.TrimSpace(cfg.Scheduler.LeaderRedisAddr) == "" {
		return Config{}, fmt.Errorf("scheduler Redis leader requires N8N_TURBO_SCHEDULER_LEADER_REDIS_ADDR or QUEUE_BULL_REDIS_HOST")
	}
	switch cfg.Execution.DispatcherMode {
	case "local":
	case "redis":
		if strings.TrimSpace(cfg.Execution.DispatcherRedisAddr) == "" {
			return Config{}, fmt.Errorf("redis execution dispatcher requires N8N_TURBO_EXECUTION_REDIS_ADDR or QUEUE_BULL_REDIS_HOST")
		}
	case "nats":
		if strings.TrimSpace(cfg.Execution.DispatcherNATSURL) == "" {
			return Config{}, fmt.Errorf("nats execution dispatcher requires N8N_TURBO_EXECUTION_NATS_URL")
		}
	default:
		return Config{}, fmt.Errorf("unsupported N8N_TURBO_EXECUTION_DISPATCHER %q", cfg.Execution.DispatcherMode)
	}
	switch cfg.BinaryData.Mode {
	case "filesystem", "default":
		cfg.BinaryData.Mode = "filesystem"
	case "memory":
	case "s3":
		if strings.TrimSpace(cfg.BinaryData.S3Bucket) == "" {
			return Config{}, fmt.Errorf("s3 binary data mode requires N8N_TURBO_BINARY_DATA_S3_BUCKET")
		}
	default:
		return Config{}, fmt.Errorf("unsupported N8N_DEFAULT_BINARY_DATA_MODE %q", cfg.BinaryData.Mode)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.Database.SQLitePath), 0o755); err != nil {
		return Config{}, fmt.Errorf("create database directory: %w", err)
	}

	if cfg.BinaryData.Mode == "filesystem" {
		if err := os.MkdirAll(cfg.BinaryData.Path, 0o755); err != nil {
			return Config{}, fmt.Errorf("create binary data directory: %w", err)
		}
	}

	return cfg, nil
}

func postgresDSNFromEnv() string {
	if dsn := strings.TrimSpace(os.Getenv("DB_POSTGRESDB_CONNECTION_URL")); dsn != "" {
		return dsn
	}
	host := strings.TrimSpace(os.Getenv("DB_POSTGRESDB_HOST"))
	if host == "" {
		return ""
	}
	port := envInt("DB_POSTGRESDB_PORT", 5432)
	database := firstNonEmpty(os.Getenv("DB_POSTGRESDB_DATABASE"), "n8n")
	user := firstNonEmpty(os.Getenv("DB_POSTGRESDB_USER"), "n8n")
	password := os.Getenv("DB_POSTGRESDB_PASSWORD")
	sslMode := firstNonEmpty(os.Getenv("DB_POSTGRESDB_SSL_MODE"), "disable")
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s", urlQueryEscape(user), urlQueryEscape(password), host, port, database, sslMode)
}

func schedulerRedisAddrFromEnv() string {
	if addr := strings.TrimSpace(os.Getenv("N8N_TURBO_SCHEDULER_LEADER_REDIS_ADDR")); addr != "" {
		return addr
	}
	host := strings.TrimSpace(os.Getenv("QUEUE_BULL_REDIS_HOST"))
	if host == "" {
		host = strings.TrimSpace(os.Getenv("REDIS_HOST"))
	}
	if host == "" {
		return ""
	}
	port := envIntFirst([]string{"QUEUE_BULL_REDIS_PORT", "REDIS_PORT"}, 6379)
	return fmt.Sprintf("%s:%d", host, port)
}

func dispatcherRedisAddrFromEnv() string {
	if addr := strings.TrimSpace(os.Getenv("N8N_TURBO_EXECUTION_REDIS_ADDR")); addr != "" {
		return addr
	}
	return schedulerRedisAddrFromEnv()
}

func urlQueryEscape(value string) string {
	replacer := strings.NewReplacer("%", "%25", ":", "%3A", "@", "%40", "/", "%2F", "?", "%3F", "#", "%23", "&", "%26", "=", "%3D")
	return replacer.Replace(value)
}

func (l ListenConfig) Address() string {
	return fmt.Sprintf("%s:%d", l.Host, l.Port)
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envIntFirst(keys []string, fallback int) int {
	for _, key := range keys {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err == nil {
			return value
		}
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func defaultFrontendDir() string {
	candidates := []string{
		filepath.Clean("./ui"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil {
			return candidate
		}
	}
	return filepath.Clean("./ui")
}
