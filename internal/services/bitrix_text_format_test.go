package services

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertEtalonHTMLToBitrix_MentionMapped(t *testing.T) {
	input := `<p>Связаться с <a href="#" class="etalon-user-link" data-etalon-user-id="12" data-etalon-user-name="Ирина">@Ирина</a></p>`
	out := convertEtalonHTMLToBitrix(input, func(etalonUserID uint) (*int64, bool) {
		if etalonUserID != 12 {
			return nil, false
		}
		id := int64(101)
		return &id, true
	})
	require.Contains(t, out, "[USER=101]Ирина[/USER]")
}

func TestConvertEtalonHTMLToBitrix_MentionFallback(t *testing.T) {
	input := `<p>Нужно связаться с <a href="#" class="etalon-user-link" data-etalon-user-id="7" data-etalon-user-name="Ирина">@Ирина</a></p>`
	out := convertEtalonHTMLToBitrix(input, func(_ uint) (*int64, bool) {
		return nil, false
	})
	require.Contains(t, out, "[Ирина]")
	require.NotContains(t, out, "[USER=")
}

func TestConvertEtalonHTMLToBitrix_FormatAndLinks(t *testing.T) {
	input := `<p><strong>Важно</strong> <em>срочно</em> <s>устарело</s> <a href="https://example.com">ссылка</a></p><blockquote>цитата</blockquote>`
	out := convertEtalonHTMLToBitrix(input, nil)
	require.Contains(t, out, "[B]Важно[/B]")
	require.Contains(t, out, "[I]срочно[/I]")
	require.Contains(t, out, "[S]устарело[/S]")
	require.Contains(t, out, "[URL=https://example.com]ссылка[/URL]")
	require.Contains(t, out, "[QUOTE]цитата[/QUOTE]")
}

func TestNormalizeBitrixCommentForEtalon_ConvertsURLAndKeepsDiskPlaceholder(t *testing.T) {
	in := "Тут [URL=https://example.com]ссылка[/URL]\n[DISK FILE ID=n36911]"
	out, authorID := normalizeBitrixCommentForEtalon(in, nil, 0)
	require.Nil(t, authorID)
	require.Contains(t, out, `<a href="https://example.com" target="_blank" rel="noreferrer">ссылка</a>`)
	require.NotContains(t, out, "[DISK FILE")
	require.NotContains(t, out, "[URL=")
	require.Contains(t, out, bitrixDiskPlaceholder("36911"))
}

func TestConvertBitrixMarkupForEtalon_KeepsDiskPlaceholderPosition(t *testing.T) {
	out := convertBitrixMarkupForEtalon(`prefix [DISK FILE ID=n36915] suffix`)
	require.Contains(t, out, "prefix")
	require.Contains(t, out, "suffix")
	require.Contains(t, out, bitrixDiskPlaceholder("36915"))
}

func TestNormalizeBitrixCommentForEtalon_ExtractsLeadingUserForIntegrationAuthor(t *testing.T) {
	integrationUserID := int64(187)
	in := "[USER=457]Тестовый автор[/USER] сообщение"
	out, authorID := normalizeBitrixCommentForEtalon(in, &integrationUserID, integrationUserID)
	require.NotNil(t, authorID)
	require.Equal(t, int64(457), *authorID)
	require.Equal(t, "сообщение", out)
}

func TestConvertBitrixDescriptionForEtalon_EscapesHTML(t *testing.T) {
	out := convertBitrixDescriptionForEtalon(`<script>alert(1)</script>`)
	require.Contains(t, out, "&lt;script&gt;")
	require.NotContains(t, out, "<script>")
}

func TestConvertEtalonHTMLToBitrix_DoesNotDuplicateSingleLineBreaks(t *testing.T) {
	input := `<p>Строка 1</p><p>Строка 2</p>`
	out := convertEtalonHTMLToBitrix(input, nil)
	require.Equal(t, "Строка 1\nСтрока 2", out)
}

func TestConvertBitrixMarkupForEtalon_DoesNotAddExtraNewlineAfterBr(t *testing.T) {
	input := "Первая строка\nВторая строка"
	out := convertBitrixMarkupForEtalon(input)
	require.Equal(t, "Первая строка<br />Вторая строка", out)
}

func TestConvertBitrixMarkupForEtalon_UnescapesHtmlEntities(t *testing.T) {
	input := "Aleksandr Naiden, &#91;21.02.2026 12:07&#93;\nОстальные принтеры также без ответа."
	out := convertBitrixMarkupForEtalon(input)
	require.Contains(t, out, "Aleksandr Naiden, [21.02.2026 12:07]")
	require.NotContains(t, out, "&#91;")
	require.NotContains(t, out, "&#93;")
}
