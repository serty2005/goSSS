//go:build windows

package autorun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"etalon-agent/internal/iikosyrverms/contract"
	"etalon-agent/internal/iikosyrverms/domain"
)

type powershellRunner interface {
	Run(context.Context, string) ([]byte, error)
}

type windowsManager struct {
	getenv func(string) string
	runner powershellRunner
}

type shortcutInfo struct {
	Path       string `json:"path"`
	TargetPath string `json:"target_path"`
	Arguments  string `json:"arguments"`
	WorkingDir string `json:"working_dir"`
}

type scheduledTaskInfo struct {
	TaskName   string `json:"task_name"`
	TaskPath   string `json:"task_path"`
	TargetPath string `json:"target_path"`
	Arguments  string `json:"arguments"`
	WorkingDir string `json:"working_dir"`
}

func New() Manager {
	return windowsManager{
		getenv: os.Getenv,
		runner: execPowerShellRunner{},
	}
}

func (m windowsManager) Inspect(ctx context.Context) ([]domain.AutorunEntry, error) {
	entries := make([]domain.AutorunEntry, 0, 8)

	for _, item := range []struct {
		source string
		path   string
	}{
		{source: contract.AutorunMethodStartupUser, path: m.startupUserPath()},
		{source: contract.AutorunMethodStartupCommon, path: m.startupCommonPath()},
	} {
		shortcuts, err := m.inspectShortcuts(ctx, item.path)
		if err != nil {
			return nil, err
		}
		for _, shortcut := range shortcuts {
			matches, softwareType := matchFrontTarget(shortcut.TargetPath)
			entries = append(entries, domain.AutorunEntry{
				Source:       item.source,
				Path:         shortcut.Path,
				TargetPath:   shortcut.TargetPath,
				Arguments:    shortcut.Arguments,
				WorkingDir:   shortcut.WorkingDir,
				MatchesFront: matches,
				SoftwareType: softwareType,
			})
		}
	}

	tasks, err := m.inspectScheduledTasks(ctx)
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		matches, softwareType := matchFrontTarget(task.TargetPath)
		entries = append(entries, domain.AutorunEntry{
			Source:       contract.AutorunMethodScheduler,
			Path:         task.TaskPath,
			TargetPath:   task.TargetPath,
			Arguments:    task.Arguments,
			WorkingDir:   task.WorkingDir,
			TaskName:     strings.TrimSpace(task.TaskName),
			MatchesFront: matches,
			SoftwareType: softwareType,
		})
	}

	return entries, nil
}

func (m windowsManager) Ensure(ctx context.Context, request EnsureRequest) (domain.AutorunEnsureResult, error) {
	targetPath := filepath.Clean(strings.TrimSpace(request.Installation.ExecutablePath))
	if targetPath == "" {
		return domain.AutorunEnsureResult{}, fmt.Errorf("не удалось определить исполняемый файл фронта")
	}

	softwareType := request.SoftwareType
	if softwareType == domain.SoftwareTypeUnknown {
		softwareType = request.Installation.SoftwareType
	}
	if softwareType == domain.SoftwareTypeUnknown {
		if _, detected := matchFrontTarget(targetPath); detected != domain.SoftwareTypeUnknown {
			softwareType = detected
		}
	}

	result := domain.AutorunEnsureResult{
		SoftwareType: softwareType,
		Method:       strings.TrimSpace(request.Method),
		TargetPath:   targetPath,
		Arguments:    strings.TrimSpace(request.Arguments),
	}
	workingDir := strings.TrimSpace(request.Installation.WorkingDir)
	if workingDir == "" {
		workingDir = filepath.Dir(targetPath)
	}

	switch strings.TrimSpace(request.Method) {
	case contract.AutorunMethodStartupUser:
		shortcutPath := filepath.Join(m.startupUserPath(), defaultShortcutName(softwareType, request.ShortcutName))
		created, updated, err := m.ensureShortcut(ctx, shortcutPath, targetPath, result.Arguments, workingDir)
		if err != nil {
			return result, err
		}
		result.Created = created
		result.Updated = updated
		result.Path = shortcutPath
		return result, nil
	case contract.AutorunMethodStartupCommon:
		shortcutPath := filepath.Join(m.startupCommonPath(), defaultShortcutName(softwareType, request.ShortcutName))
		created, updated, err := m.ensureShortcut(ctx, shortcutPath, targetPath, result.Arguments, workingDir)
		if err != nil {
			return result, err
		}
		result.Created = created
		result.Updated = updated
		result.Path = shortcutPath
		return result, nil
	case contract.AutorunMethodScheduler:
		taskName := defaultTaskName(softwareType, request.TaskName)
		created, updated, err := m.ensureScheduledTask(ctx, taskName, targetPath, result.Arguments, workingDir)
		if err != nil {
			return result, err
		}
		result.Created = created
		result.Updated = updated
		result.TaskName = taskName
		return result, nil
	default:
		return result, fmt.Errorf("неподдерживаемый метод автозапуска %q", request.Method)
	}
}

