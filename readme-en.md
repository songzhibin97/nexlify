# Nexlify

Nexlify is a distributed task scheduling and execution system designed to enable communication between Agents and Servers via gRPC. It supports dynamic distribution and management of various task types (such as shell, python, etc.). The system features high availability, task retry mechanisms, status monitoring, and is suitable for scenarios requiring distributed task processing.

## Features

- **Distributed Task Execution**: Support for multiple Agents executing different types of tasks (such as shell, python)
- **Task Type Matching**: Tasks are distributed to Agents that support the relevant type
- **Agent Status Management**: Real-time monitoring of Agent online status (ONLINE/OFFLINE), automatically updated via heartbeat mechanism
- **Task Scheduling**: Support for immediate execution, scheduled tasks (via scheduled_at), and priority ordering
- **Task Retry**: Support for timeout retries and maximum retry count configuration
- **Database Persistence**: Using MySQL or PostgreSQL to store Agent and task information
- **gRPC Communication**: Bidirectional streaming between Agent and Server for task distribution and status updates
- **HTTP API**: RESTful interface for task submission and management

## Architecture Overview

- **Server**: Responsible for task management, Agent registration, and status monitoring; communicates with Agents via gRPC services
- **Agent**: Executes tasks and sends periodic heartbeats; supports declaring task types (e.g., shell, python)
- **Database**: Persists Agent and task status; supports MySQL and PostgreSQL
- **Task Manager**: Manages task queues and ensures tasks are distributed to appropriate Agents
- **Agent Manager**: Tracks Agent status, periodically checks for heartbeat timeouts, and updates status to OFFLINE when necessary

## Installation

### Dependencies

- Go 1.18+
- MySQL or PostgreSQL
- protoc (for generating gRPC code)

### Steps

1. **Clone the Repository**

```bash
git clone https://github.com/songzhibin97/nexlify.git
cd nexlify
```

2. **Install Dependencies**

```bash
go mod tidy
```

3. **Generate gRPC Code**

```bash
protoc --go_out=. --go-grpc_out=. proto/agent.proto
```

4. **Configure the Database**

- Create a database (e.g., nexlify_db)
- Update database connection information in config/server.yaml and config/agent.yaml:

```yaml
# server.yaml
database:
  type: "mysql" # or "postgres"
  dsn: "user:password@tcp(127.0.0.1:3306)/nexlify_db?charset=utf8mb4&parseTime=True"
port: ":50051"
http_port: ":8080"
```

```yaml
# agent.yaml
database:
  type: "mysql" # or "postgres"
  dsn: "user:password@tcp(127.0.0.1:3306)/nexlify_db?charset=utf8mb4&parseTime=True"
server_addr: "localhost:50051"
heartbeat_interval: 5 # Heartbeat interval in seconds
```

5. **Compile**

```bash
go build -o bin/server cmd/server/main.go
go build -o bin/agent cmd/agent/main.go
```

## Usage

### Starting the Server

```bash
./bin/server
```

- Listens for gRPC services (default :50051) and HTTP API (default :8080)
- Automatically initializes database tables (agents and tasks)

### Starting an Agent

```bash
./bin/agent -agent-id "shell-agent" -task-types "shell"
```

- `-agent-id`: Specifies the Agent ID (a UUID will be generated if not specified)
- `-task-types`: Declares supported task types (comma-separated, e.g., shell,python)

Example of starting multiple Agents:

```bash
./bin/agent -agent-id "shell-agent" -task-types "shell" &
./bin/agent -agent-id "python-agent" -task-types "python" &
```

### Submitting a Task

Submit tasks via the HTTP API:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"task_id": "task-001", "task_type": "shell", "parameters": {"command": "echo Hello"}}'
```

### Checking Status

Check Agent status:

```sql
SELECT agent_id, status, last_heartbeat, supported_task_types FROM agents;
```

Check task status:

```sql
SELECT task_id, task_type, status, assigned_agent_id FROM tasks;
```

### Shutting Down an Agent

```bash
kill <agent-pid>
```

After an Agent shuts down, the Server will update its status to OFFLINE within 60 seconds (heartbeat timeout).

## Project Structure

```
nexlify/
├── cmd/
│   ├── agent/         # Agent main program
│   └── server/        # Server main program
├── pkg/
│   ├── common/        # Common utilities (config, database, logging)
│   ├── server/
│   │   ├── agent/     # Agent management
│   │   └── task/      # Task management
│   └── proto/         # gRPC definitions
├── config/            # Configuration files
└── README.md
```

## Configuration

### Server Configuration (config/server.yaml)

- `database.type`: Database type (mysql or postgres)
- `database.dsn`: Database connection string
- `port`: gRPC service port
- `http_port`: HTTP API port

### Agent Configuration (config/agent.yaml)

- `database.type`: Database type
- `database.dsn`: Database connection string
- `server_addr`: Server gRPC address
- `heartbeat_interval`: Heartbeat interval in seconds

## Database Schema

### agents

| Field | Type | Description |
|-------|------|-------------|
| agent_id | VARCHAR(255) | Agent unique identifier |
| hostname | VARCHAR(255) | Host name |
| ip | VARCHAR(45) | IP address |
| version | VARCHAR(50) | Agent version |
| api_token | TEXT | API token |
| created_at | BIGINT | Creation time (Unix timestamp) |
| updated_at | BIGINT | Update time |
| last_heartbeat | BIGINT | Last heartbeat time |
| status | VARCHAR(50) | Status (ONLINE/OFFLINE) |
| supported_task_types | TEXT | Supported task types (JSON) |

### tasks

| Field | Type | Description |
|-------|------|-------------|
| task_id | VARCHAR(255) | Task unique identifier |
| task_type | VARCHAR(50) | Task type (e.g., shell) |
| parameters | TEXT | Task parameters (JSON) |
| status | VARCHAR(50) | Status (PENDING/RUNNING/COMPLETED/FAILED) |
| progress | INT | Progress (0-100) |
| message | TEXT | Status message |
| result | TEXT | Execution result |
| created_at | BIGINT | Creation time |
| updated_at | BIGINT | Update time |
| scheduled_at | BIGINT | Scheduled time (optional) |
| priority | INT | Priority (default 5) |
| timeout_seconds | INT | Timeout in seconds |
| retry_count | INT | Current retry count |
| max_retries | INT | Maximum retry count |
| assigned_agent_id | VARCHAR(255) | Assigned Agent ID |

## Development Guide

- **Adding New Task Types**:
  - Extend the `-task-types` parameter support in the Agent
  - Update task execution logic (TaskStream method)

- **Adjusting Heartbeat Timeout**:
  - Modify `60*time.Second` in checkAgentStatus to another value

- **Extending the API**:
  - Add new endpoints in pkg/server/api

## Known Issues and To-Do

- **Load Balancing**: No load distribution is implemented when multiple Agents support the same task type
- **Task Queue Optimization**: Currently all tasks share a single table, which may limit performance
- **Agent Exit Notification**: Active exit notification is not implemented (currently relies on heartbeat timeout)

## Contributing

PR and Issue submissions are welcome! Please ensure your code conforms to Go standards and includes tests.

## License

MIT License