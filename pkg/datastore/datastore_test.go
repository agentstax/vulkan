package datastore

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// a password holding reserved characters, and an IPv6 host, have to survive
// into the DSN -- fmt.Sprintf produced a string pgx parsed as another host
func TestConnectionStringSurvivesReservedCharacters(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		password string
		host     string
		database string
		port     int
	}{
		{"plain", "app_user", "secret", "localhost", "app_db", 5432},
		{"reserved characters in password", "app_user", "p@ss:w/rd#1", "localhost", "app_db", 5432},
		{"no password", "app_user", "", "localhost", "app_db", 5432},
		{"ipv6 host", "app_user", "secret", "::1", "app_db", 5433},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := pgxpool.ParseConfig(connectionString(test.user, test.password, test.host, test.database, test.port))
			if err != nil {
				t.Fatalf("pgx could not parse the DSN: %v", err)
			}

			if parsed.ConnConfig.User != test.user {
				t.Errorf("user = %q, want %q", parsed.ConnConfig.User, test.user)
			}
			if parsed.ConnConfig.Password != test.password {
				t.Errorf("password = %q, want %q", parsed.ConnConfig.Password, test.password)
			}
			if parsed.ConnConfig.Host != test.host {
				t.Errorf("host = %q, want %q", parsed.ConnConfig.Host, test.host)
			}
			if parsed.ConnConfig.Database != test.database {
				t.Errorf("database = %q, want %q", parsed.ConnConfig.Database, test.database)
			}
			if int(parsed.ConnConfig.Port) != test.port {
				t.Errorf("port = %d, want %d", parsed.ConnConfig.Port, test.port)
			}
		})
	}
}
