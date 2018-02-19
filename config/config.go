package config

import (
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Paths map[string]Mapping `yaml:"paths"`
}

type Mapping struct {
	Table                string            `yaml:"table"`
	Columns              map[string]string `yaml:"columns"`
	PrimaryKey           string            `yaml:"primaryKey"`
	HasConflictDetection bool              `yaml:"hasConflictDetection"`
	Response             ResponseMapping   `yaml:"response"`
}

type ResponseMapping struct {
	Body map[string]map[string]string `yaml:"body"`
	Headers map[string]string `yaml:"headers"`
}

func ReadConfig(yml string) (*Config, error) {
	by, err := ioutil.ReadFile(yml)
	if err != nil {
		return nil, err
	}

	cfg := &Config{make(map[string]Mapping)}
	err = yaml.Unmarshal(by, cfg)
	if err != nil {
		cfg = nil
	}

	return cfg, err
}
