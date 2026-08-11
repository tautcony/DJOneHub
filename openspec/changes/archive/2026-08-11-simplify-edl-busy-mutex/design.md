# Design: 服务端 busy 互斥替代客户端租约

## 现状问题

EDL 会话控制有 lease (token + 30s TTL + 续租) + activeOperation 钉住 + WS 子协议
三层机制。服务仅绑定 loopback, 全部客户端都是本机 tab, 租约的"识别持锁者"
价值极低, 却引入了 TOCTOU、挂死锁死、虚假 409 等缺陷。

## 目标模型

```
请求 ──► BeginControlOperation(operation) ──► 会话 activeOperation = operation
                                                  │ (busy)
        ┌───────────── 409 busy ◄─────────────── 第二个并发请求
        ▼
  异步操作 (operation.Manager)
        ▼ 终态 (operation.completed 事件 / 兜底轮询)
   EndOperation ──► activeOperation = ""
```

- **互斥**: `EDLSessionManager.BeginOperation/EndOperation`, 单一 `activeOperation`
  字段, 无 token、无 TTL、无到期。
- **释放时机**: HTTP 动作经 `trackDeviceControlOperation` 在操作终态释放;
  ADB shell 在连接关闭时释放 (带 pong 心跳兜底半开连接); 挂死操作 30 分钟
  deadline 到期先 Cancel 再释放。
- **状态读路径**: 不参与互斥; 会话有活跃操作时不探测 (Firehose 独占接口),
  探测单飞, 失败保留已验证事实。
- **前端**: 删除 token/sessionStorage/租约请求头/子协议; 控件门控依据
  `active_operation` (设备忙) + 能力快照。

## 关键决策

1. `BeginOperation` 在会话不存在时按物理位置自动创建会话, 互斥不依赖先前的
   EDL 观察。
2. 会话容量驱逐仅跳过 `activeOperation != ""` 的会话。
3. `Status()` 统一附加掩码会话快照 (含 `active_operation`), HTTP 与 WS 快照
   同源; WS 快照只读缓存, 连接绝不触发探测。
4. 冲突错误统一为 `device_session_conflict` + "the device is busy with an
   in-flight operation", 不再有"另一个客户端"的误导语义。

## 删除面

- API: `POST/PUT/DELETE /api/v1/device-control/session/lease`,
  `X-DJOneHub-Device-Lease` 请求头, WS 子协议。
- 运行时: `Acquire/Renew/Release/Owns/OwnedSnapshot/expireLocked/ttl`。
- 前端: `ensureDeviceControlLease`、sessionStorage token、租约请求头、
  子协议构造。
