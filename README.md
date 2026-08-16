# 巡护协同调度系统 (Patrol Dispatch System)

面向祁连山华隆保护站的野外巡护协同调度后端服务。系统按巡护路线、保护站辖区边界和
物资仓库库存自动生成派单与领用单，支持巡护员签到、野生动物影像与人为干扰事件记录、
异常事件分级流转、应急设备当日并发占用锁定，以及野外网络中断后的本地暂存与信号恢复
自动补传去重。

## 技术栈

- Go 1.26（仅标准库，零外部依赖）
- HTTP 服务，默认监听 `53115`
- 内存持久化（线程安全，自包含）

## 项目结构

```
cmd/server/          服务入口，配置加载、种子数据、优雅关闭
internal/domain/      领域模型与状态机（计划、领用单、设备、事件）
internal/store/       内存持久化层 + 设备并发占用锁
internal/service/     应用编排（派单生成、领用审批、事件路由）
internal/sync/        离线补传管理器（去重、合并、冲突解决）
internal/httpapi/     HTTP 接入层（路由、请求/响应编码）
internal/worker/      后台任务（事件分级路由、补传重试队列）
```

## 快速启动

```bash
# 本地运行
go run ./cmd/server

# 服务启动后监听 :53115，预置 2 个分队、2 条路线、11 件设备
curl http://localhost:53115/healthz
```

环境变量（均有默认值）：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `53115` | 监听端口 |
| `STATION_CHIEF_ID` | `chief-001` | 站站长 ID（审批领用单） |
| `DUTY_ROOM_ID` | `duty-room-001` | 保护站值班室（低/中级事件） |
| `DEPARTMENT_ID` | `forestry-dept-001` | 上级林草部门（高级/严重事件） |

## 主要接口

### 巡护计划与派单

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/teams` | 注册巡护分队 |
| POST | `/api/routes` | 注册巡护路线 |
| POST | `/api/plans` | 提交巡护计划 |
| POST | `/api/plans/{id}/approve` | 审批计划 |
| POST | `/api/plans/{id}/dispatch` | 自动生成派单+领用单 |
| POST | `/api/plans/{id}/start` | 开始巡护 |
| POST | `/api/plans/{id}/complete` | 完成巡护（归还设备） |
| POST | `/api/plans/{id}/cancel` | 取消计划（释放设备占用） |
| GET  | `/api/plans/{id}` | 查询计划 |
| GET  | `/api/plans/{id}/trajectory` | 查询轨迹与事件 |

### 领用审批

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/requisitions/{id}/approve` | 站长审批领用单 |
| POST | `/api/requisitions/{id}/reject` | 站长驳回领用单 |

### 野外记录

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/checkins` | 巡护签到（设备号+巡护员号+时间戳去重） |
| POST | `/api/wildlife` | 野生动物影像记录 |
| POST | `/api/disturbances` | 人为干扰事件（按严重级别自动分级路由） |
| POST | `/api/events/route` | 手动触发事件路由 |

### 离线补传

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/sync` | 信号恢复后批量补传（去重+合并） |

### 物资

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/api/equipment` | 查询设备库存 |

## 核心业务流程

```
提交计划 → 审批 → 生成派单(边界校验+设备预留) → 站长审批领用单 → 发放设备
    → 开始巡护 → 签到/记录影像/记录干扰事件 → 完成巡护 → 归还设备
```

事件分级路由：
- `low` / `medium` → 保护站值班室 (`duty-room-001`)
- `high` / `critical` → 上级林草部门 (`forestry-dept-001`)

应急设备并发控制：同类应急设备（卫星电话、红外相机）当日仅允许一个分队占用，
冲突时返回 `409 Conflict` 并提示调度员改派。

离线补传：记录以 `设备编号 + 巡护员编号 + 时间戳` 去重，同一事件补传时若严重级别
升级则合并替换，保证每条轨迹和事件有且只有一份可追溯记录。

## 测试

```bash
go test -timeout=120s -count=1 ./...
```

测试覆盖：正常路径、错误路径、状态迁移、并发冲突、失败恢复。

## Docker

```bash
# 构建（支持 amd64 与 arm64）
docker build -t patrol-dispatch .

# 运行
docker run -p 53115:53115 patrol-dispatch

# 多架构构建
docker buildx build --platform linux/amd64,linux/arm64 -t patrol-dispatch .
```

## 配置

`config.yaml` 提供配置参考；运行时以环境变量为准（见上表）。
