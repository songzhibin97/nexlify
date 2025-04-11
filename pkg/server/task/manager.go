package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"github.com/songzhibin97/nexlify/pkg/common/config"

	"github.com/songzhibin97/nexlify/pkg/common/db"
	"github.com/songzhibin97/nexlify/pkg/common/log"
	"github.com/songzhibin97/nexlify/pkg/server/agent"
	pb "github.com/songzhibin97/nexlify/proto"
	"google.golang.org/grpc/metadata"
)

type TaskManager struct {
	tasks    sync.Map
	db       *db.DB
	agentMgr *agent.AgentManager
	stopCh   chan struct{}
}

func NewTaskManager(db *db.DB, agentMgr *agent.AgentManager) *TaskManager {
	mgr := &TaskManager{
		tasks:    sync.Map{},
		db:       db,
		agentMgr: agentMgr,
		stopCh:   make(chan struct{}),
	}
	go mgr.scheduleTasks()
	return mgr
}

func (m *TaskManager) AddTask(task *pb.Task, scheduledAt *int64, priority int) {
	m.tasks.Store(task.TaskId, task)

	params, _ := json.Marshal(task.Parameters.Params)
	var query string
	if m.db.Type == "mysql" {
		query = `
		INSERT INTO tasks (task_id, task_type, parameters, status, progress, created_at, updated_at, scheduled_at, priority, timeout_seconds, max_retries, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE status = ?, updated_at = ?, scheduled_at = ?, priority = ?, timeout_seconds = ?, max_retries = ?, version = ?`
	} else if m.db.Type == "postgres" {
		query = `
		INSERT INTO tasks (task_id, task_type, parameters, status, progress, created_at, updated_at, scheduled_at, priority, timeout_seconds, max_retries, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (task_id) DO UPDATE SET status = $13, updated_at = $14, scheduled_at = $15, priority = $16, timeout_seconds = $17, max_retries = $18, version = $19`
	}

	var scheduledAtVal *int64
	if scheduledAt != nil {
		scheduledAtVal = scheduledAt
	} else if task.Schedule != nil {
		scheduledAtVal = &task.Schedule.ScheduledAt
	}

	result, err := m.db.Exec(query,
		task.TaskId, task.TaskType, params, "PENDING", 0, task.CreateTime, time.Now().Unix(), scheduledAtVal, priority, task.TimeoutSeconds, task.MaxRetries, 0,
		"PENDING", time.Now().Unix(), scheduledAtVal, priority, task.TimeoutSeconds, task.MaxRetries, 0)
	if err != nil {
		log.Error("Failed to insert task into DB", "task_id", task.TaskId, "error", err)
		return
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		log.Warn("No rows affected when inserting task", "task_id", task.TaskId)
	} else {
		log.Info("Task inserted into DB", "task_id", task.TaskId, "scheduled_at", scheduledAtVal, "priority", priority)
	}
}

