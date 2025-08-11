package audit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

type Service struct {
	MainDB  *sqlx.DB
	AuditDB *sqlx.DB
	Driver  DBDriver

	tableCache map[string]bool
	mu         sync.RWMutex
	queue      chan Task
}

type Task struct {
	TableName string
	RecordID  interface{}
	Action    string
	User      string
}

func NewService(mainDB, auditDB *sqlx.DB, driverName string, workers, queueSize int) (*Service, error) {
	driver, err := NewDriver(driverName)
	if err != nil {
		return nil, err
	}

	s := &Service{
		MainDB:     mainDB,
		AuditDB:    auditDB,
		Driver:     driver,
		tableCache: make(map[string]bool),
		queue:      make(chan Task, queueSize),
	}

	for i := 0; i < workers; i++ {
		go s.worker()
	}

	return s, nil
}

func (s *Service) worker() {
	for task := range s.queue {
		ctx := context.Background()
		if err := s.processTask(ctx, task); err != nil {
			fmt.Println("[AUDIT ERROR]", err)
		}
	}
}

func (s *Service) processTask(ctx context.Context, task Task) error {
	// Cek dan buat table jika belum ada
	if err := s.ensureTable(ctx, task.TableName); err != nil {
		return err
	}

	// Ambil row dari main DB
	rowData, err := s.Driver.SelectRow(s.MainDB, task.TableName, task.RecordID)
	if err != nil {
		return err
	}

	// Tambahkan kolom audit
	rowData["audit_action"] = task.Action
	rowData["audit_timestamp"] = time.Now()
	rowData["audit_by"] = task.User

	// Build query & insert
	query, vals := s.Driver.BuildInsertQuery(task.TableName, rowData)
	_, err = s.AuditDB.ExecContext(ctx, query, vals...)
	return err
}

func (s *Service) ensureTable(ctx context.Context, tableName string) error {
	s.mu.RLock()
	if s.tableCache[tableName] {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	if err := s.Driver.EnsureTable(ctx, s.MainDB, s.AuditDB, tableName); err != nil {
		return err
	}

	s.mu.Lock()
	s.tableCache[tableName] = true
	s.mu.Unlock()
	return nil
}

func (s *Service) LogSnapshotAsync(tableName string, recordID interface{}, action, user string) {
	select {
	case s.queue <- Task{TableName: tableName, RecordID: recordID, Action: action, User: user}:
	default:
		fmt.Println("[AUDIT WARNING] Queue penuh, task dibuang")
	}
}
