package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/songzhibin97/nexlify/pkg/common/db"
	"github.com/songzhibin97/nexlify/pkg/common/log"
	"github.com/songzhibin97/nexlify/pkg/server/task"
	pb "github.com/songzhibin97/nexlify/proto"
)

type TaskRequest struct {
	TaskID         string            `json:"task_id" binding:"required"`
	TaskType       string            `json:"task_type" binding:"required"`
	Parameters     map[string]string `json:"parameters" binding:"required"`
	ScheduledAt    *int64            `json:"scheduled_at"`
	Priority       *int              `json:"priority"`
	TimeoutSeconds *int32            `json:"timeout_seconds"`
	MaxRetries     *int              `json:"max_retries"` // 新增字段
}

type TaskResponse struct {
	TaskID         string            `json:"task_id"`
	TaskType       string            `json:"task_type"`
	Parameters     map[string]string `json:"parameters"`
	Status         string            `json:"status"`
	Progress       int               `json:"progress"`
	Message        string            `json:"message"`
	Result         string            `json:"result"`
	CreatedAt      int64             `json:"created_at"`
	UpdatedAt      int64             `json:"updated_at"`
	ScheduledAt    *int64            `json:"scheduled_at"`
	Priority       int               `json:"priority"`
	TimeoutSeconds int32             `json:"timeout_seconds"`
	RetryCount     int               `json:"retry_count"` // 新增字段
	MaxRetries     int               `json:"max_retries"` // 新增字段
}

type APIHandler struct {
	taskMgr *task.TaskManager
	db      *db.DB
}

func NewAPIHandler(taskMgr *task.TaskManager, db *db.DB) *APIHandler {
	return &APIHandler{taskMgr: taskMgr, db: db}
}

func (h *APIHandler) SubmitTask(c *gin.Context) {
	var req TaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Error("Failed to bind task request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	priority := 5
	if req.Priority != nil {
		if *req.Priority < 1 || *req.Priority > 10 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Priority must be between 1 and 10"})
			return
		}
		priority = *req.Priority
	}

	timeout := int32(0)
	if req.TimeoutSeconds != nil {
		if *req.TimeoutSeconds < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "TimeoutSeconds must be positive"})
			return
		}
		timeout = *req.TimeoutSeconds
	}

	maxRetries := 0
	if req.MaxRetries != nil {
		if *req.MaxRetries < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "MaxRetries must be non-negative"})
			return
		}
		maxRetries = *req.MaxRetries
	}

	task := &pb.Task{
		TaskId:     req.TaskID,
		TaskType:   req.TaskType,
		CreateTime: time.Now().Unix(),
		Parameters: &pb.TaskParameters{
			Params: req.Parameters,
		},
		Priority:       int32(priority),
		TimeoutSeconds: timeout,
		MaxRetries:     int32(maxRetries), // 添加到 task 中
		Schedule: &pb.TaskSchedule{
			ScheduleType: "IMMEDIATE",
			ScheduledAt:  0,
		},
	}

	if req.ScheduledAt != nil {
		task.Schedule = &pb.TaskSchedule{
			ScheduleType: "IMMEDIATE",
			ScheduledAt:  *req.ScheduledAt,
		}
	}

	h.taskMgr.AddTask(task, req.ScheduledAt, priority)
	log.Info("Task submitted via API", "task_id", req.TaskID, "scheduled_at", req.ScheduledAt, "priority", priority, "timeout_seconds", timeout, "max_retries", maxRetries)
	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"task_id": req.TaskID,
	})
}

func (h *APIHandler) GetTask(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task ID is required"})
		return
	}

	var query string
	if h.db.Type == "mysql" {
		query = `
		SELECT task_id, task_type, parameters, status, progress, message, result, created_at, updated_at, scheduled_at, priority, timeout_seconds, retry_count, max_retries
		FROM tasks WHERE task_id = ?`
	} else if h.db.Type == "postgres" {
		query = `
		SELECT task_id, task_type, parameters, status, progress, message, result, created_at, updated_at, scheduled_at, priority, timeout_seconds, retry_count, max_retries
		FROM tasks WHERE task_id = $1`
	}

	var task struct {
		TaskID         string  `db:"task_id"`
		TaskType       string  `db:"task_type"`
		Parameters     string  `db:"parameters"`
		Status         string  `db:"status"`
		Progress       int     `db:"progress"`
		Message        *string `db:"message"`
		Result         *string `db:"result"`
		CreatedAt      int64   `db:"created_at"`
		UpdatedAt      int64   `db:"updated_at"`
		ScheduledAt    *int64  `db:"scheduled_at"`
		Priority       int     `db:"priority"`
		TimeoutSeconds int32   `db:"timeout_seconds"`
		RetryCount     int     `db:"retry_count"`
		MaxRetries     int     `db:"max_retries"`
	}

	err := h.db.Get(&task, query, taskID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Info("Task not found in database", "task_id", taskID)
			c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		} else {
			log.Error("Failed to query task from database", "task_id", taskID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database query failed"})
		}
		return
	}

	var params map[string]string
	if err := json.Unmarshal([]byte(task.Parameters), &params); err != nil {
		log.Error("Failed to unmarshal task parameters", "task_id", taskID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process task data"})
		return
	}

	message := ""
	if task.Message != nil {
		message = *task.Message
	}
	result := ""
	if task.Result != nil {
		result = *task.Result
	}

	resp := TaskResponse{
		TaskID:         task.TaskID,
		TaskType:       task.TaskType,
		Parameters:     params,
		Status:         task.Status,
		Progress:       task.Progress,
		Message:        message,
		Result:         result,
		CreatedAt:      task.CreatedAt,
		UpdatedAt:      task.UpdatedAt,
		ScheduledAt:    task.ScheduledAt,
		Priority:       task.Priority,
		TimeoutSeconds: task.TimeoutSeconds,
		RetryCount:     task.RetryCount,
		MaxRetries:     task.MaxRetries,
	}

	log.Info("Task retrieved successfully", "task_id", taskID, "status", task.Status)
	c.JSON(http.StatusOK, resp)
}

