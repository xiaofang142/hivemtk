"""
scripts/inference-host/laya/server.py —— Laya 决策服务（ModernBERT-large 421M，非自回归）

与 llama.cpp 三件套对齐的契约：
  - 端口：读 .env 的 LAYA_PORT（默认 8210，与 ports.go DefaultLayaPort 一致）
  - 健康：GET /health 与 GET /v1/health（start-laya.sh 的 wait_health 只认 /health）
  - 模型：默认读本地产物 models/laya（由 laya/download-model.sh 从 ModelScope 下载）
  - 运行时：PID $HIVEMTK_RUNTIME_DIR/laya.pid，日志 $HIVEMTK_RUNTIME_DIR/laya.log
    由 scripts/inference-host/start-laya.sh 统一管理

Laya 不是 LLM（不支持 chat/completions）：输入是 state + typed questions，
输出是结构化决策（choice / score / noul + calibrated confidence）。
详见 pip 包 laya.Agent.system_one / predict_batch。

端点：
  - GET  /health, /v1/health      健康检查
  - GET  /v1/models               模型列表（OpenAI 风格）
  - GET  /v1/stats                累计统计（对齐 mlx/server.py）
  - POST /v1/decide               单 state 决策 {state, questions, lang?, min_confidence?}
  - POST /v1/decide/batch         多 state 批量 {states[], questions, lang?, batch_size?, min_confidence?}
"""

import os
import threading
import time
import uuid
from datetime import datetime
from typing import Any, Dict, List, Optional

import uvicorn
from fastapi import FastAPI
from fastapi.responses import JSONResponse
from pydantic import BaseModel

VERSION = "1.0.0"

