package services

import "testing"

func TestBuildPyrusCommentTextPlainParagraphsFallbackToText(t *testing.T) {
	plainText, formattedText := buildPyrusCommentText("<p>Первая строка</p><p>Вторая строка</p>")
	if plainText != "Первая строка\n\nВторая строка" {
		t.Fatalf("ожидали plain text с переносом абзацев, получили %q", plainText)
	}
	if formattedText != "" {
		t.Fatalf("не ожидали formatted_text для простых абзацев, получили %q", formattedText)
	}
}

func TestBuildPyrusCommentTextKeepsSupportedFormatting(t *testing.T) {
	raw := `<p>Привет, <strong>мир</strong>.<br/>` +
		`<blockquote>Цитата</blockquote>` +
		`<a href="https://example.com/docs">Документация</a></p>`

	plainText, formattedText := buildPyrusCommentText(raw)
	if plainText != "Привет, мир.\nЦитатаДокументация" {
		t.Fatalf("ожидали нормализованный plain text, получили %q", plainText)
	}
	expected := `Привет, <b>мир</b>.<br/><q>Цитата</q><a href="https://example.com/docs">Документация</a>`
	if formattedText != expected {
		t.Fatalf("ожидали formatted_text %q, получили %q", expected, formattedText)
	}
}

func TestBuildPyrusCommentTextStripsUnsupportedEditorMarkup(t *testing.T) {
	raw := `<p><a href="#" class="etalon-user-link" data-etalon-user-name="@Иван">@Иван</a> ` +
		`<img src="/api/static/tickets/test.png" alt="img" />` +
		`<a href="/api/static/tickets/file.txt">file.txt</a></p>`

	plainText, formattedText := buildPyrusCommentText(raw)
	if plainText != "Иван file.txt" {
		t.Fatalf("ожидали plain text без внутреннего HTML, получили %q", plainText)
	}
	if formattedText != "" {
		t.Fatalf("не ожидали formatted_text для неподдерживаемых внутренних ссылок, получили %q", formattedText)
	}
}
