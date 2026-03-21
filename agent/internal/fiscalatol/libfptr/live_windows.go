//go:build windows

package libfptr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"etalon-agent/internal/fiscalatol/domain"
	"golang.org/x/sys/windows"
)

const (
	settingModel     = "Model"
	settingPort      = "Port"
	settingCOMFile   = "ComFile"
	settingBaudRate  = "BaudRate"
	settingIPAddress = "IPAddress"
	settingIPPort    = "IPPort"

	modelAtolAuto = 500
	portCOM       = 0
	portTCPIP     = 2
)

type liveBridge struct{}

type driverRuntime struct {
	Path       string
	Version    string
	Profile    profile
	SearchPath []string
}

type nativeLibrary struct {
	dll                  *windows.LazyDLL
	createProc           *windows.LazyProc
	destroyProc          *windows.LazyProc
	getVersionProc       *windows.LazyProc
	setSettingsProc      *windows.LazyProc
	openProc             *windows.LazyProc
	closeProc            *windows.LazyProc
	isOpenedProc         *windows.LazyProc
	errorCodeProc        *windows.LazyProc
	errorDescriptionProc *windows.LazyProc
	setParamIntProc      *windows.LazyProc
	setParamStringProc   *windows.LazyProc
	getParamIntProc      *windows.LazyProc
	getParamBoolProc     *windows.LazyProc
	getParamDateTimeProc *windows.LazyProc
	getParamStringProc   *windows.LazyProc
	queryDataProc        *windows.LazyProc
	fnQueryDataProc      *windows.LazyProc
	beginReadRecordsProc *windows.LazyProc
	readNextRecordProc   *windows.LazyProc
	endReadRecordsProc   *windows.LazyProc
}

type deviceHandle struct {
	library *nativeLibrary
	handle  uintptr
}

func NewBridge() Bridge {
	return &liveBridge{}
}

func (b *liveBridge) Probe(context.Context) (ProbeResult, error) {
	searchPaths := defaultDriverSearchPaths()
	result := ProbeResult{
		Supported:   true,
		SearchPaths: searchPaths,
	}

	driverPath, found := findDriverPath(searchPaths)
	if !found {
		result.Message = "Драйвер Атол не найден в стандартных путях поиска"
		return result, nil
	}

	result.DriverPresent = true
	result.DriverPath = driverPath

	driverVersion, err := readDriverVersion(driverPath)
	if err != nil {
		result.Message = fmt.Sprintf("Драйвер найден, но его версия не определена: %v", err)
		return result, nil
	}
	result.DriverVersion = driverVersion

	currentProfile, err := selectProfile(driverVersion)
	if err != nil {
		result.Message = fmt.Sprintf("Не удалось определить ветку драйвера: %v", err)
		return result, nil
	}
	result.DriverVariant = currentProfile.Variant
	result.Message = "Драйвер Атол найден"
	return result, nil
}

func (b *liveBridge) Collect(_ context.Context, endpoint domain.Endpoint) (domain.FiscalPayload, CollectMeta, []string, error) {
	runtimeInfo, err := resolveRuntime()
	meta := CollectMeta{
		ConnectionLabel: endpoint.ConnectionLabel(),
		Transport:       endpoint.Transport,
	}
	if err != nil {
		return domain.FiscalPayload{}, meta, nil, err
	}
	meta.DriverVersion = runtimeInfo.Version

	library, err := loadNativeLibrary(runtimeInfo.Path)
	if err != nil {
		return domain.FiscalPayload{}, meta, nil, err
	}

	handle, err := library.newHandle()
	if err != nil {
		return domain.FiscalPayload{}, meta, nil, err
	}
	defer handle.destroy()

	if err := handle.configure(endpoint); err != nil {
		return domain.FiscalPayload{}, meta, nil, err
	}
	if err := handle.open(); err != nil {
		return domain.FiscalPayload{}, meta, nil, err
	}

	isOpened, err := handle.isOpened()
	if err != nil {
		return domain.FiscalPayload{}, meta, nil, err
	}
	if !isOpened {
		return domain.FiscalPayload{}, meta, nil, fmt.Errorf("драйвер не открыл соединение с endpoint %s", endpoint.ConnectionLabel())
	}

	payload, warnings, err := collectPayload(handle, runtimeInfo.Profile, runtimeInfo.Version)
	closeErr := handle.close()
	if closeErr != nil {
		warnings = append(warnings, fmt.Sprintf("соединение с ККТ завершилось с предупреждением: %v", closeErr))
	}
	if err != nil {
		return payload, meta, warnings, err
	}
	return payload, meta, warnings, nil
}

