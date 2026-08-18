# 终端办件时限监测告警服务（termduty）

政务服务中心终端与办件时限监测告警后端服务。采集各办事窗口和自助终端的运行读数（排队人数、办件耗时、故障码），按时间窗口聚合，越过约定范围生成告警工单通知责任人，支持抢占式接单、处置回执闭环、误报撤销、批量调整与补偿回滚。

## 架构概览

- **领域模型** (`internal/domain`)：告警状态机、采集点、规则、读数、分配、审计等核心类型与不变量。
- **业务编排** (`internal/orchestration`)：接入上报、告警评估与抑制、抢占式接单裁定、批量操作补偿、查询统计。
- **持久化** (`internal/store`)：嵌入式 SQLite 索引 + 分片 JSONL 读数文件，清单索引与校验值，事务提交/回滚，重启恢复。
- **对外接入** (`internal/httpapi`)：chi 路由，REST API，角色中间件，分页查询，前端静态托管。
- **后台任务** (`internal/scheduler`)：Ticker 驱动的轮询拉取（租约与超时回收）和告警评估，可优雅停止。
- **横切关注** (`internal/crosscut`)：结构化日志、错误链映射。
- **配置** (`internal/config`)：YAML + 环境变量覆盖。

## 快速开始

```bash
# 1. 编译
go build ./...

# 2. 初始化数据目录
go run ./cmd/ops init -data ./data

# 3. 启动服务（默认端口 57615）
go run ./cmd/server -config config.yaml
```

服务启动后访问 `http://localhost:57615`。

## 运维命令行

```bash
go run ./cmd/ops init           # 创建数据目录并应用迁移
go run ./cmd/ops status         # 存储诊断
go run ./cmd/ops import -file readings.jsonl   # 导入读数
go run ./cmd/ops export -file out.jsonl        # 导出读数
go run ./cmd/ops verify         # 校验分片校验和
go run ./cmd/ops rebuild-index  # 从磁盘分片重建索引
```

## 配置

配置文件 `config.yaml`（参考 `config.yaml.example`），端口、数据目录和超时均可通过环境变量覆盖：

| 环境变量 | 说明 | 默认值 |
|---|---|---|
| `TERMDUTY_HTTP_ADDR` | HTTP 监听地址 | `:57615` |
| `TERMDUTY_DATA_DIR` | 数据目录 | `./data` |
| `TERMDUTY_INGEST_INTERVAL` | 拉取间隔 | `5s` |
| `TERMDUTY_EVAL_INTERVAL` | 告警评估间隔 | `10s` |
| `TERMDUTY_LEASE_TTL` | 租约超时 | `60s` |

## 前端

前端使用 Vue 3 + TypeScript + Element Plus，源码在 `web/` 目录。

```bash
cd web
npm install
npm run build    # 产物输出到 web/dist/
```

Go 服务自动托管 `web/dist/`，构建后直接访问 `http://localhost:57615`。开发模式可用 `npm run dev`（Vite 代理 `/api` 到 `localhost:57615`）。

前端页面：
- **告警工单**：分页列表与多条件筛选（采集点、状态、级别），点击进入详情。
- **告警详情**：展示工单信息，支持接单、开始处置、完成处置、退回、撤销、关闭等状态流转操作。
- **运行读数**：分页列表与筛选，支持导出为 JSONL。
- **采集点管理**：列表筛选、新增、批量停用（补偿回滚）。
- **积压清单**：超时告警与死信队列概览。

## 测试

```bash
go test ./...
go test -race ./...
```

## Docker 评测

```bash
./build_eval_docker.sh termduty-eval linux/amd64
./build_eval_docker.sh termduty-eval linux/arm64
```
