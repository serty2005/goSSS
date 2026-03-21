package hostinfo

import "testing"

func TestExtractServerURLFromXMLReadsElement(t *testing.T) {
	t.Parallel()

	raw := []byte(`<configuration><serverUrl>https://demo.iiko.ru:443/resto/</serverUrl></configuration>`)
	got := extractServerURLFromXML(raw)
	want := "https://demo.iiko.ru:443/resto/"

	if got != want {
		t.Fatalf("ожидался serverUrl %q, получено %q", want, got)
	}
}

func TestExtractServerURLFromXMLReadsAttribute(t *testing.T) {
	t.Parallel()

	raw := []byte(`<cashServer settings="default" serverUrl="https://demo.syrve.online/resto/" />`)
	got := extractServerURLFromXML(raw)
	want := "https://demo.syrve.online/resto/"

	if got != want {
		t.Fatalf("ожидался serverUrl %q, получено %q", want, got)
	}
}

func TestNormalizeCommandOutputReturnsFirstNonEmptyLine(t *testing.T) {
	t.Parallel()

	raw := []byte("\r\n  \r\n123456789\r\nsecondary line\r\n")
	got := normalizeCommandOutput(raw)

	if got != "123456789" {
		t.Fatalf("ожидался ID %q, получено %q", "123456789", got)
	}
}
