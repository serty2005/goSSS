package services

import "testing"

func TestMergeBitrixAttachmentMarkup_PreservesInlinePosition(t *testing.T) {
	base := "line1 " + bitrixDiskPlaceholder("36915") + " line2"
	renderedByID := map[int64]string{
		36915: `<img src="/api/static/tickets/t1/bitrix/disk-36915.png" alt="img" />`,
	}
	renderedOrder := []int64{36915}

	got := mergeBitrixAttachmentMarkup(base, renderedByID, renderedOrder)
	want := `line1 <img src="/api/static/tickets/t1/bitrix/disk-36915.png" alt="img" /> line2`
	if got != want {
		t.Fatalf("ожидался inline-рендер в позиции плейсхолдера:\nwant: %s\ngot:  %s", want, got)
	}
}

func TestMergeBitrixAttachmentMarkup_AppendsUnplacedFiles(t *testing.T) {
	base := "text"
	renderedByID := map[int64]string{
		36915: `<img src="/api/static/tickets/t1/bitrix/disk-36915.png" alt="img" />`,
		36916: `<a href="/api/static/tickets/t1/bitrix/disk-36916.txt" target="_blank" rel="noreferrer">disk-36916.txt</a>`,
	}
	renderedOrder := []int64{36915, 36916}

	got := mergeBitrixAttachmentMarkup(base, renderedByID, renderedOrder)
	want := "text\n" +
		`<img src="/api/static/tickets/t1/bitrix/disk-36915.png" alt="img" />` + "\n" +
		`<a href="/api/static/tickets/t1/bitrix/disk-36916.txt" target="_blank" rel="noreferrer">disk-36916.txt</a>`
	if got != want {
		t.Fatalf("неверная склейка хвостовых вложений:\nwant: %s\ngot:  %s", want, got)
	}
}
