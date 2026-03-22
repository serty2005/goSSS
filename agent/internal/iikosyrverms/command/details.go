package command

import (
	"time"

	"etalon-agent/internal/iikosyrverms/domain"
)

func detailsFromReport(report domain.ScanReport) map[string]any {
	details := map[string]any{
		"supported_environment": report.Supported,
		"expected_os":           report.ExpectedOS,
		"expected_arch":         report.ExpectedArch,
		"current_os":            report.CurrentOS,
		"current_arch":          report.CurrentArch,
		"appdata_env_available": report.AppDataEnvAvailable,
		"software_type":         report.SoftwareType,
		"known_paths":           knownPathDetails(report.KnownPaths),
		"matched_candidates":    candidateDetails(report.Candidates),
		"detection_reason":      report.DetectionReason,
	}

	if report.AppDataEnvPath != "" {
		details["appdata_env_path"] = report.AppDataEnvPath
	}
	if len(report.AppDataRoots) > 0 {
		details["appdata_roots"] = report.AppDataRoots
	}
	if report.ActiveCandidate != nil {
		details["active_path"] = report.ActiveCandidate.ActivityPath
	}
	if report.SourceFile != "" {
		details["source_file"] = report.SourceFile
	}
	if report.RMSURL != "" {
		details["rms_url"] = report.RMSURL
	}

	return details
}

func knownPathDetails(items []domain.KnownPathStatus) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{
			"software_type": item.SoftwareType,
			"path":          item.Path,
			"kind":          item.Kind,
			"exists":        item.Exists,
		})
	}
	return result
}

func candidateDetails(items []domain.Candidate) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		details := map[string]any{
			"software_type": item.SoftwareType,
			"root_path":     item.RootPath,
			"active_path":   item.ActivityPath,
			"activity_at":   item.ActivityAt.UTC().Format(time.RFC3339),
		}
		if item.AppDataRoot != "" {
			details["appdata_root"] = item.AppDataRoot
		}
		if len(item.ConfigFiles) > 0 {
			details["config_files"] = item.ConfigFiles
		}
		result = append(result, details)
	}
	return result
}
