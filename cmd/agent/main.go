package main

import (
	"context"
	"flag"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/google/uuid"
	"github.com/songzhibin97/nexlify/pkg/common/config"
	"github.com/songzhibin97/nexlify/pkg/common/db"
	"github.com/songzhibin97/nexlify/pkg/common/log"
	pb "github.com/songzhibin97/nexlify/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type Agent struct {
	conn               *grpc.ClientConn
	client             pb.AgentServiceClient
	db                 *db.DB
	agentID            string
	supportedTaskTypes []string // 新增：支持的任务类型
}

func NewAgent(serverAddr string, db *db.DB, agentID string, supportedTaskTypes []string) (*Agent, error) {
	conn, err := grpc.Dial(serverAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	return &Agent{
		conn:               conn,
		client:             pb.NewAgentServiceClient(conn),
		db:                 db,
		agentID:            agentID,
		supportedTaskTypes: supportedTaskTypes,
	}, nil
}

func (a *Agent) Register() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := a.client.RegisterAgent(ctx, &pb.RegisterRequest{
		AgentId: a.agentID,
		Metadata: &pb.AgentMetadata{
			Hostname:           "localhost",
			Ip:                 "127.0.0.1",
			Version:            "v1.0",
			SupportedTaskTypes: a.supportedTaskTypes, // 声明支持的任务类型
		},
	})
	if err != nil {
		return "", err
	}
	log.Info("Agent registered successfully", "agent_id", a.agentID, "supported_task_types", a.supportedTaskTypes)
	return resp.ApiToken, nil
}

func (a *Agent) Heartbeat(token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := a.client.Heartbeat(ctx, &pb.HeartbeatRequest{
		AgentId:   a.agentID,
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		log.Error("Heartbeat failed", "agent_id", a.agentID, "error", err)
		return err
	}
	log.Info("Heartbeat sent", "agent_id", a.agentID)
	return nil
}

func (a *Agent) TaskStream(stream pb.AgentService_TaskStreamClient) error {
	for {
		task, err := stream.Recv()
		if err != nil {
			log.Error("Failed to receive task", "agent_id", a.agentID, "error", err)
			return err
		}
		log.Info("Received task", "agent_id", a.agentID, "task_id", task.TaskId, "priority", task.Priority)

		resp := &pb.TaskStatusUpdate{
			TaskId:     task.TaskId,
			Status:     "RUNNING",
			Progress:   0,
			Message:    "Task started",
			UpdateTime: time.Now().Unix(),
		}
		if err := stream.Send(resp); err != nil {
			log.Error("Failed to send running status", "agent_id", a.agentID, "task_id", task.TaskId, "error", err)
			return err
		}
		log.Info("Sent RUNNING status", "agent_id", a.agentID, "task_id", task.TaskId)

		cmd := task.Parameters.Params["command"]
		output, err := exec.Command("sh", "-c", cmd).Output()
		status := "COMPLETED"
		message := "Task completed"
		progress := int32(100)
		var errorInfo *pb.ErrorInfo
		if err != nil {
			status = "FAILED"
			message = "Task failed"
			progress = 0
			errorInfo = &pb.ErrorInfo{
				ErrorCode:    "EXEC_ERROR",
				ErrorMessage: err.Error(),
			}
		}

		resp = &pb.TaskStatusUpdate{
			TaskId:     task.TaskId,
			Status:     status,
			Progress:   progress,
			Message:    message,
			Result:     output,
			Error:      errorInfo,
			UpdateTime: time.Now().Unix(),
		}
		if err := stream.Send(resp); err != nil {
			log.Error("Failed to send final status", "agent_id", a.agentID, "task_id", task.TaskId, "error", err)
			return err
		}
		log.Info("Sent final status", "agent_id", a.agentID, "task_id", task.TaskId, "status", status)
	}
}

func (a *Agent) RunTaskStream() {
	for {
		ctx := context.Background()
		md := metadata.Pairs("agent_id", a.agentID)
		ctx = metadata.NewOutgoingContext(ctx, md)
		stream, err := a.client.TaskStream(ctx)
		if err != nil {
			log.Error("Failed to start task stream", "agent_id", a.agentID, "error", err)
			time.Sleep(5 * time.Second)
			continue
		}
		if err := a.TaskStream(stream); err != nil {
			log.Error("Task stream interrupted", "agent_id", a.agentID, "error", err)
			time.Sleep(5 * time.Second)
			continue
		}
	}
}

func updateAgentConfig(agentID string) error {
	viper.Set("agent.agent_id", agentID)
	return viper.WriteConfig()
}

func main() {
	log.Info("Starting agent with default logger")

	agentIDFlag := flag.String("agent-id", "", "Specify the agent ID (overrides config)")
	supportedTypesFlag := flag.String("task-types", "shell", "Comma-separated list of supported task types (e.g., shell,python)")
	flag.Parse()

	cfg, err := config.LoadAgentConfig()
	if err != nil {
		log.Fatal("Failed to load config", "error", err)
	}

	var agentID string
	if *agentIDFlag != "" {
		agentID = *agentIDFlag
		log.Info("Using agent ID from command line", "agent_id", agentID)
	} else if cfg.AgentID != "" {
		agentID = cfg.AgentID
		log.Info("Using agent ID from config", "agent_id", agentID)
	} else {
		agentID = uuid.New().String()
		log.Info("Generated new agent ID", "agent_id", agentID)
		if err := updateAgentConfig(agentID); err != nil {
			log.Error("Failed to persist agent ID to config", "error", err)
		} else {
			log.Info("Agent ID persisted to config", "agent_id", agentID)
		}
	}

	supportedTaskTypes := strings.Split(*supportedTypesFlag, ",")

	db, err := db.NewDB(&cfg.Database)
	if err != nil {
		log.Fatal("Failed to initialize database", "error", err)
	}
	defer db.Close()

	agent, err := NewAgent(cfg.ServerAddr, db, agentID, supportedTaskTypes)
	if err != nil {
		log.Fatal("Failed to create agent", "error", err)
	}

	token, err := agent.Register()
	if err != nil {
		log.Fatal("Failed to register agent", "error", err)
	}
	log.Info("Registered with token", "agent_id", agent.agentID, "token", token)

	go agent.RunTaskStream()

	go func() {
		ticker := time.NewTicker(time.Duration(cfg.HeartbeatInterval) * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := agent.Heartbeat(token); err != nil {
				log.Error("Heartbeat failed", "agent_id", agent.agentID, "error", err)
			}
		}
	}()

	select {}
}
