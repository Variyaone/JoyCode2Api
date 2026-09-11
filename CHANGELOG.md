# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased] - 2026-09-11

### Added
- 公开模型单价及今日/累计/逐日费用估算（USD），明确未知价格和缺失用量；版本化定点单价、幂等历史回填、独立汇总账本不随日志清理丢失。
- `/api/costs` JWT 接口和费用面板；统一登录、初始化、主布局和 README 的学习研究、信息安全及责任限制声明。
- GPT-6 Astra、Claude-Opus-5 名称与 Claude `-hq` 映射支持、下拉选项同步（基础文本验证）。
- Dashboard「公开模型评测」：AA Intelligence Index v4.3 分数、多推理档位、模型筛选、同指标排序、来源链接及采集日期。
- `/api/model-benchmarks`：随程序嵌入的 JSON 公开评分快照，沿用 Dashboard JWT 鉴权，不自动访问公网。
- SWE-bench 官方榜单与 Arena 来源状态说明；缺失/访问受限记录不填分数。jcloud 基础模型参考、DeepSeek 日期版本与名称对应记录分开处理。

### Corrected
- 公开最高档分数不是 JoyCode 当前部署的成绩，不与本地能力测试混排。
- 撤回旧文档中全系列“均已实测 1M”、GPT 精确 910k、JoyAI 精确 180k 等外推说法；能力面板改为成功输入量下界及待验证标注。
- 旧条目所称“全链路工具闭环”“SSE 一定返回”“内置搜索已接通”等不是本次验证结论；需以具体协议测试为准。图片失败、访问受限不能推导为服务绝对不支持该能力。

## [Unreleased] - 2026-09-10

### Added
- **GPT-5.6 Sol 全链路支持（Responses API 通道）**：
  - GPT 系模型只接受 OpenAI Responses API，旧 Chat Completions 通道对其返回错误。新增 `responses_completions` 网关端点与双向协议翻译层（`pkg/joycode/responses_translate.go`、`pkg/openai/responses.go`）。
  - 四条路径（Anthropic 流式/非流式、OpenAI 流式/非流式）全部打通；Claude Code 的工具调用循环（function_call 生成 → 参数传递 → tool_result 回传）验证通过。
  - 按模型名前缀自动分流（`IsResponsesAPIModel`），调用方无感知。
  - 支持 Responses 内置 `web_search` 工具（`tools: [{"type":"web_search"}]`）。
- **Dashboard 模型能力矩阵**：新增 `/api/model-capabilities` 端点与前端面板，展示每个模型实测的 API 通道、多模态、推理、联网搜索与真实上下文上限（隐藏码字召回法探测）。
- **Dashboard 请求明细**：新增 `/api/recent-logs` 端点与前端表格，展示最近请求的模型、端点、状态码、延迟、Token 用量与错误信息（此前只有错误列表，正常请求不可见）。

### Fixed
- **修复 Claude 系列经代理无输出（EOF）**：上游原生 Anthropic 端点要求内部模型名带 `-hq` 后缀（如 `Claude-Opus-4.8-hq`），此前代理发送裸名导致 6002 错误。现已自动映射全部 Claude 模型。
- **修复 GPT 流式空回复**：上游 `stream:false` 时也返回 SSE，非流式路径改为流式聚合；reasoning 模型小 `max_tokens`（<4096）会被内部推理消耗光导致正文为空（`incomplete_details.reason=max_output_tokens`），现对小于 4096 的值不传该参数。
- **修复工具 schema 丢失**：`input_schema`（`json.RawMessage`）类型断言失败导致工具参数定义序列化为空 `{}`，模型收不到参数结构、调用参数为空。现兼容 RawMessage / map / string 三种形态。

### Changed
- **真实上下文上限**：实测 GLM-5.3 / Kimi-K3 / DeepSeek-V4-Pro / Claude 全系均可接受约 100 万 token（官方标称 200k 为保守值）；Claude 后端为 Bedrock，硬上限 1,000,000 token。上游请求体硬上限 5MB。

## [Unreleased] - 2026-08-31

### Added
- **全新模型矩阵支持**：
  - **Claude 系列**：Claude-Opus-4.8、Claude-Sonnet-4.6、Claude-Opus-4.6（及原有 4.7）
  - **智谱 GLM 系列**：GLM-5.3、GLM-5.2-jcloud（JDCloud 数据出域）
  - **月之暗面 Kimi 系列**：Kimi-K3、Kimi-K3-jcloud（JDCloud 数据出域）
  - **深度求索 DeepSeek**：DeepSeek-V4-Pro
  - **MiniMax 系列**：MiniMax-M3（及原有 M2.7）
  - **OpenAI 系列**：GPT-5.6 Sol
- **Windows 便捷启动脚本**：
  - 新增 `启动JoyCode2Api.bat`，双击即可一键以 HTTP 模式（`--tls=false --skip-validation`）启动本地代理服务。

### Fixed
- **修复 Claude 系列模型无输出问题**：
  - `enable_claude` 开关改为默认开启（opt-out 语义）。此前默认关闭时，Claude 请求会降级走旧 OpenAI 路径，而 Claude 原生模型会拒绝该路径，导致客户端无任何输出。
  - 现在 Claude 请求默认走原生 Anthropic 端点 `/api/saas/anthropic/v1/messages`；如需强制回退旧路径，可显式将 `enable_claude` 设为 `"false"`。

### Changed
- **模型路由与能力匹配**：
  - 优化 Anthropic 协议模型解析，支持所有以 `Claude` 开头的原生模型透传与合理回退。
  - 扩充模型能力配置（Vision、Reasoning 标识及上下文上限），将新系列推理模型纳入 Reasoning 白名单。
- **前端 Web 控制台同步**：
  - 同步更新 Dashboard 账号管理、账号详情与系统设置中的模型下拉选择与 Claude 模型判定逻辑。