func (m *TaskManager) StreamTasks(stream pb.AgentService_TaskStreamServer) error {
	cfg, err := config.LoadServerConfig()
	if err != nil {
		log.Error("Failed to load config in StreamTasks", "error", err)
		return err
	}

	md, ok := metadata.FromIncomingContext(stream.Context())
	agentID := "unknown"
	if ok {
		if tokens := md.Get("api_token"); len(tokens) > 0 {
			token := tokens[0]
			if id, err := m.agentMgr.GetAgentIDByToken(token); err == nil {
				agentID = id
			}
		}
	}
	log.Info("Task stream started for agent", "agent_id", agentID)

	lastSkippedTask := ""
	lastLogTime := time.Now()
	backoff := time.Duration(cfg.Task.InitialBackoff) * time.Second
	maxBackoff := time.Duration(cfg.Task.MaxBackoff) * time.Second

	for {
		var task struct {
			TaskID     string `db:"task_id"`
			TaskType   string `db:"task_type"`
			Parameters string `db:"parameters"`
			Priority   int    `db:"priority"`
			Timeout    int32  `db:"timeout_seconds"`
			RetryCount int    `db:"retry_count"`
			MaxRetries int    `db:"max_retries"`
			Version    int    `db:"version"`
		}
		var query string
		now := time.Now().Unix()
		if m.db.Type == "mysql" {
			query = `
            SELECT task_id, task_type, parameters, priority, timeout_seconds, retry_count, max_retries, version
            FROM tasks 
            WHERE status = 'PENDING' AND (scheduled_at IS NULL OR scheduled_at <= ?)
            ORDER BY priority DESC, created_at ASC
            LIMIT 1 
            FOR UPDATE`
		} else if m.db.Type == "postgres" {
			query = `
            SELECT task_id, task_type, parameters, priority, timeout_seconds, retry_count, max_retries, version
            FROM tasks 
            WHERE status = 'PENDING' AND (scheduled_at IS NULL OR scheduled_at <= $1)
            ORDER BY priority DESC, created_at ASC
            LIMIT 1 
            FOR UPDATE`
		}

		tx, err := m.db.Beginx()
		if err != nil {
			log.Error("Failed to start transaction", "agent_id", agentID, "error", err)
			return err
		}

		err = tx.Get(&task, query, now)
		if err != nil {
			tx.Rollback()
			if err == sql.ErrNoRows {
				time.Sleep(backoff)
				backoff = time.Duration(float64(backoff) * 1.5)
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}
			log.Error("Failed to fetch pending task", "agent_id", agentID, "error", err)
			return err
		}
		backoff = 1 * time.Second

		supportedAgents := m.agentMgr.GetAgentsForTaskType(task.TaskType)
		taskSupported := false
		for _, supportedAgentID := range supportedAgents {
			if supportedAgentID == agentID {
				taskSupported = true
				break
			}
		}
		if !taskSupported {
			tx.Rollback()
			if task.TaskID != lastSkippedTask || time.Since(lastLogTime) > 5*time.Second {
				log.Info("Agent does not support task type, skipping", "agent_id", agentID, "task_type", task.TaskType, "task_id", task.TaskID)
				lastSkippedTask = task.TaskID
				lastLogTime = time.Now()
			}
			time.Sleep(time.Duration(cfg.Task.InitialBackoff) * time.Second)
			continue
		}

		var updateQuery string
		if m.db.Type == "mysql" {
			updateQuery = `
            UPDATE tasks 
            SET status = ?, updated_at = ?, assigned_agent_id = ?, version = version + 1
            WHERE task_id = ? AND status = 'PENDING' AND version = ?`
		} else if m.db.Type == "postgres" {
			updateQuery = `
            UPDATE tasks 
            SET status = $1, updated_at = $2, assigned_agent_id = $3, version = version + 1
            WHERE task_id = $4 AND status = 'PENDING' AND version = $5`
		}
		result, err := tx.Exec(updateQuery, "RUNNING", time.Now().Unix(), agentID, task.TaskID, task.Version)
		if err != nil {
			tx.Rollback()
			log.Error("Failed to update task to RUNNING", "agent_id", agentID, "task_id", task.TaskID, "error", err)
			continue
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil || rowsAffected == 0 {
			tx.Rollback()
			log.Warn("Task already processed or version mismatch", "agent_id", agentID, "task_id", task.TaskID, "rows_affected", rowsAffected)
			continue
		}

		if err = tx.Commit(); err != nil {
			log.Error("Failed to commit transaction", "agent_id", agentID, "task_id", task.TaskID, "error", err)
			continue
		}

		var params map[string]string
		json.Unmarshal([]byte(task.Parameters), &params)
		pbTask := &pb.Task{
			TaskId:         task.TaskID,
			TaskType:       task.TaskType,
			Parameters:     &pb.TaskParameters{Params: params},
			Priority:       int32(task.Priority),
			TimeoutSeconds: task.Timeout,
			MaxRetries:     int32(task.MaxRetries),
		}

		if err := stream.Send(pbTask); err != nil {
			log.Error("Failed to send task", "agent_id", agentID, "task_id", task.TaskID, "error", err)
			return err
		}
		log.Info("Sent task to agent", "agent_id", agentID, "task_id", task.TaskID, "task_type", task.TaskType)

		timeout := time.Duration(task.Timeout) * time.Second
		if timeout == 0 {
			timeout = time.Duration(cfg.Task.DefaultTimeout) * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)

		completed := false
		for {
			select {
			case <-ctx.Done():
				log.Warn("Task timed out", "agent_id", agentID, "task_id", task.TaskID, "retry_count", task.RetryCount, "max_retries", task.MaxRetries)
				var timeoutQuery string
				var newStatus string
				var newMessage string
				var newRetryCount int
				var scheduledAt *int64

				tx, _ := m.db.Beginx()
				agentInfo, ok := m.agentMgr.GetAgentInfo(agentID)
				agentOnline := ok && time.Since(agentInfo.LastHeartbeat) < 60*time.Second

				if task.RetryCount < task.MaxRetries && !agentOnline {
					newStatus = "PENDING"
					newMessage = "Task timed out, scheduling retry"
					newRetryCount = task.RetryCount + 1
					retryDelay := time.Now().Unix() + int64(task.Timeout*2)
					scheduledAt = &retryDelay
				} else {
					newStatus = "FAILED"
					newMessage = "Task timed out, max retries reached or agent still online"
					newRetryCount = task.RetryCount
				}

				if m.db.Type == "mysql" {
					timeoutQuery = `
                    UPDATE tasks 
                    SET status = ?, progress = ?, message = ?, retry_count = ?, updated_at = ?, scheduled_at = ?, assigned_agent_id = NULL, version = version + 1
                    WHERE task_id = ? AND version = ?`
					_, err = tx.Exec(timeoutQuery, newStatus, 0, newMessage, newRetryCount, time.Now().Unix(), scheduledAt, task.TaskID, task.Version+1)
				} else if m.db.Type == "postgres" {
					timeoutQuery = `
                    UPDATE tasks 
                    SET status = $1, progress = $2, message = $3, retry_count = $4, updated_at = $5, scheduled_at = $6, assigned_agent_id = NULL, version = version + 1
                    WHERE task_id = $7 AND version = $8`
					_, err = tx.Exec(timeoutQuery, newStatus, 0, newMessage, newRetryCount, time.Now().Unix(), scheduledAt, task.TaskID, task.Version+1)
				}
				if err != nil {
					tx.Rollback()
					log.Error("Failed to update task on timeout", "agent_id", agentID, "task_id", task.TaskID, "error", err)
				} else {
					tx.Commit()
					log.Info("Task updated on timeout", "agent_id", agentID, "task_id", task.TaskID, "status", newStatus, "retry_count", newRetryCount, "scheduled_at", scheduledAt)
				}
				completed = true
				cancel()
				break

			case status, ok := <-func() chan *pb.TaskStatusUpdate {
				ch := make(chan *pb.TaskStatusUpdate, 1)
				go func() {
					defer close(ch)
					status, err := stream.Recv()
					if err != nil {
						log.Error("Stream closed or error", "agent_id", agentID, "task_id", task.TaskID, "error", err)
						return
					}
					ch <- status
				}()
				return ch
			}():
				if !ok {
					cancel()
					return nil
				}
				log.Info("Received status update", "agent_id", agentID, "task_id", status.TaskId, "status", status.Status)

				tx, _ := m.db.Beginx()
				var result []byte
				if status.Result != nil {
					result = status.Result
				}
				var statusQuery string
				if m.db.Type == "mysql" {
					statusQuery = `
                    UPDATE tasks 
                    SET status = ?, progress = ?, message = ?, result = ?, updated_at = ?, version = version + 1
                    WHERE task_id = ? AND version = ?`
					_, err = tx.Exec(statusQuery,
						status.Status, status.Progress, status.Message, result, time.Now().Unix(), status.TaskId, task.Version+1)
				} else if m.db.Type == "postgres" {
					statusQuery = `
                    UPDATE tasks 
                    SET status = $1, progress = $2, message = $3, result = $4, updated_at = $5, version = version + 1
                    WHERE task_id = $6 AND version = $7`
					_, err = tx.Exec(statusQuery,
						status.Status, status.Progress, status.Message, result, time.Now().Unix(), status.TaskId, task.Version+1)
				}
				if err != nil {
					tx.Rollback()
					log.Error("Failed to update task status in DB", "agent_id", agentID, "task_id", status.TaskId, "error", err)
				} else {
					result, _ := tx.Exec(statusQuery, status.Status, status.Progress, status.Message, result, time.Now().Unix(), status.TaskId, task.Version+1)
					rowsAffected, _ := result.RowsAffected()
					if rowsAffected == 0 {
						tx.Rollback()
						log.Warn("Version conflict on status update", "agent_id", agentID, "task_id", status.TaskId)
					} else {
						tx.Commit()
						log.Info("Task status updated in DB", "agent_id", agentID, "task_id", status.TaskId, "status", status.Status)
					}
				}

				if status.Status == "COMPLETED" || status.Status == "FAILED" {
					m.tasks.Delete(status.TaskId)
					completed = true
					break
				}
			}
			if completed {
				cancel()
				break
			}
		}
	}
}

func (m *TaskManager) scheduleTasks() {
	cfg, err := config.LoadServerConfig()
	if err != nil {
		log.Fatal("Failed to load config in scheduleTasks", "error", err)
	}
	ticker := time.NewTicker(time.Duration(cfg.Task.ScheduleInterval) * time.Second)

	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			log.Info("Task scheduler stopped")
			return
		case <-ticker.C:
			now := time.Now().Unix()
			var query string
			if m.db.Type == "mysql" {
				query = `
				SELECT COUNT(*) 
				FROM tasks 
				WHERE status = 'PENDING' AND (scheduled_at IS NULL OR scheduled_at <= ?)`
			} else if m.db.Type == "postgres" {
				query = `
				SELECT COUNT(*) 
				FROM tasks 
				WHERE status = 'PENDING' AND (scheduled_at IS NULL OR scheduled_at <= $1)`
			}
			var count int
			err := m.db.Get(&count, query, now)
			if err != nil {
				log.Error("Failed to check scheduled tasks", "error", err)
				continue
			}
			if count > 0 {
				log.Info("Found scheduled tasks ready to execute", "count", count)
			}
		}
	}
}

func (m *TaskManager) Stop() {
	close(m.stopCh)
}
