# THz-TDS 物料水分含量反演服务

基于太赫兹时域光谱（THz-TDS）技术的物料水分含量反演后端服务，采用 Go + Python 混合架构。

## 系统架构

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Go + Gin      │────▶│   PostgreSQL    │     │  Python Celery  │
│   API Server    │     │   Database      │     │    Workers      │
│                 │     │                 │     │                 │
│ • File Upload   │     │ • Raw Waveforms │     │ • FFT 变换      │
│ • RabbitMQ Prod │     │ • Frequency Spec│     │ • Dorney-Duvill. │
│ • WS Progress   │     │ • Optical Params│     │ • PLSR ONNX     │
│ • Result Consum │     │ • Results       │     │   Prediction    │
└─────────────────┘     └─────────────────┘     └─────────────────┘
          │                       │                        │
          │                       ▼                        │
          │              ┌─────────────────┐               │
          └─────────────▶│   RabbitMQ      │◀──────────────┘
                         │   Message Broker│
                         │                 │
                         │ • Task Queue    │
                         │ • Result Queue  │
                         └─────────────────┘
```

## 技术栈

### Go 后端
- **Web Framework**: Gin v1.9.1
- **Database**: GORM + PostgreSQL
- **Message Queue**: RabbitMQ (streadway/amqp)
- **WebSocket**: Gorilla WebSocket
- **CORS**: gin-contrib/cors

### Python Worker
- **Task Queue**: Celery v5.3+
- **Message Queue**: Pika + RabbitMQ
- **FFT/信号处理**: NumPy + SciPy
- **ML 推理**: ONNX Runtime
- **算法**: Dorney-Duvillaret 光学参数提取, PLSR 回归

## 核心功能

1. **文件上传** - 支持 CSV 和 JSON 格式的太赫兹时域波形数据
2. **FFT 频域转换** - 汉宁窗 + 快速傅里叶变换
3. **Dorney-Duvillaret 算法** - 提取吸收系数 α 和折射率 n
4. **PLSR 模型预测** - 基于 ONNX 的偏最小二乘回归预测水分含量
5. **异步处理** - RabbitMQ + 多线程/Celery Worker
6. **实时进度** - WebSocket 推送处理进度
7. **数据持久化** - PostgreSQL 存储原始波形和分析结果

## API 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/health` | 健康检查 |
| POST | `/api/v1/analyses/upload` | 上传波形文件 |
| GET | `/api/v1/analyses` | 获取分析列表 |
| GET | `/api/v1/analyses/:id` | 获取分析详情 |
| GET | `/api/v1/analyses/:id/ws` | WebSocket 进度推送 |

### 文件上传示例

```bash
# JSON 格式
curl -X POST http://localhost:8080/api/v1/analyses/upload \
  -F "sample_name=小麦样本001" \
  -F "material_type=grain" \
  -F "sample_thickness_mm=2.5" \
  -F "file=@sample.json"

# CSV 格式
curl -X POST http://localhost:8080/api/v1/analyses/upload \
  -F "sample_name=小麦样本001" \
  -F "sample_thickness_mm=2.5" \
  -F "file=@sample.csv"
```

### 数据格式

**JSON**:
```json
{
  "time": [0.0, 0.097, 0.195, ...],
  "sample_field": [0.001, 0.005, -0.002, ...],
  "reference_field": [0.001, 0.006, -0.001, ...]
}
```

**CSV**:
```csv
time,sample_field,reference_field
0.0,0.001,0.001
0.097,0.005,0.006
0.195,-0.002,-0.001
...
```

## 快速开始

### 方式一：Docker Compose（推荐）

```bash
# 启动所有服务
docker-compose up -d

# 查看服务状态
docker-compose ps

# 生成测试数据
cd python-worker
python generate_test_data.py --n-samples 5

# 运行 API 测试
python test_api.py --gen-test-data
```

### 方式二：本地开发

#### 1. 启动基础设施

```bash
# PostgreSQL
docker run -d \
  --name thz-postgres \
  -e POSTGRES_DB=thz_db \
  -e POSTGRES_USER=thz_user \
  -e POSTGRES_PASSWORD=thz_password \
  -p 5432:5432 \
  -v $(pwd)/db/schema.sql:/docker-entrypoint-initdb.d/init.sql \
  postgres:16-alpine

# RabbitMQ
docker run -d \
  --name thz-rabbitmq \
  -p 5672:5672 \
  -p 15672:15672 \
  rabbitmq:3.12-management-alpine
```

