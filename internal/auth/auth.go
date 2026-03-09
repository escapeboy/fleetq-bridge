package auth

import (
	"errors"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"
)

const (
	service = "fleetq-bridge"
	account = "api-key"
)

// ErrNoKey is returned when no API key is stored.
var ErrNoKey = errors.New("no API key found; run 'fleetq-bridge login --api-key <key>'")

// Store saves the API key to the OS keychain.
func Store(apiKey string) error {
	return keyring.Set(service, account, apiKey)
}

// Load retrieves the API key from the OS keychain.
// Falls back to FLEETQ_API_KEY env var, then config file value.
func Load(configFileKey string) (string, error) {
	// 1. Environment variable (highest priority, useful for CI/Docker)
	if k := os.Getenv("FLEETQ_API_KEY"); k != "" {
		return k, nil
	}

	// 2. OS keychain
	k, err := keyring.Get(service, account)
	if err == nil && k != "" {
		return k, nil
	}

	// 3. Config file fallback
	if configFileKey != "" {
		return configFileKey, nil
	}

	return "", ErrNoKey
}

// Delete removes the stored API key.
func Delete() error {
	err := keyring.Delete(service, account)
	if err != nil && err.Error() == "secret not found in keyring" {
		return nil // already gone
	}
	return err
}

// Validate does a basic format check on the API key.
func Validate(key string) error {
	if len(key) < 10 {
		return fmt.Errorf("API key too short (got %d chars)", len(key))
	}
	return nil
}
