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
	DetectionReason     string
}

func (r ScanReport) HasKnownSoftware() bool {
	return r.SoftwareType != SoftwareTypeUnknown
}

func (r ScanReport) HasUsableRoots() bool {
	return len(r.AppDataRoots) > 0
}
