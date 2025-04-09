package main

import (
	"context"
	"crypto/tls"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type agentServer struct {
	pb.UnimplementedAgentServiceServer
	agentMgr *agent.AgentManager
	taskMgr  *task.TaskManager
}

func (s *agentServer) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	log.Info("Heartbeat received", "agent_id", req.AgentId)
	if err := s.agentMgr.Heartbeat(req.AgentId); err != nil {
		return nil, err
	}
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

// UnaryInterceptor 验证 API Token，豁免 RegisterAgent 方法
func UnaryInterceptor(agentMgr *agent.AgentManager) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		log.Info("Incoming gRPC method", "method", info.FullMethod)

		// 豁免 RegisterAgent 方法，修正包名为 proto
		if info.FullMethod == "/proto.AgentService/RegisterAgent" {
			log.Info("RegisterAgent method, skipping token validation")
			return handler(ctx, req)
		}

		// 其他方法需要验证 API Token
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok || len(md.Get("api_token")) == 0 {
			return nil, status.Errorf(codes.Unauthenticated, "Missing API token")
		}
		token := md.Get("api_token")[0]
		if !agentMgr.ValidateToken(token) {
			return nil, status.Errorf(codes.Unauthenticated, "Invalid API token")
		}
		return handler(ctx, req)
	}
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

	// 加载 TLS 证书
	cert, err := tls.LoadX509KeyPair("config/cert.pem", "config/key.pem")
	if err != nil {
		log.Fatal("Failed to load TLS credentials", "error", err)
	}
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.NoClientCert,
	})

	agentMgr := agent.NewAgentManager(db)
	s := grpc.NewServer(grpc.Creds(creds), grpc.UnaryInterceptor(UnaryInterceptor(agentMgr)))
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

	go api.StartHTTPServer(cfg.HTTPPort, taskMgr, db, agentMgr)

	defer func() {
		agentMgr.Stop()
		taskMgr.Stop()
		s.GracefulStop()
	}()

	select {}
}
