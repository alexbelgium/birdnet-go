package main

import (
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfig_MySQLDSN pins that the component DSN survives delimiters in the
// password and database name, and that the printed DSN hides the whole
// password, including the part after an "@" in it.
func TestConfig_MySQLDSN(t *testing.T) {
	t.Parallel()

	c := &Config{MySQLUser: "birdnet", MySQLPass: "p@ss/w:rd", MySQLHost: "db.example", MySQLPort: 3306, MySQLDatabase: "bird?net"}

	parsed, err := mysql.ParseDSN(c.GetMySQLDSN())
	require.NoError(t, err)
	assert.Equal(t, "p@ss/w:rd", parsed.Passwd)
	assert.Equal(t, "bird?net", parsed.DBName)
	assert.Equal(t, "db.example:3306", parsed.Addr)

	sanitized := c.GetSanitizedMySQLDSN()
	assert.NotContains(t, sanitized, "ss/w")
	assert.Contains(t, sanitized, "birdnet:"+maskedPassword+"@tcp(db.example:3306)")
}

// TestConfig_SanitizedMySQLDSNUnparsable pins that a DSN the driver cannot
// parse is not printed at all, since its password cannot be located.
func TestConfig_SanitizedMySQLDSNUnparsable(t *testing.T) {
	t.Parallel()

	c := &Config{MySQLDSN: "user:secret@tcp(host:3306)"}
	assert.Equal(t, unparsableDSNPlaceholder, c.GetSanitizedMySQLDSN())
}
