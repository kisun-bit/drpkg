# 客户端克隆检测与唯一备份控制设计

## 1. 概述

在整机备份场景中，客户端完成备份后可以恢复到另一台主机（物理机、虚拟机或异构硬件）。

恢复完成后，会同时存在两台内容完全相同的客户端：

- **源客户端（Original Client）**
- **恢复客户端（Cloned Client）**

由于恢复的是整个系统盘，包括客户端程序、配置文件以及身份信息，两台机器会拥有相同的客户端身份。

若允许两者同时执行备份，将导致：

- 同一备份链被两个客户端同时写入；
- 增量备份、CBT、CDP 日志产生冲突；
- 服务端无法确定真正的备份所有者；
- 后续恢复点可能损坏或不可用。

因此，需要保证：

> **同一个客户端身份（ClientID）在任意时刻只能有一个活动备份实例。**

---

## 2. 设计目标

- 保证同一个 ClientID 同时只能有一个客户端执行备份。
- 正常重启后能够继续备份。
- 源主机断电后能够恢复备份能力。
- 恢复出的克隆客户端不能自动接管备份链。
- 不依赖硬件 UUID（兼容异构恢复）。
- 支持后续管理员主动执行接管（Takeover）。

---

## 3. 总体方案

采用 **ClientID + Server Lease（服务端租约）** 的设计。

其中：

- **ClientID**：客户端永久身份，安装时生成，整机恢复后保持不变。
- **Lease（租约）**：由服务端管理，用于保证同一 ClientID 在任意时刻只有一个活动实例。

整个流程如下：

```text
                 Acquire Lease
┌────────────┐ ───────────────▶ ┌──────────────┐
│ 源客户端    │                  │   服务端      │
│ClientID=A123│ ◀─────────────── │Lease=A123    │
└────────────┘     LeaseToken    └──────────────┘
        │                                ▲
        │Heartbeat                       │
        └────────────────────────────────┘

恢复后的另一台主机

┌────────────┐
│ 恢复客户端  │
│ClientID=A123│
└────────────┘
        │
        │Acquire Lease
        ▼

服务端发现已有有效租约

        │
        ▼

     拒绝备份
```

---

# 4. ClientID 设计

## 4.1 定义

ClientID 表示客户端安装身份。

特点：

| 属性 | 说明 |
|------|------|
| 生命周期 | 安装时生成 |
| 是否持久化 | 是 |
| 整机恢复后 | 保持不变 |
| 用途 | 标识备份链所有者 |

示例：

Windows：

```text
C:\ProgramData\RunStor\client.id
```

Linux：

```text
/etc/runstor/client.id
```

内容：

```text
550e8400-e29b-41d4-a716-446655440000
```

要求：

- 安装时生成 UUID。
- 永久保存。
- 整机恢复时随系统一起恢复。
- 不依赖 SMBIOS UUID、MAC、CPU ID 等硬件信息。

---

## 4.2 Go 包建议

建议将身份管理独立成一个包。

目录：

```text
identity/
    client.go
```

接口：

```go
package identity

func LoadOrCreate() (string, error)
func Load() (string, error)
```

职责：

- 首次安装生成 ClientID。
- 持久化到磁盘。
- 后续读取已有 ClientID。

---

# 5. Lease（服务端租约）设计

## 5.1 定义

Lease 表示客户端当前拥有的备份执行权限。

只有持有有效 Lease 的客户端才能执行备份。

## 5.2 数据结构

| 字段 | 说明 |
|------|------|
| ClientID | 客户端身份 |
| LeaseToken | 当前租约 |
| ExpireTime | 过期时间 |
| ConnectionID | 当前连接 |
| LastHeartbeat | 最近心跳 |

示例：

| ClientID | LeaseToken | ExpireTime |
|----------|------------|------------|
| A123 | 8f2c... | 10:30:00 |

---

## 5.3 Go 包建议

建议将租约管理独立成一个包。

目录：

```text
lease/
    manager.go
    token.go
    store.go
```

核心接口：

```go
package lease

func Acquire(clientID string) (LeaseToken, error)
func Renew(clientID string, token LeaseToken) error
func Release(clientID string, token LeaseToken) error
func Validate(clientID string, token LeaseToken) error
```

职责：

- 获取租约。
- 心跳续约。
- 主动释放。
- 校验租约。

---

# 6. 工作流程

## 6.1 获取租约

客户端开始备份前：

```text
AcquireLease(ClientID)
```

服务端逻辑：

```text
是否存在租约？

├── 否
│   └── 发放新租约
│
└── 是
    │
    ├── 已过期
    │   ├── 回收旧租约
    │   └── 发放新租约
    │
    └── 未过期
        └── 拒绝请求
```

返回：

- LeaseToken
- ExpireTime

---

## 6.2 心跳续约

客户端定时发送：

```text
Heartbeat(
    ClientID,
    LeaseToken
)
```

服务端：

- 校验 LeaseToken。
- 更新 LastHeartbeat。
- 延长 ExpireTime。

推荐参数：

