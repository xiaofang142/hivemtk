"""
scripts/inference-host/mlx/server.py —— MLX 版 LLM 服务（SmolLM3-3B 4bit，产品化版）

与 llama.cpp 栈对齐的契约：
  - 端口：读 .env 的 LLM_PORT（默认 8207，与 ports.go DefaultLLMPort 一致）
  - 路径：POST /v1/chat/completions（OpenAI 兼容，user-server dispatcher 直连）
  - 模型：默认读本地转换产物 models/llm/SmolLM3-3B-4bit-mlx
          （由 download-model.sh 从 ModelScope 下载 + mlx_lm.convert 生成）

OpenAI 兼容要点（对齐 user-server/internal/aiagent/llm/llm.go 的 chatRequest/chatResponse）：
  - 请求体接受 messages（system/user/assistant 多轮），经 chat template 拼为模型输入；
    同时兼容简化客户端的 prompt 字段
  - 响应返回 choices[0].message.content + usage（token 统计），
    dispatcher 读取 resp.Usage 与 choice.Message.Content
  - stream=true 时走 SSE（chat.completion.chunk + [DONE]），与 OpenAI 规范一致
  - temperature/top_p 经 make_sampler 透传

工具调用（Function Calling）策略：
  - SmolLM3-3B 不支持原生并行 tool_calls 输出；provider 侧 no_fc=true 时
    dispatcher 不会下发 tools。若客户端仍传入 tools，则将工具定义注入 system
    提示，引导模型按 ReAct 文本协议输出 Action/Action Input（优雅降级），
    不报错、不阻塞

SmolLM3 双模推理：
  - chat template 默认 enable_thinking=true，思考链会泄漏进 content 且小模型
    思考时易陷入重复循环，客服场景默认关闭（MLX_ENABLE_THINKING=true 可开启）
  - 请求体 system 消息含 /think 时按模板原生语义临时开启

统计：
  - /v1/stats 返回累计请求数、成功/失败、prompt/completion/total tokens、
    平均延迟、今日分桶；快照定期落盘 $MLX_STATS_DIR/stats.json，重启不丢

生命周期：
  - 统一由 scripts/inference-host/start-llm.sh（LLM_ENGINE=mlx）管理
    PID 文件：$HIVEMTK_RUNTIME_DIR/llm.pid，日志：$HIVEMTK_RUNTIME_DIR/llm.log
"""

import json
import os
import threading
import time
import uuid
from datetime import datetime
from typing import List, Optional

import uvicorn
from fastapi import FastAPI, HTTPException
from fastapi.responses import JSONResponse, StreamingResponse
from mlx_lm import generate, load, stream_generate
from mlx_lm.generate import make_sampler
from pydantic import BaseModel

VERSION = "1.1.0"

