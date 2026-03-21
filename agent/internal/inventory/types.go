package inventory

import (
	"time"

	"etalon-agent/internal/hostinfo"
)

type Snapshot struct {
	CollectedAt       time.Time           `json:"collected_at"`
	Hostname          string              `json:"hostname"`
	OS                string              `json:"os"`
	Arch              string              `json:"arch"`
	ExecutablePath    string              `json:"executable_path,omitempty"`
	HostInfo          *hostinfo.Snapshot  `json:"host_info,omitempty"`
	NetworkInterfaces []NetworkInterface  `json:"network_interfaces,omitempty"`
	COMPorts          []COMPort           `json:"com_ports,omitempty"`
	InstalledSoftware []InstalledSoftware `json:"installed_software,omitempty"`
	KnownComponents   []KnownComponent    `json:"known_components,omitempty"`
}

type NetworkInterface struct {
	Name         string   `json:"name"`
	Index        int      `json:"index,omitempty"`
	MTU          int      `json:"mtu,omitempty"`
	HardwareAddr string   `json:"hardware_addr,omitempty"`
	Addresses    []string `json:"addresses,omitempty"`
	Flags        []string `json:"flags,omitempty"`
}

type COMPort struct {
	Name   string `json:"name"`
	Device string `json:"device,omitempty"`
	Source string `json:"source,omitempty"`
}

type InstalledSoftware struct {
	Name            string `json:"name"`
	Version         string `json:"version,omitempty"`
	Publisher       string `json:"publisher,omitempty"`
	InstallLocation string `json:"install_location,omitempty"`
	UninstallString string `json:"uninstall_string,omitempty"`
	Source          string `json:"source,omitempty"`
}

type KnownComponent struct {
	Key      string              `json:"key"`
	Name     string              `json:"name"`
	Category string              `json:"category,omitempty"`
	Detected bool                `json:"detected"`
	Version  string              `json:"version,omitempty"`
	Evidence []ComponentEvidence `json:"evidence,omitempty"`
}

type ComponentEvidence struct {
	Type   string `json:"type"`
	Source string `json:"source,omitempty"`
	Value  string `json:"value,omitempty"`
}
