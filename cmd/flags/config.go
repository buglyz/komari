package flags

import "strings"

const (
	DatabaseTypeSQLite   = "sqlite"
	DatabaseTypeMySQL    = "mysql"
	DatabaseTypePostgres = "postgres"
)

var (
	// 数据库配置
	DatabaseType string // 数据库类型：sqlite / mysql / postgres
	DatabaseFile string // SQLite 数据库文件路径；MySQL/PostgreSQL 时为连接 DSN

	Listen string
)

func NormalizeDatabaseType(databaseType string) string {
	databaseType = strings.ToLower(strings.TrimSpace(databaseType))
	if databaseType == "" {
		return DatabaseTypeSQLite
	}
	switch databaseType {
	case "mariadb":
		return DatabaseTypeMySQL
	case "postgresql", "pg", "pgx":
		return DatabaseTypePostgres
	}
	return databaseType
}

func ApplyDatabaseTypeNormalization() string {
	DatabaseType = NormalizeDatabaseType(DatabaseType)
	return DatabaseType
}

func IsSQLite() bool {
	return NormalizeDatabaseType(DatabaseType) == DatabaseTypeSQLite
}

func SupportedDatabaseTypes() string {
	return DatabaseTypeSQLite + ", " + DatabaseTypeMySQL + ", " + DatabaseTypePostgres
}
