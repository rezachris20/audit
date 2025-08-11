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
	placeholders := strings.TrimRight(strings.Repeat("?,", len(columns)), ",")

	query := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)", auditTable, colNames, placeholders)
	_, err := s.auditDB.Exec(query, values...)
	return err
}

func (s *Service) ensureAuditTable(tableName string, data any) error {
	// Cek apakah table sudah ada
	var exists int
	err := s.auditDB.Get(&exists, `
		SELECT COUNT(*) 
		FROM information_schema.tables 
		WHERE table_schema = DATABASE() 
		AND table_name = ?`, tableName)
	if err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}

	// Ambil kolom & tipe dari struct
	cols, types := structToColumnsAndTypes(data)
	var ddlCols []string
	for i, col := range cols {
		ddlCols = append(ddlCols, fmt.Sprintf("`%s` %s", col, types[i]))
	}
	ddlCols = append(ddlCols, "`audit_action` VARCHAR(50)", "`audit_actor` VARCHAR(255)", "`audit_created_at` DATETIME")

	ddl := fmt.Sprintf("CREATE TABLE `%s` (%s)", tableName, strings.Join(ddlCols, ","))
	_, err = s.auditDB.Exec(ddl)
	return err
}

// structToColumnsAndTypes: ambil kolom & tipe data dari tag gorm
func structToColumnsAndTypes(data any) ([]string, []string) {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	t := v.Type()

	var cols []string
	var types []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Tag.Get("audit") != "true" {
			continue
		}

		// Ambil nama kolom
		colName := parseColumnName(field)

		// Default tipe TEXT
		colType := "TEXT"
		if gormTag := field.Tag.Get("gorm"); gormTag != "" {
			for _, part := range strings.Split(gormTag, ";") {
				partLower := strings.ToLower(part)
				if strings.Contains(partLower, "type:") {
					colType = strings.TrimPrefix(part, "type:")
				} else if strings.Contains(partLower, "varchar") ||
					strings.Contains(partLower, "datetime") ||
					strings.Contains(partLower, "boolean") {
					colType = part
				}
			}
		} else {
			// Auto detect tipe kalau gorm type tidak ada
			switch field.Type.Kind() {
			case reflect.String:
				colType = "TEXT"
			case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64:
				colType = "BIGINT"
			case reflect.Bool:
				colType = "BOOLEAN"
			default:
				if field.Type == reflect.TypeOf(time.Time{}) {
					colType = "DATETIME"
				}
			}
		}

		cols = append(cols, colName)
		types = append(types, colType)
	}
	return cols, types
}

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
		if field.Tag.Get("audit") != "true" {
			continue
		}

		colName := parseColumnName(field)
		valField := v.Field(i)
		val := valField.Interface()

		// Handle pointer
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
		case string:
			if realVal == "" {
				val = nil
			}
		default:
			rv := reflect.ValueOf(val)
			if rv.Kind() == reflect.Struct {
				// Skip nested struct non-time
				if _, ok := val.(time.Time); !ok {
					continue
				}
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

func parseColumnName(field reflect.StructField) string {
	// Prioritas: gorm:column -> json -> nama field lowercase
	if gormTag := field.Tag.Get("gorm"); gormTag != "" {
		for _, part := range strings.Split(gormTag, ";") {
			if strings.HasPrefix(strings.ToLower(part), "column:") {
				return strings.TrimPrefix(part, "column:")
			}
		}
	}
	if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
		return strings.Split(jsonTag, ",")[0]
	}
	return strings.ToLower(field.Name)
}
