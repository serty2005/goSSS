package inventory

import (
	"net"
	"reflect"
	"testing"
)

func TestInterfaceFlagsStableOrder(t *testing.T) {
	t.Parallel()

	flags := net.FlagUp | net.FlagBroadcast | net.FlagLoopback | net.FlagPointToPoint | net.FlagMulticast | net.FlagRunning
	got := interfaceFlags(flags)
	want := []string{"up", "broadcast", "loopback", "point_to_point", "multicast", "running"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ожидался стабильный порядок флагов %v, получено %v", want, got)
	}
}
