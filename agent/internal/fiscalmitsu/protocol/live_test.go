package protocol

import "testing"

func TestBuildCOMPacket(t *testing.T) {
	t.Parallel()

	packet := buildCOMPacket([]byte("ABC"))
	if len(packet) != 8 {
		t.Fatalf("ожидалась длина пакета 8, получено %d", len(packet))
	}
	if packet[0] != packetStartByte {
		t.Fatalf("ожидался STX 0x02, получено %#x", packet[0])
	}
	if packet[3] != 'A' || packet[5] != 'C' {
		t.Fatalf("команда в пакете сформирована неверно: %v", packet)
	}
	if packet[len(packet)-2] != packetEndByte {
		t.Fatalf("ожидался ETX перед LRC, получено %#x", packet[len(packet)-2])
	}
	if packet[len(packet)-1] != calculateLRC(packet[:len(packet)-1]) {
		t.Fatal("LRC пакета рассчитан неверно")
	}
}

func TestParseCOMResponse(t *testing.T) {
	t.Parallel()

	data := []byte("<OK DEV='Mitsu M' />")
	raw := append(append([]byte{}, data...), packetEndByte)
	raw = append(raw, calculateLRC(raw))

	response, err := parseCOMResponse(raw)
	if err != nil {
		t.Fatalf("parseCOMResponse вернул ошибку: %v", err)
	}
	if response != "<OK DEV='Mitsu M' />" {
		t.Fatalf("ожидался декодированный ответ, получено %q", response)
	}
}

func TestSplitTCPPackets(t *testing.T) {
	t.Parallel()

	command := make([]byte, maxCommandChunkSize+10)
	packets := splitTCPPackets(command)
	if len(packets) != 2 {
		t.Fatalf("ожидалось 2 TCP-пакета, получено %d", len(packets))
	}
	if packets[0][len(packets[0])-1] != packetChunkByte {
		t.Fatalf("у первого пакета должен быть ETB, получено %#x", packets[0][len(packets[0])-1])
	}
}
