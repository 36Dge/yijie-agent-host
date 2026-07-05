package app

import (
	"encoding/json"
	"net/http"
	"os"
)

const ServiceName = "yijie-agent-host"

type Config struct {
	Environment       string `json:"environment"`
	Port              string `json:"port"`
	CodexAppServerURL string `json:"codex_app_server_url,omitempty"`
}

type Status struct {
	Service           string `json:"service"`
	Status            string `json:"status"`
	Environment       string `json:"environment"`
	RuntimeMode       string `json:"runtime_mode"`
	CodexAppServerURL string `json:"codex_app_server_url,omitempty"`
}

func LoadConfig() Config {
	return Config{
		Environment:       env("YIJIE_ENV", "local"),
		Port:              env("YIJIE_AGENT_HOST_PORT", "18080"),
		CodexAppServerURL: os.Getenv("YIJIE_CODEX_APP_SERVER_URL"),
	}
}

func NewHandler(config Config) http.Handler {
	mux := http.NewServeMux()
	status := Status{
		Service:           ServiceName,
		Status:            "ok",
		Environment:       config.Environment,
		RuntimeMode:       "desktop-host-placeholder",
		CodexAppServerURL: config.CodexAppServerURL,
	}
	mux.HandleFunc("/healthz", jsonHandler(status))
	mux.HandleFunc("/readyz", jsonHandler(map[string]string{"status": "ready"}))
	mux.HandleFunc("/v1/status", jsonHandler(status))
	return mux
}

func jsonHandler(payload any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