| 参数 | 建议值 |
|------|--------|
| 心跳周期 | 10 秒 |
| 租约超时 | 60 秒 |

---

## 6.3 备份请求

所有备份接口统一携带：

```text
ClientID
LeaseToken
```

服务端统一校验：

- ClientID 是否存在；
- LeaseToken 是否匹配；
- 是否仍处于有效租约。

通过后才允许执行：

- 全量备份
- 增量备份
- CBT
- CDP
- 日志提交

---

# 7. 场景处理

## 场景一：正常重启

流程：

1. 客户端重启。
2. TCP 连接断开。
3. 重新连接。
4. 重新申请租约。
5. 服务端发放租约。

结果：

允许继续备份。

---

## 场景二：恢复后同时存在两台客户端

源主机：

```text
ClientID=A123
Lease=有效
```

恢复主机：

```text
ClientID=A123
申请Lease
```

服务端：

发现 ClientID 已存在有效租约。

返回：

```text
ERR_DUPLICATE_CLIENT

检测到客户端已在另一台主机运行。
```

恢复主机无法开始备份。

---

## 场景三：源主机断电

流程：

1. 心跳停止。
2. 超过租约超时时间。
3. 服务端回收租约。
4. 客户端重新启动。
5. 获取新租约。

结果：

恢复备份能力。

---

## 场景四：网络短暂中断

例如：

- 心跳：10 秒；
- 网络中断：20 秒。

由于租约超时为 60 秒，因此：

不会误判为克隆。

---

# 8. 客户端状态机

```text
          Acquire
┌──────────────┐
│ 未持有租约    │
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ 持有租约      │
└──────┬───────┘
       │ StartBackup
       ▼
┌──────────────┐
│ 备份进行中    │
└──────┬───────┘
       │
       │ 心跳超时
       ▼
┌──────────────┐
│ 租约失效      │
└──────┬───────┘
       │
       └────── Acquire ──────▶
```

状态转换：

| 当前状态 | 事件 | 下一状态 |
|----------|------|----------|
| 未持有租约 | 获取成功 | 持有租约 |
| 持有租约 | 开始备份 | 备份进行中 |
| 备份进行中 | 心跳超时 | 租约失效 |
| 租约失效 | 重新获取 | 持有租约 |

---

# 9. 异常处理

| 异常 | 处理方式 |
|------|----------|
| 客户端崩溃 | 等待租约超时 |
| 服务端重启 | 持久化租约或重新建立 |
| 网络抖动 | 保留租约直到超时 |
| 克隆客户端上线 | 拒绝获取租约 |
| 非法 LeaseToken | 返回认证失败 |

---

# 10. Takeover（接管）设计

## 10.1 背景

恢复后的客户端默认不能自动接管备份链。

当管理员确认需要让恢复主机成为新的备份客户端时，可以执行 Takeover。

## 10.2 流程

管理员执行：

```text
Takeover(ClientID)
```

服务端：

1. 回收旧租约；
2. （可选）通知旧客户端租约失效；
3. 为新客户端发放租约。

流程：

```text
管理员
   │
   ▼
Takeover(ClientID)
   │
   ▼
服务端回收旧Lease
   │
   ▼
发放新Lease
   │
   ▼
恢复主机成为唯一备份客户端
```

该机制保证：

- 不会自动接管；
- 接管过程可控；
- 备份链保持一致。

---

# 11. 接口建议（gRPC）

## AcquireLease

```protobuf
message AcquireLeaseRequest {
    string client_id = 1;
}

message AcquireLeaseResponse {
    string lease_token = 1;
    uint64 expire_time = 2;
}
```

---

## Heartbeat

```protobuf
message HeartbeatRequest {
    string client_id = 1;
    string lease_token = 2;
}

message HeartbeatResponse {
    bool success = 1;
}
```

---

## BackupRequest

建议所有备份相关接口统一增加：

```protobuf
message BackupRequest {
    string client_id = 1;
    string lease_token = 2;
}
```

服务端统一校验：

- ClientID；
- LeaseToken；
- 租约有效性。

---

# 12. 错误码建议

| 错误码 | 说明 |
|---------|------|
| ERR_DUPLICATE_CLIENT | 客户端已有活动实例 |
| ERR_LEASE_EXPIRED | 租约已过期 |
| ERR_LEASE_INVALID | 租约无效 |
| ERR_CLIENT_NOT_FOUND | ClientID 不存在 |
| ERR_TAKEOVER_REQUIRED | 需要管理员接管 |

---

# 13. 方案优势

- **兼容整机恢复**：ClientID 随系统恢复，不依赖硬件信息。
- **兼容异构恢复**：物理→虚拟、虚拟→物理均不会误判。
- **避免克隆冲突**：同一 ClientID 始终只有一个活动备份实例。
- **正常重启无影响**：客户端可重新获取租约继续备份。
- **支持灾备接管**：可扩展管理员控制的 Takeover 流程。
- **职责清晰**：身份管理（identity）与租约管理（lease）解耦，符合 Go 包设计习惯，便于后续与 CBT、CDP、增量链等备份状态统一集成。