func (h *APIHandler) GetTaskList(c *gin.Context) {
	status := c.Query("status")
	pageStr := c.DefaultQuery("page", "1")
	limitStr := c.DefaultQuery("limit", "10")

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		limit = 10
	}
	offset := (page - 1) * limit

	var query string
	var countQuery string
	var args []interface{}
	if h.db.Type == "mysql" {
		query = `
		SELECT task_id, task_type, parameters, status, progress, message, result, created_at, updated_at, scheduled_at, priority, timeout_seconds, retry_count, max_retries
		FROM tasks`
		countQuery = `SELECT COUNT(*) FROM tasks`
		if status != "" {
			query += " WHERE status = ?"
			countQuery += " WHERE status = ?"
			args = append(args, status)
		}
		query += " ORDER BY priority DESC, created_at DESC LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	} else if h.db.Type == "postgres" {
		query = `
		SELECT task_id, task_type, parameters, status, progress, message, result, created_at, updated_at, scheduled_at, priority, timeout_seconds, retry_count, max_retries
		FROM tasks`
		countQuery = `SELECT COUNT(*) FROM tasks`
		if status != "" {
			query += " WHERE status = $1"
			countQuery += " WHERE status = $1"
			args = append(args, status)
		}
		query += " ORDER BY priority DESC, created_at DESC LIMIT $2 OFFSET $3"
		args = append(args, limit, offset)
	}

	var tasks []struct {
		TaskID         string  `db:"task_id"`
		TaskType       string  `db:"task_type"`
		Parameters     string  `db:"parameters"`
		Status         string  `db:"status"`
		Progress       int     `db:"progress"`
		Message        *string `db:"message"`
		Result         *string `db:"result"`
		CreatedAt      int64   `db:"created_at"`
		UpdatedAt      int64   `db:"updated_at"`
		ScheduledAt    *int64  `db:"scheduled_at"`
		Priority       int     `db:"priority"`
		TimeoutSeconds int32   `db:"timeout_seconds"`
		RetryCount     int     `db:"retry_count"`
		MaxRetries     int     `db:"max_retries"`
	}
	err = h.db.Select(&tasks, query, args...)
	if err != nil {
		log.Error("Failed to query task list", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database query failed"})
		return
	}

	var total int
	countArgs := args[:len(args)-2]
	err = h.db.Get(&total, countQuery, countArgs...)
	if err != nil {
		log.Error("Failed to count tasks", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database query failed"})
		return
	}

	var resp []TaskResponse
	for _, task := range tasks {
		var params map[string]string
		if err := json.Unmarshal([]byte(task.Parameters), &params); err != nil {
			log.Error("Failed to unmarshal task parameters", "task_id", task.TaskID, "error", err)
			continue
		}
		message := ""
		if task.Message != nil {
			message = *task.Message
		}
		result := ""
		if task.Result != nil {
			result = *task.Result
		}
		resp = append(resp, TaskResponse{
			TaskID:         task.TaskID,
			TaskType:       task.TaskType,
			Parameters:     params,
			Status:         task.Status,
			Progress:       task.Progress,
			Message:        message,
			Result:         result,
			CreatedAt:      task.CreatedAt,
			UpdatedAt:      task.UpdatedAt,
			ScheduledAt:    task.ScheduledAt,
			Priority:       task.Priority,
			TimeoutSeconds: task.TimeoutSeconds,
			RetryCount:     task.RetryCount,
			MaxRetries:     task.MaxRetries,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"tasks":       resp,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": (total + limit - 1) / limit,
	})
}

func StartHTTPServer(port string, taskMgr *task.TaskManager, db *db.DB) {
	r := gin.Default()
	handler := NewAPIHandler(taskMgr, db)

	api := r.Group("/api/v1")
	{
		api.POST("/tasks", handler.SubmitTask)
		api.GET("/tasks/:task_id", handler.GetTask)
		api.GET("/tasks", handler.GetTaskList)
	}

	log.Info("Starting HTTP server", "port", port)
	if err := r.Run(port); err != nil {
		log.Fatal("Failed to start HTTP server", "error", err)
	}
}
