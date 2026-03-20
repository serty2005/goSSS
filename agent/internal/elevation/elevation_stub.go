//go:build !windows

package elevation

func EnsureAdmin() (bool, error) {
	return false, nil
}
