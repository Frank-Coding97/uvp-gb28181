package auth

import (
	"errors"

	mysql "github.com/go-sql-driver/mysql"
	mssql "github.com/microsoft/go-mssqldb"
	"gorm.io/gorm"
)

// Classify typed driver errors, never match potentially sensitive SQL text.
func isUniqueViolation(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var my *mysql.MySQLError
	if errors.As(err, &my) {
		return my.Number == 1062
	}
	var ms mssql.Error
	if errors.As(err, &ms) {
		return ms.Number == 2601 || ms.Number == 2627
	}
	var pg interface{ SQLState() string }
	if errors.As(err, &pg) {
		return pg.SQLState() == "23505"
	}
	var sq interface{ Code() int }
	return errors.As(err, &sq) && (sq.Code() == 1555 || sq.Code() == 2067)
}