# ---- 配置（单一源：.env → env.sh，未 source 时用项目默认值）----
_PROJECT_ROOT = os.path.normpath(
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", ".."))
LAYA_PORT = int(os.getenv("LAYA_PORT", "8210"))
LAYA_HOST = os.getenv("LAYA_HOST", "0.0.0.0")
MODEL_PATH = os.path.normpath(
    os.getenv("LAYA_MODEL", os.getenv("LAYA_MODEL_DIR",
              os.path.join(_PROJECT_ROOT, "models", "laya"))))
SERVED_MODEL_NAME = os.getenv("LAYA_SERVED_NAME", "laya")
LAYA_DEVICE = os.getenv("LAYA_DEVICE", "") or None  # None = 让 laya 自动选择（MPS/CPU）
LAYA_SUBFOLDER = os.getenv("LAYA_SUBFOLDER", "") or None
_RUNTIME_DIR = os.getenv("HIVEMTK_RUNTIME_DIR", os.path.join(_PROJECT_ROOT, ".runtime"))
STATS_DIR = os.path.join(_RUNTIME_DIR, "laya-stats")
STATS_FILE = os.path.join(STATS_DIR, "stats.json")
STATS_FLUSH_INTERVAL = int(os.getenv("LAYA_STATS_FLUSH_INTERVAL", "30"))


def _fail(msg: str):
    print(f"[laya] ❌ {msg}", flush=True)
    raise SystemExit(1)


# ---- 启动前配置校验 ----
if not os.path.isdir(MODEL_PATH):
    _fail(f"模型目录不存在: {MODEL_PATH}（先执行 bash scripts/inference-host/laya/download-model.sh）")
if not any(f.endswith(".safetensors") for f in os.listdir(MODEL_PATH)):
    _fail(f"模型目录缺少权重文件(.safetensors): {MODEL_PATH}")
try:
    import laya  # noqa: F401
except ImportError:
    _fail("laya 未安装，请先执行：pip install laya fastapi uvicorn pydantic")

os.makedirs(STATS_DIR, exist_ok=True)
_START_TIME = time.time()

app = FastAPI(title="hivemtk-laya-decision", version=VERSION)
print(f"[laya] 加载模型: {MODEL_PATH} device={LAYA_DEVICE or 'auto'}", flush=True)
_AGENT = laya.load(MODEL_PATH, device=LAYA_DEVICE, subfolder=LAYA_SUBFOLDER)
print("[laya] 模型就绪", flush=True)

# torch 单进程推理串行化（与 mlx/server.py 的 _INFER_LOCK 同理）
_INFER_LOCK = threading.Lock()


class Stats:
    def __init__(self):
        self.lock = threading.Lock()
        self.started_at = datetime.now().isoformat(timespec="seconds")
        self.requests_total = 0
        self.requests_ok = 0
        self.requests_failed = 0
        self.batch_requests = 0
        self.latency_sum_ms = 0.0
        self.today = ""
        self.today_requests = 0
        self.last_error = ""

    def _roll_day(self):
        d = datetime.now().strftime("%Y-%m-%d")
        if d != self.today:
            self.today = d
            self.today_requests = 0

    def record(self, ok: bool, latency_ms=0.0, batch=False, error=""):
        with self.lock:
            self._roll_day()
            self.requests_total += 1
            self.today_requests += 1
            if ok:
                self.requests_ok += 1
                self.latency_sum_ms += latency_ms
                if batch:
                    self.batch_requests += 1
            else:
                self.requests_failed += 1
                self.last_error = error[:200]
            now = time.time()
            if now - getattr(self, "_last_flush", 0.0) >= STATS_FLUSH_INTERVAL:
                self._last_flush = now
                self._flush_locked()

    def _flush_locked(self):
        try:
            import json
            with open(STATS_FILE, "w", encoding="utf-8") as f:
                json.dump(self.snapshot_locked(), f, ensure_ascii=False, indent=2)
        except OSError as e:
            print(f"[laya] stats flush 失败: {e}", flush=True)

    def snapshot_locked(self):
        ok = self.requests_ok
        return {
            "version": VERSION,
            "model": SERVED_MODEL_NAME,
            "model_path": MODEL_PATH,
            "started_at": self.started_at,
            "uptime_seconds": int(time.time() - _START_TIME),
            "requests_total": self.requests_total,
            "requests_ok": ok,
            "requests_failed": self.requests_failed,
            "batch_requests": self.batch_requests,
            "avg_latency_ms": round(self.latency_sum_ms / ok, 1) if ok else 0,
            "today": {"date": self.today, "requests": self.today_requests},
            "last_error": self.last_error,
        }

    def snapshot(self):
        with self.lock:
            return self.snapshot_locked()


STATS = Stats()


@app.on_event("shutdown")
def _on_shutdown():
    with STATS.lock:
        STATS._flush_locked()


class DecideReq(BaseModel):
    state: Any
    questions: Dict[str, Any]
    lang: Optional[str] = None
    min_confidence: Optional[float] = None
    max_len: Optional[int] = None
    head_max_len: Optional[int] = None


class BatchDecideReq(BaseModel):
    states: List[Any]
    questions: Dict[str, Any]
    lang: Optional[str] = None
    batch_size: Optional[int] = None
    min_confidence: Optional[float] = None
    max_len: Optional[int] = None
    head_max_len: Optional[int] = None


@app.get("/health")
@app.get("/v1/health")
def health():
    return {
        "status": "ok", "model": SERVED_MODEL_NAME, "path": MODEL_PATH,
        "version": VERSION, "uptime_seconds": int(time.time() - _START_TIME),
        "engine": "laya",
    }


@app.get("/v1/models")
def list_models():
    return {
        "object": "list",
        "data": [{"id": SERVED_MODEL_NAME, "object": "model", "owned_by": "hivemtk-local"}],
    }


@app.get("/v1/stats")
def stats():
    return STATS.snapshot()


@app.post("/v1/decide")
def decide(req: DecideReq):
    if not isinstance(req.questions, dict) or not req.questions:
        STATS.record(ok=False, error="questions must be non-empty dict")
        return JSONResponse(status_code=400, content={"error": "questions must be a non-empty dict"})
    req_id = f"laya-{uuid.uuid4().hex[:12]}"
    start = time.perf_counter()
    try:
        with _INFER_LOCK:
            result = _AGENT.system_one(
                req.state, req.questions, lang=req.lang,
                min_confidence=req.min_confidence,
                max_len=req.max_len, head_max_len=req.head_max_len)
    except Exception as e:  # noqa: BLE001
        STATS.record(ok=False, error=str(e))
        return JSONResponse(status_code=500, content={"error": str(e)[:500], "id": req_id})
    latency_ms = (time.perf_counter() - start) * 1000
    STATS.record(ok=True, latency_ms=latency_ms)
    return {"id": req_id, "model": SERVED_MODEL_NAME, "latency_ms": round(latency_ms, 1),
            "result": result}


@app.post("/v1/decide/batch")
def decide_batch(req: BatchDecideReq):
    if not req.states:
        STATS.record(ok=False, error="states must be non-empty")
        return JSONResponse(status_code=400, content={"error": "states must be a non-empty list"})
    if not isinstance(req.questions, dict) or not req.questions:
        STATS.record(ok=False, error="questions must be non-empty dict")
        return JSONResponse(status_code=400, content={"error": "questions must be a non-empty dict"})
    req_id = f"laya-{uuid.uuid4().hex[:12]}"
    start = time.perf_counter()
    try:
        with _INFER_LOCK:
            results = _AGENT.predict_batch(
                list(req.states), req.questions, lang=req.lang,
                batch_size=req.batch_size, min_confidence=req.min_confidence,
                max_len=req.max_len, head_max_len=req.head_max_len)
    except Exception as e:  # noqa: BLE001
        STATS.record(ok=False, error=str(e))
        return JSONResponse(status_code=500, content={"error": str(e)[:500], "id": req_id})
    latency_ms = (time.perf_counter() - start) * 1000
    STATS.record(ok=True, latency_ms=latency_ms, batch=True)
    return {"id": req_id, "model": SERVED_MODEL_NAME, "latency_ms": round(latency_ms, 1),
            "results": results}


if __name__ == "__main__":
    print(f"[laya] 启动: host={LAYA_HOST} port={LAYA_PORT} model={SERVED_MODEL_NAME} "
          f"stats={STATS_FILE}", flush=True)
    uvicorn.run(app, host=LAYA_HOST, port=LAYA_PORT)
