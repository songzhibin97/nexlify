package agent

import (
	"encoding/json"
	"sync"
	"time"

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
}

type AgentInfo struct {
	AgentID            string
	LastHeartbeat      time.Time
	Token              string
	Metadata           *pb.AgentMetadata
	SupportedTaskTypes []string
	Status             string // 新增字段：记录 Agent 状态
}

func NewAgentManager(db *db.DB) *AgentManager {
	mgr := &AgentManager{
		agents: sync.Map{},
		db:     db,
		stopCh: make(chan struct{}),
	}
	go mgr.checkAgentStatus()
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
		Status:             "ONLINE", // 初始化状态为 ONLINE
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

	supportedTypesJSON, _ := json.Marshal(req.Metadata.SupportedTaskTypes)
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
	log.Info("Agent stored in manager", "agent_id", req.AgentId, "supported_task_types", req.Metadata.SupportedTaskTypes)
	return &pb.RegisterResponse{
		Status:   "success",
		ApiToken: token,
	}, nil
}

func (m *AgentManager) Heartbeat(agentID string) error {
	now := time.Now()
	if agent, ok := m.agents.Load(agentID); ok {
		agentInfo := agent.(*AgentInfo)
		agentInfo.LastHeartbeat = now
		agentInfo.Status = "ONLINE" // 心跳时更新状态为 ONLINE
		m.agents.Store(agentID, agentInfo)
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
		SET last_heartbeat = ?, updated_at = ?, status = 'ONLINE'
		WHERE agent_id = ?`
	} else if m.db.Type == "postgres" {
		query = `
		UPDATE agents 
		SET last_heartbeat = $1, updated_at = $2, status = 'ONLINE'
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
		return status.Errorf(codes.NotFound, "Agent not found in DB")
	}

	if err = tx.Commit(); err != nil {
		log.Error("Failed to commit transaction", "agent_id", agentID, "error", err)
		return err
	}

	log.Info("Agent heartbeat updated in DB", "agent_id", agentID)
	return nil
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

// 新增方法：根据 api_token 获取 agent_id
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

func (m *AgentManager) checkAgentStatus() {
	ticker := time.NewTicker(10 * time.Second)
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
				if time.Since(agentInfo.LastHeartbeat) > 60*time.Second {
					// 仅当状态为 ONLINE 时更新为 OFFLINE
					if agentInfo.Status == "ONLINE" {
						log.Info("Agent offline detected", "agent_id", agentInfo.AgentID)
						agentInfo.Status = "OFFLINE" // 更新内存中的状态
						var query string
						if m.db.Type == "mysql" {
							query = `
							UPDATE agents 
							SET status = 'OFFLINE', updated_at = ?
							WHERE agent_id = ?`
						} else if m.db.Type == "postgres" {
							query = `
							UPDATE agents 
							SET status = 'OFFLINE', updated_at = $1
							WHERE agent_id = $2`
						}
						_, err := m.db.Exec(query, now.Unix(), agentInfo.AgentID)
						if err != nil {
							log.Error("Failed to update agent status to OFFLINE", "agent_id", agentInfo.AgentID, "error", err)
						} else {
							log.Info("Agent status updated to OFFLINE in DB", "agent_id", agentInfo.AgentID)
						}
					}
				}
				return true
			})
		}
	}
}

func (m *AgentManager) Stop() {
	close(m.stopCh)
}
