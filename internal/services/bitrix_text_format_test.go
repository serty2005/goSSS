package services

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertEtalonHTMLToBitrix_MentionMapped(t *testing.T) {
	html := `<p>Связаться с <a href="#" class="etalon-user-link" data-etalon-user-id="12" data-etalon-user-name="Ирина">@Ирина</a></p>`
	out := convertEtalonHTMLToBitrix(html, func(etalonUserID uint) (*int64, bool) {
		if etalonUserID != 12 {
			return nil, false
		}
		id := int64(101)
		return &id, true
	})

	require.Contains(t, out, "[USER=101]Ирина[/USER]")
}

func TestConvertEtalonHTMLToBitrix_MentionFallback(t *testing.T) {
	html := `<p>Должна созвониться <a href="#" class="etalon-user-link" data-etalon-user-id="7" data-etalon-user-name="Ирина">@Ирина</a></p>`
	out := convertEtalonHTMLToBitrix(html, func(_ uint) (*int64, bool) {
		return nil, false
	})

	require.Contains(t, out, "[Ирина]")
	require.NotContains(t, out, "[USER=")
}

func TestConvertEtalonHTMLToBitrix_FormatAndLinks(t *testing.T) {
	html := `<p><strong>Важно</strong> <em>срочно</em> <s>устарело</s> <a href="https://example.com">ссылка</a></p><blockquote>цитата</blockquote>`
	out := convertEtalonHTMLToBitrix(html, nil)

	require.Contains(t, out, "[B]Важно[/B]")
	require.Contains(t, out, "[I]срочно[/I]")
	require.Contains(t, out, "[S]устарело[/S]")
	require.Contains(t, out, "[URL=https://example.com]ссылка[/URL]")
	require.Contains(t, out, "[QUOTE]цитата[/QUOTE]")
}
