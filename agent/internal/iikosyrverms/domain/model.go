package domain

import "time"

type SoftwareType string

const (
	SoftwareTypeIiko    SoftwareType = "iiko"
	SoftwareTypeSyrve   SoftwareType = "syrve"
	SoftwareTypeUnknown SoftwareType = "unknown"
)

type PathKind string

const (
	PathKindDirectory PathKind = "directory"
	PathKindFile      PathKind = "file"
)

type KnownPathStatus struct {
	SoftwareType SoftwareType
	Path         string
	Kind         PathKind
	Exists       bool
}

type ActivitySignal struct {
	Path      string
	Kind      PathKind
	UpdatedAt time.Time
}

type PluginInfo struct {
	Name         string `json:"name"`
	APIVersion   string `json:"api_version,omitempty"`
	Version      string `json:"version,omitempty"`
	Directory    string `json:"directory,omitempty"`
	ManifestFile string `json:"manifest_file,omitempty"`
	DLLFile      string `json:"dll_file,omitempty"`
}

type FrontInstallation struct {
	SoftwareType   SoftwareType `json:"software_type"`
	RootPath       string       `json:"root_path,omitempty"`
	ExecutablePath string       `json:"executable_path,omitempty"`
	PluginsRoot    string       `json:"plugins_root,omitempty"`
	WorkingDir     string       `json:"working_dir,omitempty"`
	Source         string       `json:"source,omitempty"`
}

type AutorunEntry struct {
	Source       string       `json:"source"`
	Path         string       `json:"path,omitempty"`
	TargetPath   string       `json:"target_path,omitempty"`
	Arguments    string       `json:"arguments,omitempty"`
	WorkingDir   string       `json:"working_dir,omitempty"`
	TaskName     string       `json:"task_name,omitempty"`
	MatchesFront bool         `json:"matches_front"`
	SoftwareType SoftwareType `json:"software_type,omitempty"`
}

type ConfigSetting struct {
	Path       string            `json:"path"`
	Name       string            `json:"name"`
	Value      string            `json:"value,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	ParentPath string            `json:"parent_path,omitempty"`
	Index      int               `json:"index"`
	Repeated   bool              `json:"repeated,omitempty"`
}

type ConfigNode struct {
	Name       string            `json:"name"`
	Path       string            `json:"path"`
	Value      string            `json:"value,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	ParentPath string            `json:"parent_path,omitempty"`
	Index      int               `json:"index"`
	Repeated   bool              `json:"repeated,omitempty"`
	Children   []ConfigNode      `json:"children,omitempty"`
}

type ConfigSnapshot struct {
	SourceFile       string          `json:"source_file,omitempty"`
	RootElement      string          `json:"root_element,omitempty"`
	Settings         []ConfigSetting `json:"settings,omitempty"`
	Tree             *ConfigNode     `json:"tree,omitempty"`
	HasRepeatedNodes bool            `json:"has_repeated_nodes,omitempty"`
	ServerURL        string          `json:"server_url,omitempty"`
}

type ShutdownResult struct {
	SoftwareType  SoftwareType `json:"software_type"`
	ProcessName   string       `json:"process_name,omitempty"`
	MatchedPIDs   []uint32     `json:"matched_pids,omitempty"`
	WindowsClosed int          `json:"windows_closed,omitempty"`
	CloseSent     bool         `json:"close_sent,omitempty"`
}

type AutorunInspectionResult struct {
	SoftwareType SoftwareType   `json:"software_type"`
	Entries      []AutorunEntry `json:"entries,omitempty"`
}

type AutorunEnsureResult struct {
	SoftwareType SoftwareType `json:"software_type"`
	Method       string       `json:"method,omitempty"`
	Created      bool         `json:"created,omitempty"`
	Updated      bool         `json:"updated,omitempty"`
	Path         string       `json:"path,omitempty"`
	TaskName     string       `json:"task_name,omitempty"`
	TargetPath   string       `json:"target_path,omitempty"`
	Arguments    string       `json:"arguments,omitempty"`
}

type Candidate struct {
	SoftwareType    SoftwareType
	AppDataRoot     string
	AppDataPriority int
	RootPath        string
	ActivityPath    string
	ActivityAt      time.Time
	ConfigFiles     []string
	ActivitySignals []ActivitySignal
}

type ScanReport struct {
	Supported           bool
	CurrentOS           string
	CurrentArch         string
	ExpectedOS          string
	ExpectedArch        string
	AppDataEnvPath      string
	AppDataEnvAvailable bool
	AppDataRoots        []string
	KnownPaths          []KnownPathStatus
	Candidates          []Candidate
	ActiveCandidate     *Candidate
	SoftwareType        SoftwareType
	RMSURL              string
	SourceFile          string
	ConfigSnapshot      ConfigSnapshot
	CRMID               string
	CashServerLog       string
	FrontInstallation   *FrontInstallation
	FrontExecutable     string
	PluginsRoot         string
	Plugins             []PluginInfo
	AutorunEntries      []AutorunEntry
	Warnings            []string
	DetectionReason     string
}

func (r ScanReport) HasKnownSoftware() bool {
	return r.SoftwareType != SoftwareTypeUnknown
}

func (r ScanReport) HasUsableRoots() bool {
	return len(r.AppDataRoots) > 0
}
