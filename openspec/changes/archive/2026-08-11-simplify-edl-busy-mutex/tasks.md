# Tasks: 服务端 busy 互斥替代客户端租约

## 1. 后端核心

- [x] 1.1 `internal/runtime/edl_session.go` 删除租约字段与方法 (Acquire/Renew/Release/Owns/OwnedSnapshot/expireLocked/ttl), Begin/EndOperation 去掉 token
- [x] 1.2 `internal/runtime/edl_session.go` BeginOperation 会话不存在时自动创建; 驱逐仅跳过 busy 会话
- [x] 1.3 `internal/domain/device/edl.go` EDLSessionSnapshot 删除 lease_held/lease_owned/lease_expires_at
- [x] 1.4 `internal/runtime/runtime.go` NewEDLSessionManager 去掉 ttl 参数

## 2. 服务与 HTTP 层

- [x] 2.1 `internal/application/firmware/service.go` 删除 Acquire/Renew/ReleaseControlLease; BeginControlOperation 去掉 token
- [x] 2.2 `internal/application/firmware/service.go` Status 统一附加掩码会话快照 (含 active_operation), 删除 StatusForLease
- [x] 2.3 `internal/api/http/server.go` 删除租约端点/请求头/WS 子协议; shell 连接持有 busy
- [x] 2.4 `internal/api/http/openapi.go` 删除租约路径与请求头参数

## 3. 测试

- [x] 3.1 `internal/runtime/edl_session_test.go` 重写为 busy 语义 (单操作互斥/释放/驱逐)
- [x] 3.2 `internal/api/http/device_control_contract_test.go` 重写: 空闲直接接受动作, busy 409, active_operation 展示
- [x] 3.3 全量 go build/vet/test 通过

## 4. 前端

- [x] 4.1 `web/src/services/api.ts` 删除租约助手与请求头
- [x] 4.2 `web/src/App.vue` 删除 ensureDeviceControlLease 调用
- [x] 4.3 `web/src/views/FirmwareView.vue` 删除租约逻辑, controlBlocked 基于 active_operation
- [x] 4.4 `web/src/types.ts` 删除租约字段; vue-tsc + vite build 通过

## 5. 规范与文档

- [x] 5.1 `openspec/specs/edl-session-control/spec.md` 租约需求改为 busy 互斥
- [x] 5.2 `docs/device-control.md`、`docs/code-map/contracts.md`、`docs/code-map/frontend.md`、`docs/code-map/diagnostics.md` 移除租约描述
- [ ] 5.3 归档 change (openspec archive)
