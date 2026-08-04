package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	KafkaBrokers     []string
	JWTSecret        string
	PostgresUser     string
	PostgresPassword string
	PostgresPort     int
	ServicePorts     map[string]int
	AllowedOrigins   []string
}

type MissingEnvError struct{ Key string }

func (e *MissingEnvError) Error() string {
	return fmt.Sprintf("required environment variable %s is missing", e.Key)
}

func MissingKey(err error) string {
	var missing *MissingEnvError
	if errors.As(err, &missing) {
		return missing.Key
	}
	return ""
}

func Load() (Config, error) {
	get := func(key string) (string, error) {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			return "", &MissingEnvError{Key: key}
		}
		return value, nil
	}
	brokers, err := get("KAFKA_BROKERS")
	if err != nil {
		return Config{}, err
	}
	secret, err := get("JWT_SECRET")
	if err != nil {
		return Config{}, err
	}
	user, err := get("POSTGRES_USER")
	if err != nil {
		return Config{}, err
	}
	password, err := get("POSTGRES_PASSWORD")
	if err != nil {
		return Config{}, err
	}
	portText, err := get("POSTGRES_PORT")
	if err != nil {
		return Config{}, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("invalid POSTGRES_PORT %q", portText)
	}

	servicePorts := make(map[string]int)
	for _, name := range []string{"API_GATEWAY", "USER_SERVICE", "PRODUCT_SERVICE", "AUCTION_SERVICE", "TRANSACTION_SERVICE", "NOTIFICATION_SERVICE"} {
		if value := strings.TrimSpace(os.Getenv(name + "_PORT")); value != "" {
			parsed, parseErr := strconv.Atoi(value)
			if parseErr != nil || parsed < 1 || parsed > 65535 {
				return Config{}, fmt.Errorf("invalid %s_PORT %q", name, value)
			}
			servicePorts[name] = parsed
		}
	}

	var parsedBrokers []string
	for _, broker := range strings.Split(brokers, ",") {
		if broker = strings.TrimSpace(broker); broker != "" {
			parsedBrokers = append(parsedBrokers, broker)
		}
	}
	if len(parsedBrokers) == 0 {
		return Config{}, errors.New("KAFKA_BROKERS contains no brokers")
	}
	var allowedOrigins []string
	if origins := strings.TrimSpace(os.Getenv("ALLOWED_ORIGINS")); origins != "" {
		for _, o := range strings.Split(origins, ",") {
			if o = strings.TrimSpace(o); o != "" {
				allowedOrigins = append(allowedOrigins, o)
			}
		}
	}
	return Config{
		KafkaBrokers:     parsedBrokers,
		JWTSecret:        secret,
		PostgresUser:     user,
		PostgresPassword: password,
		PostgresPort:     port,
		ServicePorts:     servicePorts,
		AllowedOrigins:   allowedOrigins,
	}, nil
}
