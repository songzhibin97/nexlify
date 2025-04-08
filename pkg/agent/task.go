package agent

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/songzhibin97/nexlify/pkg/common/config"
	"github.com/songzhibin97/nexlify/pkg/common/log"
	pb "github.com/songzhibin97/nexlify/proto"
)

type TaskExecutor struct{}

func (e *TaskExecutor) ExecuteTask(task *pb.Task, stream pb.AgentService_TaskStreamClient) error {
	startStatus := &pb.TaskStatusUpdate{
		TaskId:     task.TaskId,
		Status:     "RUNNING",
		Progress:   0,
		Message:    "Task started",
		UpdateTime: time.Now().Unix(),
	}
	if err := stream.Send(startStatus); err != nil {
		return err
	}

	cmd := task.Parameters.Params["command"]
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if err != nil {
		return stream.Send(&pb.TaskStatusUpdate{
			TaskId:     task.TaskId,
			Status:     "FAILED",
			Message:    fmt.Sprintf("Task failed: %v", err),
			UpdateTime: time.Now().Unix(),
		})
	}

	return stream.Send(&pb.TaskStatusUpdate{
		TaskId:     task.TaskId,
		Status:     "COMPLETED",
		Progress:   100,
		Message:    "Task completed",
		Result:     out,
		UpdateTime: time.Now().Unix(),
	})
}

func StartTaskStream(client pb.AgentServiceClient, agentID string) {
	cfg, err := config.LoadAgentConfig()
	if err != nil {
		log.Fatal("Failed to load config", "error", err)
	}

	ctx := context.Background()
	stream, err := client.TaskStream(ctx)
	if err != nil {
		log.Fatal("Failed to start task stream", "error", err)
	}

	executor := &TaskExecutor{}
	stopCh := make(chan struct{})

	go func() {
		defer func() {
			log.Info("Task stream shutting down", "agent_id", agentID)
			close(stopCh)
		}()
		for {
			task, err := stream.Recv()
			if err != nil {
				log.Info("Task stream closed", "agent_id", agentID, "reason", err)
				return
			}
			log.Info("Received task", "task_id", task.TaskId)
			go executor.ExecuteTask(task, stream)
		}
	}()

	ticker := time.NewTicker(time.Duration(cfg.HeartbeatInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			log.Info("Heartbeat loop shutting down", "agent_id", agentID)
			return
		case <-ticker.C:
			_, err := client.Heartbeat(ctx, &pb.HeartbeatRequest{AgentId: agentID})
			if err != nil {
				log.Info("Heartbeat stopped", "agent_id", agentID, "reason", err)
				return
			}
		}
	}
}
