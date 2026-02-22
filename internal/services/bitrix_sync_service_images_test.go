package services

import (
	b24 "etalon-server/internal/infra/plugins/bitrix"
	"testing"
)

func TestReplaceBitrixImageTagsWithDiskMarkers(t *testing.T) {
	source := "Текст\n[IMG]/api/static/tickets/a.png[/IMG]\nеще\n[IMG]/api/static/tickets/b.png[/IMG]"
	got := replaceBitrixImageTagsWithDiskMarkers(source, []int64{36475, 36476})
	want := "Текст\n[DISK FILE ID=n36475]\nеще\n[DISK FILE ID=n36476]"
	if got != want {
		t.Fatalf("неверный результат замены:\nwant: %s\ngot:  %s", want, got)
	}
}

func TestExtractImageEntriesFromRawFiles_ImageObjectIsDetected(t *testing.T) {
	filesRaw := map[string]interface{}{
		"36913": map[string]interface{}{
			"id":    36913,
			"name":  "изображение (20).png",
			"type":  "image",
			"image": map[string]interface{}{"width": 840, "height": 486},
		},
	}

	entries := extractImageEntriesFromRawFiles(filesRaw)
	if len(entries) != 1 {
		t.Fatalf("ожидалась одна запись, получено %d", len(entries))
	}
	if entries[0].id != 36913 {
		t.Fatalf("ожидался id=36913, получено %d", entries[0].id)
	}
	if entries[0].name != "изображение (20).png" {
		t.Fatalf("ожидалось исходное имя файла, получено %q", entries[0].name)
	}
}

func TestMatchImageDiskFileIDsByName_FallbackByOrder(t *testing.T) {
	raw := map[string]interface{}{
		"FILES": map[string]interface{}{
			"36913": map[string]interface{}{
				"id":    36913,
				"name":  "изображение (20).png",
				"type":  "image",
				"image": map[string]interface{}{"width": 840, "height": 486},
			},
		},
	}
	files := []b24.FileToUpload{
		{Name: "изображение.png", Base64Content: "c29tZQ=="},
	}

	imageIDs := matchImageDiskFileIDsByName(raw, files)
	if len(imageIDs) != 1 {
		t.Fatalf("ожидался один ID, получено %d", len(imageIDs))
	}
	if imageIDs[0] != 36913 {
		t.Fatalf("ожидался ID=36913, получено %d", imageIDs[0])
	}
}

func TestReplaceBitrixInlineFileReferencesWithDiskMarkers_RewritesStaticURLTags(t *testing.T) {
	source := `[URL=/api/static/tickets/t1/file.bin]file.bin[/URL]
[IMG]/api/static/tickets/t1/image.png[/IMG]`
	got := replaceBitrixInlineFileReferencesWithDiskMarkers(source, []int64{9001, 9002})
	want := "[DISK FILE ID=n9001]\n[DISK FILE ID=n9002]"
	if got != want {
		t.Fatalf("неверная замена ссылок на файлы:\nwant: %s\ngot:  %s", want, got)
	}
}
