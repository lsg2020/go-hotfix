package go_hotfix

import (
	"errors"
)

var (
	ErrOnlySupportLinux   = errors.New("only support linux")
	ErrTooManyLibraries   = errors.New("number of loaded libraries exceeds maximum")
	ErrSearchPluginFailed = errors.New("search plugin image failed")
)

// TracerPath from go install github.com/lsg2020/go-hotfix/tools/tracer
var TracerPath = "./tracer"

// Hotfix is a use plugin and debug symbol hotfix function
// Only support linux
// For example, Hotfix("hello_v1.so", []string{ "github.com/lsg2020/go-hotfix/examples/data.TestAdd"}, true)
//
func Hotfix(path string, names []string, variadic []bool, threadSafe bool) (string, error) {
	return hotfix(path, names, variadic, threadSafe)
}
