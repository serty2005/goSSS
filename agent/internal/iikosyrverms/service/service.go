package service

import (
	"context"
	"runtime"
	"strings"

	"etalon-agent/internal/iikosyrverms/contract"
	"etalon-agent/internal/iikosyrverms/detector"
	"etalon-agent/internal/iikosyrverms/domain"
	"etalon-agent/internal/iikosyrverms/parser"
	"etalon-agent/internal/iikosyrverms/registry"
	"etalon-agent/internal/iikosyrverms/selector"
)

type Scanner interface {
	Scan(context.Context) (domain.ScanReport, error)
}

type Option func(*Service)

type Service struct {
	currentOS    string
	currentArch  string
	products     []registry.ProductDefinition
	discovery    registry.Discovery
	discoverFunc func() registry.Discovery
}

func New(opts ...Option) *Service {
	service := &Service{
		currentOS:    runtime.GOOS,
		currentArch:  runtime.GOARCH,
		products:     registry.DefaultProducts(),
		discoverFunc: registry.Discover,
	}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

func WithDiscovery(discovery registry.Discovery) Option {
	return func(service *Service) {
		service.discovery = discovery
		service.discoverFunc = nil
	}
}

func WithPlatform(goos, goarch string) Option {
	return func(service *Service) {
		service.currentOS = strings.TrimSpace(goos)
		service.currentArch = strings.TrimSpace(goarch)
	}
}

func (s *Service) Scan(ctx context.Context) (domain.ScanReport, error) {
	report := domain.ScanReport{
		Supported:    s.currentOS == contract.TargetOS && s.currentArch == contract.TargetArch,
		CurrentOS:    s.currentOS,
		CurrentArch:  s.currentArch,
		ExpectedOS:   contract.TargetOS,
		ExpectedArch: contract.TargetArch,
		SoftwareType: domain.SoftwareTypeUnknown,
	}

	discovery := s.discovery
	if s.discoverFunc != nil {
		discovery = s.discoverFunc()
	}

	report.AppDataEnvPath = discovery.EnvPath
	report.AppDataEnvAvailable = discovery.EnvAvailable
	report.AppDataRoots = make([]string, 0, len(discovery.Roots))
	for _, root := range discovery.Roots {
		report.AppDataRoots = append(report.AppDataRoots, root.Path)
	}

	knownPaths, candidates, err := detector.Detect(ctx, discovery.Roots, s.products)
	if err != nil {
		return domain.ScanReport{}, err
	}
	report.KnownPaths = knownPaths
	report.Candidates = candidates

	selected, reason, ok := selector.Select(candidates)
	if !ok {
		report.DetectionReason = "Поддерживаемое ПО iiko/syrve не найдено в известных путях"
		return report, nil
	}

	selectedCopy := selected
	report.ActiveCandidate = &selectedCopy
	report.SoftwareType = selected.SoftwareType
	report.DetectionReason = reason

	rmsURL, sourceFile, parseReason := parser.ParseConfigFiles(selected.ConfigFiles)
	report.RMSURL = rmsURL
	report.SourceFile = sourceFile
	if strings.TrimSpace(parseReason) != "" {
		report.DetectionReason = strings.TrimSpace(report.DetectionReason + "; " + parseReason)
	}

	return report, nil
}
