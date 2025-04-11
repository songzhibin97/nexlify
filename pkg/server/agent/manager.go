package agent

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/songzhibin97/nexlify/pkg/common/config"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/songzhibin97/nexlify/pkg/common/db"
	"github.com/songzhibin97/nexlify/pkg/common/log"
	pb "github.com/songzhibin97/nexlify/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AgentManager struct {
	agents sync.Map // map[string]*AgentInfo
	db     *db.DB
	stopCh chan struct{}
	cfg    *config.ServerConfig
}

type AgentInfo struct {
	AgentID            string
	LastHeartbeat      time.Time
	Token              string
	Metadata           *pb.AgentMetadata
	SupportedTaskTypes []string
	Status             string
	HeartbeatFailures  int
}

func NewAgentManager(db *db.DB, cfg *config.ServerConfig) *AgentManager {
	mgr := &AgentManager{
		agents: sync.Map{},
		db:     db,
		stopCh: make(chan struct{}),
		cfg:    cfg,
	}
	go mgr.checkAgentStatus()
	return mgr
}

func (m *AgentManager) RegisterAgent(req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	token := "token-" + time.Now().Format("20060102150405")
	now := time.Now().Unix()

	// 规范化任务类型（小写，去空格），但不验证白名单
	supportedTaskTypes := make([]string, 0, len(req.Metadata.SupportedTaskTypes))
	for _, taskType := range req.Metadata.SupportedTaskTypes {
		normalizedType := strings.ToLower(strings.TrimSpace(taskType))
		if normalizedType != "" {
			supportedTaskTypes = append(supportedTaskTypes, normalizedType)
		}
	}
	if len(supportedTaskTypes) == 0 {
		log.Warn("No valid task types provided, proceeding with empty list", "agent_id", req.AgentId)
	}

	agentInfo := &AgentInfo{
		AgentID:            req.AgentId,
		LastHeartbeat:      time.Now(),
		Token:              token,
		Metadata:           req.Metadata,
		SupportedTaskTypes: supportedTaskTypes,
		Status:             "ONLINE",
	}

	tx, err := m.db.Beginx()
	if err != nil {
		log.Error("Failed to start transaction", "error", err)
		return nil, status.Errorf(codes.Internal, "Failed to start transaction")
	}
	defer tx.Rollback()

	var query string
	if m.db.Type == "mysql" {
		query = `
        INSERT INTO agents (agent_id, hostname, ip, version, api_token, created_at, updated_at, last_heartbeat, status, supported_task_types)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON DUPLICATE KEY UPDATE 
            hostname = ?, ip = ?, version = ?, api_token = ?, updated_at = ?, status = ?, supported_task_types = ?`
	} else if m.db.Type == "postgres" {
		query = `
        INSERT INTO agents (agent_id, hostname, ip, version, api_token, created_at, updated_at, last_heartbeat, status, supported_task_types)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
        ON CONFLICT (agent_id) DO UPDATE SET 
            hostname = $11, ip = $12, version = $13, api_token = $14, updated_at = $15, status = $16, supported_task_types = $17`
	}

	supportedTypesJSON, _ := json.Marshal(supportedTaskTypes)
	_, err = tx.Exec(query,
		req.AgentId, req.Metadata.Hostname, req.Metadata.Ip, req.Metadata.Version, token, now, now, now, "ONLINE", string(supportedTypesJSON),
		req.Metadata.Hostname, req.Metadata.Ip, req.Metadata.Version, token, now, "ONLINE", string(supportedTypesJSON))
	if err != nil {
		log.Error("Failed to insert agent into DB", "agent_id", req.AgentId, "error", err)
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		log.Error("Failed to commit transaction", "agent_id", req.AgentId, "error", err)
		return nil, err
	}

	m.agents.Store(req.AgentId, agentInfo)
	log.Info("Agent stored in manager", "agent_id", req.AgentId, "supported_task_types", supportedTaskTypes)
	return &pb.RegisterResponse{
		Status:   "success",
		ApiToken: token,
	}, nil
}

