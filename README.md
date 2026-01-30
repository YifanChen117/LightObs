# LightObs

LightObs 是一个面向本地 Kubernetes（kind）自测的轻量网络可观测性 Demo：在节点侧抓取 HTTP 流量并上报到服务端存储，再通过 CLI 查询验证闭环。

## 项目说明

### 组件

- Agent：AF_PACKET 抓包 + cBPF 过滤（仅 TCP/80）+ Best-Effort HTTP 请求/响应匹配，上报到 Server
- Server：Gin API（接收/查询）+ DuckDB 持久化
- Client：CLI 查询工具（tablewriter 表格展示）

### API

- `POST /api/v1/upload`：接收 Agent 上报的 JSON 并写入 DuckDB
- `GET /api/v1/query?ip=<ip>&limit=<n>`：按 `src_ip/dst_ip` 查询，时间倒序返回（默认 limit=200）

### 关键实现入口

- AF_PACKET 抓包：[afpacket.go]
- cBPF 过滤器：[bpf.go]
- HTTP 匹配器：[matcher.go]
- Server 路由：[server.go]
- Client 渲染：[app.go]

### 目录结构

```
.
├── README.md
├── go.mod
├── go.sum
├── build/                         # 镜像构建（用于 kind 内自测）
│   ├── Dockerfile.agent
│   └── Dockerfile.server
├── cmd/                           # 三个可执行组件入口（agent/server/client）
│   ├── agent/
│   │   └── main.go                # Agent CLI 入口：解析参数并启动抓包流程
│   ├── client/
│   │   └── main.go                # Client CLI 入口：解析参数并执行查询
│   └── server/
│       └── main.go                # Server 入口：启动 HTTP 服务与优雅退出
├── deploy/                        # Kubernetes 自测清单（LightObs + demo）
│   ├── k8s-demo.yaml
│   └── k8s-lightobs.yaml
├── docs/
│   ├── report.md
│   └── troubleshooting.md
├── internal/                      # 业务实现（按组件拆分）
│   ├── agent/
│   │   ├── app/
│   │   │   ├── app.go             # Agent 主循环：抓包、匹配、上报
│   │   │   └── config.go          # Agent 配置结构
│   │   ├── capture/
│   │   │   └── afpacket.go        # AF_PACKET 抓包句柄封装
│   │   ├── filter/
│   │   │   └── bpf.go             # cBPF 过滤器生成（仅 TCP/80）
│   │   ├── httpmatcher/
│   │   │   └── matcher.go         # HTTP 请求/响应匹配与延迟计算
│   │   └── report/
│   │       └── client.go          # 上报客户端：POST /api/v1/upload
│   ├── client/
│   │   └── app/
│   │       ├── app.go             # 发起查询并渲染表格
│   │       └── config.go          # Client 配置结构
│   └── server/
│       ├── api/
│       │   └── handlers.go        # Upload/Query 的 HTTP 处理器
│       ├── app/
│       │   ├── server.go          # 路由组装与 HTTP Server 构建
│       │   └── config.go          # Server 配置结构
│       └── storage/
│           ├── store.go           # 存储接口定义
│           └── duckdb/
│               └── store.go       # DuckDB 实现：建表、写入、查询
├── pkg/
│   └── model/
│       └── traffic.go             # TrafficLog 数据结构
├── scripts/
│   └── start.sh
└── bin/                           # 本地构建产物
    ├── lightobs-agent
    ├── lightobs-client
    └── lightobs-server
```

## 使用说明

目标：在 kind 中运行 `demo-curl -> demo-nginx` 产生 HTTP 流量，由 Agent 抓取并上报到 Server，然后用 Client 查询到记录。

### 0) 前置条件

- Go：1.21.9
- kind + kubectl
- Docker 可用（kind 依赖）
- 权限：Agent 需要抓包权限（在集群内以 privileged + NET_RAW/NET_ADMIN 运行）

### 1) 每次重新启动的清晰步骤

适用于你每次都希望从一个确定状态重新跑通整个项目。

#### 1.1 清理旧资源

```bash
kubectl delete -f deploy/k8s-demo.yaml --ignore-not-found
kubectl delete -f deploy/k8s-lightobs.yaml --ignore-not-found
```

说明：
- 先删 demo 和 LightObs 资源，避免旧 Pod 影响结果
- `--ignore-not-found` 让重复执行不会报错

#### 1.2 （可选）重建 kind 集群

```bash
kind delete cluster
kind create cluster
kubectl cluster-info
```

说明：
- 如果你想彻底重置环境，执行本步
- 若只想重新部署应用，可以跳过本步

#### 1.3 构建镜像并加载到 kind

```bash
mkdir -p /tmp/docker-nocreds
printf '{"auths": {}}\n' > /tmp/docker-nocreds/config.json

DOCKER_CONFIG=/tmp/docker-nocreds docker build -t lightobs-server:dev -f build/Dockerfile.server .
DOCKER_CONFIG=/tmp/docker-nocreds docker build -t lightobs-agent:dev  -f build/Dockerfile.agent  .
kind load docker-image lightobs-server:dev
kind load docker-image lightobs-agent:dev
```

说明：
- 这一步确保 Agent/Server 镜像是最新的
- `kind load` 用于把本机镜像导入 kind 节点的 containerd

#### 1.4 部署 LightObs

```bash
kubectl apply -f deploy/k8s-lightobs.yaml
kubectl -n lightobs get pods -o wide
```

说明：
- 等 `lightobs-server` 和 `lightobs-agent` 都是 Running 再继续

#### 1.5 部署 demo 产生流量

```bash
kubectl apply -f deploy/k8s-demo.yaml
kubectl get pod -l app=demo-nginx -o wide
kubectl get pod demo-curl -o wide
```