func (m windowsManager) inspectShortcuts(ctx context.Context, dir string) ([]shortcutInfo, error) {
	script := fmt.Sprintf(`
$dir = %s
if (-not (Test-Path -LiteralPath $dir)) {
  @() | ConvertTo-Json -Compress
  exit 0
}
$shell = New-Object -ComObject WScript.Shell
$items = Get-ChildItem -LiteralPath $dir -Filter *.lnk -File | ForEach-Object {
  $shortcut = $shell.CreateShortcut($_.FullName)
  [PSCustomObject]@{
    path = $_.FullName
    target_path = $shortcut.TargetPath
    arguments = $shortcut.Arguments
    working_dir = $shortcut.WorkingDirectory
  }
}
$items | ConvertTo-Json -Compress -Depth 4
`, psQuote(dir))
	output, err := m.runner.Run(ctx, script)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать ярлыки автозапуска из %q: %w", dir, err)
	}

	var items []shortcutInfo
	if err := decodeJSONArray(output, &items); err != nil {
		return nil, fmt.Errorf("не удалось разобрать список ярлыков из %q: %w", dir, err)
	}
	return items, nil
}

func (m windowsManager) inspectScheduledTasks(ctx context.Context) ([]scheduledTaskInfo, error) {
	script := `
$tasks = Get-ScheduledTask | ForEach-Object {
  $task = $_
  foreach ($action in $_.Actions) {
    [PSCustomObject]@{
      task_name = $task.TaskName
      task_path = $task.TaskPath
      target_path = $action.Execute
      arguments = $action.Arguments
      working_dir = $action.WorkingDirectory
    }
  }
}
$tasks | ConvertTo-Json -Compress -Depth 5
`
	output, err := m.runner.Run(ctx, script)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить список задач автозапуска: %w", err)
	}

	var items []scheduledTaskInfo
	if err := decodeJSONArray(output, &items); err != nil {
		return nil, fmt.Errorf("не удалось разобрать список задач автозапуска: %w", err)
	}
	return items, nil
}

