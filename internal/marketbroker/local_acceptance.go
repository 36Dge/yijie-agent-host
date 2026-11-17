package marketbroker

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Profile bytes are copied only by the Contracts canonical synchronizer.
//
//go:embed market-local-acceptance.json
var localAcceptanceFiles embed.FS

func localAcceptanceArgument() string {
	data, err := localAcceptanceFiles.ReadFile("market-local-acceptance.json")
	if err != nil {
		panic(err)
	}
	var profile struct {
		WorkerArgument string `json:"workerArgument"`
	}
	if json.Unmarshal(data, &profile) != nil || profile.WorkerArgument != "--broker-local-acceptance-stdio" {
		panic("invalid local acceptance profile")
	}
	return profile.WorkerArgument
}

var localLabel = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)

func ValidLocalAcceptance(label, home string) bool {
	if !localLabel.MatchString(label) || !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return false
	}
	suffix := filepath.Join(".local", "market-local-acceptance", label, "host-home", "market-worker", "library-home")
	return len(home) > len(suffix) && home[len(home)-len(suffix)-1:] == string(filepath.Separator)+suffix
}

func NewLocalAcceptanceOwner(manifest, home, label string) (*Owner, error) {
	if !ValidLocalAcceptance(label, home) {
		return nil, ErrBinding
	}
	owner, err := NewOwnerWithStorage(manifest, home)
	if err != nil {
		return nil, err
	}
	owner.localAcceptance = label
	return owner, nil
}

// Bounded acceptance-only observation, containing neither arguments nor authority.
func RecordLocalAcceptanceRead(kind string) {
	label := os.Getenv("YIJIE_MARKET_LOCAL_ACCEPTANCE")
	home := os.Getenv("YIJIE_AGENT_HOST_HOME")
	if !ValidLocalAcceptance(label, filepath.Join(home, "market-worker", "library-home")) {
		return
	}
	switch kind {
	case "approvals", "history", "events", "thread_read", "native_history", "native_sse_open", "native_sse_close", "legacy_sse_open", "legacy_sse_close":
	default:
		return
	}
	file, err := os.OpenFile(filepath.Join(home, "local-market-reads.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	if stat, err := file.Stat(); err != nil || stat.Size() > 1<<20 {
		return
	}
	fmt.Fprintf(file, "{\"timeMs\":%d,\"kind\":%q}\n", time.Now().UnixMilli(), kind)
}
