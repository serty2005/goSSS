//go:build windows

package inventory

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	serialCommRegistryPath = `HARDWARE\DEVICEMAP\SERIALCOMM`
	enumRegistryPath       = `SYSTEM\CurrentControlSet\Enum`

	comDeviceTypeFiscalRegister = "fiscal_register"
	comDeviceTypeBarcodeScanner = "barcode_scanner"
	comDeviceTypeBankTerminal   = "bank_terminal"
	comDeviceTypeScales         = "scales"

	comDictionarySource = "local_signature_dictionary"
)

var (
	usbSignaturePattern = regexp.MustCompile(`(?i)\bVID_([0-9A-F]{4}).*?\bPID_([0-9A-F]{4})\b`)
	pciSignaturePattern = regexp.MustCompile(`(?i)\bVEN_([0-9A-F]{4}).*?\bDEV_([0-9A-F]{4})\b`)
)

type comSignature struct {
	VendorID   string
	ProductID  string
	DeviceKey  string
	MatchedRaw string
}

type comDeviceRule struct {
	SignatureKey     string
	DeviceType       string
	Label            string
	Confidence       string
	SuggestedAdapter string
}

var defaultCOMSignatureDictionary = []comDeviceRule{
	// Словарь намеренно оставлен пустым до накопления подтвержденных оператором сигнатур.
	// Сюда должны попадать только реальные VEN/DEV или VID/PID, снятые с клиентских машин.
}

func collectCOMPorts() ([]COMPort, error) {
	portsByName := make(map[string]COMPort)

	serialPorts, err := collectSerialCommPorts()
	if err != nil {
		return nil, err
	}
	for _, port := range serialPorts {
		portsByName[strings.ToUpper(port.Name)] = port
	}

	enumPorts, err := collectEnumeratedCOMPorts()
	if err != nil {
		return nil, err
	}
	for _, port := range enumPorts {
		key := strings.ToUpper(port.Name)
		if current, ok := portsByName[key]; ok {
			portsByName[key] = mergeCOMPort(current, port)
			continue
		}
		portsByName[key] = port
	}

	result := slices.Collect(maps.Values(portsByName))
	slices.SortFunc(result, func(a, b COMPort) int {
		return cmp.Or(
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.FriendlyName, b.FriendlyName),
			cmp.Compare(a.InstanceID, b.InstanceID),
		)
	})
	return result, nil
}

