package protocol

import "testing"

func TestExtractValueByTag(t *testing.T) {
	t.Parallel()

	xml := `<OK DEV='Mitsu M' SERIAL="123"><T1048>ООО &quot;Ромашка&quot;</T1048></OK>`

	value, ok := extractValueByTag(xml, "DEV=")
	if !ok || value != "Mitsu M" {
		t.Fatalf("ожидалось значение DEV=Mitsu M, получено %q", value)
	}

	value, ok = extractValueByTag(xml, "<T1048>")
	if !ok || value != `ООО &quot;Ромашка&quot;` {
		t.Fatalf("ожидалось значение тега T1048, получено %q", value)
	}
}

func TestBuildPayloadRejectsUnknownFFDCode(t *testing.T) {
	t.Parallel()

	_, err := buildPayload(
		`<OK DEV='Mitsu M' />`,
		`<OK SERIAL='1234567890' VER='3.5.7' />`,
		`<OK T1188='Ф' T1037='1' DATE='2024-03-04 10:11:12' T1209='99' T1018='7701234567' ExtMODE='0'><T1048>ООО Тест</T1048><T1046>ОФД</T1046><T1009>Адрес</T1009></OK>`,
		`<OK FN='1' VALID='2025-03-04 10:11:12' EDITION='ФН-1.2' />`,
		"1.2.3.4",
	)
	if err == nil {
		t.Fatal("ожидалась ошибка для неподдерживаемого кода FFD")
	}
}
