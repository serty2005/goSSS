package protocol

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"etalon-agent/internal/fiscalmitsu/domain"
	"github.com/tarm/serial"
	"golang.org/x/text/encoding/charmap"
)

const (
	defaultDriverName        = "MitsuCube.exe"
	defaultCOMTimeout        = 200 * time.Millisecond
	defaultTCPTimeout        = 10 * time.Second
	maxCommandChunkSize      = 535
	packetStartByte     byte = 0x02
	packetEndByte       byte = 0x03
	packetChunkByte     byte = 0x17
)

type liveRuntime struct {
	searchPaths []string
}

func newRuntime() runtimeAPI {
	return &liveRuntime{
		searchPaths: defaultDriverSearchPaths(),
	}
}

func (r *liveRuntime) Probe(context.Context) (ProbeResult, error) {
	result := ProbeResult{
		Supported:   true,
		SearchPaths: r.searchPaths,
	}

	driverPath, found := findDriverPath(r.searchPaths)
	if !found {
		result.Message = "MitsuCube.exe не найден в стандартных путях поиска"
		return result, nil
	}

	result.DriverPresent = true
	result.DriverPath = driverPath

	driverVersion, err := readFileVersion(driverPath)
	if err != nil {
		result.Message = fmt.Sprintf("MitsuCube.exe найден, но его версия не определена: %v", err)
		return result, nil
	}

	result.DriverVersion = driverVersion
	result.Message = "MitsuCube.exe найден"
	return result, nil
}

func (r *liveRuntime) SendCommand(ctx context.Context, endpoint domain.Endpoint, command string) (string, error) {
	switch endpoint.Transport {
	case domain.TransportCOM:
		baudRate, err := strconv.Atoi(strings.TrimSpace(endpoint.BaudRate))
		if err != nil {
			return "", fmt.Errorf("не удалось разобрать baudrate для %s: %w", endpoint.ConnectionLabel(), err)
		}
		return sendCommandToCOM(ctx, command, endpoint.COMPort, baudRate)
	case domain.TransportTCP:
		return sendCommandToTCP(ctx, command, endpoint.IP, endpoint.Port)
	default:
		return "", fmt.Errorf("неподдерживаемый transport %q", endpoint.Transport)
	}
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
		addPath(filepath.Join(filepath.Dir(executable), defaultDriverName))
	}
	if workingDir, err := os.Getwd(); err == nil {
		addPath(filepath.Join(workingDir, defaultDriverName))
	}
	if programFiles := strings.TrimSpace(os.Getenv("ProgramFiles")); programFiles != "" {
		addPath(filepath.Join(programFiles, "MITSU.1-F", defaultDriverName))
	}
	if programFiles86 := strings.TrimSpace(os.Getenv("ProgramFiles(x86)")); programFiles86 != "" {
		addPath(filepath.Join(programFiles86, "MITSU.1-F", defaultDriverName))
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

func sendCommandToCOM(ctx context.Context, command, port string, baudRate int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	commandBytes, err := encodeCP1251(command)
	if err != nil {
		return "", fmt.Errorf("не удалось закодировать команду Mitsu: %w", err)
	}

	packet := buildCOMPacket(commandBytes)
	serialPort, err := serial.OpenPort(&serial.Config{
		Name:        port,
		Baud:        baudRate,
		ReadTimeout: defaultCOMTimeout,
	})
	if err != nil {
		return "", fmt.Errorf("не удалось открыть COM-порт %s: %w", port, err)
	}
	defer serialPort.Close()

	if _, err := serialPort.Write(packet); err != nil {
		return "", fmt.Errorf("не удалось отправить команду Mitsu в COM-порт %s: %w", port, err)
	}

	response, err := readCOMResponse(ctx, serialPort)
	if err != nil {
		return "", err
	}
	return parseCOMResponse(response)
}

func sendCommandToTCP(ctx context.Context, command, host string, port int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	commandBytes, err := encodeCP1251(command)
	if err != nil {
		return "", fmt.Errorf("не удалось закодировать команду Mitsu: %w", err)
	}

	dialer := net.Dialer{Timeout: defaultTCPTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return "", fmt.Errorf("не удалось подключиться к Mitsu по TCP %s:%d: %w", host, port, err)
	}
	defer conn.Close()

	if deadlineErr := conn.SetDeadline(time.Now().Add(defaultTCPTimeout)); deadlineErr != nil {
		return "", fmt.Errorf("не удалось установить дедлайн TCP-соединения: %w", deadlineErr)
	}

	for _, packet := range splitTCPPackets(commandBytes) {
		if _, err := conn.Write(packet); err != nil {
			return "", fmt.Errorf("не удалось отправить команду Mitsu по TCP %s:%d: %w", host, port, err)
		}
	}

	response, err := io.ReadAll(conn)
	if err != nil {
		if errors.Is(err, net.ErrClosed) {
			return "", nil
		}
		return "", fmt.Errorf("не удалось получить ответ Mitsu по TCP %s:%d: %w", host, port, err)
	}

	decoded, err := decodeCP1251(response)
	if err != nil {
		return "", fmt.Errorf("не удалось декодировать ответ Mitsu по TCP %s:%d: %w", host, port, err)
	}
	return decoded, nil
}

func buildCOMPacket(command []byte) []byte {
	packet := make([]byte, 0, len(command)+5)
	packet = append(packet, packetStartByte)

	lengthBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(lengthBytes, uint16(len(command)))
	packet = append(packet, lengthBytes...)
	packet = append(packet, command...)
	packet = append(packet, packetEndByte)
	packet = append(packet, calculateLRC(packet))
	return packet
}

func readCOMResponse(ctx context.Context, reader io.Reader) ([]byte, error) {
	response := make([]byte, 0, 128)
	buffer := make([]byte, 1)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		readBytes, err := reader.Read(buffer)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("не удалось прочитать ответ Mitsu из COM-порта: %w", err)
		}
		if readBytes == 0 {
			break
		}

		response = append(response, buffer[0])
		if len(response) >= 2 && response[len(response)-2] == packetEndByte {
			break
		}
	}

	if len(response) == 0 {
		return nil, fmt.Errorf("Mitsu не вернул ответ из COM-порта")
	}
	return response, nil
}