# ---- 配置（单一源：.env → env.sh，未 source 时用项目默认值）----
_PROJECT_ROOT = os.path.normpath(
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", ".."))
LLM_PORT = int(os.getenv("LLM_PORT", "8207"))
LLM_HOST = os.getenv("MLX_HOST", "0.0.0.0")
MODEL_PATH = os.path.normpath(
    os.getenv("MLX_MODEL", os.path.join(_PROJECT_ROOT, "models", "llm", "SmolLM3-3B-4bit-mlx")))
SERVED_MODEL_NAME = os.getenv("LLM_SERVED_NAME", "smollm3-3b-4bit-mlx")
MAX_TOKENS_DEFAULT = int(os.getenv("MLX_MAX_TOKENS", "1024"))
# KV cache 上限：限制上下文窗口，避免超长历史撑爆统一内存（M1 16GB 敏感）。
# 不设置则使用模型原生窗口（SmolLM3 为 4096）。
MAX_KV_SIZE = int(os.getenv("MLX_MAX_KV_SIZE", "0")) or None
ENABLE_THINKING = os.getenv("MLX_ENABLE_THINKING", "false").lower() in ("1", "true", "yes")
_RUNTIME_DIR = os.getenv("HIVEMTK_RUNTIME_DIR", os.path.join(_PROJECT_ROOT, ".runtime"))
STATS_DIR = os.getenv("MLX_STATS_DIR", os.path.join(_RUNTIME_DIR, "mlx-stats"))
STATS_FILE = os.path.join(STATS_DIR, "stats.json")
STATS_FLUSH_INTERVAL = int(os.getenv("MLX_STATS_FLUSH_INTERVAL", "30"))


def _safe_stats_file():
    """stats 落盘路径闸：realpath+commonpath 必须留在 STATS_DIR 内（拒绝 ../ / 符号链接逃逸）。"""
    root = os.path.realpath(STATS_DIR)
    real = os.path.realpath(STATS_FILE)
    if real == root or os.path.commonpath([root, real]) != root:
        raise ValueError(f"stats 路径越出允许目录: {STATS_FILE!r}")
    return real


def _fail(msg: str):
    print(f"[mlx-llm] ❌ {msg}", flush=True)
    raise SystemExit(1)


# ---- 启动前配置校验 ----
if not os.path.isdir(MODEL_PATH):
    _fail(f"模型目录不存在: {MODEL_PATH}（先执行 bash mlx/download-model.sh）")
if not any(f.endswith(".safetensors") for f in os.listdir(MODEL_PATH)):
    _fail(f"模型目录缺少权重文件(.safetensors): {MODEL_PATH}")
try:
    import mlx_lm  # noqa: F401
except ImportError:
    _fail("mlx_lm 未安装，请先执行：pip install mlx-lm fastapi uvicorn pydantic")

os.makedirs(STATS_DIR, exist_ok=True)
_START_TIME = time.time()

app = FastAPI(title="hivemtk-mlx-llm", version=VERSION)
print(f"[mlx-llm] 加载模型: {MODEL_PATH}", flush=True)
if MAX_KV_SIZE:
    print(f"[mlx-llm] 限制 KV cache 窗口: {MAX_KV_SIZE}", flush=True)
model, tokenizer = load(MODEL_PATH)

# 显式加载 chat_template.jinja（部分 mlx_lm 版本不自动读取该文件，
# 显式注入保证双模模板生效）
_TEMPLATE_PATH = os.path.join(MODEL_PATH, "chat_template.jinja")
if os.path.exists(_TEMPLATE_PATH):
    with open(_TEMPLATE_PATH, "r", encoding="utf-8") as f:
        tokenizer.chat_template = f.read()

# MLX 单进程推理非并发安全，全局锁串行执行生成
_INFER_LOCK = threading.Lock()


# ============================================================
# 统计（内存累计 + 定期落盘）
# ============================================================
class Stats:
    def __init__(self):
        self.lock = threading.Lock()
        self.started_at = datetime.now().isoformat(timespec="seconds")
        self.requests_total = 0        # 累计请求（含流式）
        self.requests_ok = 0
        self.requests_failed = 0
        self.stream_requests = 0
        self.tool_fallback_requests = 0  # tools 注入降级次数
        self.prompt_tokens = 0
        self.completion_tokens = 0
        self.total_tokens = 0
        self.latency_sum_ms = 0.0
        self.today = ""                  # YYYY-MM-DD 分桶键
        self.today_requests = 0
        self.today_tokens = 0
        self.last_error = ""

    def _roll_day(self):
        d = datetime.now().strftime("%Y-%m-%d")
        if d != self.today:
            self.today = d
            self.today_requests = 0
            self.today_tokens = 0

    def record(self, ok: bool, prompt_tokens=0, completion_tokens=0,
               latency_ms=0.0, stream=False, tool_fallback=False, error=""):
        with self.lock:
            self._roll_day()
            self.requests_total += 1
            self.today_requests += 1
            if ok:
                self.requests_ok += 1
                self.prompt_tokens += prompt_tokens
                self.completion_tokens += completion_tokens
                self.total_tokens += prompt_tokens + completion_tokens
                self.today_tokens += prompt_tokens + completion_tokens
                self.latency_sum_ms += latency_ms
                if stream:
                    self.stream_requests += 1
                if tool_fallback:
                    self.tool_fallback_requests += 1
            else:
                self.requests_failed += 1
                self.last_error = error[:200]
            self._maybe_flush_locked()

    _last_flush = 0.0

    def _maybe_flush_locked(self):
        now = time.time()
        if now - self._last_flush >= STATS_FLUSH_INTERVAL:
            self._last_flush = now
            self._flush_locked()

    def _flush_locked(self):
        try:
            with open(_safe_stats_file(), "w", encoding="utf-8") as f:
                json.dump(self.snapshot_locked(), f, ensure_ascii=False, indent=2)
        except (OSError, ValueError) as e:
            print(f"[mlx-llm] stats flush 失败: {e}", flush=True)

    def flush(self):
        with self.lock:
            self._flush_locked()

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
            "stream_requests": self.stream_requests,
            "tool_fallback_requests": self.tool_fallback_requests,
            "prompt_tokens": self.prompt_tokens,
            "completion_tokens": self.completion_tokens,
            "total_tokens": self.total_tokens,
            "avg_latency_ms": round(self.latency_sum_ms / ok, 1) if ok else 0,
            "today": {"date": self.today, "requests": self.today_requests,
                      "tokens": self.today_tokens},
            "last_error": self.last_error,
        }

    def snapshot(self):
        with self.lock:
            return self.snapshot_locked()

    def load_persisted(self):
        """恢复历史累计值（started_at/uptime 以本次进程为准）"""
        try:
            with open(_safe_stats_file(), "r", encoding="utf-8") as f:
                old = json.load(f)
        except (OSError, ValueError):
            return
        with self.lock:
            for k in ("requests_total", "requests_ok", "requests_failed",
                      "stream_requests", "tool_fallback_requests",
                      "prompt_tokens", "completion_tokens", "total_tokens",
                      "latency_sum_ms"):
                if isinstance(old.get(k), (int, float)):
                    setattr(self, k, old[k])


