## Why

EDL 会话控制引入了一套客户端租约协议 (token + 30s TTL + 续租 + sessionStorage +
WS 子协议), 但本服务仅绑定 loopback, 所有"其他客户端"都是本机另一个 tab。
租约协议为这个场景带来了不成比例的复杂度, 也是多次代码评审发现的主要缺陷
来源 (TOCTOU 矛盾状态、挂死操作锁死全部客户端、虚假 409、误导性冲突消息)。
目标是只保留核心不变量: 一个设备同时刻不被多个请求端调用。

## What Changes

- **BREAKING**: 删除客户端租约: `POST/PUT/DELETE /api/v1/device-control/session/lease`
  端点、`X-DJOneHub-Device-Lease` 请求头、WS 子协议、sessionStorage token 管理。
- 互斥改为纯服务端 busy 状态: 动作请求直接检查并持有 busy, 同时刻第二个请求
  收到 409 `device_session_conflict`。
- busy 的释放与操作真实生命周期绑定: 操作到达终态或 shell 连接关闭时释放;
  挂死操作在有限 deadline 后被取消再释放。
- 状态读路径 (状态轮询、WebSocket 快照) 不参与互斥, 也不在操作进行中探测设备。
- 前端删除 `ensureDeviceControlLease` 与租约请求头; 控件门控依据能力快照与
  设备忙状态 (`active_operation`)。

## Capabilities

- **Modified Capabilities**:
  - `edl-session-control`: 会话互斥需求从"可续租约 + 操作钉住"改为"服务端 busy
    状态互斥", 删除租约/续租/到期场景。

## Impact

- 后端: `internal/runtime/edl_session.go` (删除 Acquire/Renew/Release/Owns/
  OwnedSnapshot/expireLocked/ttl), `internal/application/firmware/service.go`
  (删除 AcquireControlLease/RenewControlLease/ReleaseControlLease, 状态附加
  会话快照), `internal/api/http/server.go` (删除租约端点/请求头/子协议),
  `internal/api/http/openapi.go`, `internal/domain/device/edl.go`
  (EDLSessionSnapshot 删除 lease_held/lease_owned/lease_expires_at)。
- 前端: `web/src/services/api.ts`, `web/src/App.vue`, `web/src/views/FirmwareView.vue`,
  `web/src/types.ts`。
- 测试: `internal/runtime/edl_session_test.go`、`internal/api/http/device_control_contract_test.go`
  重写为 busy 语义。
- 文档: `docs/device-control.md`、`docs/code-map/contracts.md`、
  `docs/code-map/frontend.md`、`docs/code-map/diagnostics.md`。
