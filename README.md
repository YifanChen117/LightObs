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
  
## 新增需求 3: Client端支持PID关联的ip流向查询
- Server : 新增 QueryByPID 查询逻辑，支持按进程 ID 过滤流量日志。
- Client : 命令行工具新增 -pid 参数，支持开发者直接追踪特定进程的流量。
- 测试 : 完善了 internal/server/api 的单元测试，通过 fakeStore 覆盖了所有查询路径。 

# LightObs V3 引入 Operator 架构升级

在之前的版本中，Agent 以 DaemonSet 形式全局部署，缺乏针对性和动态管理能力。V3 版本引入了 Kubernetes Operator 模式与自定义资源（CRD），实现了完全声明式的观测目标管理。具体内容可参考README_Operator.md

## 核心功能与优势 
1. **声明式目标管理 (CRD)**：引入 `PodObservation` CRD。用户只需提交一份 YAML 声明需要观测哪个应用（通过 Label Selector）、哪个端口，无需关心底层 Agent 的生命周期。
2. **动态自动发现与扩缩容感知 (Auto-Discovery)**：Operator 实时监听 K8s 集群。当目标业务 Pod 扩容或缩容时，Operator 会自动发现新 Pod 的 IP，并动态更新 Agent 的抓包白名单，做到**配置零干预**。
3. **精准按需观测 (Targeted Observation)**：废弃了全局 DaemonSet 抓包。Operator 会精确计算目标业务 Pod 所在的节点，并**仅在这些节点上**按需拉起专属的 Agent Pod，大幅降低集群性能开销。
4. **故障自愈与状态对账 (Self-Healing)**：如果底层 Agent Pod 意外崩溃或被误删，Operator 的 Reconcile 循环会在秒级内发现实际状态与期望状态的差异，并瞬间重建 Agent。
5. **配置热更新 (Dynamic Config)**：通过 `kubectl patch` 修改 CRD（例如更改观测端口），Operator 会自动感知并更新 Agent 配置，全程无需 SSH 登录节点。

## 新增文件结构与功能
- **cmd/operator/main.go**: Operator 的入口程序，负责初始化 Manager 并注册 Controller。
- **internal/operator/api/v1alpha1/types.go**: 定义 `PodObservation` CRD 的 Go 结构体（Spec 与 Status）。
- **internal/operator/controller/podobservation_controller.go**: 核心控制器逻辑：执行对账循环 (Reconcile)，计算目标节点并动态管理 Agent Pod 的生命周期。
- **deploy/crds/podobservation.yaml**: `PodObservation` 的 Kubernetes CustomResourceDefinition 资源注册文件。
- **deploy/operator/operator.yaml**: Operator 控制器本身的 K8s 部署清单 (包含 Deployment, ClusterRole 等 RBAC 权限声明, ServiceAccount)。
- **scripts/start_with_operator.sh**: 一键构建并启动包含 Operator 的完整集群与 Demo 业务环境的自动化脚本。
- **scripts/demo_operator_magic.sh**: Operator 核心优势交互式演示脚本 (带你一步步体验扩容感知、故障自愈、配置热更新的 Magic 效果)。

## 控制流向 (Operator Control Flow)
1. **Apply CRD**: 用户向 K8s 提交 `PodObservation` 资源，声明期望观测的目标（如打有 `app: web` 标签的 Pod）。
2. **Watch/Event**: Operator 监听到 `PodObservation` 的创建/更新事件，以及业务 Pod 的生命周期变动。
3. **Calculate**: Operator 提取所有匹配业务 Pod 的 IP，并归纳它们当前分布在哪些宿主机节点上。
4. **Reconcile (对账)**: Operator 检查对应节点上是否已存在服务于该 CR 的 Agent Pod；若缺失则动态拉起，若存在但过期参数（如目标 IP 增减）则进行更新/重建，抹平期望与实际状态的差异。
5. **Targeted Capture**: 动态创建出的 Agent 进程被精准注入了 `-target-ips` 参数，仅对指定 IP 开启 eBPF 或 Pcap 抓包，极大降低系统开销。

# 完整项目结构
```text
LiteObs/
├── build/              # Dockerfile (Server, Agent, Operator)
├── cmd/                # 各个组件的入口程序
│   ├── agent/  
│   ├── server/ 
│   ├── client/ 
│   └── operator/       # Operator 启动入口
├── pkg/
│   └── model/          # 共享数据模型 (TrafficLog)
├── internal/
│   ├── agent/          # Agent 核心抓包侧逻辑 (eBPF pidmap, pcap parser, report)
│   ├── server/         # Server 核心存储侧逻辑 (API Handler, SQLite/DuckDB Storage)
│   ├── client/         # CLI 客户端逻辑
│   └── operator/       # Operator 核心控制面逻辑
│       ├── api/        # CRD 结构体定义 (v1alpha1)
│       └── controller/ # Reconcile 状态对账控制器
├── deploy/             # K8s 部署清单集合
│   ├── crds/           # 自定义资源定义
│   ├── operator/       # Operator 本身部署文件
│   ├── k8s-lightobs.yaml # Server 部署文件
│   └── k8s-demo.yaml   # 用于供用户演示的 demo 空间应用
└── scripts/            # 自动化脚本工具
    ├── start_with_operator.sh  # 包含 Operator 架构的一键建站/部署测试脚本
    └── demo_operator_magic.sh  # 交互式 Operator 优势演示脚本
```

# 使用方法

**启动带有 Operator 完整能力的演示集群：**
```bash
# 自动部署 Kind 集群、打包镜像、拉起 Server + Operator + Demo 应用并进行完整连通性测试
bash scripts/start_with_operator.sh full

#  Operator 的动态自愈/发现机制 demo
bash scripts/demo_operator_magic.sh
```

*(附：旧版本全局 DaemonSet 启动方式 )*
```bash
bash scripts/start.sh full
```
