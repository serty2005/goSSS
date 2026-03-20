package inventory

import (
	"cmp"
	"context"
	"net"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"
)

type collector struct{}

func newCollector() Collector {
	return collector{}
}

func (collector) Collect(ctx context.Context) (Snapshot, error) {
	select {
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	default:
	}

	host, _ := os.Hostname()
	executablePath, _ := os.Executable()

	snapshot := Snapshot{
		CollectedAt:    time.Now().UTC(),
		Hostname:       strings.TrimSpace(host),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		ExecutablePath: executablePath,
	}

	networkInterfaces, err := collectNetworkInterfaces()
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.NetworkInterfaces = networkInterfaces

	comPorts, err := collectCOMPorts()
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.COMPorts = comPorts

	installedSoftware, err := collectInstalledSoftware()
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.InstalledSoftware = installedSoftware

	knownComponents, err := collectKnownComponents(installedSoftware)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.KnownComponents = knownComponents

	return snapshot, nil
}

func collectNetworkInterfaces() ([]NetworkInterface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	result := make([]NetworkInterface, 0, len(interfaces))
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			return nil, err
		}

		item := NetworkInterface{
			Name:         iface.Name,
			Index:        iface.Index,
			MTU:          iface.MTU,
			HardwareAddr: iface.HardwareAddr.String(),
			Addresses:    make([]string, 0, len(addresses)),
			Flags:        interfaceFlags(iface.Flags),
		}

		for _, addr := range addresses {
			item.Addresses = append(item.Addresses, addr.String())
		}
		slices.Sort(item.Addresses)
		result = append(result, item)
	}

	slices.SortFunc(result, func(a, b NetworkInterface) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Index, b.Index))
	})
	return result, nil
}

func interfaceFlags(flags net.Flags) []string {
	out := make([]string, 0, 6)

	if flags&net.FlagUp != 0 {
		out = append(out, "up")
	}
	if flags&net.FlagBroadcast != 0 {
		out = append(out, "broadcast")
	}
	if flags&net.FlagLoopback != 0 {
		out = append(out, "loopback")
	}
	if flags&net.FlagPointToPoint != 0 {
		out = append(out, "point_to_point")
	}
	if flags&net.FlagMulticast != 0 {
		out = append(out, "multicast")
	}
	if flags&net.FlagRunning != 0 {
		out = append(out, "running")
	}

	return out
}