STATS = Stats()
STATS.load_persisted()


@app.on_event("shutdown")
def _on_shutdown():
    STATS.flush()


# ---- 请求模型（OpenAI 兼容子集）----
class ChatMessage(BaseModel):
    role: str
    content: str = ""


class ChatReq(BaseModel):
    # OpenAI 标准字段
    model: Optional[str] = None
    messages: Optional[List[ChatMessage]] = None
    temperature: Optional[float] = None
    max_tokens: Optional[int] = None
    top_p: Optional[float] = None
    stream: Optional[bool] = False
    # 简化客户端兼容字段（原始脚本形态）
    prompt: Optional[str] = None
    # 工具调用：SmolLM3-3B 无原生 FC，注入 system 走 ReAct 文本协议降级
    tools: Optional[list] = None
    tool_choice: Optional[object] = None
    response_format: Optional[object] = None


def _tool_injection_note(tools: Optional[list]) -> str:
    """将 tools 定义压缩为 ReAct 文本协议指令，追加进 system 消息。

    2026-08-11 优化（提速，不换模型）：
    原实现把每个工具的完整 parameters JSON schema（type/description/required/properties
    全量）整段写入 system，导致 18 个工具轻松占 2000+ token，小模型（SmolLM3-3B）prefill
    与首 token 延迟爆炸（实测单次请求 input≈4640 token、延迟≈98s）。
    3B 模型走 ReAct 文本降级时，主要靠 工具名+用途 识别该调哪个工具，冗长 JSON schema
    既看不懂也用不好。故精简为：工具名 + 一句话描述 + 参数 key 列表（不带 type/描述），
    通常可砍掉 60%-70% 的工具注入 token，直接降低 prefill 耗时与首 token 延迟。
    """
    if not tools:
        return ""
    lines = []
    for t in tools:
        fn = (t or {}).get("function") or {}
        name = fn.get("name", "")
        if not name:
            continue
        desc = (fn.get("description") or "").strip()
        # 仅取参数 property 的 key 名（最多 8 个），丢弃 type/描述/required 等冗余
        props = (((fn.get("parameters") or {}).get("properties")) or {})
        keys = list(props.keys())[:8]
        param_hint = f" [参数: {', '.join(keys)}]" if keys else ""
        lines.append(f"- {name}{param_hint}: {desc}")
    if not lines:
        return ""
    return (
        "\n\n你可以调用以下工具。需要调用时严格按此文本协议输出（每次仅一个）：\n"
        "Action: 工具名\nAction Input: JSON 参数\n"
        "不要输出协议之外的多余内容。可用工具：\n" + "\n".join(lines)
    )