func resolveRuntime() (driverRuntime, error) {
	searchPaths := defaultDriverSearchPaths()
	driverPath, found := findDriverPath(searchPaths)
	if !found {
		return driverRuntime{}, fmt.Errorf("драйвер Атол не найден в стандартных путях поиска")
	}

	driverVersion, err := readDriverVersion(driverPath)
	if err != nil {
		return driverRuntime{}, fmt.Errorf("не удалось получить версию драйвера Атол: %w", err)
	}

	currentProfile, err := selectProfile(driverVersion)
	if err != nil {
		return driverRuntime{}, fmt.Errorf("не удалось определить ветку драйвера Атол: %w", err)
	}

	return driverRuntime{
		Path:       driverPath,
		Version:    driverVersion,
		Profile:    currentProfile,
		SearchPath: searchPaths,
	}, nil
}

func defaultDriverSearchPaths() []string {
	result := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)

	addPath := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		cleaned := filepath.Clean(path)
		if _, ok := seen[cleaned]; ok {
			return
		}
		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}

	if executable, err := os.Executable(); err == nil {
		addPath(filepath.Join(filepath.Dir(executable), "fptr10.dll"))
	}
	if workingDir, err := os.Getwd(); err == nil {
		addPath(filepath.Join(workingDir, "fptr10.dll"))
	}
	if programFiles86 := strings.TrimSpace(os.Getenv("ProgramFiles(x86)")); programFiles86 != "" {
		addPath(filepath.Join(programFiles86, "ATOL", "Drivers10", "KKT", "bin", "fptr10.dll"))
	}
	if programFiles := strings.TrimSpace(os.Getenv("ProgramFiles")); programFiles != "" {
		addPath(filepath.Join(programFiles, "ATOL", "Drivers10", "KKT", "bin", "fptr10.dll"))
	}

	return result
}

