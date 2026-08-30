package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const Filename = "config.json"

type Duration struct {
	time.Duration
}

type Config struct {
	Server       Server       `json:"server"`
	Tasks        Tasks        `json:"tasks"`
	Delivery     Delivery     `json:"delivery"`
	Logging      Logging      `json:"logging"`
	Integrations Integrations `json:"integrations"`
}

type Server struct {
	Address     string `json:"address"`
	Port        int    `json:"port"`
	OpenBrowser bool   `json:"open_browser"`
}

type Tasks struct {
	MaxConcurrent   int      `json:"max_concurrent"`
	MaxQueued       int      `json:"max_queued"`
	ShutdownTimeout Duration `json:"shutdown_timeout"`
}

type Delivery struct {
	SendTimeout        Duration `json:"send_timeout"`
	SMTPConnectTimeout Duration `json:"smtp_connect_timeout"`
}

type Logging struct {
	Level string `json:"level"`
	File  string `json:"file"`
}

type Integrations struct {
	Google      Google      `json:"google"`
	LibreOffice LibreOffice `json:"libreoffice"`
}

type Google struct {
	ClientID string `json:"client_id"`
}

type LibreOffice struct {
	Executable   string   `json:"executable"`
	BatchTimeout Duration `json:"batch_timeout"`
}

func Default() Config {
	return Config{
		Server: Server{Address: "127.0.0.1", Port: 0, OpenBrowser: true},
		Tasks: Tasks{
			MaxConcurrent:   4,
			MaxQueued:       8,
			ShutdownTimeout: Duration{Duration: 30 * time.Second},
		},
		Delivery: Delivery{
			SendTimeout:        Duration{Duration: 2 * time.Minute},
			SMTPConnectTimeout: Duration{Duration: 15 * time.Second},
		},
		Logging: Logging{Level: "info"},
		Integrations: Integrations{
			LibreOffice: LibreOffice{BatchTimeout: Duration{Duration: 2 * time.Minute}},
		},
	}
}

func Load(path string) (Config, bool, error) {
	result := Default()
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return Config{}, true, fmt.Errorf("decode configuration: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Config{}, true, err
	}
	return result, true, nil
}

func WriteDefault(path string) error {
	data, err := json.MarshalIndent(Default(), "", "    ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func (cfg *Config) ApplyEnvironment() error {
	if value, ok := os.LookupEnv("BULK_MAIL_SERVER_ADDRESS"); ok {
		cfg.Server.Address = value
	}
	if err := applyIntEnvironment("BULK_MAIL_SERVER_PORT", &cfg.Server.Port); err != nil {
		return err
	}
	if value, ok := os.LookupEnv("BULK_MAIL_OPEN_BROWSER"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("BULK_MAIL_OPEN_BROWSER: %w", err)
		}
		cfg.Server.OpenBrowser = parsed
	}
	if err := applyIntEnvironment("BULK_MAIL_TASKS_MAX_CONCURRENT", &cfg.Tasks.MaxConcurrent); err != nil {
		return err
	}
	if err := applyIntEnvironment("BULK_MAIL_TASKS_MAX_QUEUED", &cfg.Tasks.MaxQueued); err != nil {
		return err
	}
	if value, ok := os.LookupEnv("BULK_MAIL_SHUTDOWN_TIMEOUT"); ok {
		if err := cfg.Tasks.ShutdownTimeout.Set(value); err != nil {
			return fmt.Errorf("BULK_MAIL_SHUTDOWN_TIMEOUT: %w", err)
		}
	}
	if value, ok := os.LookupEnv("BULK_MAIL_DELIVERY_SEND_TIMEOUT"); ok {
		if err := cfg.Delivery.SendTimeout.Set(value); err != nil {
			return fmt.Errorf("BULK_MAIL_DELIVERY_SEND_TIMEOUT: %w", err)
		}
	}
	if value, ok := os.LookupEnv("BULK_MAIL_SMTP_CONNECT_TIMEOUT"); ok {
		if err := cfg.Delivery.SMTPConnectTimeout.Set(value); err != nil {
			return fmt.Errorf("BULK_MAIL_SMTP_CONNECT_TIMEOUT: %w", err)
		}
	}
	if value, ok := os.LookupEnv("BULK_MAIL_LOG_LEVEL"); ok {
		cfg.Logging.Level = value
	}
	if value, ok := os.LookupEnv("BULK_MAIL_LOG_FILE"); ok {
		cfg.Logging.File = value
	}
	if value, ok := os.LookupEnv("BULK_MAIL_GOOGLE_CLIENT_ID"); ok {
		cfg.Integrations.Google.ClientID = value
	}
	if value, ok := os.LookupEnv("BULK_MAIL_LIBREOFFICE_PATH"); ok {
		cfg.Integrations.LibreOffice.Executable = value
	}
	if value, ok := os.LookupEnv("BULK_MAIL_LIBREOFFICE_BATCH_TIMEOUT"); ok {
		if err := cfg.Integrations.LibreOffice.BatchTimeout.Set(value); err != nil {
			return fmt.Errorf("BULK_MAIL_LIBREOFFICE_BATCH_TIMEOUT: %w", err)
		}
	}
	return nil
}

