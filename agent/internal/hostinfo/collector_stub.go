//go:build !windows

package hostinfo

import "context"

func Collect(context.Context) (Snapshot, error) {
	return Snapshot{}, nil
}