func findDriverPath(paths []string) (string, bool) {
	for _, candidate := range paths {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func readDriverVersion(path string) (string, error) {
	library, err := loadNativeLibrary(path)
	if err != nil {
		return "", err
	}
	return library.version()
}

func loadNativeLibrary(path string) (*nativeLibrary, error) {
	dll := windows.NewLazyDLL(path)
	if err := dll.Load(); err != nil {
		return nil, fmt.Errorf("не удалось загрузить %s: %w", path, err)
	}

	return &nativeLibrary{
		dll:                  dll,
		createProc:           dll.NewProc("libfptr_create"),
		destroyProc:          dll.NewProc("libfptr_destroy"),
		getVersionProc:       dll.NewProc("libfptr_get_version_string"),
		setSettingsProc:      dll.NewProc("libfptr_set_settings"),
		openProc:             dll.NewProc("libfptr_open"),
		closeProc:            dll.NewProc("libfptr_close"),
		isOpenedProc:         dll.NewProc("libfptr_is_opened"),
		errorCodeProc:        dll.NewProc("libfptr_error_code"),
		errorDescriptionProc: dll.NewProc("libfptr_error_description"),
		setParamIntProc:      dll.NewProc("libfptr_set_param_int"),
		setParamStringProc:   dll.NewProc("libfptr_set_param_str"),
		getParamIntProc:      dll.NewProc("libfptr_get_param_int"),
		getParamBoolProc:     dll.NewProc("libfptr_get_param_bool"),
		getParamDateTimeProc: dll.NewProc("libfptr_get_param_datetime"),
		getParamStringProc:   dll.NewProc("libfptr_get_param_str"),
		queryDataProc:        dll.NewProc("libfptr_query_data"),
		fnQueryDataProc:      dll.NewProc("libfptr_fn_query_data"),
		beginReadRecordsProc: dll.NewProc("libfptr_begin_read_records"),
		readNextRecordProc:   dll.NewProc("libfptr_read_next_record"),
		endReadRecordsProc:   dll.NewProc("libfptr_end_read_records"),
	}, nil
}

func (l *nativeLibrary) version() (string, error) {
	address, _, callErr := l.getVersionProc.Call()
	if address == 0 {
		return "", fmt.Errorf("драйвер не вернул строку версии: %v", callErr)
	}
	return cString(address), nil
}

func (l *nativeLibrary) newHandle() (*deviceHandle, error) {
	var handle uintptr
	result, _, _ := l.createProc.Call(uintptr(unsafe.Pointer(&handle)))
	if int(result) != 0 || handle == 0 {
		return nil, fmt.Errorf("не удалось создать экземпляр драйвера Атол, код=%d", int(result))
	}
	return &deviceHandle{library: l, handle: handle}, nil
}

func (h *deviceHandle) destroy() {
	if h == nil || h.handle == 0 {
		return
	}
	handle := h.handle
	h.library.destroyProc.Call(uintptr(unsafe.Pointer(&handle)))
	h.handle = 0
}

func (h *deviceHandle) configure(endpoint domain.Endpoint) error {
	settings := map[string]any{
		settingModel: modelAtolAuto,
	}

	switch endpoint.Transport {
	case domain.TransportCOM:
		baudRate, err := strconv.Atoi(endpoint.BaudRate)
		if err != nil {
			return fmt.Errorf("не удалось разобрать baudrate для %s: %w", endpoint.ConnectionLabel(), err)
		}
		settings[settingPort] = portCOM
		settings[settingCOMFile] = endpoint.COMPort
		settings[settingBaudRate] = baudRate
	case domain.TransportTCP:
		settings[settingPort] = portTCPIP
		settings[settingIPAddress] = endpoint.IP
		settings[settingIPPort] = endpoint.Port
	default:
		return fmt.Errorf("неподдерживаемый transport %q", endpoint.Transport)
	}

	rawSettings, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("не удалось сериализовать настройки драйвера: %w", err)
	}
	settingsPtr, err := windows.UTF16PtrFromString(string(rawSettings))
	if err != nil {
		return fmt.Errorf("не удалось подготовить строку настроек драйвера: %w", err)
	}

	result, _, _ := h.library.setSettingsProc.Call(h.handle, uintptr(unsafe.Pointer(settingsPtr)))
	if int(result) != 0 {
		return h.driverError("setSettings", int(result))
	}
	return nil
}

func (h *deviceHandle) open() error {
	result, _, _ := h.library.openProc.Call(h.handle)
	if int(result) != 0 {
		return h.driverError("open", int(result))
	}
	return nil
}

func (h *deviceHandle) close() error {
	result, _, _ := h.library.closeProc.Call(h.handle)
	if int(result) != 0 {
		return h.driverError("close", int(result))
	}
	return nil
}

func (h *deviceHandle) isOpened() (bool, error) {
	result, _, callErr := h.library.isOpenedProc.Call(h.handle)
	if callErr != windows.ERROR_SUCCESS && callErr != nil {
		return false, fmt.Errorf("не удалось проверить состояние соединения: %w", callErr)
	}
	return result != 0, nil
}

func (h *deviceHandle) SetParamInt(id, value int) error {
	h.library.setParamIntProc.Call(h.handle, uintptr(id), uintptr(uint32(value)))
	return nil
}

func (h *deviceHandle) SetParamString(id int, value string) error {
	textPtr, err := windows.UTF16PtrFromString(value)
	if err != nil {
		return fmt.Errorf("не удалось подготовить строковый параметр %d: %w", id, err)
	}
	h.library.setParamStringProc.Call(h.handle, uintptr(id), uintptr(unsafe.Pointer(textPtr)))
	return nil
}

func (h *deviceHandle) QueryData() error {
	result, _, _ := h.library.queryDataProc.Call(h.handle)
	if int(result) != 0 {
		return h.driverError("queryData", int(result))
	}
	return nil
}

func (h *deviceHandle) FNQueryData() error {
	result, _, _ := h.library.fnQueryDataProc.Call(h.handle)
	if int(result) != 0 {
		return h.driverError("fnQueryData", int(result))
	}
	return nil
}

func (h *deviceHandle) GetParamString(id int) (string, error) {
	buffer := make([]uint16, 256)
	result, _, _ := h.library.getParamStringProc.Call(
		h.handle,
		uintptr(id),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
	)
	size := int(result)
	if size > len(buffer) {
		buffer = make([]uint16, size)
		_, _, _ = h.library.getParamStringProc.Call(
			h.handle,
			uintptr(id),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
		)
	}
	return windows.UTF16ToString(buffer), nil
}

func (h *deviceHandle) GetParamInt(id int) (int, error) {
	result, _, _ := h.library.getParamIntProc.Call(h.handle, uintptr(id))
	return int(uint32(result)), nil
}

func (h *deviceHandle) GetParamBool(id int) (bool, error) {
	result, _, _ := h.library.getParamBoolProc.Call(h.handle, uintptr(id))
	return result != 0, nil
}

func (h *deviceHandle) GetParamDateTime(id int) (time.Time, error) {
	var year int32
	var month int32
	var day int32
	var hour int32
	var minute int32
	var second int32

	h.library.getParamDateTimeProc.Call(
		h.handle,
		uintptr(id),
		uintptr(unsafe.Pointer(&year)),
		uintptr(unsafe.Pointer(&month)),
		uintptr(unsafe.Pointer(&day)),
		uintptr(unsafe.Pointer(&hour)),
		uintptr(unsafe.Pointer(&minute)),
		uintptr(unsafe.Pointer(&second)),
	)

	if year == 0 || month == 0 || day == 0 {
		return time.Time{}, nil
	}
	return time.Date(int(year), time.Month(month), int(day), int(hour), int(minute), int(second), 0, time.Local), nil
}

func (h *deviceHandle) BeginReadRecords() error {
	result, _, _ := h.library.beginReadRecordsProc.Call(h.handle)
	if int(result) != 0 {
		return h.driverError("beginReadRecords", int(result))
	}
	return nil
}

func (h *deviceHandle) ReadNextRecord(recordsID string) (int, error) {
	if err := h.SetParamString(paramRecordsID, recordsID); err != nil {
		return 0, err
	}
	result, _, _ := h.library.readNextRecordProc.Call(h.handle)
	if int(result) == driverOK || int(result) == driverNoMoreData {
		return int(result), nil
	}

	if code := h.errorCode(); code == driverNoMoreData {
		return driverNoMoreData, nil
	}
	return int(result), h.driverError("readNextRecord", int(result))
}

func (h *deviceHandle) EndReadRecords(recordsID string) error {
	if err := h.SetParamString(paramRecordsID, recordsID); err != nil {
		return err
	}
	result, _, _ := h.library.endReadRecordsProc.Call(h.handle)
	if int(result) != 0 {
		return h.driverError("endReadRecords", int(result))
	}
	return nil
}

func (h *deviceHandle) driverError(operation string, callResult int) error {
	errorCode := h.errorCode()
	errorDescription := ""

	buffer := make([]uint16, 256)
	if result, _, _ := h.library.errorDescriptionProc.Call(
		h.handle,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
	); result > 0 {
		size := int(result)
		if size > len(buffer) {
			buffer = make([]uint16, size)
			_, _, _ = h.library.errorDescriptionProc.Call(
				h.handle,
				uintptr(unsafe.Pointer(&buffer[0])),
				uintptr(len(buffer)),
			)
		}
		errorDescription = windows.UTF16ToString(buffer)
	}

	if errorDescription == "" {
		return fmt.Errorf("операция %s завершилась ошибкой драйвера, code=%d, call_result=%d", operation, errorCode, callResult)
	}
	return fmt.Errorf("операция %s завершилась ошибкой драйвера, code=%d, call_result=%d: %s", operation, errorCode, callResult, errorDescription)
}

func (h *deviceHandle) errorCode() int {
	if result, _, _ := h.library.errorCodeProc.Call(h.handle); result != 0 {
		return int(result)
	}
	return 0
}

func cString(address uintptr) string {
	if address == 0 {
		return ""
	}

	bytes := make([]byte, 0, 32)
	for offset := uintptr(0); ; offset++ {
		value := *(*byte)(unsafe.Pointer(address + offset))
		if value == 0 {
			return string(bytes)
		}
		bytes = append(bytes, value)
	}
}
