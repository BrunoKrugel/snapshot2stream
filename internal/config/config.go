package config

import (
	"github.com/caarlos0/env/v9"
)

type Config struct {
	Authorization Authorization
	Cameras       Cameras
	Server        Server
}

type Authorization struct {
	Cookie string `env:"cookie"`
	Token  string `env:"token"`
}

type Cameras struct {
	Camera1 string `env:"camera1"`
	Camera2 string `env:"camera2"`
	Camera3 string `env:"camera3"`
	Camera4 string `env:"camera4"`
	Camera5 string `env:"camera5"`
	Camera6 string `env:"camera6"`
}

type Server struct {
	Port     string `env:"PORT" envDefault:"8081"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
	FPS      int    `env:"FPS" envDefault:"10"`
	FetchFPS int    `env:"FETCH_FPS" envDefault:"30"`
	UseCache bool   `env:"USE_CACHE" envDefault:"true"`
}

// TODO: Use viper and parse from config.yaml
func NewConfig() (*Config, error) {
	cfg := &Config{}
	err := env.Parse(cfg)
	if err != nil {
		return cfg, err
	}

	return cfg, nil
}
