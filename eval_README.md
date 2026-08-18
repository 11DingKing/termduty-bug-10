# eval_README — 评测镜像说明

## 项目用途

政务服务中心终端与办件时限监测告警服务（termduty）。各办事窗口和自助终端按固定节奏在各自台账留存运行读数（排队人数、办件耗时、故障码），值班台按周期拉取新读数并按时间窗口聚合看趋势；读数越过约定范围且问题未消除才生成告警单通知对应责任人，同一对象同一问题未消除前不再重复打扰，多名责任人抢接同一告警单只能一人生效。

## 数据目录

服务默认数据目录为 `./data`，包含嵌入式 SQLite 索引库（`termduty.db`）和分片 JSONL 读数文件（`shards/`）。所有路径可通过配置文件或环境变量覆盖。

## 标准命令

```bash
# 编译全部 Go 包
go build ./...

# 运行 HTTP 服务（默认监听 :57615）
go run ./cmd/server -config config.yaml

# 运维命令行（至少六个子命令）
go run ./cmd/ops init -data ./data
go run ./cmd/ops status -data ./data
go run ./cmd/ops import -file readings.jsonl -data ./data
go run ./cmd/ops export -file out.jsonl -data ./data
go run ./cmd/ops verify -data ./data
go run ./cmd/ops rebuild-index -data ./data

# 运行全部测试
go test ./...
go test -race ./...

# 前端构建
cd web && npm install && npm run build
```

## 服务端口 57615 运行示例

```bash
# 方式一：配置文件
cp config.yaml.example config.yaml
go run ./cmd/server -config config.yaml
# 服务监听 http://localhost:57615

# 方式二：环境变量覆盖
TERMDUTY_HTTP_ADDR=:57615 TERMDUTY_DATA_DIR=./data go run ./cmd/server

# 方式三：初始化数据后启动
go run ./cmd/ops init -data ./data
go run ./cmd/server -config config.yaml
```

前端访问：先 `cd web && npm run build`，构建产物在 `web/dist/`，Go 服务会自动托管该目录，浏览器打开 `http://localhost:57615` 即可。

## 多架构构建命令

```bash
# amd64 镜像
./build_eval_docker.sh termduty-eval:amd64 linux/amd64

# arm64 镜像
./build_eval_docker.sh termduty-eval:arm64 linux/arm64
```