func (m *AgentManager) ValidateToken(token string) bool {
	var valid bool
	m.agents.Range(func(key, value interface{}) bool {
		agentInfo := value.(*AgentInfo)
		if agentInfo.Token == token {
			valid = true
			return false // 停止遍历
		}
		return true
	})
	return valid
}

func (m *AgentManager) GetAgentIDByToken(token string) (string, error) {
	var agentID string
	var found bool
	m.agents.Range(func(key, value interface{}) bool {
		agentInfo := value.(*AgentInfo)
		if agentInfo.Token == token {
			agentID = agentInfo.AgentID
			found = true
			return false // 停止遍历
		}
		return true
	})
	if !found {
		return "", status.Errorf(codes.NotFound, "Agent not found for token")
	}
	return agentID, nil
}

func (m *AgentManager) GetActiveAgents() []string {

	var activeAgents []string
	m.agents.Range(func(key, value interface{}) bool {
		agentInfo := value.(*AgentInfo)
		if time.Since(agentInfo.LastHeartbeat) < time.Duration(m.cfg.Agent.ActiveTimeout)*time.Second {
			activeAgents = append(activeAgents, agentInfo.AgentID)
		}
		return true
	})
	return activeAgents
}

func (m *AgentManager) GetAgentsForTaskType(taskType string) []string {

	var matchingAgents []string
	m.agents.Range(func(key, value interface{}) bool {
		agentInfo := value.(*AgentInfo)
		if time.Since(agentInfo.LastHeartbeat) < time.Duration(m.cfg.Agent.ActiveTimeout)*time.Second {
			for _, supportedType := range agentInfo.SupportedTaskTypes {
				if supportedType == taskType {
					matchingAgents = append(matchingAgents, agentInfo.AgentID)
					break
				}
			}
		}
		return true
	})
	return matchingAgents
}

func (m *AgentManager) checkAgentStatus() {
	ticker := time.NewTicker(time.Duration(m.cfg.Agent.CheckInterval) * time.Second)

	for range ticker.C {
		now := time.Now().Unix()
		timeoutThreshold := now - int64(m.cfg.Agent.ActiveTimeout)

		var query string
		if m.db.Type == "mysql" {
			query = `
            SELECT agent_id 
            FROM agents 
            WHERE last_heartbeat < ? AND status = 'ONLINE'`
		} else if m.db.Type == "postgres" {
			query = `
            SELECT agent_id 
            FROM agents 
            WHERE last_heartbeat < $1 AND status = 'ONLINE'`
		}

		var timedOutAgents []string
		err := m.db.Select(&timedOutAgents, query, timeoutThreshold)
		if err != nil {
			log.Error("Failed to fetch timed out agents", "error", err)
			continue
		}

		if len(timedOutAgents) == 0 {
			continue
		}

		go func(agents []string) {
			tx, err := m.db.Beginx()
			if err != nil {
				log.Error("Failed to start transaction for status update", "error", err)
				return
			}
			defer tx.Rollback()

			// 增加心跳失败计数
			var updateFailuresQuery string
			var args []interface{}
			if m.db.Type == "mysql" {
				updateFailuresQuery = `
                UPDATE agents 
                SET heartbeat_failures = heartbeat_failures + 1, updated_at = ? 
                WHERE agent_id IN (?) AND status = 'ONLINE'`
				updateFailuresQuery, args, err = sqlx.In(updateFailuresQuery, now, agents)
				if err != nil {
					log.Error("Failed to build IN query for failures", "error", err)
					return
				}
			} else if m.db.Type == "postgres" {
				updateFailuresQuery = `
                UPDATE agents 
                SET heartbeat_failures = heartbeat_failures + 1, updated_at = $1 
                WHERE agent_id = ANY($2) AND status = 'ONLINE'`
				args = []interface{}{now, pq.Array(agents)}
			}

			_, err = tx.Exec(updateFailuresQuery, args...)
			if err != nil {
				log.Error("Failed to increment heartbeat failures", "agent_ids", agents, "error", err)
				return
			}

			// 标记超限 Agent 为 OFFLINE
			var offlineQuery string
			if m.db.Type == "mysql" {
				offlineQuery = `
                UPDATE agents 
                SET status = 'OFFLINE', updated_at = ?, heartbeat_failures = 0 
                WHERE agent_id IN (?) AND heartbeat_failures >= ? AND status = 'ONLINE'`
				offlineQuery, args, err = sqlx.In(offlineQuery, now, agents, m.cfg.Agent.MaxHeartbeatFailures)
				if err != nil {
					log.Error("Failed to build IN query for offline", "error", err)
					return
				}
			} else if m.db.Type == "postgres" {
				offlineQuery = `
                UPDATE agents 
                SET status = 'OFFLINE', updated_at = $1, heartbeat_failures = 0 
                WHERE agent_id = ANY($2) AND heartbeat_failures >= $3 AND status = 'ONLINE'`
				args = []interface{}{now, pq.Array(agents), m.cfg.Agent.MaxHeartbeatFailures}
			}

			result, err := tx.Exec(offlineQuery, args...)
			if err != nil {
				log.Error("Failed to update agents to OFFLINE", "agent_ids", agents, "error", err)
				return
			}

			if err = tx.Commit(); err != nil {
				log.Error("Failed to commit transaction for status update", "agent_ids", agents, "error", err)
				return
			}

			rowsAffected, _ := result.RowsAffected()
			if rowsAffected > 0 {
				for _, agentID := range agents {
					if agent, ok := m.agents.Load(agentID); ok {
						agentInfo := agent.(*AgentInfo)
						agentInfo.Status = "OFFLINE"
						agentInfo.HeartbeatFailures = 0
						m.agents.Store(agentID, agentInfo)
					}
				}
				log.Info("Updated agents to OFFLINE", "agent_ids", agents)
			}
		}(timedOutAgents)
	}
}

