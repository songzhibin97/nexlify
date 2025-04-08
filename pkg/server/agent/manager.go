package agent

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/songzhibin97/nexlify/pkg/common/db"
	"github.com/songzhibin97/nexlify/pkg/common/log"
	pb "github.com/songzhibin97/nexlify/proto"
)

type AgentManager struct {
	agents sync.Map // map[string]*AgentInfo
	db     *db.DB
	stopCh chan struct{} // 新增：用于停止状态检查
}

type AgentInfo struct {
	AgentID            string
	LastHeartbeat      time.Time
	Token              string
	Metadata           *pb.AgentMetadata
	SupportedTaskTypes []string
}

func NewAgentManager(db *db.DB) *AgentManager {
	mgr := &AgentManager{
		agents: sync.Map{},
		db:     db,
		stopCh: make(chan struct{}),
	}
	go mgr.checkAgentStatus() // 启动状态检查
	return mgr
}

func (m *AgentManager) RegisterAgent(req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	token := "token-" + time.Now().Format("20060102150405")
	now := time.Now().Unix()
	agentInfo := &AgentInfo{
		AgentID:            req.AgentId,
		LastHeartbeat:      time.Now(),
		Token:              token,
		Metadata:           req.Metadata,
		SupportedTaskTypes: req.Metadata.SupportedTaskTypes,
	}
	m.agents.Store(req.AgentId, agentInfo)
	log.Info("Agent stored in manager", "agent_id", req.AgentId, "supported_task_types", req.Metadata.SupportedTaskTypes)

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

	supportedTypesJSON, _ := json.Marshal(req.Metadata.SupportedTaskTypes)
	_, err := m.db.Exec(query,
		req.AgentId, req.Metadata.Hostname, req.Metadata.Ip, req.Metadata.Version, token, now, now, now, "ONLINE", string(supportedTypesJSON),
		req.Metadata.Hostname, req.Metadata.Ip, req.Metadata.Version, token, now, "ONLINE", string(supportedTypesJSON))
	if err != nil {
		log.Error("Failed to insert agent into DB", "agent_id", req.AgentId, "error", err)
		return nil, err
	}
	log.Info("Agent inserted into DB", "agent_id", req.AgentId)

	return &pb.RegisterResponse{
		Status:   "success",
		ApiToken: token,
	}, nil
}

func (m *AgentManager) Heartbeat(agentID string) {
	now := time.Now()
	if agent, ok := m.agents.Load(agentID); ok {
		agentInfo := agent.(*AgentInfo)
		agentInfo.LastHeartbeat = now
		m.agents.Store(agentID, agentInfo)
		log.Info("Agent heartbeat updated in memory", "agent_id", agentID)
	} else {
		log.Warn("Unknown agent heartbeat", "agent_id", agentID)
		return
	}

	var query string
	if m.db.Type == "mysql" {
		query = `
		UPDATE agents 
		SET last_heartbeat = ?, updated_at = ?, status = 'ONLINE'
		WHERE agent_id = ?`
	} else if m.db.Type == "postgres" {
		query = `
		UPDATE agents 
		SET last_heartbeat = $1, updated_at = $2, status = 'ONLINE'
		WHERE agent_id = $3`
	}

	result, err := m.db.Exec(query, now.Unix(), now.Unix(), agentID)
	if err != nil {
		log.Error("Failed to update agent heartbeat in DB", "agent_id", agentID, "error", err)
		return
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		log.Warn("No agent found in DB for heartbeat update", "agent_id", agentID)
	} else {
		log.Info("Agent heartbeat updated in DB", "agent_id", agentID)
	}
}

func (m *AgentManager) GetActiveAgents() []string {
	var activeAgents []string
	m.agents.Range(func(key, value interface{}) bool {
		agentInfo := value.(*AgentInfo)
		if time.Since(agentInfo.LastHeartbeat) < 60*time.Second {
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
		if time.Since(agentInfo.LastHeartbeat) < 60*time.Second {
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

// 新增：定期检查 Agent 状态
func (m *AgentManager) checkAgentStatus() {
	ticker := time.NewTicker(10 * time.Second) // 每 10 秒检查一次
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			log.Info("Agent status checker stopped")
			return
		case <-ticker.C:
			now := time.Now()
			m.agents.Range(func(key, value interface{}) bool {
				agentInfo := value.(*AgentInfo)
				if time.Since(agentInfo.LastHeartbeat) > 60*time.Second { // 60 秒未收到心跳
					log.Info("Agent offline detected", "agent_id", agentInfo.AgentID)
					var query string
					if m.db.Type == "mysql" {
						query = `
						UPDATE agents 
						SET status = 'OFFLINE', updated_at = ?
						WHERE agent_id = ? AND status != 'OFFLINE'`
					} else if m.db.Type == "postgres" {
						query = `
						UPDATE agents 
						SET status = 'OFFLINE', updated_at = $1
						WHERE agent_id = $2 AND status != 'OFFLINE'`
					}
					_, err := m.db.Exec(query, now.Unix(), agentInfo.AgentID)
					if err != nil {
						log.Error("Failed to update agent status to OFFLINE", "agent_id", agentInfo.AgentID, "error", err)
					} else {
						log.Info("Agent status updated to OFFLINE in DB", "agent_id", agentInfo.AgentID)
					}
				}
				return true
			})
		}
	}
}

// 新增：停止状态检查
func (m *AgentManager) Stop() {
	close(m.stopCh)
}
