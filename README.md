# LocalSend Monitor

LocalSend 多播消息监听、桥接与转发工具。用于跨子网发现 LocalSend 设备，并提供 HTTP API 进行监控。

## 功能特性

- **多播消息监听** - 监听 LocalSend 的 UDP 多播发现消息，支持消息预过滤和速率限制
- **多网卡桥接** - 在不同网络接口之间转发发现消息，让设备在多个子网中互相可见
- **源 IP 保留** - 通过 RAW Socket 在转发时保留原始发送者的 IP 地址，确保 LocalSend 客户端正确识别设备
- **消息去重** - SHA256 哈希 + 接口级去重，防止桥接环路导致的消息重复转发
- **设备追踪** - 自动追踪在线/离线设备，支持超时自动清理，提供设备加入/离开/更新回调
- **REST API** - 提供 HTTP API 用于查询设备状态、统计信息和网络接口

## 架构

```
┌─────────────────────────────────────────────────────────────────┐
│                      localsend-monitor                           │
│                                                                  │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐                     │
│  │ Listener  │   │ Listener  │   │ Listener  │   ...             │
│  │  (eth0)  │   │  (wlan0)  │   │  (eth1)   │                    │
│  └────┬─────┘   └────┬─────┘   └────┬─────┘                     │
│       │              │              │                            │
│       └──────────────┼──────────────┘                            │
│                      │  Message Channels                         │
│               ┌──────▼──────┐                                    │
│               │  Multiplexer │  ← 消息汇聚与分发                  │
│               └──────┬──────┘                                    │
│                      │                                           │
│               ┌──────▼──────┐                                    │
│               │    Dedup    │  ← SHA256 去重（防环路）            │
│               └──────┬──────┘                                    │
│                      │                                           │
│          ┌───────────┼───────────┐                               │
│          │           │           │                               │
│   ┌──────▼─────┐ ┌───▼────┐ ┌───▼──────┐                        │
│   │  Device    │ │ Sender  │ │ Sender   │  ...                   │
│   │  Tracker   │ │ (eth0)  │ │ (wlan0)  │  ← RAW Socket 源IP保留 │
│   └────────────┘ └────────┘ └──────────┘                        │
│                                                                  │
│   ┌──────────────┐                                               │
│   │  API Server  │─── /api/devices, /api/stats, /api/health, ... │
│   └──────────────┘                                               │
└─────────────────────────────────────────────────────────────────┘
```

## 环境要求

- Go 1.26+
- Linux（需要 RAW Socket 支持以保留源 IP，推荐使用 root 或赋予 `CAP_NET_RAW` 权限）

## 快速开始

### 安装

```bash
git clone https://github.com/imwuwei/localsend-monitor.git
cd localsend-monitor
```

#### 使用 Makefile 构建

```bash
# 构建当前平台
make build

# 构建所有 Linux 平台（amd64 / arm64 / arm）
make build-all

# 运行测试
make test

# 清理构建产物
make clean
```

#### 直接使用 Go 构建

```bash
go build -o localsend-monitor .
```

### 运行

```bash
# 查看可用网卡
./localsend-monitor -L

# 指定网卡运行（必需）
./localsend-monitor -i eth0

# 指定多个网卡
./localsend-monitor -i eth0,wlan0

# 同时使用长参数
./localsend-monitor --interfaces eth0,wlan0

# 查看版本
./localsend-monitor -v
```

### Docker

```bash
# 先构建二进制文件
make build-all

# 构建镜像
docker build -t localsend-monitor .

# 运行容器（使用 host 网络模式以访问多播）
docker run --network host localsend-monitor -i eth0
```

#### Docker Compose

```bash
# 编辑 docker-compose.yml 中的网卡名称后启动
docker compose up -d

# 查看日志
docker compose logs -f
```

## 命令行参数

| 参数 | 简写 | 默认值 | 说明 |
|------|------|--------|------|
| `--interfaces` | `-i` | `""` | 监听网卡列表，逗号分隔（必需） |
| `--group-addr` | `-g` | `"224.0.0.167"` | 多播组地址 |
| `--port` | `-p` | `53317` | 多播端口 |
| `--offline-timeout` | `-t` | `"5m"` | 设备离线超时时间（如 `5m`、`30s`） |
| `--cleanup-interval` | `-c` | `"1m"` | 清理间隔（如 `1m`、`30s`） |
| `--exclude-fp` | 无 | `""` | 排除的指纹列表，逗号分隔 |
| `--api` | 无 | `false` | 启用 API 服务（默认关闭） |
| `--api-addr` | 无 | `"0.0.0.0"` | API 服务监听地址 |
| `--api-port` | 无 | `53318` | API 服务端口 |
| `--list-interfaces` | `-L` | `false` | 列出可用网卡 |
| `--version` | `-v` | `false` | 显示版本信息 |
| `--help` | `-h` | `false` | 显示帮助信息 |