func collectSerialCommPorts() ([]COMPort, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, serialCommRegistryPath, registry.READ)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil, nil
		}
		return nil, fmt.Errorf("не удалось открыть список COM-портов: %w", err)
	}
	defer key.Close()

	names, err := key.ReadValueNames(-1)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать список COM-портов: %w", err)
	}

	result := make([]COMPort, 0, len(names))
	for _, device := range names {
		value, _, err := key.GetStringValue(device)
		if err != nil {
			continue
		}

		name := normalizeCOMPortName(value)
		if name == "" {
			continue
		}

		result = append(result, COMPort{
			Name:   name,
			Device: strings.TrimSpace(device),
			Source: `HKLM\` + serialCommRegistryPath,
		})
	}
	return result, nil
}

func collectEnumeratedCOMPorts() ([]COMPort, error) {
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, enumRegistryPath, registry.READ)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil, nil
		}
		return nil, fmt.Errorf("не удалось открыть дерево устройств Enum: %w", err)
	}
	defer root.Close()

	enumerators, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать список enumerator в Enum: %w", err)
	}

	portsByName := make(map[string]COMPort)
	for _, enumerator := range enumerators {
		enumKey, err := registry.OpenKey(root, enumerator, registry.READ)
		if err != nil {
			continue
		}

		deviceIDs, err := enumKey.ReadSubKeyNames(-1)
		if err != nil {
			enumKey.Close()
			continue
		}

		for _, deviceID := range deviceIDs {
			deviceKey, err := registry.OpenKey(enumKey, deviceID, registry.READ)
			if err != nil {
				continue
			}

			instanceIDs, err := deviceKey.ReadSubKeyNames(-1)
			if err != nil {
				deviceKey.Close()
				continue
			}

			for _, instanceID := range instanceIDs {
				instanceKey, err := registry.OpenKey(deviceKey, instanceID, registry.READ)
				if err != nil {
					continue
				}

				port, ok := buildEnumeratedCOMPort(enumerator, deviceID, instanceID, instanceKey)
				instanceKey.Close()
				if !ok {
					continue
				}

				key := strings.ToUpper(port.Name)
				if current, exists := portsByName[key]; exists {
					portsByName[key] = mergeCOMPort(current, port)
					continue
				}
				portsByName[key] = port
			}

			deviceKey.Close()
		}

		enumKey.Close()
	}

	return slices.Collect(maps.Values(portsByName)), nil
}

func buildEnumeratedCOMPort(enumerator, deviceID, instanceID string, instanceKey registry.Key) (COMPort, bool) {
	portName := firstNonEmpty(
		readDeviceParametersString(instanceKey, "PortName"),
		readRegistryStringValue(instanceKey, "PortName"),
	)
	portName = normalizeCOMPortName(portName)
	if portName == "" {
		return COMPort{}, false
	}

	hardwareIDs := readRegistryStrings(instanceKey, "HardwareID")
	compatibleIDs := readRegistryStrings(instanceKey, "CompatibleIDs")
	signature := resolveCOMSignature(append([]string{
		strings.TrimSpace(enumerator) + `\` + strings.TrimSpace(deviceID) + `\` + strings.TrimSpace(instanceID),
	}, append(hardwareIDs, compatibleIDs...)...))

	port := COMPort{
		Name:          portName,
		Source:        `HKLM\` + enumRegistryPath,
		Enumerator:    strings.TrimSpace(enumerator),
		InstanceID:    strings.TrimSpace(enumerator) + `\` + strings.TrimSpace(deviceID) + `\` + strings.TrimSpace(instanceID),
		FriendlyName:  cleanRegistryPresentation(readRegistryStringValue(instanceKey, "FriendlyName")),
		Description:   cleanRegistryPresentation(firstNonEmpty(readRegistryStringValue(instanceKey, "DeviceDesc"), readRegistryStringValue(instanceKey, "BusReportedDeviceDesc"))),
		Manufacturer:  cleanRegistryPresentation(readRegistryStringValue(instanceKey, "Mfg")),
		Service:       cleanRegistryPresentation(readRegistryStringValue(instanceKey, "Service")),
		Class:         cleanRegistryPresentation(readRegistryStringValue(instanceKey, "Class")),
		Location:      cleanRegistryPresentation(firstNonEmpty(readRegistryStringValue(instanceKey, "LocationInformation"), readRegistryStringValue(instanceKey, "LocationPaths"))),
		HardwareIDs:   hardwareIDs,
		CompatibleIDs: compatibleIDs,
		VendorID:      signature.VendorID,
		ProductID:     signature.ProductID,
		SignatureKey:  signature.DeviceKey,
	}
	port.Classification = classifyCOMPort(port)
	return port, true
}

func mergeCOMPort(base, overlay COMPort) COMPort {
	result := base

	result.Name = firstNonEmpty(result.Name, overlay.Name)
	result.Device = firstNonEmpty(result.Device, overlay.Device)
	result.Source = combineSources(result.Source, overlay.Source)
	result.Enumerator = firstNonEmpty(result.Enumerator, overlay.Enumerator)
	result.InstanceID = firstNonEmpty(result.InstanceID, overlay.InstanceID)
	result.FriendlyName = firstNonEmpty(result.FriendlyName, overlay.FriendlyName)
	result.Description = firstNonEmpty(result.Description, overlay.Description)
	result.Manufacturer = firstNonEmpty(result.Manufacturer, overlay.Manufacturer)
	result.Service = firstNonEmpty(result.Service, overlay.Service)
	result.Class = firstNonEmpty(result.Class, overlay.Class)
	result.Location = firstNonEmpty(result.Location, overlay.Location)
	result.HardwareIDs = mergeUniqueStrings(result.HardwareIDs, overlay.HardwareIDs)
	result.CompatibleIDs = mergeUniqueStrings(result.CompatibleIDs, overlay.CompatibleIDs)
	result.VendorID = firstNonEmpty(result.VendorID, overlay.VendorID)
	result.ProductID = firstNonEmpty(result.ProductID, overlay.ProductID)
	result.SignatureKey = firstNonEmpty(result.SignatureKey, overlay.SignatureKey)

	if result.Classification == nil {
		result.Classification = overlay.Classification
	}
	if result.Classification == nil {
		result.Classification = classifyCOMPort(result)
	}
	return result
}

func mergeUniqueStrings(current, next []string) []string {
	if len(current) == 0 {
		return slices.Clone(next)
	}
	if len(next) == 0 {
		return slices.Clone(current)
	}

	seen := make(map[string]string, len(current)+len(next))
	add := func(values []string) {
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			key := strings.ToLower(trimmed)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = trimmed
		}
	}

	add(current)
	add(next)

	result := make([]string, 0, len(seen))
	for _, value := range seen {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func combineSources(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	switch {
	case left == "":
		return right
	case right == "":
		return left
	case strings.EqualFold(left, right):
		return left
	default:
		return left + "; " + right
	}
}

func readDeviceParametersString(instanceKey registry.Key, valueName string) string {
	deviceParamsKey, err := registry.OpenKey(instanceKey, `Device Parameters`, registry.READ)
	if err != nil {
		return ""
	}
	defer deviceParamsKey.Close()
	return readRegistryStringValue(deviceParamsKey, valueName)
}

func readRegistryStringValue(key registry.Key, valueName string) string {
	value, _, err := key.GetStringValue(valueName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func readRegistryStrings(key registry.Key, valueName string) []string {
	values, _, err := key.GetStringsValue(valueName)
	if err == nil {
		return normalizeStringList(values)
	}

	value := readRegistryStringValue(key, valueName)
	if value == "" {
		return nil
	}
	return []string{value}
}

func normalizeStringList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	slices.Sort(result)
	return result
}

func normalizeCOMPortName(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if !strings.HasPrefix(value, "COM") {
		return ""
	}
	if len(value) <= 3 {
		return ""
	}
	return value
}

func cleanRegistryPresentation(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "@") {
		if _, tail, ok := strings.Cut(value, ";"); ok {
			value = strings.TrimSpace(tail)
		}
	}
	return strings.TrimSpace(value)
}

func resolveCOMSignature(values []string) comSignature {
	for _, value := range values {
		raw := strings.TrimSpace(value)
		if raw == "" {
			continue
		}

		if match := usbSignaturePattern.FindStringSubmatch(raw); len(match) == 3 {
			vendorID := strings.ToUpper(match[1])
			productID := strings.ToUpper(match[2])
			return comSignature{
				VendorID:   vendorID,
				ProductID:  productID,
				DeviceKey:  "usb:vid_" + strings.ToLower(vendorID) + "&pid_" + strings.ToLower(productID),
				MatchedRaw: raw,
			}
		}

		if match := pciSignaturePattern.FindStringSubmatch(raw); len(match) == 3 {
			vendorID := strings.ToUpper(match[1])
			productID := strings.ToUpper(match[2])
			return comSignature{
				VendorID:   vendorID,
				ProductID:  productID,
				DeviceKey:  "pci:ven_" + strings.ToLower(vendorID) + "&dev_" + strings.ToLower(productID),
				MatchedRaw: raw,
			}
		}
	}
	return comSignature{}
}

func classifyCOMPort(port COMPort) *COMPortClassification {
	signatureKey := strings.ToLower(strings.TrimSpace(port.SignatureKey))
	if signatureKey == "" {
		return nil
	}

	for _, rule := range defaultCOMSignatureDictionary {
		if strings.ToLower(strings.TrimSpace(rule.SignatureKey)) != signatureKey {
			continue
		}

		return &COMPortClassification{
			DeviceType:       strings.TrimSpace(rule.DeviceType),
			Label:            strings.TrimSpace(rule.Label),
			Confidence:       firstNonEmpty(strings.TrimSpace(rule.Confidence), "high"),
			Source:           comDictionarySource,
			MatchedSignature: strings.TrimSpace(rule.SignatureKey),
			SuggestedAdapter: strings.TrimSpace(rule.SuggestedAdapter),
		}
	}

	return nil
}
