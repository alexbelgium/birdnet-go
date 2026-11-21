package datastore

import (
	"time"

	"github.com/go-sql-driver/mysql"
)

// MySQLDialTimeout bounds how long opening one connection to a MySQL or MariaDB
// server may take. Without it, a server that drops packets instead of refusing
// the connection holds every new connection, and the query waiting for it, for
// the operating system's TCP connect timeout (about two minutes on Linux).
// Established connections need no read timeout for that case: the driver
// enables TCP keepalive, so a peer that went away fails the read on its own.
const MySQLDialTimeout = 10 * time.Second

const (
	// mysqlNetwork is the driver network of a MySQL connection.
	mysqlNetwork = "tcp"
	// mysqlCharsetParam is the DSN parameter that sets the connection character set.
	mysqlCharsetParam = "charset"
	// mysqlCharset is the connection character set of every MySQL connection.
	mysqlCharset = "utf8mb4"
)

// NewMySQLConfig returns the driver configuration of a connection to a MySQL or
// MariaDB server: utf8mb4, DATETIME columns parsed into time.Time in the local
// zone, and a dial timeout of MySQLDialTimeout. Callers adjust it as needed and
// pass its FormatDSN result to the driver. FormatDSN escapes the database name,
// so a password or database name that holds a DSN delimiter ("/", "?", "@", ":")
// is read back unchanged instead of splitting the DSN.
func NewMySQLConfig(username, password, host, port, database string) *mysql.Config {
	cfg := mysql.NewConfig()
	cfg.User = username
	cfg.Passwd = password
	cfg.Net = mysqlNetwork
	cfg.Addr = MySQLAddr(host, port)
	cfg.DBName = database
	cfg.ParseTime = true
	cfg.Loc = time.Local
	cfg.Timeout = MySQLDialTimeout
	cfg.Params = map[string]string{mysqlCharsetParam: mysqlCharset}
	return cfg
}
