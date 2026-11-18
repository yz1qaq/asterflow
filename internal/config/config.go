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
	HTTPAddr              string
	DatabaseURL           string
	JWTSecretKey          string
	AutoseedAdmin         bool
	AutoseedAdminEmail    string
	AutoseedAdminPassword string
	CredentialSecretKey   string
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

	databaseURL := os.Getenv("ASTERFLOW_DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		return Config{}, fmt.Errorf(
			"ASTERFLOW_DATABASE_URL is required",
		)
	}

	jwtSecretKey := os.Getenv("ASTERFLOW_JWT_SECRET_KEY")
	if len(jwtSecretKey) < 32 || strings.TrimSpace(jwtSecretKey) == "" {
		return Config{}, fmt.Errorf(
			"ASTERFLOW_JWT_SECRET_KEY must contain at least 32 bytes and not be all whitespace",
		)
	}

	credentialSecretKey := os.Getenv("ASTERFLOW_CREDENTIAL_SECRET_KEY")
	if len(credentialSecretKey) < 32 ||
		strings.TrimSpace(credentialSecretKey) == "" {
		return Config{}, fmt.Errorf(
			"ASTERFLOW_CREDENTIAL_SECRET_KEY must contain at least 32 bytes and not be all whitespace",
		)
	}

	autoseedAdmin := false
	if raw, exists := os.LookupEnv("ASTERFLOW_AUTOSEED_ADMIN"); exists {
		autoseedAdmin, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf(
				"ASTERFLOW_AUTOSEED_ADMIN must be a boolean",
			)
		}
	}

	adminEmail := strings.TrimSpace(
		os.Getenv("ASTERFLOW_AUTOSEED_ADMIN_EMAIL"),
	)
	adminPassword := os.Getenv("ASTERFLOW_AUTOSEED_ADMIN_PASSWORD")

	if autoseedAdmin && (adminEmail == "" || adminPassword == "") {
		return Config{}, fmt.Errorf(
			"admin email and password are required when ASTERFLOW_AUTOSEED_ADMIN is enabled",
		)
	}

	return Config{
		HTTPAddr:              addr,
		DatabaseURL:           databaseURL,
		JWTSecretKey:          jwtSecretKey,
		CredentialSecretKey:   credentialSecretKey,
		AutoseedAdmin:         autoseedAdmin,
		AutoseedAdminEmail:    adminEmail,
		AutoseedAdminPassword: adminPassword,
	}, nil
}