def _build_prompt(req: ChatReq):
    """构造模型输入，返回 (prompt_text, tool_fallback)"""
    tool_fallback = False
    if req.messages:
        msgs = [{"role": m.role, "content": m.content} for m in req.messages]
        if req.tools:
            note = _tool_injection_note(req.tools)
            # 追加到已有 system；无 system 则前置一条
            if msgs and msgs[0]["role"] == "system":
                msgs[0]["content"] += note
            else:
                msgs.insert(0, {"role": "system", "content": note.strip()})
            tool_fallback = True
        # system 中带 /think（且无 /no_think）时临时开启思考，模板原生支持
        thinking = ENABLE_THINKING or any(
            m["role"] == "system" and "/think" in m["content"] and "/no_think" not in m["content"]
            for m in msgs
        )
        try:
            prompt_text = tokenizer.apply_chat_template(
                msgs, add_generation_prompt=True, tokenize=False, enable_thinking=thinking)
        except TypeError:
            # 模板不接受 enable_thinking 参数时降级重试
            prompt_text = tokenizer.apply_chat_template(
                msgs, add_generation_prompt=True, tokenize=False)
        except Exception:
            # chat_template 缺失时退化为朴素拼接
            prompt_text = "\n".join(f"{m['role']}: {m['content']}" for m in msgs) + "\nassistant:"
    elif req.prompt:
        prompt_text = req.prompt
    else:
        raise HTTPException(status_code=400, detail="messages or prompt is required")
    return prompt_text, tool_fallback


def _sampler(req: ChatReq):
    temp = req.temperature if req.temperature is not None else 0.7
    # mlx_lm 0.31.x：generate() 不接受 temp 关键字，需 make_sampler 构造采样器
    # （temp=0 时为 greedy argmax）
    return make_sampler(temp, top_p=req.top_p or 0.0)


def _error_body(msg: str, status: int = 400):
    return {"error": {"message": msg, "type": "invalid_request_error", "code": status}}


