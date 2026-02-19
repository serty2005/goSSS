package services

import "testing"

func TestReplaceBitrixImageTagsWithDiskMarkers(t *testing.T) {
	source := "Текст\n[IMG]/api/static/tickets/a.png[/IMG]\nеще\n[IMG]/api/static/tickets/b.png[/IMG]"
	got := replaceBitrixImageTagsWithDiskMarkers(source, []int64{36475, 36476})
	want := "Текст\n[IMG][DISK FILE ID=n36475][/IMG]\nеще\n[IMG][DISK FILE ID=n36476][/IMG]"
	if got != want {
		t.Fatalf("неверный результат замены:\nwant: %s\ngot:  %s", want, got)
	}
}
