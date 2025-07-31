package models

import (
	"github.com/ilyakaznacheev/cleanenv"
	"log"
	"time"
)

// Config with yaml-tags
type Config struct {
	ServConf ServerCfg   `yaml:"server"`
	DBConf   DatabaseCfg `yaml:"database"`
	RDBConf  Redis       `yaml:"redis"`
}

type Redis struct {
	RedisAddress  string `yaml:"redis_address"`
	RedisPassword string `yaml:"redis_password"`
	RedisDB       int    `yaml:"redis_db"`
}

type ServerCfg struct {
	Timeout time.Duration `yaml:"timeout" env:"TIMEOUT" env-default:"10s"`
	Host    string        `yaml:"hostGateway" env:"HostGateway" env-default:":8081"`
}

type DatabaseCfg struct {
	Port     string `yaml:"port" env:"DB_PORT" env-default:"5432"`
	User     string `yaml:"user" env:"DB_USER" env-default:"postgres"`
	Password string `yaml:"password" env:"DB_PASSWORD" env-default:"1234"`
	DBName   string `yaml:"dbname" env:"DB_NAME" env-default:"postgres"`
	Host     string `yaml:"host" env:"DB_HOST" env-default:"localhost"`
}

func MustLoad(path string) *Config {
	conf := &Config{}
	if err := cleanenv.ReadConfig(path, conf); err != nil {
		log.Fatal("Can't read the common config")
		return nil
	}
	return conf
}