func (m windowsManager) ensureShortcut(ctx context.Context, shortcutPath, targetPath, arguments, workingDir string) (bool, bool, error) {
	script := fmt.Sprintf(`
$path = %s
$target = %s
$arguments = %s
$workingDir = %s
$shell = New-Object -ComObject WScript.Shell
$created = $false
$updated = $false
if (Test-Path -LiteralPath $path) {
  $existing = $shell.CreateShortcut($path)
  if (($existing.TargetPath -eq $target) -and ($existing.Arguments -eq $arguments) -and ($existing.WorkingDirectory -eq $workingDir)) {
    [PSCustomObject]@{created=$false;updated=$false} | ConvertTo-Json -Compress
    exit 0
  }
  $updated = $true
} else {
  $created = $true
}
New-Item -ItemType Directory -Path (Split-Path -Parent $path) -Force | Out-Null
$shortcut = $shell.CreateShortcut($path)
$shortcut.TargetPath = $target
$shortcut.Arguments = $arguments
$shortcut.WorkingDirectory = $workingDir
$shortcut.Save()
[PSCustomObject]@{created=$created;updated=$updated} | ConvertTo-Json -Compress
`, psQuote(shortcutPath), psQuote(targetPath), psQuote(arguments), psQuote(workingDir))
	output, err := m.runner.Run(ctx, script)
	if err != nil {
		return false, false, fmt.Errorf("не удалось создать или обновить ярлык %q: %w", shortcutPath, err)
	}

	var response struct {
		Created bool `json:"created"`
		Updated bool `json:"updated"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return false, false, fmt.Errorf("не удалось разобрать результат создания ярлыка %q: %w", shortcutPath, err)
	}
	return response.Created, response.Updated, nil
}

func (m windowsManager) ensureScheduledTask(ctx context.Context, taskName, targetPath, arguments, workingDir string) (bool, bool, error) {
	script := fmt.Sprintf(`
$taskName = %s
$target = %s
$arguments = %s
$workingDir = %s
$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($existing) {
  $action = $existing.Actions | Select-Object -First 1
  if (($action.Execute -eq $target) -and ($action.Arguments -eq $arguments) -and ($action.WorkingDirectory -eq $workingDir)) {
    [PSCustomObject]@{created=$false;updated=$false} | ConvertTo-Json -Compress
    exit 0
  }
  $updated = $true
} else {
  $updated = $false
}
$action = New-ScheduledTaskAction -Execute $target -Argument $arguments -WorkingDirectory $workingDir
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Force | Out-Null
[PSCustomObject]@{created=$(-not $existing);updated=$updated} | ConvertTo-Json -Compress
`, psQuote(taskName), psQuote(targetPath), psQuote(arguments), psQuote(workingDir))
	output, err := m.runner.Run(ctx, script)
	if err != nil {
		return false, false, fmt.Errorf("не удалось создать или обновить задачу %q: %w", taskName, err)
	}

	var response struct {
		Created bool `json:"created"`
		Updated bool `json:"updated"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return false, false, fmt.Errorf("не удалось разобрать результат создания задачи %q: %w", taskName, err)
	}
	return response.Created, response.Updated, nil
}

func (m windowsManager) startupUserPath() string {
	appData := strings.TrimSpace(m.getenv("APPDATA"))
	if appData == "" {
		appData = filepath.Join(strings.TrimSpace(m.getenv("USERPROFILE")), "AppData", "Roaming")
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
}

func (m windowsManager) startupCommonPath() string {
	programData := strings.TrimSpace(m.getenv("ProgramData"))
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, "Microsoft", "Windows", "Start Menu", "Programs", "StartUp")
}

func defaultShortcutName(softwareType domain.SoftwareType, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	switch softwareType {
	case domain.SoftwareTypeSyrve:
		return "SyrveFront.lnk"
	default:
		return "iikoFront.lnk"
	}
}

func defaultTaskName(softwareType domain.SoftwareType, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	switch softwareType {
	case domain.SoftwareTypeSyrve:
		return "goSSS Syrve Front Autorun"
	default:
		return "goSSS iiko Front Autorun"
	}
}

func matchFrontTarget(targetPath string) (bool, domain.SoftwareType) {
	normalized := strings.ToLower(strings.TrimSpace(targetPath))
	baseName := strings.ToLower(filepath.Base(normalized))

	switch {
	case strings.Contains(baseName, "iikofront"):
		return true, domain.SoftwareTypeIiko
	case strings.Contains(normalized, `\iiko\`) && strings.Contains(baseName, "front"):
		return true, domain.SoftwareTypeIiko
	case strings.Contains(normalized, "syrve") && strings.Contains(baseName, "front"):
		return true, domain.SoftwareTypeSyrve
	case baseName == "front.net.exe" && strings.Contains(normalized, "syrve"):
		return true, domain.SoftwareTypeSyrve
	default:
		return false, domain.SoftwareTypeUnknown
	}
}

func decodeJSONArray[T any](raw []byte, target *[]T) error {
	if len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw)) == "null" {
		*target = nil
		return nil
	}
	if err := json.Unmarshal(raw, target); err == nil {
		return nil
	}

	var single T
	if err := json.Unmarshal(raw, &single); err != nil {
		return err
	}
	*target = []T{single}
	return nil
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

type execPowerShellRunner struct{}

func (execPowerShellRunner) Run(ctx context.Context, script string) ([]byte, error) {
	command := exec.CommandContext(
		ctx,
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; $OutputEncoding = [System.Text.Encoding]::UTF8; "+script,
	)
	output, err := command.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}
	return output, nil
}
