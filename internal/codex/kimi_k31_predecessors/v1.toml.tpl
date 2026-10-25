# Managed by yijie-agent-host FEAT-156.
model = "kimi-k3"
model_provider = "kimi"
model_reasoning_effort = "max"
model_reasoning_summary = "none"
model_catalog_json = {{CATALOG_PATH}}
[model_providers.kimi]
name = "Kimi"
base_url = "https://api.moonshot.cn/v1"
env_key = "KIMI_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
request_max_retries = 0
stream_max_retries = 0
[shell_environment_policy]
inherit = "core"
exclude = ["YIJIE_FEAT144_SORFTIME_ACCOUNT_SK", "YIJIE_FEAT144_SORFTIME_ENABLED", "MINIMAX_API_KEY", "KIMI_API_KEY", "MOONSHOT_API_KEY", "YIJIE_KIMI_API_KEY", "YIJIE_KIMI_API_KEY_FILE"]
set = {}
