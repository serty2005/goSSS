//go:build windows

package inventory

import "testing"

func TestResolveCOMSignatureSupportsUSBVIDPID(t *testing.T) {
	t.Parallel()

	got := resolveCOMSignature([]string{
		`USB\VID_1A86&PID_7523&REV_0264`,
	})

	if got.VendorID != "1A86" {
		t.Fatalf("ожидался VendorID 1A86, получено %q", got.VendorID)
	}
	if got.ProductID != "7523" {
		t.Fatalf("ожидался ProductID 7523, получено %q", got.ProductID)
	}
	if got.DeviceKey != "usb:vid_1a86&pid_7523" {
		t.Fatalf("ожидался ключ usb:vid_1a86&pid_7523, получено %q", got.DeviceKey)
	}
}

func TestResolveCOMSignatureSupportsPCIVENDEV(t *testing.T) {
	t.Parallel()

	got := resolveCOMSignature([]string{
		`PCI\VEN_8086&DEV_4DF8&SUBSYS_00000000`,
	})

	if got.VendorID != "8086" {
		t.Fatalf("ожидался VendorID 8086, получено %q", got.VendorID)
	}
	if got.ProductID != "4DF8" {
		t.Fatalf("ожидался ProductID 4DF8, получено %q", got.ProductID)
	}
	if got.DeviceKey != "pci:ven_8086&dev_4df8" {
		t.Fatalf("ожидался ключ pci:ven_8086&dev_4df8, получено %q", got.DeviceKey)
	}
}

func TestMergeCOMPortPrefersDetailedEnumData(t *testing.T) {
	t.Parallel()

	base := COMPort{
		Name:   "COM4",
		Device: `\Device\Serial0`,
		Source: `HKLM\HARDWARE\DEVICEMAP\SERIALCOMM`,
	}
	overlay := COMPort{
		Name:         "COM4",
		Source:       `HKLM\SYSTEM\CurrentControlSet\Enum`,
		Enumerator:   "USB",
		InstanceID:   `USB\VID_1A86&PID_7523\1234`,
		FriendlyName: "USB-SERIAL CH340 (COM4)",
		HardwareIDs:  []string{`USB\VID_1A86&PID_7523`},
		SignatureKey: "usb:vid_1a86&pid_7523",
	}

	got := mergeCOMPort(base, overlay)

	if got.Device != `\Device\Serial0` {
		t.Fatalf("ожидался исходный device из SERIALCOMM, получено %q", got.Device)
	}
	if got.Enumerator != "USB" {
		t.Fatalf("ожидался Enumerator USB, получено %q", got.Enumerator)
	}
	if got.FriendlyName != "USB-SERIAL CH340 (COM4)" {
		t.Fatalf("ожидался FriendlyName из Enum, получено %q", got.FriendlyName)
	}
	if got.SignatureKey != "usb:vid_1a86&pid_7523" {
		t.Fatalf("ожидался SignatureKey из Enum, получено %q", got.SignatureKey)
	}
	if got.Source != `HKLM\HARDWARE\DEVICEMAP\SERIALCOMM; HKLM\SYSTEM\CurrentControlSet\Enum` {
		t.Fatalf("ожидался объединенный source, получено %q", got.Source)
	}
	if len(got.HardwareIDs) != 1 || got.HardwareIDs[0] != `USB\VID_1A86&PID_7523` {
		t.Fatalf("ожидался перенос hardware_ids из Enum, получено %+v", got.HardwareIDs)
	}
}

func TestCleanRegistryPresentationDropsResourcePrefix(t *testing.T) {
	t.Parallel()

	got := cleanRegistryPresentation(`@usbser.inf,%usb\class_02.devicedesc%;USB Serial Device`)
	if got != "USB Serial Device" {
		t.Fatalf("ожидался очищенный текст USB Serial Device, получено %q", got)
	}
}
