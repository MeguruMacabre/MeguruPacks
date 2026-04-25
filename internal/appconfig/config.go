package appconfig

import (
	"errors"
	"strconv"
	"strings"
)

type Config struct {
	S3 S3Config
}

type S3Config struct {
	Bucket        string
	Region        string
	Endpoint      string
	PathStyle     bool
	Prefix        string
	AccessKeyID   string
	SecretKey     string
	SessionToken  string
	CapacityBytes int64
}

var (
	S3Bucket      = ""
	S3Region      = ""
	S3Endpoint    = ""
	S3PathStyle   = "false"
	S3Prefix      = ""
	S3AccessKeyID = ""
	S3SecretKey   = ""
	S3CapacityGB  = "10"
)

func Load() (Config, error) {
	cfg := Config{
		S3: S3Config{
			Bucket:      strings.TrimSpace(S3Bucket),
			Region:      strings.TrimSpace(S3Region),
			Endpoint:    strings.TrimSpace(S3Endpoint),
			Prefix:      strings.Trim(strings.TrimSpace(S3Prefix), "/"),
			AccessKeyID: strings.TrimSpace(S3AccessKeyID),
			SecretKey:   strings.TrimSpace(S3SecretKey),
		},
	}

	if strings.TrimSpace(S3PathStyle) != "" {
		parsed, err := strconv.ParseBool(strings.TrimSpace(S3PathStyle))
		if err != nil {
			return Config{}, errors.New("invalid embedded S3PathStyle")
		}
		cfg.S3.PathStyle = parsed
	}

	capacityGB := 10.0
	if strings.TrimSpace(S3CapacityGB) != "" {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(S3CapacityGB), 64)
		if err != nil {
			return Config{}, errors.New("invalid embedded S3CapacityGB")
		}
		capacityGB = parsed
	}
	if capacityGB <= 0 {
		return Config{}, errors.New("embedded S3 capacity must be > 0")
	}

	cfg.S3.CapacityBytes = int64(capacityGB * 1024 * 1024 * 1024)

	if cfg.S3.Bucket == "" {
		return Config{}, errors.New("embedded S3 bucket is empty")
	}
	if cfg.S3.Region == "" {
		return Config{}, errors.New("embedded S3 region is empty")
	}
	if cfg.S3.Endpoint == "" {
		return Config{}, errors.New("embedded S3 endpoint is empty")
	}

	return cfg, nil
}

func (c Config) HasS3Credentials() bool {
	return strings.TrimSpace(c.S3.AccessKeyID) != "" && strings.TrimSpace(c.S3.SecretKey) != ""
}