func (cfg Config) Validate() error {
	address := strings.Trim(strings.TrimSpace(cfg.Server.Address), "[]")
	if address == "" {
		return errors.New("server.address is required")
	}
	if !strings.EqualFold(address, "localhost") {
		ip := net.ParseIP(address)
		if ip == nil || !ip.IsLoopback() {
			return errors.New("server.address must be a loopback address")
		}
	}
	if cfg.Server.Port < 0 || cfg.Server.Port > 65535 {
		return errors.New("server.port must be between 0 and 65535")
	}
	if cfg.Tasks.MaxConcurrent < 1 {
		return errors.New("tasks.max_concurrent must be at least 1")
	}
	if cfg.Tasks.MaxQueued < 1 {
		return errors.New("tasks.max_queued must be at least 1")
	}
	if cfg.Tasks.ShutdownTimeout.Duration <= 0 {
		return errors.New("tasks.shutdown_timeout must be positive")
	}
	if err := validateDurationRange(
		"delivery.send_timeout",
		cfg.Delivery.SendTimeout.Duration,
		30*time.Second,
		10*time.Minute,
	); err != nil {
		return err
	}
	if err := validateDurationRange(
		"delivery.smtp_connect_timeout",
		cfg.Delivery.SMTPConnectTimeout.Duration,
		3*time.Second,
		2*time.Minute,
	); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Logging.Level)) {
	case "debug", "info", "warn", "error":
	default:
		return errors.New("logging.level must be debug, info, warn, or error")
	}
	if executable := strings.TrimSpace(cfg.Integrations.LibreOffice.Executable); executable != "" &&
		!filepath.IsAbs(executable) {
		return errors.New("integrations.libreoffice.executable must be an absolute path")
	}
	if err := validateDurationRange(
		"integrations.libreoffice.batch_timeout",
		cfg.Integrations.LibreOffice.BatchTimeout.Duration,
		10*time.Second,
		10*time.Minute,
	); err != nil {
		return err
	}
	return nil
}

func validateDurationRange(name string, value, minimum, maximum time.Duration) error {
	if value < minimum || value > maximum {
		return fmt.Errorf("%s must be between %s and %s", name, minimum, maximum)
	}
	return nil
}

func (duration *Duration) Set(value string) error {
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	duration.Duration = parsed
	return nil
}

func (duration *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("duration must be a string")
	}
	return duration.Set(value)
}

func (duration Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(duration.String())
}

func applyIntEnvironment(name string, target *int) error {
	value, ok := os.LookupEnv(name)
	if !ok {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	*target = parsed
	return nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode configuration: %w", err)
	}
	return errors.New("decode configuration: multiple JSON values")
}
