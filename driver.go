package audit

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

type DBDriver interface {
	EnsureTable(ctx context.Context, mainDB, auditDB *sqlx.DB, tableName string) error
	SelectRow(mainDB *sqlx.DB, tableName string, recordID interface{}) (map[string]interface{}, error)
	BuildInsertQuery(tableName string, rowData map[string]interface{}) (string, []interface{})
}

func NewDriver(driverName string) (DBDriver, error) {
	switch strings.ToLower(driverName) {
	case "mysql":
		return &MySQLDriver{}, nil
	case "postgres", "postgresql":
		return &PostgresDriver{}, nil
	default:
		return nil, fmt.Errorf("unsupported driver: %s", driverName)
	}
}

type MySQLDriver struct{}

func (d *MySQLDriver) EnsureTable(ctx context.Context, mainDB, auditDB *sqlx.DB, tableName string) error {
	var exists string
	err := auditDB.GetContext(ctx, &exists, "SHOW TABLES LIKE ?", tableName)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if exists != "" {
		return nil
	}

	var tblName, createStmt string
	err = mainDB.QueryRowContext(ctx, fmt.Sprintf("SHOW CREATE TABLE `%s`", tableName)).Scan(&tblName, &createStmt)
	if err != nil {
		return err
	}

	lines := strings.Split(createStmt, "\n")
	lines = lines[:len(lines)-1]
	lines = append(lines,
		"  `audit_action` VARCHAR(10) NOT NULL,",
		"  `audit_timestamp` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,",
		"  `audit_by` VARCHAR(50) DEFAULT NULL",
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;")

	newCreate := strings.Join(lines, "\n")
	_, err = auditDB.ExecContext(ctx, newCreate)
	return err
}

func (d *MySQLDriver) SelectRow(mainDB *sqlx.DB, tableName string, recordID interface{}) (map[string]interface{}, error) {
	rowData := make(map[string]interface{})
	query := fmt.Sprintf("SELECT * FROM `%s` WHERE id = ?", tableName)
	err := mainDB.QueryRowx(query, recordID).MapScan(rowData)
	return rowData, err
}

func (d *MySQLDriver) BuildInsertQuery(tableName string, rowData map[string]interface{}) (string, []interface{}) {
	cols := []string{}
	placeholders := []string{}
	vals := []interface{}{}
	for k, v := range rowData {
		cols = append(cols, fmt.Sprintf("`%s`", k))
		placeholders = append(placeholders, "?")
		vals = append(vals, v)
	}
	query := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)",
		tableName, strings.Join(cols, ","), strings.Join(placeholders, ","))
	return query, vals
}

type PostgresDriver struct{}

func (d *PostgresDriver) EnsureTable(ctx context.Context, mainDB, auditDB *sqlx.DB, tableName string) error {
	var exists bool
	err := auditDB.GetContext(ctx, &exists, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' AND table_name = $1
		)`, tableName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// Get column definitions from main DB
	var createCols []string
	rows, err := mainDB.Queryx(`
		SELECT column_name, data_type 
		FROM information_schema.columns 
		WHERE table_name = $1`, tableName)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var colName, dataType string
		if err := rows.Scan(&colName, &dataType); err != nil {
			return err
		}
		createCols = append(createCols, fmt.Sprintf(`"%s" %s`, colName, dataType))
	}

	createCols = append(createCols,
		`"audit_action" VARCHAR(10) NOT NULL`,
		`"audit_timestamp" TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP`,
		`"audit_by" VARCHAR(50)`)

	createStmt := fmt.Sprintf(`CREATE TABLE "%s" (%s);`, tableName, strings.Join(createCols, ","))
	_, err = auditDB.ExecContext(ctx, createStmt)
	return err
}

func (d *PostgresDriver) SelectRow(mainDB *sqlx.DB, tableName string, recordID interface{}) (map[string]interface{}, error) {
	rowData := make(map[string]interface{})
	query := fmt.Sprintf(`SELECT * FROM "%s" WHERE id = $1`, tableName)
	err := mainDB.QueryRowx(query, recordID).MapScan(rowData)
	return rowData, err
}

func (d *PostgresDriver) BuildInsertQuery(tableName string, rowData map[string]interface{}) (string, []interface{}) {
	cols := []string{}
	placeholders := []string{}
	vals := []interface{}{}
	i := 1
	for k, v := range rowData {
		cols = append(cols, fmt.Sprintf(`"%s"`, k))
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		vals = append(vals, v)
		i++
	}
	query := fmt.Sprintf(`INSERT INTO "%s" (%s) VALUES (%s)`,
		tableName, strings.Join(cols, ","), strings.Join(placeholders, ","))
	return query, vals
}
