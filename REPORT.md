# 本次迭代报告

## 需求与实现对照

1. SQLite 纯 Go 接入并支持与 DuckDB 双后端切换  
   - 功能说明：新增 SQLite 纯 Go 驱动，实现与 DuckDB 可切换的存储后端。  
   - 相关文件：
     - internal/server/storage/sqlite/store.go（SQLite 实现）
     - internal/server/storage/duckdb/store.go（DuckDB 实现补充 PID 字段）
     - internal/server/storage/store.go（新增 QueryByPID 接口）
     - internal/server/app/server.go（按 db-driver 选择后端）
     - cmd/server/main.go（新增 db-driver 与 db 参数）
     - go.mod（引入 modernc.org/sqlite）

2. eBPF 进程采集链路并完成最小上报闭环  
   - 功能说明：基于 tracepoint 采集 TCP 连接建立事件，建立 5 元组到 PID 的映射，用户态在 HTTP 匹配时补充 PID 并上报。  
   - 相关文件：
     - internal/agent/pidmap/pidmap.go（eBPF 程序、map 与查询）
     - internal/agent/app/app.go（在上报前补齐 PID）
     - internal/agent/app/config.go（启用 eBPF 配置）
     - cmd/agent/main.go（启用 eBPF CLI 参数）
     - pkg/model/traffic.go（TrafficLog 增加 PID 字段）

3. 客户端 PID 关联查询与服务端 API 逻辑  
   - 功能说明：新增 PID 查询入口，客户端可按 PID 查询 HTTP 记录，服务端支持按 PID 检索。  
   - 相关文件：
     - internal/server/api/handlers.go（Query 增加 pid 解析与查询）
     - internal/server/storage/store.go（新增 QueryByPID 接口）
     - internal/server/storage/duckdb/store.go（QueryByPID 实现）
     - internal/server/storage/sqlite/store.go（QueryByPID 实现）
     - internal/client/app/app.go（pid 查询参数构造与展示）
     - internal/client/app/config.go（新增 PID 配置）
     - cmd/client/main.go（新增 -pid 参数）

4. 服务端查询单元测试  
   - 功能说明：覆盖 PID 查询、IP 查询以及缺少参数场景。  
   - 相关文件：
     - internal/server/api/handlers_test.go

## 当前版本运行方式

### 环境要求
- Go 1.21.9
- Linux（运行 eBPF 需要内核支持与足够权限）

### 启动 Server
```bash
go build -o bin/lightobs-server ./cmd/server
./bin/lightobs-server -listen :8080 -db-driver duckdb -db ./traffic.duckdb
```
或使用 SQLite：
```bash
./bin/lightobs-server -listen :8080 -db-driver sqlite -db ./traffic.sqlite
```

### 启动 Agent
```bash
go build -o bin/lightobs-agent ./cmd/agent
sudo ./bin/lightobs-agent -interface <网卡名> -server-ip 127.0.0.1 -server-port 8080 -enable-ebpf=true
```

### 使用 Client 查询
```bash
go build -o bin/lightobs-client ./cmd/client
./bin/lightobs-client -ip <目标IP> -server http://127.0.0.1:8080
```
按 PID 查询：
```bash
./bin/lightobs-client -pid <进程ID> -server http://127.0.0.1:8080
```
