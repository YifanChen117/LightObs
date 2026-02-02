# LightObs V1
LiteObs 初始版本定位于一个轻量级的 Kubernetes 流量观测系统，采用经典的 Agent-Server-Client 三层架构。
 核心组件与职责
- Agent ( cmd/agent ) :
  - 职责 : 部署在 K8s 节点上（DaemonSet），负责捕获网络流量。
  - 技术栈 : 基于 gopacket/pcap 实现底层抓包。
  - 功能 : 监听指定网卡，解析 HTTP 协议（Method, URL, Status, Latency），并将日志聚合后上报给 Server。
- Server ( cmd/server ) :
  - 职责 : 集中式存储与查询服务。
  - 存储 : 初始集成 DuckDB ( internal/server/storage/duckdb )，利用其 OLAP 特性处理日志分析。
  - 接口 : 提供 HTTP REST API ( POST /upload 接收日志, GET /query 查询日志)。
- Client ( cmd/client ) :
  - 职责 : 开发者使用的命令行工具。
  - 功能 : 通过 CLI 参数构建查询请求，格式化展示流量表格。 初始数据模型
- TrafficLog ( pkg/model/traffic.go ): 包含源/目的 IP、端口、HTTP 方法、路径、状态码、耗时和响应体大小。

# LightObs V2 迭代升级
## 新增需求1: 引入 eBPF 实现agent支持进程采集收发http报文的事件并上报
为了解决“流量归属于哪个进程”的问题，Agent 引入了 eBPF 技术栈。

- 新增模块 : internal/agent/pidmap
- 技术实现 :
  - Kernel Space : 使用 cilium/ebpf 编写和加载 BPF 程序，挂载到 tracepoint/sock/inet_sock_set_state 。
  - Map 机制 : 维护一个 BPF_MAP_TYPE_HASH ，记录 (SrcIP, DstIP, SrcPort, DstPort) -> PID 的映射关系。
  - 多字节序支持 : 实现了对 Big Endian (Network Order) 和 Little Endian (Host Order) 的兼容处理，确保在不同架构下 Key 的一致性。
  - 端口过滤 : 在汇编层增加了对常用开发端口（如 8080）的捕获支持。
  - **考虑到该项目为轻量级可观测应用，因此使用依赖更少，开销更少的纯go方案实现而没有使用C**
- 架构变更 : Agent 在捕获到 HTTP 流量后，会查询 PID Map，将流量日志补充 PID 字段后再上报。 

## 新增需求 2: 存储层重构与 SQLite 支持（同时支持SQLite和DuckDB）
为了增强在不同环境下的兼容性（特别是去 CGO 依赖），Server 端重构了存储层。
- 接口抽象 : 定义了 Store 接口 ( internal/server/storage/store.go )，包含 Insert , QueryByIP , QueryByPID 等方法。
- 新增实现 : internal/server/storage/sqlite
  - Pure Go : 引入 modernc.org/sqlite ，实现了无 CGO 依赖的嵌入式数据库支持。
  - 双模式切换 : 支持通过 Server 启动参数 -db-type 在 DuckDB 和 SQLite 之间切换。 
  
## 新增需求3 3: Client端支持PID关联的ip流向查询
- Server : 新增 QueryByPID 查询逻辑，支持按进程 ID 过滤流量日志。
- Client : 命令行工具新增 -pid 参数，支持开发者直接追踪特定进程的流量。
- 测试 : 完善了 internal/server/api 的单元测试，通过 fakeStore 覆盖了所有查询路径。 

# 项目结构
```
LiteObs/
├── cmd/                # 入口文件 (Agent, Server, Client)
├── pkg/model/          # 共享数据模型 (TrafficLog)
├── internal/
│   ├── agent/          # Agent 核心逻辑
│   │   ├── capture/    # gopacket 抓包
│   │   ├── pidmap/     # eBPF 进程关联 (Cilium/ebpf)
│   │   └── report/     # 日志上报
│   ├── server/         # Server 核心逻辑
│   │   ├── api/        # HTTP Handler & 路由
│   │   └── storage/    # 存储接口 (SQLite/DuckDB 实现)
│   └── client/         # CLI 客户端逻辑
├── deploy/             # K8s 部署清单 (DaemonSet, Deployment)
└── scripts/            # 自动化构建与部署脚本
```
# 数据流向
数据流向 (Data Flow)
1. Capture : Agent ( libpcap ) 捕获宿主机网络接口的原始数据包。
2. Enrichment : Agent 解析 HTTP 协议，并通过 eBPF Map ( pidmap ) 实时查找对应的宿主机 PID。
3. Transport : 结构化的 TrafficLog (含 PID) 被发送至 Server。
4. Storage : Server 根据配置将数据写入 SQLite 或 DuckDB。
5. Query : Client 发起查询请求，Server 检索数据库并返回带进程信息的流量视图。

# 使用方法
```
bash scripts/start.sh full（完整构建-部署-查询）
bash scripts/start.sh query（仅查询）
bash scripts/start.sh local（本地查询）
```