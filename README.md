# LocalSend Monitor

LocalSend 多播消息监听、桥接与转发工具。用于跨子网发现 LocalSend 设备，并提供 HTTP API 进行监控。

## 功能特性

- **多播消息监听** - 监听 LocalSend 的 UDP 多播发现消息
- **多网卡桥接** - 在不同网络接口之间转发发现消息，让设备在多个子网中互相可见
- **设备追踪** - 自动追踪在线/离线设备，支持超时自动清理
- **HTTP 代理** - 代理设备间的注册请求，辅助跨子网文件传输
- **主动转发** - 定时向其他子网广播已知设备列表
- **REST API** - 提供 HTTP API 用于查询设备状态和统计信息
- **状态文件** - 可选的 JSON 状态文件输出，方便外部程序集成

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
│   │  Device    │ │Proxy │ │Forwarder │                 │
│   │  Tracker   │ │(HTTP)│ │(HTTP)   │                 │
│   └────────────┘ └──────┘ └──────────┘                 │
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
./localsend-monitor --list-interfaces

# 使用默认配置运行
./localsend-monitor

# 使用自定义配置文件
./localsend-monitor --config /path/to/config.json
```

### Docker

```bash
# 构建镜像
docker build -t localsend-monitor .

# 运行容器（使用 host 网络模式以访问多播）
docker run --network host -v $(pwd)/config.json:/app/config.json localsend-monitor
```

## 配置

参考 `config.json` 文件：

```json
{
  "interfaces": ["eth0", "wlan0"],
  "groupAddr": "224.0.0.167",
  "port": 53317,
  "deviceAlias": "localsend-bridge",
  "fingerprint": "",
  "offlineTimeout": 300000000000,
  "cleanupInterval": 60000000000,
  "proxyEnabled": false,
  "proxyPort": 53317,
  "forwarderEnabled": false,
  "forwarderPort": 53318,
  "logLevel": "info",
  "excludeFP": [],
  "apiServerEnabled": true,
  "apiServerPort": 8080,
  "apiListenAddr": "0.0.0.0",
  "statusFile": "/tmp/localsend-status.json"
}
```

### 配置项说明

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `interfaces` | `[]` | 监听网卡列表，留空自动检测 |
| `groupAddr` | `224.0.0.167` | 多播组地址 |
| `port` | `53317` | 多播端口 |
| `deviceAlias` | `localsend-bridge` | 设备显示名称 |
| `fingerprint` | `""` | 设备指纹，用于过滤自身消息 |
| `offlineTimeout` | `5m` | 设备离线超时时间 |
| `cleanupInterval` | `1m` | 清理间隔 |
| `proxyEnabled` | `false` | 启用 HTTP 代理 |
| `forwarderEnabled` | `false` | 启用主动转发 |
| `logLevel` | `"info"` | 日志级别: debug, info, warn, error |
| `apiServerEnabled` | `true` | 启用 API 服务器 |
| `apiServerPort` | `8080` | API 端口 |
| `statusFile` | `""` | 状态文件路径，留空不输出 |

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

### 方案二：HTTP 代理

启用 HTTP 代理后，设备可通过代理互相发送注册请求，实现跨子网文件传输。

### 方案三：主动转发

在多个子网中分别部署，通过 HTTP 定时同步设备列表。

## License

MIT