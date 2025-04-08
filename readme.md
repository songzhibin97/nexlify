# Nexlify

Nexlify 是一个分布式任务调度和执行系统，旨在通过 gRPC 实现 Agent 和 Server 之间的通信，支持多种任务类型（如 shell、python 等）的动态分发和管理。系统具备高可用性、任务重试、状态监控等功能，适用于需要分布式任务处理的场景。

## 功能特性

- **分布式任务执行**：支持多个 Agent 执行不同类型的任务（如 shell、python）
- **任务类型匹配**：任务根据类型分发给支持该类型的 Agent
- **Agent 状态管理**：实时监控 Agent 的在线状态（ONLINE/OFFLINE），通过心跳机制自动更新
- **任务调度**：支持立即执行、定时任务（通过 scheduled_at）和优先级排序
- **任务重试**：支持超时重试和最大重试次数配置
- **数据库持久化**：使用 MySQL 或 PostgreSQL 存储 Agent 和任务信息
- **gRPC 通信**：Agent 和 Server 之间通过 gRPC 双向流进行任务分发和状态更新
- **HTTP API**：提供 RESTful 接口用于任务提交和管理

## 架构概述

- **Server**：负责任务管理、Agent 注册和状态监控，通过 gRPC 服务与 Agent 通信
- **Agent**：执行任务并定期发送心跳，支持声明任务类型（如 shell, python）
- **Database**：持久化 Agent 和任务状态，支持 MySQL 和 PostgreSQL
- **Task Manager**：管理任务队列，确保任务分发到合适的 Agent
- **Agent Manager**：跟踪 Agent 状态，定期检查心跳超时并更新为 OFFLINE

## 安装

### 依赖

- Go 1.18+
- MySQL 或 PostgreSQL
- protoc（用于生成 gRPC 代码）

### 步骤

1. **克隆仓库**

```bash
git clone https://github.com/songzhibin97/nexlify.git
cd nexlify
```

2. **安装依赖**

```bash
go mod tidy
```

3. **生成 gRPC 代码**

```bash
protoc --go_out=. --go-grpc_out=. proto/agent.proto
```

4. **配置数据库**

- 创建数据库（如 nexlify_db）
- 更新 config/server.yaml 和 config/agent.yaml 中的数据库连接信息：

```yaml
# server.yaml
database:
  type: "mysql" # 或 "postgres"
  dsn: "user:password@tcp(127.0.0.1:3306)/nexlify_db?charset=utf8mb4&parseTime=True"
port: ":50051"
http_port: ":8080"
```

```yaml
# agent.yaml
database:
  type: "mysql" # 或 "postgres"
  dsn: "user:password@tcp(127.0.0.1:3306)/nexlify_db?charset=utf8mb4&parseTime=True"
server_addr: "localhost:50051"
heartbeat_interval: 5 # 心跳间隔（秒）
```

5. **编译**

```bash
go build -o bin/server cmd/server/main.go
go build -o bin/agent cmd/agent/main.go
```

## 使用方法

### 启动 Server

```bash
./bin/server
```

- 监听 gRPC 服务（默认 :50051）和 HTTP API（默认 :8080）
- 自动初始化数据库表（agents 和 tasks）

### 启动 Agent

```bash
./bin/agent -agent-id "shell-agent" -task-types "shell"
```

- `-agent-id`：指定 Agent ID（若不指定，会生成 UUID）
- `-task-types`：声明支持的任务类型（逗号分隔，如 shell,python）

启动多个 Agent 示例：

```bash
./bin/agent -agent-id "shell-agent" -task-types "shell" &
./bin/agent -agent-id "python-agent" -task-types "python" &
```

### 提交任务

通过 HTTP API 提交任务：

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"task_id": "task-001", "task_type": "shell", "parameters": {"command": "echo Hello"}}'
```

### 查看状态

检查 Agent 状态：

```sql
SELECT agent_id, status, last_heartbeat, supported_task_types FROM agents;
```

检查任务状态：

```sql
SELECT task_id, task_type, status, assigned_agent_id FROM tasks;
```

### 关闭 Agent

```bash
kill <agent-pid>
```

Agent 关闭后，Server 会在 60 秒内（心跳超时）将状态更新为 OFFLINE。

## 项目结构

```
nexlify/
├── cmd/
│   ├── agent/         # Agent 主程序
│   └── server/        # Server 主程序
├── pkg/
│   ├── common/        # 通用工具（如配置、数据库、日志）
│   ├── server/
│   │   ├── agent/     # Agent 管理
│   │   └── task/      # 任务管理
│   └── proto/         # gRPC 定义
├── config/            # 配置文件
└── README.md
```

## 配置说明

### Server 配置（config/server.yaml）

- `database.type`: 数据库类型（mysql 或 postgres）
- `database.dsn`: 数据库连接字符串
- `port`: gRPC 服务端口
- `http_port`: HTTP API 端口

### Agent 配置（config/agent.yaml）

- `database.type`: 数据库类型
- `database.dsn`: 数据库连接字符串
- `server_addr`: Server gRPC 地址
- `heartbeat_interval`: 心跳间隔（秒）

## 数据库表结构

### agents

| 字段 | 类型 | 描述 |
|------|------|------|
| agent_id | VARCHAR(255) | Agent 唯一标识 |
| hostname | VARCHAR(255) | 主机名 |
| ip | VARCHAR(45) | IP 地址 |
| version | VARCHAR(50) | Agent 版本 |
| api_token | TEXT | API 令牌 |
| created_at | BIGINT | 创建时间（Unix 时间） |
| updated_at | BIGINT | 更新时间 |
| last_heartbeat | BIGINT | 最后心跳时间 |
| status | VARCHAR(50) | 状态（ONLINE/OFFLINE） |
| supported_task_types | TEXT | 支持的任务类型（JSON） |

### tasks

| 字段 | 类型 | 描述 |
|------|------|------|
| task_id | VARCHAR(255) | 任务唯一标识 |
| task_type | VARCHAR(50) | 任务类型（如 shell） |
| parameters | TEXT | 任务参数（JSON） |
| status | VARCHAR(50) | 状态（PENDING/RUNNING/COMPLETED/FAILED） |
| progress | INT | 进度（0-100） |
| message | TEXT | 状态消息 |
| result | TEXT | 执行结果 |
| created_at | BIGINT | 创建时间 |
| updated_at | BIGINT | 更新时间 |
| scheduled_at | BIGINT | 调度时间（可选） |
| priority | INT | 优先级（默认 5） |
| timeout_seconds | INT | 超时时间（秒） |
| retry_count | INT | 当前重试次数 |
| max_retries | INT | 最大重试次数 |
| assigned_agent_id | VARCHAR(255) | 分配的 Agent ID |

## 开发指南

- **添加新任务类型**：
  - 在 Agent 中扩展 `-task-types` 参数支持
  - 更新任务执行逻辑（TaskStream 方法）

- **调整心跳超时**：
  - 修改 checkAgentStatus 中的 `60*time.Second` 为其他值

- **扩展 API**：
  - 在 pkg/server/api 中添加新 endpoint

## 已知问题与待办

- **负载均衡**：多个 Agent 支持同一任务类型时，未实现负载分配
- **任务队列优化**：当前所有任务共用一个表，性能可能受限
- **Agent 退出通知**：未实现主动退出通知（当前依赖心跳超时）

## 贡献

欢迎提交 PR 或 Issue！请确保代码符合 Go 规范并附带测试。

## 许可证

MIT License