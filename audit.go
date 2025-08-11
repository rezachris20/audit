package audit

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type Service struct {
	auditDB *sqlx.DB
}

func NewService(auditDB *sqlx.DB) *Service {
	return &Service{auditDB: auditDB}
}

// LogSnapshotAsync membuat log audit secara async
func (s *Service) LogSnapshotAsync(table string, id any, action, actor string, data any) {
	go func() {
		if err := s.logSnapshot(table, id, action, actor, data); err != nil {
			log.Printf("[AUDIT ERROR] %v", err)
		}
	}()
}

func (s *Service) logSnapshot(table string, id any, action, actor string, data any) error {
	auditTable := table

	// Pastikan tabel audit ada
	if err := s.ensureAuditTable(auditTable, data); err != nil {
		return err
	}

	// Extract kolom & nilai
	columns, values := structToColumnsValues(data)
	columns = append(columns, "audit_action", "audit_actor", "audit_created_at")
	values = append(values, action, actor, time.Now())

	// Build query INSERT
	colNames := "`" + strings.Join(columns, "`, `") + "`"
	placeholders := strings.Repeat("?,", len(columns))
	placeholders = strings.TrimRight(placeholders, ",")

	query := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)", auditTable, colNames, placeholders)

	_, err := s.auditDB.Exec(query, values...)
	return err
}

func (s *Service) ensureAuditTable(tableName string, data any) error {
	// Cek apakah table sudah ada
	var exists int
	err := s.auditDB.Get(&exists, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", tableName)
	if err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}

	// Buat DDL berdasarkan struct data
	columns, _ := structToColumnsValues(data)
	var ddlCols []string
	for _, col := range columns {
		ddlCols = append(ddlCols, fmt.Sprintf("`%s` TEXT", col))
	}
	ddlCols = append(ddlCols, "`audit_action` VARCHAR(50)", "`audit_actor` VARCHAR(255)", "`audit_created_at` DATETIME")

	ddl := fmt.Sprintf("CREATE TABLE `%s` (%s)", tableName, strings.Join(ddlCols, ","))
	_, err = s.auditDB.Exec(ddl)
	return err
}

// structToColumnsValues mengubah struct menjadi slice kolom & nilai
func structToColumnsValues(data any) ([]string, []any) {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	t := v.Type()

	var cols []string
	var vals []any

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)

		// ambil tag audit
		auditTag := field.Tag.Get("audit")
		if auditTag != "true" {
			continue
		}

		// Ambil nama kolom dari gorm:"column:..."
		colName := ""
		if gormTag := field.Tag.Get("gorm"); gormTag != "" {
			for _, part := range strings.Split(gormTag, ";") {
				if strings.HasPrefix(strings.ToLower(part), "column:") {
					colName = strings.TrimPrefix(part, "column:")
				}
			}
		}

		// Kalau tidak ada, ambil dari json
		if colName == "" {
			if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
				colName = jsonTag
			} else {
				colName = strings.ToLower(field.Name)
			}
		}

		valField := v.Field(i)
		val := valField.Interface()

		// Null & pointer handling
		if valField.Kind() == reflect.Ptr {
			if valField.IsNil() {
				val = nil
			} else {
				val = valField.Elem().Interface()
			}
		}

		switch realVal := val.(type) {
		case time.Time:
			if realVal.IsZero() {
				val = nil
			} else {
				val = realVal.Format("2006-01-02 15:04:05")
			}
		default:
			rv := reflect.ValueOf(val)
			if rv.Kind() == reflect.Struct {
				// Skip nested struct yang bukan time.Time
				continue
			}
			if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Map {
				b, _ := json.Marshal(val)
				val = string(b)
			}
		}

		cols = append(cols, colName)
		vals = append(vals, val)
	}

	return cols, vals
}