# ============================================================
# 端点
# ============================================================
@app.get("/health")
@app.get("/v1/health")
def health():
    return {
        "status": "ok", "model": SERVED_MODEL_NAME, "path": MODEL_PATH,
        "version": VERSION, "uptime_seconds": int(time.time() - _START_TIME),
        "engine": "mlx",
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


@app.post("/v1/chat/completions")
def chat(req: ChatReq):
    try:
        prompt_text, tool_fallback = _build_prompt(req)
    except HTTPException as e:
        STATS.record(ok=False, error=str(e.detail))
        return JSONResponse(status_code=400, content=_error_body(str(e.detail)))

    max_tokens = req.max_tokens or MAX_TOKENS_DEFAULT
    sampler = _sampler(req)
    prompt_ids = tokenizer.encode(prompt_text)
    start = time.perf_counter()
    cmpl_id = f"chatcmpl-{uuid.uuid4().hex[:24]}"
    created = int(time.time())

    # KV cache 窗口限制（M1 统一内存敏感）：透传给 stream_generate/generate
    _gen_kwargs = {}
    if MAX_KV_SIZE:
        _gen_kwargs["max_kv_size"] = MAX_KV_SIZE

    # ---- 流式 SSE ----
    if req.stream:
        def event_stream():
            comp_tokens = 0
            try:
                with _INFER_LOCK:
                    for resp in stream_generate(
                            model, tokenizer, prompt=prompt_text,
                            max_tokens=max_tokens, sampler=sampler, **_gen_kwargs):
                        # 0.31.x 返回 GenerationResponse 对象，旧版为 dict，getattr 兼容
                        text = getattr(resp, "text", None)
                        if text is None and isinstance(resp, dict):
                            text = resp.get("text", "")
                        if text:
                            chunk = {
                                "id": cmpl_id, "object": "chat.completion.chunk",
                                "created": created, "model": SERVED_MODEL_NAME,
                                "choices": [{"index": 0, "delta": {"content": text},
                                             "finish_reason": None}],
                            }
                            yield f"data: {json.dumps(chunk, ensure_ascii=False)}\n\n"
                        tok_idx = getattr(resp, "token", None)
                        if tok_idx is None and isinstance(resp, dict):
                            tok_idx = resp.get("token", 0)
                        comp_tokens = max(comp_tokens, (tok_idx or 0) + 1)
                latency_ms = (time.perf_counter() - start) * 1000
                p_tok = len(prompt_ids)
                c_tok = max(comp_tokens, 1)
                STATS.record(ok=True, prompt_tokens=p_tok, completion_tokens=c_tok,
                             latency_ms=latency_ms, stream=True, tool_fallback=tool_fallback)
                final = {
                    "id": cmpl_id, "object": "chat.completion.chunk",
                    "created": created, "model": SERVED_MODEL_NAME,
                    "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
                    "usage": {"prompt_tokens": p_tok, "completion_tokens": c_tok,
                              "total_tokens": p_tok + c_tok},
                }
                yield f"data: {json.dumps(final, ensure_ascii=False)}\n\n"
                yield "data: [DONE]\n\n"
            except Exception as e:  # noqa: BLE001
                STATS.record(ok=False, error=str(e))
                yield f"data: {json.dumps(_error_body(str(e), 500))}\n\n"
                yield "data: [DONE]\n\n"

        return StreamingResponse(event_stream(), media_type="text/event-stream")

    # ---- 非流式 ----
    try:
        with _INFER_LOCK:
            output = generate(model, tokenizer, prompt=prompt_text,
                              max_tokens=max_tokens, sampler=sampler, **_gen_kwargs)
    except Exception as e:  # noqa: BLE001
        STATS.record(ok=False, error=str(e))
        return JSONResponse(status_code=500, content=_error_body(str(e), 500))

    latency_ms = (time.perf_counter() - start) * 1000
    completion_ids = tokenizer.encode(output)
    p_tok = len(prompt_ids)
    c_tok = max(len(completion_ids), 1)
    finish = "length" if c_tok >= max_tokens else "stop"
    STATS.record(ok=True, prompt_tokens=p_tok, completion_tokens=c_tok,
                 latency_ms=latency_ms, tool_fallback=tool_fallback)
    return {
        "id": cmpl_id,
        "object": "chat.completion",
        "created": created,
        "model": SERVED_MODEL_NAME,
        "choices": [{
            "index": 0,
            "message": {"role": "assistant", "content": output},
            "finish_reason": finish,
        }],
        "usage": {"prompt_tokens": p_tok, "completion_tokens": c_tok,
                  "total_tokens": p_tok + c_tok},
    }


if __name__ == "__main__":
    print(f"[mlx-llm] 启动: host={LLM_HOST} port={LLM_PORT} model={SERVED_MODEL_NAME} "
          f"thinking={ENABLE_THINKING} stats={STATS_FILE}", flush=True)
    uvicorn.run(app, host=LLM_HOST, port=LLM_PORT)
