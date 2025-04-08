package main

import (
	"context"
	"net"
	"time"

	"github.com/songzhibin97/nexlify/pkg/common/config"
	"github.com/songzhibin97/nexlify/pkg/common/db"
	"github.com/songzhibin97/nexlify/pkg/common/log"
	"github.com/songzhibin97/nexlify/pkg/server/agent"
	"github.com/songzhibin97/nexlify/pkg/server/api"
	"github.com/songzhibin97/nexlify/pkg/server/task"
	pb "github.com/songzhibin97/nexlify/proto"
	"google.golang.org/grpc"
)

type agentServer struct {
	pb.UnimplementedAgentServiceServer
	agentMgr *agent.AgentManager
	taskMgr  *task.TaskManager
}

func (s *agentServer) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	log.Info("Heartbeat received", "agent_id", req.AgentId)
	s.agentMgr.Heartbeat(req.AgentId)
	return &pb.HeartbeatResponse{
		ServerId:   "server-001",
		ServerTime: time.Now().Unix(),
	}, nil
}

func (s *agentServer) RegisterAgent(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	resp, err := s.agentMgr.RegisterAgent(req)
	if err != nil {
		log.Error("Failed to register agent", "agent_id", req.AgentId, "error", err)
		return nil, err
	}
	log.Info("Agent registered successfully", "agent_id", req.AgentId)
	return resp, nil
}

func (s *agentServer) TaskStream(stream pb.AgentService_TaskStreamServer) error {
	return s.taskMgr.StreamTasks(stream)
}

func main() {
	log.Info("Starting server with default logger")

	cfg, err := config.LoadServerConfig()
	if err != nil {
		log.Fatal("Failed to load config", "error", err)
	}

	db, err := db.NewDB(&cfg.Database)
	if err != nil {
		log.Fatal("Failed to initialize database", "error", err)
	}
	defer db.Close()

	lis, err := net.Listen("tcp", cfg.Port)
	if err != nil {
		log.Fatal("Failed to listen", "port", cfg.Port, "error", err)
	}

	s := grpc.NewServer()
	agentMgr := agent.NewAgentManager(db)
	taskMgr := task.NewTaskManager(db, agentMgr)

	pb.RegisterAgentServiceServer(s, &agentServer{
		agentMgr: agentMgr,
		taskMgr:  taskMgr,
	})

	go func() {
		log.Info("Starting gRPC server", "port", cfg.Port)
		if err := s.Serve(lis); err != nil {
			log.Fatal("Failed to serve gRPC", "error", err)
		}
	}()

	go api.StartHTTPServer(cfg.HTTPPort, taskMgr, db)

	// 优雅关闭
	defer func() {
		agentMgr.Stop()
		taskMgr.Stop()
		s.GracefulStop()
	}()

	select {}
}
