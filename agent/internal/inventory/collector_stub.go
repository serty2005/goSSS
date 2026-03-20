//go:build !windows

package inventory

func collectCOMPorts() ([]COMPort, error) {
	return nil, nil
}

func collectInstalledSoftware() ([]InstalledSoftware, error) {
	return nil, nil
}

func collectKnownComponents([]InstalledSoftware) ([]KnownComponent, error) {
	return nil, nil
}
