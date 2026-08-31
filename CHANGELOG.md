# Changelog

All notable changes to this project will be documented in this file.

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

### Changed
- **模型路由与能力匹配**：
  - 优化 Anthropic 协议模型解析，支持所有以 `Claude` 开头的原生模型透传与合理回退。
  - 扩充模型能力配置（Vision、Reasoning 标识及上下文上限），将新系列推理模型纳入 Reasoning 白名单。
- **前端 Web 控制台同步**：
  - 同步更新 Dashboard 账号管理、账号详情与系统设置中的模型下拉选择与 Claude 模型判定逻辑。