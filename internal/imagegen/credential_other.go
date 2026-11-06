//go:build !darwin || !cgo

package imagegen

import "context"

func ArkCredential(string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return "", &ProviderError{Code: ErrorAuthentication} }
}
