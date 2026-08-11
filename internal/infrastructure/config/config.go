package config

import (
	_ "embed"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

// Database ...
type Database struct {
	Host     string        `yaml:"host"`
	Database string        `yaml:"database" validate:"required"`
	Username string        `yaml:"username"`
	Password string        `yaml:"password"`
	Timeout  time.Duration `yaml:"timeout" validate:"gte=1s"`
	PoolSize int           `yaml:"pool_size" validate:"gte=1,lte=100"`
	SSLMode  string        `yaml:"ssl_mode" validate:"oneof=disable require verify-ca verify-full"`
}

// Observability ...
type Observability struct {
	OTLPEndpoint string `yaml:"otlp_endpoint" validate:"omitempty,url"`
}

// Redis ...
type Redis struct {
	Host     string `yaml:"host"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// Server ...
type Server struct {
	Port string `yaml:"port"`
	Host string `yaml:"host"`
}

// Settings ...
type Settings struct {
	ApplicationName string `yaml:"application_name" validate:"required"`
	Environment     string `yaml:"environment" validate:"oneof=development test production"`
	Secret          string `yaml:"secret" validate:"required"`
	Server          Server `yaml:"server"`
}

// Config ...
type Config struct {
	Database      Database      `yaml:"database" validate:"required"`
	Observability Observability `yaml:"observability"`
	Redis         Redis         `yaml:"redis"`
	Settings      Settings      `yaml:"settings" validate:"required"`
}

var (
	//go:embed config.yaml
	embeddedConfig []byte

	validate = validator.New()
)

// NewConfig ...
func NewConfig() (*Config, error) {
	data := []byte(os.ExpandEnv(string(embeddedConfig)))

	configs := &Config{}

	if err := yaml.Unmarshal(data, configs); err != nil {
		return nil, err
	}

	if err := configs.withDefaults().Validate(); err != nil {
		return nil, err
	}

	return configs, nil
}

func (c *Config) GetDatabaseURL() string {
	databaseURL := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.Database.Username, c.Database.Password),
		Host:     c.Database.Host,
		Path:     c.Database.Database,
		RawQuery: "sslmode=" + c.Database.SSLMode,
	}

	return databaseURL.String()
}

func (c *Config) GetPoolSize() int32 {
	return int32(c.Database.PoolSize)
}

func (c *Config) GetRedisAddr() string {
	return c.Redis.Host
}

func (c *Config) withDefaults() *Config {
	if c.Database.Host == "" {
		c.Database.Host = "localhost:5432"
	}

	if c.Redis.Host == "" {
		c.Redis.Host = "localhost:6379"
	}

	if c.Database.Timeout == 0 {
		c.Database.Timeout = 5 * time.Second
	}

	if c.Database.PoolSize == 0 {
		c.Database.PoolSize = 5
	}

	if c.Database.SSLMode == "" {
		c.Database.SSLMode = "disable"
	}

	if c.Settings.Server.Port == "" {
		c.Settings.Server.Port = ":8080"
	}

	if c.Settings.Environment == "" {
		c.Settings.Environment = "development"
	}

	return c
}

func (c *Config) Validate() error {
	if err := validate.Struct(c); err != nil {
		var buf strings.Builder

		for _, err := range err.(validator.ValidationErrors) {
			buf.WriteString("Field ")
			buf.WriteString(err.Field())
			buf.WriteString(" is ")
			buf.WriteString(err.ActualTag())
			buf.WriteByte('.')
		}

		return errors.New(buf.String())
	}

	return nil
}
