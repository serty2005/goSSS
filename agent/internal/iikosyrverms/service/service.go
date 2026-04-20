package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"etalon-agent/internal/iikosyrverms/autorun"
	"etalon-agent/internal/iikosyrverms/contract"
	"etalon-agent/internal/iikosyrverms/detector"
	"etalon-agent/internal/iikosyrverms/domain"
	"etalon-agent/internal/iikosyrverms/frontinstall"
	"etalon-agent/internal/iikosyrverms/parser"
	"etalon-agent/internal/iikosyrverms/pluginscanner"
	"etalon-agent/internal/iikosyrverms/registry"
	"etalon-agent/internal/iikosyrverms/selector"
	"etalon-agent/internal/iikosyrverms/shutdown"
)

type Scanner interface {
	Scan(context.Context) (domain.ScanReport, error)
}

type frontResolver interface {
	Resolve(context.Context, []domain.SoftwareType) (domain.FrontInstallation, error)
}

type pluginScannerRunner interface {
	Scan(string) ([]domain.PluginInfo, []string, error)
}

type shutdownController interface {
	SoftShutdown(context.Context, domain.SoftwareType, string) (domain.ShutdownResult, error)
}

type autorunManager interface {
	Inspect(context.Context) ([]domain.AutorunEntry, error)
	Ensure(context.Context, autorun.EnsureRequest) (domain.AutorunEnsureResult, error)
}

type Option func(*Service)

type Service struct {
	currentOS    string
	currentArch  string
	products     []registry.ProductDefinition
	discovery    registry.Discovery
	discoverFunc func() registry.Discovery
	fronts       frontResolver
	plugins      pluginScannerRunner
	shutdown     shutdownController
	autorun      autorunManager
}

func New(opts ...Option) *Service {
	service := &Service{
		currentOS:    runtime.GOOS,
		currentArch:  runtime.GOARCH,
		products:     registry.DefaultProducts(),
		discoverFunc: registry.Discover,
		fronts:       frontinstall.New(),
		plugins:      pluginscanner.New(),
		shutdown:     shutdown.New(),
		autorun:      autorun.New(),
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

func WithFrontResolver(resolver frontResolver) Option {
	return func(service *Service) {
		service.fronts = resolver
	}
}

func WithPluginScanner(scanner pluginScannerRunner) Option {
	return func(service *Service) {
		service.plugins = scanner
	}
}

func WithShutdownController(controller shutdownController) Option {
	return func(service *Service) {
		service.shutdown = controller
	}
}

func WithAutorunManager(manager autorunManager) Option {
	return func(service *Service) {
		service.autorun = manager
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

	snapshot, parseReason := parser.ReadConfigFiles(selected.ConfigFiles)
	report.ConfigSnapshot = snapshot
	report.SourceFile = snapshot.SourceFile
	report.RMSURL = snapshot.ServerURL
	if strings.TrimSpace(parseReason) != "" {
		report.DetectionReason = strings.TrimSpace(report.DetectionReason + "; " + parseReason)
	}

	return report, nil
}

func (s *Service) Collect(ctx context.Context) (domain.ScanReport, error) {
	report, err := s.Scan(ctx)
	if err != nil || !report.Supported || !report.HasUsableRoots() || !report.HasKnownSoftware() {
		return report, err
	}

	if report.SoftwareType == domain.SoftwareTypeSyrve {
		report.Warnings = append(report.Warnings, "Полный сбор для Syrve пока ограничен чтением config.xml и RMS URL")
	}

	installation, err := s.fronts.Resolve(ctx, []domain.SoftwareType{report.SoftwareType})
	if err == nil {
		report.FrontInstallation = &installation
		report.FrontExecutable = installation.ExecutablePath
		report.PluginsRoot = installation.PluginsRoot
	} else if !errors.Is(err, frontinstall.ErrNotFound) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("Не удалось определить установку фронта: %v", err))
	} else {
		report.Warnings = append(report.Warnings, "Установку фронта определить не удалось")
	}

	report.CashServerLog = resolveCashServerLog(report)
	switch report.SoftwareType {
	case domain.SoftwareTypeIiko:
		if report.CashServerLog == "" {
			report.Warnings = append(report.Warnings, "cash-server.log не найден в известных каталогах")
		} else {
			crmID, diagnostic, err := parser.ReadCRMID(report.CashServerLog)
			if err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("Не удалось прочитать cash-server.log: %v", err))
			} else if crmID == "" {
				report.Warnings = append(report.Warnings, diagnostic)
			} else {
				report.CRMID = crmID
			}
		}

		if report.PluginsRoot == "" {
			report.Warnings = append(report.Warnings, "Каталог Plugins определить не удалось")
			return report, nil
		}

		plugins, warnings, err := s.plugins.Scan(report.PluginsRoot)
		report.Warnings = append(report.Warnings, warnings...)
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("Не удалось выполнить сканирование Plugins: %v", err))
			return report, nil
		}
		report.Plugins = plugins
	case domain.SoftwareTypeSyrve:
		report.Warnings = append(report.Warnings, "Сбор CRMID и плагинов для Syrve будет добавлен на следующем шаге")
	}

	return report, nil
}

func (s *Service) ReadFrontConfig(ctx context.Context) (domain.ScanReport, error) {
	return s.Scan(ctx)
}

