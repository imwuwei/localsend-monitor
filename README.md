# LocalSend Monitor

LocalSend 多播消息监听、桥接与转发工具。用于跨子网发现 LocalSend 设备，并提供 HTTP API 进行监控。

## 功能特性

- **多播消息监听** - 监听 LocalSend 的 UDP 多播发现消息
- **多网卡桥接** - 在不同网络接口之间转发发现消息，让设备在多个子网中互相可见
- **设备追踪** - 自动追踪在线/离线设备，支持超时自动清理
- **主动转发** - 定时向其他子网广播已知设备列表
- **REST API** - 提供 HTTP API 用于查询设备状态和统计信息

## 架构

```
┌─────────────────────────────────────────────────────────┐
│                   localsend-monitor                      │
│                                                          │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐             │
│  │ Listener  │   │ Listener  │   │ Listener  │   ...     │
│  │ (eth0)   │   │ (wlan0)  │   │ (eth1)   │             │
│  └────┬─────┘   └────┬─────┘   └────┬─────┘             │
│       │              │              │                    │
│       └──────────────┼──────────────┘                    │
│                      │                                   │
│               ┌──────▼──────┐                            │
│               │   Bridge    │                            │
│               │ (Multiplexer)│                           │
│               └──────┬──────┘                            │
│                      │                                   │
│          ┌───────────┼───────────┐                       │
│          │           │           │                       │
│   ┌──────▼─────┐ ┌──▼───┐ ┌────▼─────┐                 │
│   │  Device    │ │Forwarder │                         │
│   │  Tracker   │ │(HTTP)   │                         │
│   └────────────┘ └──────────┘                         │
│                                                          │
│   ┌──────────────┐                                       │
│   │  API Server  │─── /api/devices, /api/stats, ...      │
│   └──────────────┘                                       │
└─────────────────────────────────────────────────────────┘
```

## 快速开始

### 安装

```bash
git clone https://github.com/yourusername/localsend-monitor.git
cd localsend-monitor
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
# 构建镜像
docker build -t localsend-monitor .

# 运行容器（使用 host 网络模式以访问多播）
docker run --network host localsend-monitor -i eth0
```

## 命令行参数

| 参数 | 简写 | 默认值 | 说明 |
|------|------|--------|------|
| `--interfaces` | `-i` | `""` | 监听网卡列表，逗号分隔（必需） |
| `--group-addr` | `-g` | `"224.0.0.167"` | 多播组地址 |
| `--port` | `-p` | `53317` | 多播端口 |
| `--device-alias` | `-a` | `""` | 设备别名 |
| `--fingerprint` | `-f` | `""` | 设备指纹 |
| `--offline-timeout` | `-t` | `"5m"` | 设备离线超时时间（如 `5m`、`30s`） |
| `--cleanup-interval` | `-c` | `"1m"` | 清理间隔（如 `1m`、`30s`） |
| `--exclude-fp` | 无 | `""` | 排除的指纹列表，逗号分隔 |
| `--list-interfaces` | `-L` | `false` | 列出可用网卡 |
| `--version` | `-v` | `false` | 显示版本信息 |

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
      "deviceType": "phone",
      "fingerprint": "abc123...",
      "ip": "192.168.1.100",
      "port": 53317,
      "protocol": "http",
      "download": true,
      "sourceIface": "eth0",
      "firstSeen": "2024-01-01T00:00:00Z",
      "lastSeen": "2024-01-01T01:00:00Z",
      "online": true
    }
  ]
}
```

### 获取设备详情

```bash
GET /api/devices/{fingerprint}
```

### 获取统计信息

```bash
GET /api/stats
```

### 健康检查

```bash
GET /api/health
```

## 跨子网方案

### 方案一：多网卡桥接（推荐）

在一台同时连接多个子网的机器上运行，自动在不同网卡间转发发现消息。

```
Device A (192.168.1.0/24) ←→ Bridge ←→ Device B (192.168.2.0/24)
```

```bash
./localsend-monitor -i eth0,eth1
```

### 方案二：主动转发

在多个子网中分别部署，通过 HTTP 定时同步设备列表。

## License

MIT