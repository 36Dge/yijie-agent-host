package marketbroker

import (
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
)

// NewOwnerWithModelCredential reuses the model snapshot already loaded by Host.
// The private startup pipe is the only additional carrier; no second vault copy
// or public provider/control field is created.
func NewOwnerWithModelCredential(manifest, home, key string) (*Owner, error) {
	owner, err := NewOwnerWithStorage(manifest, home)
	if err != nil {
		return nil, err
	}
	if key != "" && len(key) <= wire.KimiModelCredentialMaxKeyBytes && !strings.ContainsFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		owner.kimiModelKey = key
	}
	return owner, nil
}

// prepareModelCredential is called before spawn. Writing starts after spawn so
// a key at the size limit cannot block on an unread pipe buffer.
func prepareModelCredential(key string) (*os.File, *os.File, error) {
	if key == "" {
		return nil, nil, nil
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, nil, ErrProtocol
	}
	if writer.SetWriteDeadline(time.Now().Add(controlTimeout)) != nil {
		reader.Close()
		writer.Close()
		return nil, nil, ErrProtocol
	}
	return reader, writer, nil
}

func modelCredentialHint() string {
	return wire.KimiModelCredentialEnvironmentKey + "=" + strconv.Itoa(wire.KimiModelCredentialDescriptor)
}

func writeModelCredential(writer *os.File, key string) {
	defer writer.Close()
	// A failed/partial write produces an unavailable local source. Never log raw
	// errors or key bytes and never retry or terminate the child.
	_, _ = io.WriteString(writer, wire.KimiModelCredentialPreamble+key+wire.KimiModelCredentialTerminator)
}
