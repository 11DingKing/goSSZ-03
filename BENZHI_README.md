# BENZHI_README

## 项目说明

- 项目：11DingKing/goSSZ-03
- 项目用途：面向祁连山华隆保护站的野外巡护协同调度后端服务。系统按巡护路线、保护站辖区边界和 物资仓库库存自动生成派单与领用单，支持巡护员签到、野生动物影像与人为干扰事件记录、 异常事件分级流转、应急设备当日并发占用锁定，以及野外网络中断后的本地暂存与信号恢复 自动补传去重。
- Go 工具链：`golang:1.26`
- 前端工具链：无

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd '/app' && GOTOOLCHAIN=local go build ./...

# 启动
cd '/app' && GOTOOLCHAIN=local go run ./cmd/server

# 测试
cd '/app' && GOTOOLCHAIN=local go test ./...
```

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-task-18-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-task-18-arm64 linux/arm64
docker run -it benzhi-task-18-amd64:latest
docker run -it --platform linux/arm64 benzhi-task-18-arm64:latest
```

## 题目验证命令

1. 预期退出码 1：`go test ./internal/service/ -run "^TestEachDispatchOrderKeepsItsOwnEquipmentList$" -count=1 -v`

## Bug 复现

Bug 现象、触发步骤和完整错误信息见 `BUG_REPRO.md`。
