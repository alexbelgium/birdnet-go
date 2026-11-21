package datastore

import (
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewMySQLConfig_DSNRoundTrip pins that credentials and database names
// holding DSN delimiters survive FormatDSN and the driver's own parse, which a
// DSN joined with fmt.Sprintf did not: a "?" cut the database name short and a
// "/" made the DSN unparsable.
func TestNewMySQLConfig_DSNRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		password string
		database string
	}{
		{"plain", "birdnet", "secret", "birdnet"},
		{"password with DSN delimiters", "birdnet", "p/w@x:y?z#)(", "birdnet"},
		{"user name with at sign", "bird@host", "secret", "birdnet"},
		{"database name with slash", "birdnet", "secret", "bird/net"},
		{"database name with question mark", "birdnet", "secret", "bird?net"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dsn := NewMySQLConfig(tt.username, tt.password, "db.example", "3306", tt.database).FormatDSN()
			parsed, err := mysql.ParseDSN(dsn)
			require.NoError(t, err)

			assert.Equal(t, tt.username, parsed.User)
			assert.Equal(t, tt.password, parsed.Passwd)
			assert.Equal(t, tt.database, parsed.DBName)
			assert.Equal(t, "db.example:3306", parsed.Addr)
		})
	}
}

// TestNewMySQLConfig_ConnectionDefaults pins the connection settings every
// MySQL pool relies on.
func TestNewMySQLConfig_ConnectionDefaults(t *testing.T) {
	t.Parallel()

	parsed, err := mysql.ParseDSN(NewMySQLConfig("u", "p", "::1", "3306", "d").FormatDSN())
	require.NoError(t, err)

	assert.Equal(t, "tcp", parsed.Net)
	assert.Equal(t, "[::1]:3306", parsed.Addr)
	assert.True(t, parsed.ParseTime)
	assert.Equal(t, time.Local, parsed.Loc)
	assert.Equal(t, MySQLDialTimeout, parsed.Timeout)
	assert.True(t, parsed.AllowNativePasswords, "MariaDB users with mysql_native_password must still log in")
	assert.True(t, parsed.CheckConnLiveness)
	assert.Contains(t, parsed.FormatDSN(), "charset=utf8mb4")
	assert.Zero(t, parsed.ReadTimeout, "a read timeout would abort long schema migrations on large databases")
}
