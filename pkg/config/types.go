package config

import "time"

type ServerConfig struct {
	URL     string        `yaml:"url" json:"url"`
	Timeout time.Duration `yaml:"timeout" json:"timeout"`
}

type AdminConfig struct {
	Email string `yaml:"email" json:"email"`
	Name  string `yaml:"name,omitempty" json:"name,omitempty"`
}

type SMTPConfig struct {
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
	User string `yaml:"user" json:"user"`
	Pass string `yaml:"pass" json:"pass"`
	From string `yaml:"from" json:"from"`
	TLS  bool   `yaml:"tls" json:"tls"`
}

type DefaultsConfig struct {
	Scope      string `yaml:"scope" json:"scope"`
	ExpiryDays int    `yaml:"expiry_days" json:"expiry_days"`
}

type Config struct {
	Server   ServerConfig   `yaml:"server" json:"server"`
	Admin    AdminConfig    `yaml:"admin" json:"admin"`
	SMTP     SMTPConfig     `yaml:"smtp" json:"smtp"`
	Defaults DefaultsConfig `yaml:"defaults" json:"defaults"`
}

type ResolveOptions struct {
	ConfigPath string
	ServerFlag string
	OwnerFlag  string
	NameFlag   string
	SMTPHost   string
	SMTPPort   int
	SMTPUser   string
	SMTPPass   string
	SMTPFrom   string
}