func (s *Service) SoftShutdownFront(ctx context.Context) (domain.ShutdownResult, error) {
	report, err := s.Scan(ctx)
	if err != nil {
		return domain.ShutdownResult{}, err
	}

	preferred := preferredSoftwareTypes(report.SoftwareType)
	installation, resolveErr := s.fronts.Resolve(ctx, preferred)
	processName := frontProcessName(report.SoftwareType, installation)
	softwareType := report.SoftwareType
	if softwareType == domain.SoftwareTypeUnknown {
		softwareType = installation.SoftwareType
	}
	if processName == "" && resolveErr != nil {
		return domain.ShutdownResult{}, fmt.Errorf("не удалось определить процесс фронта: %w", resolveErr)
	}
	if processName == "" {
		return domain.ShutdownResult{}, fmt.Errorf("не удалось определить процесс фронта")
	}

	return s.shutdown.SoftShutdown(ctx, softwareType, processName)
}

func (s *Service) InspectAutorun(ctx context.Context) (domain.AutorunInspectionResult, error) {
	report, err := s.Scan(ctx)
	if err != nil {
		return domain.AutorunInspectionResult{}, err
	}

	entries, err := s.autorun.Inspect(ctx)
	if err != nil {
		return domain.AutorunInspectionResult{}, err
	}

	result := domain.AutorunInspectionResult{
		SoftwareType: report.SoftwareType,
		Entries:      make([]domain.AutorunEntry, 0, len(entries)),
	}
	for _, entry := range entries {
		if !entry.MatchesFront {
			continue
		}
		if result.SoftwareType == domain.SoftwareTypeUnknown && entry.SoftwareType != domain.SoftwareTypeUnknown {
			result.SoftwareType = entry.SoftwareType
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

func (s *Service) EnsureAutorun(ctx context.Context, payload contract.AutorunEnsurePayload) (domain.AutorunEnsureResult, error) {
	softwareType := payload.SoftwareType
	if softwareType == domain.SoftwareTypeUnknown {
		report, err := s.Scan(ctx)
		if err != nil {
			return domain.AutorunEnsureResult{}, err
		}
		softwareType = report.SoftwareType
	}

	installation, err := s.fronts.Resolve(ctx, preferredSoftwareTypes(softwareType))
	if err != nil {
		return domain.AutorunEnsureResult{}, err
	}
	if softwareType == domain.SoftwareTypeUnknown {
		softwareType = installation.SoftwareType
	}

	return s.autorun.Ensure(ctx, autorun.EnsureRequest{
		Method:       payload.Method,
		SoftwareType: softwareType,
		Installation: installation,
		Arguments:    payload.Arguments,
		TaskName:     payload.TaskName,
		ShortcutName: payload.ShortcutName,
	})
}

func resolveCashServerLog(report domain.ScanReport) string {
	candidates := make([]string, 0, 16)
	addCandidate := func(path string) {
		path = strings.TrimSpace(path)
		if path != "" {
			candidates = append(candidates, filepath.Clean(path))
		}
	}

	if report.SourceFile != "" {
		baseDir := filepath.Dir(report.SourceFile)
		addCandidate(filepath.Join(baseDir, "cash-server.log"))
		addCandidate(filepath.Join(baseDir, "logs", "cash-server.log"))
		addCandidate(filepath.Join(baseDir, "log", "cash-server.log"))
	}
	if report.ActiveCandidate != nil {
		for _, dir := range []string{
			filepath.Join(report.ActiveCandidate.RootPath, "cashserver"),
			filepath.Join(report.ActiveCandidate.RootPath, "CashServer"),
			report.ActiveCandidate.RootPath,
		} {
			addCandidate(filepath.Join(dir, "cash-server.log"))
			addCandidate(filepath.Join(dir, "logs", "cash-server.log"))
			addCandidate(filepath.Join(dir, "log", "cash-server.log"))
		}
	}
	if report.FrontExecutable != "" {
		for _, dir := range []string{
			filepath.Dir(report.FrontExecutable),
			filepath.Dir(filepath.Dir(report.FrontExecutable)),
		} {
			addCandidate(filepath.Join(dir, "cash-server.log"))
			addCandidate(filepath.Join(dir, "logs", "cash-server.log"))
			addCandidate(filepath.Join(dir, "log", "cash-server.log"))
		}
	}

	type candidate struct {
		path      string
		updatedAt int64
	}
	seen := make(map[string]struct{})
	found := make([]candidate, 0, len(candidates))
	for _, path := range candidates {
		key := strings.ToLower(path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		found = append(found, candidate{
			path:      path,
			updatedAt: info.ModTime().UnixNano(),
		})
	}

	slices.SortFunc(found, func(a, b candidate) int {
		switch {
		case a.updatedAt > b.updatedAt:
			return -1
		case a.updatedAt < b.updatedAt:
			return 1
		default:
			return strings.Compare(strings.ToLower(a.path), strings.ToLower(b.path))
		}
	})
	if len(found) == 0 {
		return ""
	}
	return found[0].path
}

func preferredSoftwareTypes(softwareType domain.SoftwareType) []domain.SoftwareType {
	switch softwareType {
	case domain.SoftwareTypeIiko:
		return []domain.SoftwareType{domain.SoftwareTypeIiko, domain.SoftwareTypeSyrve}
	case domain.SoftwareTypeSyrve:
		return []domain.SoftwareType{domain.SoftwareTypeSyrve, domain.SoftwareTypeIiko}
	default:
		return []domain.SoftwareType{domain.SoftwareTypeIiko, domain.SoftwareTypeSyrve}
	}
}

func frontProcessName(softwareType domain.SoftwareType, installation domain.FrontInstallation) string {
	if installation.ExecutablePath != "" {
		return filepath.Base(installation.ExecutablePath)
	}
	switch softwareType {
	case domain.SoftwareTypeIiko:
		return "iikoFront.Net.exe"
	default:
		return ""
	}
}