func parseCOMResponse(response []byte) (string, error) {
	if len(response) < 2 {
		return "", fmt.Errorf("ответ COM-устройства Mitsu не подходит под ожидаемый формат")
	}

	receivedLRC := response[len(response)-1]
	data := response[:len(response)-1]
	calculatedLRC := calculateLRC(data)
	if receivedLRC != calculatedLRC {
		return "", fmt.Errorf("ответ Mitsu содержит неверную контрольную сумму")
	}

	if len(data) == 0 || data[len(data)-1] != packetEndByte {
		return "", fmt.Errorf("ответ Mitsu не содержит завершающий байт ETX")
	}

	decoded, err := decodeCP1251(data[:len(data)-1])
	if err != nil {
		return "", fmt.Errorf("не удалось декодировать ответ Mitsu из COM-порта: %w", err)
	}
	return decoded, nil
}

func splitTCPPackets(command []byte) [][]byte {
	if len(command) <= maxCommandChunkSize {
		return [][]byte{bytes.Clone(command)}
	}

	packets := make([][]byte, 0, len(command)/maxCommandChunkSize+1)
	for start := 0; start < len(command); start += maxCommandChunkSize {
		end := min(start+maxCommandChunkSize, len(command))
		chunk := bytes.Clone(command[start:end])
		if end < len(command) {
			chunk = append(chunk, packetChunkByte)
		}
		packets = append(packets, chunk)
	}
	return packets
}

func encodeCP1251(value string) ([]byte, error) {
	return charmap.Windows1251.NewEncoder().Bytes([]byte(value))
}

func decodeCP1251(value []byte) (string, error) {
	decoded, err := charmap.Windows1251.NewDecoder().Bytes(value)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func calculateLRC(data []byte) byte {
	var lrc byte
	for _, value := range data {
		lrc ^= value
	}
	return lrc
}
