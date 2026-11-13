//go:build !linux

package check

import (
	"context"
	"errors"
)

func readCPU() (cpuTimes, error) { return cpuTimes{}, errServerOS }

var errServerOS = errors.New("server metrics are only available on Linux")

func collectServer(context.Context, int64, []string) (*ServerMetrics, error) {
	return nil, errServerOS
}