#### 2. 训练 PLSR 模型

```bash
cd python-worker
pip install -r requirements.txt
python train_plsr_model.py
```

#### 3. 启动 Go 后端

```bash
cd go-backend
go mod tidy
go build -o thz-server ./cmd/server
cp .env.example .env  # 按需修改配置
./thz-server
```

#### 4. 启动 Python Worker

```bash
cd python-worker
cp .env.example .env  # 按需修改配置

# 方式 A: 独立多线程 Worker
python worker.py --threads 3

# 方式 B: Celery Worker
python worker.py --celery --threads 3
```

## 项目结构

```
037/
├── go-backend/                    # Go 后端服务
│   ├── cmd/server/                # 主入口
│   │   └── main.go
│   ├── internal/
│   │   ├── config/                # 配置管理
│   │   ├── handlers/              # HTTP 处理器
│   │   ├── models/                # 数据模型
│   │   ├── rabbitmq/              # 消息队列
│   │   ├── repository/            # 数据库操作
│   │   └── websocket/             # WebSocket 管理
│   ├── go.mod
│   ├── .env.example
│   └── Dockerfile
├── python-worker/                 # Python 计算服务
│   ├── algorithms/                # 核心算法
│   │   ├── fft_processor.py       # FFT 处理器
│   │   ├── dorney_duvillaret.py   # 光学参数提取
│   │   └── plsr_predictor.py      # PLSR 预测
│   ├── models/                    # ONNX 模型
│   ├── celery_app.py              # Celery 任务定义
│   ├── worker.py                  # Worker 主程序
│   ├── train_plsr_model.py        # 模型训练脚本
│   ├── generate_test_data.py      # 测试数据生成
│   ├── test_api.py                # API 测试脚本
│   ├── config.py                  # 配置
│   ├── requirements.txt
│   ├── .env.example
│   └── Dockerfile
├── db/
│   └── schema.sql                 # 数据库 Schema
├── docker-compose.yml             # Docker Compose 配置
└── README.md
```

## 算法说明

### Dorney-Duvillaret 光学参数提取算法

该算法基于太赫兹时域光谱的参考脉冲和样品脉冲的傅里叶变换，提取样品的光学参数：

1. **折射率 n(ω)**:
   ```
   n(ω) = 1 - (c · φ_samp(ω) - φ_ref(ω)) / (ω · d)
   ```
   其中 φ 为相位，d 为样品厚度，c 为光速

2. **消光系数 k(ω)**:
   ```
   k(ω) = -(c / (ω · d)) · ln[ |E_samp| · (n+1)² / (4n · |E_ref|) ]
   ```

3. **吸收系数 α(ω)**:
   ```
   α(ω) = 4πf · k(ω) / c
   ```

### PLSR 预测模型

偏最小二乘回归（PLSR）模型用于从光学参数预测水分含量：
- 输入特征：10个频率点的吸收系数 + 10个频率点的折射率 + 10个统计特征
- 模型通过 `train_plsr_model.py` 离线训练，导出为 ONNX 格式
- 支持 fallback 预测模式（当 ONNX 模型不可用时）

## WebSocket 进度消息格式

```json
{
  "analysis_id": "uuid",
  "status": "processing",
  "progress": 45,
  "message": "Extracting optical parameters",
  "data": { ... }
}
```

**状态流转**:
- `pending` → `queued` → `processing` → `fft_done` → `params_done` → `completed`
- 任何阶段失败 → `failed`

## 数据库 Schema

- `analyses` - 分析任务主表
- `raw_waveforms` - 原始时域波形数据
- `frequency_spectra` - FFT 频域光谱数据
- `optical_params` - 提取的光学参数

## 监控

- RabbitMQ 管理界面: http://localhost:15672 (guest/guest)
- 健康检查: http://localhost:8080/api/v1/health
- 分析列表: http://localhost:8080/api/v1/analyses

## 扩展建议

1. **模型更新**: 使用真实实验数据重新训练 PLSR 模型
2. **验证扩展**: 增加更多的物理模型验证
3. **性能优化**: 对高频 FFT 计算使用 GPU 加速
4. **API 认证**: 添加 JWT 认证
5. **限流**: 添加 API 限流和熔断机制
6. **监控**: 集成 Prometheus + Grafana

## License

MIT License