所有配置通过命令行参数传入，无需配置文件。

### 使用示例

```bash
# 查看完整帮助
./localsend-monitor --help

# 指定网卡和自定义多播地址
./localsend-monitor -i eth0 --group-addr 224.0.0.168 --port 53318

# 设置离线超时
./localsend-monitor -i eth0,wlan0 --offline-timeout 10m

# 使用长选项配置
./localsend-monitor --interfaces eth0 --offline-timeout 5m --cleanup-interval 30s

# 启用 API 服务
./localsend-monitor -i eth0 --api --api-port 53318
```

## API 接口

### 获取所有设备

```bash
GET /api/devices
```

响应示例：
```json
{
  "count": 2,
  "devices": [
    {
      "alias": "MyPhone",
      "version": "1.14.0",
      "deviceModel": "Pixel 7",
      "deviceType": "mobile",
      "fingerprint": "abc123...",
      "ip": "192.168.1.100",
      "port": 53317,
      "protocol": "http",
      "download": true,
      "sourceIface": "eth0",
      "lastSeen": 1704067200,
      "online": true
    }
  ]
}
```

### 获取设备详情

```bash
GET /api/devices/{key}
```

`{key}` 支持设备指纹（fingerprint）或 `IP:端口` 格式。

```bash
# 通过指纹查询
curl http://localhost:53318/api/devices/abc123...

# 通过 IP:端口查询
curl http://localhost:53318/api/devices/192.168.1.100:53317
```

### 获取统计信息

```bash
GET /api/stats
```

响应示例：
```json
{
  "totalDevices": 10,
  "onlineDevices": 8,
  "offlineDevices": 2,
  "byInterface": {
    "eth0": 6,
    "wlan0": 4
  },
  "byDeviceType": {
    "mobile": 5,
    "desktop": 3,
    "server": 2
  },
  "uptime": "1h30m15s",
  "version": "1.0.0"
}
```

### 健康检查

```bash
GET /api/health
```

响应示例：
```json
{
  "status": "ok",
  "uptime": "1h30m15s",
  "version": "1.0.0",
  "devices": 10
}
```

### 获取网络接口

```bash
GET /api/interfaces
```

响应示例：
```json
{
  "interfaces": ["eth0", "wlan0"]
}
```

## 跨子网示例

在一台同时连接多个子网的机器上运行，自动在不同网卡间转发发现消息。

```
Device A (192.168.1.0/24 eth0) ←→ Bridge ←→ Device B (192.168.2.0/24 eth1)
```

```bash
./localsend-monitor -i eth0,eth1
```

桥接机制会自动将 eth0 上收到的发现消息转发到 eth1，反之亦然。消息去重功能可防止因桥接接口（如 br10 + vxlan10）导致的转发环路。

## CI/CD

本项目使用 GitHub Actions 进行持续集成和发布。

### CI（持续集成）

推送至 `main` 分支或发起 Pull Request 时自动触发：
- 运行测试 (`make test`)
- 构建所有平台二进制文件 (`make build-all`)

### Release（发布）

推送 `v*` 格式的 tag 时自动触发：
- 运行测试
- 构建所有平台二进制文件
- 生成 SHA256 校验和
- 创建 GitHub Release 并上传构建产物

## 项目结构

```
localsend-monitor/
├── main.go                    # 程序入口，命令行参数解析
├── api.go                     # HTTP API 服务
├── go.mod / go.sum            # Go 模块依赖
├── Makefile                   # 构建脚本（支持交叉编译）
├── Dockerfile                 # Docker 镜像构建
├── docker-compose.yml         # Docker Compose 配置
├── .github/
│   └── workflows/
│       ├── ci.yml             # CI 工作流
│       └── release.yml        # Release 工作流
└── src/
    ├── multicast/
    │   ├── listener.go        # UDP 多播监听器（预过滤、速率限制）
    │   └── sender.go          # 多播发送器（RAW Socket 源IP保留）
    ├── protocol/
    │   └── message.go         # LocalSend 协议消息定义与解析
    ├── relay/
    │   ├── bridge.go          # 核心桥接逻辑（消息去重、多路复用、跨网卡转发）
    │   └── device.go          # 设备追踪器（在线/离线管理、回调通知）
    └── forwarder/
        └── forwarder.go       # 主动跨子网转发器（HTTP 设备列表同步）
```

## License

MIT