说明：
- `demo-curl` 会持续请求 `demo-nginx`，用于制造 HTTP/80 流量

#### 1.6 验证抓包与查询结果

```bash
kubectl -n lightobs logs -l app=lightobs-agent --tail=50
kubectl -n lightobs logs -l app=lightobs-server --tail=50
```

说明：
- 看到 `开始抓包：iface=any` 表示 Agent 已正常工作

```bash
go build -o bin/lightobs-client ./cmd/client
kubectl -n lightobs port-forward svc/lightobs-server 8080:8080
```

说明：
- 先编译 Client，再用 port-forward 把 Server 暴露到本地

另开一个终端查询：

```bash
kubectl get pod -l app=demo-nginx -o wide
./bin/lightobs-client -ip <nginx_pod_ip> -server http://127.0.0.1:8080
```

说明：
- `-ip` 填 nginx Pod IP 或 curl Pod IP 都可以
- 查询结果应看到 `GET / 200` 等记录

### 2) 创建 kind 集群（首次或不重建时使用）

在网络条件正常时，直接创建：

```bash
kind create cluster
kubectl cluster-info
```

如果你在 WSL2 + Docker Desktop 下遇到 Docker 凭据/网络相关报错（例如 `docker-credential-desktop.exe: Invalid argument` 或直连 Docker Hub 失败），使用下面命令创建一个“无凭据配置”的临时 Docker 配置，并从镜像加速源拉取 kind 节点镜像：

```bash
mkdir -p /tmp/docker-nocreds
printf '{"auths": {}}\n' > /tmp/docker-nocreds/config.json

DOCKER_CONFIG=/tmp/docker-nocreds docker pull docker.m.daocloud.io/kindest/node:v1.27.3
docker tag docker.m.daocloud.io/kindest/node:v1.27.3 kindest/node:v1.27.3

DOCKER_CONFIG=/tmp/docker-nocreds kind create cluster --image kindest/node:v1.27.3
kubectl cluster-info --context kind-kind
```

说明：
- `DOCKER_CONFIG=/tmp/docker-nocreds` 用于绕过凭据助手，避免构建/拉取时被凭据链路阻塞
- `docker.m.daocloud.io/...` 用于避免直连 Docker Hub 失败

### 3) 构建 LightObs 镜像并加载到 kind

构建 Server/Agent 镜像：

```bash
mkdir -p /tmp/docker-nocreds
printf '{"auths": {}}\n' > /tmp/docker-nocreds/config.json

DOCKER_CONFIG=/tmp/docker-nocreds docker build -t lightobs-server:dev -f build/Dockerfile.server .
DOCKER_CONFIG=/tmp/docker-nocreds docker build -t lightobs-agent:dev  -f build/Dockerfile.agent  .
```

说明：
- 这里构建的是用于 kind 内运行的镜像（不是本机直接运行的二进制）
- Dockerfile 默认已使用 Go 代理与基础镜像加速源，适合在受限网络下构建

将镜像导入 kind 节点的 containerd：

```bash
kind load docker-image lightobs-server:dev
kind load docker-image lightobs-agent:dev
```

说明：
- kind 节点使用 containerd，不会自动复用你宿主机的本地镜像；需要显式 load

### 4) 部署 LightObs（Server + Agent）

```bash
kubectl apply -f deploy/k8s-lightobs.yaml
kubectl -n lightobs get pods -o wide
```

说明：
- Server 以 Deployment 运行，暴露 `lightobs-server.lightobs.svc.cluster.local:8080`
- Agent 以 DaemonSet 运行（hostNetwork），默认 `-interface=any` 覆盖更多网卡，适配 kind 场景

确认 Agent/Server 正常工作：

```bash
kubectl -n lightobs logs -l app=lightobs-agent --tail=50
kubectl -n lightobs logs -l app=lightobs-server --tail=50
```

### 5) 部署 demo 产生 HTTP 流量（nginx + curl）

```bash
kubectl apply -f deploy/k8s-demo.yaml
kubectl get pod -l app=demo-nginx -o wide
kubectl get pod demo-curl -o wide
```

说明：
- `demo-curl` 会持续请求 `demo-nginx` 的 Service，从而产生稳定的 HTTP/80 流量
- demo YAML 已使用镜像加速源（daocloud），避免因直连 Docker Hub 导致 `ImagePullBackOff`

### 6) （可选）精确选择抓包网卡

默认 `-interface=any` 通常就能抓到流量；如果你想只抓某个 Pod 的流量，可先找到对应 veth：

```bash
kubectl get pod -l app=demo-nginx -o wide
docker exec -it kind-control-plane sh -c 'ip route get <POD_IP> && ip link'
```

说明：
- `ip route get <POD_IP>` 输出里带有 `dev vethXXXX`，该 veth 即 Pod 在节点侧的接口
- 需要时可把 `deploy/k8s-lightobs.yaml` 的 `-interface=any` 改为 `-interface=vethXXXX`

### 7) 编译并使用 Client 查询记录

编译：

```bash
go build -o bin/lightobs-client ./cmd/client
```

说明：
- Client 通过 HTTP 调用 Server 的查询接口输出表格

用 port-forward 把 Server 暴露到本地（保持该命令运行）：

```bash
kubectl -n lightobs port-forward svc/lightobs-server 8080:8080
```

另开一个终端，先获取 nginx Pod IP，然后查询：

```bash
kubectl get pod -l app=demo-nginx -o wide
./bin/lightobs-client -ip <nginx_pod_ip> -server http://127.0.0.1:8080
```

说明：
- `-ip` 用来指定按源/目的 IP 过滤查询（填 nginx Pod IP 或 curl Pod IP 都可以）
- 查询结果应能看到 `GET / 200` 等记录持续增长
