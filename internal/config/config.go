package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"unicode"
)

type Config struct {
	HTTPAddr string
}

func Load() (Config, error) {
	addr, exists := os.LookupEnv("ASTERFLOW_HTTP_ADDR")
	if !exists {
		addr = ":18090"
	}

	if addr == "" || strings.IndexFunc(addr, unicode.IsSpace) >= 0 {
		return Config{}, fmt.Errorf(
			"ASTERFLOW_HTTP_ADDR must not be empty or contain whitespace",
		)
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return Config{}, fmt.Errorf(
			"invalid ASTERFLOW_HTTP_ADDR: %w", err,
		)
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return Config{}, fmt.Errorf(
			"ASTERFLOW_HTTP_ADDR port must be between 1 and 65535",
		)
	}
	return Config{HTTPAddr: addr}, nil
}