func (m *AgentManager) Heartbeat(agentID string) error {
	now := time.Now()
	var agentInfo *AgentInfo
	if agent, ok := m.agents.Load(agentID); ok {
		agentInfo = agent.(*AgentInfo)
	} else {
		log.Warn("Unknown agent heartbeat", "agent_id", agentID)
		return status.Errorf(codes.NotFound, "Agent not found")
	}

	tx, err := m.db.Beginx()
	if err != nil {
		log.Error("Failed to start transaction", "agent_id", agentID, "error", err)
		return err
	}
	defer tx.Rollback()

	var query string
	if m.db.Type == "mysql" {
		query = `
        UPDATE agents 
        SET last_heartbeat = ?, updated_at = ?, status = 'ONLINE', heartbeat_failures = 0 
        WHERE agent_id = ?`
	} else if m.db.Type == "postgres" {
		query = `
        UPDATE agents 
        SET last_heartbeat = $1, updated_at = $2, status = 'ONLINE', heartbeat_failures = 0 
        WHERE agent_id = $3`
	}

	result, err := tx.Exec(query, now.Unix(), now.Unix(), agentID)
	if err != nil {
		log.Error("Failed to update agent heartbeat in DB", "agent_id", agentID, "error", err)
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		log.Warn("No agent found in DB for heartbeat update", "agent_id", agentID)
		m.agents.Delete(agentID)
		return status.Errorf(codes.NotFound, "Agent not found in DB")
	}

	if err = tx.Commit(); err != nil {
		log.Error("Failed to commit transaction", "agent_id", agentID, "error", err)
		return err
	}

	agentInfo.LastHeartbeat = now
	agentInfo.Status = "ONLINE"
	agentInfo.HeartbeatFailures = 0
	m.agents.Store(agentID, agentInfo)

	log.Info("Agent heartbeat updated in DB", "agent_id", agentID)
	return nil
}

func (m *AgentManager) GetAgentInfo(agentID string) (*AgentInfo, bool) {
	if agent, ok := m.agents.Load(agentID); ok {
		return agent.(*AgentInfo), true
	}
	return nil, false
}

func (m *AgentManager) Stop() {
	close(m.stopCh)
}
