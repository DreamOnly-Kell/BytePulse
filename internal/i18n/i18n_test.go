package i18n

import "testing"

func TestSetLangAndT(t *testing.T) {
	SetLang("en")
	if T("tui.wait.headline") == "" || Lang() != LangEN {
		t.Fatal("en missing")
	}
	SetLang("zh")
	if Lang() != LangZH {
		t.Fatalf("lang=%s", Lang())
	}
	if T("tui.wait.headline") != "后台采集未运行。" {
		t.Fatalf("zh=%q", T("tui.wait.headline"))
	}
	SetLang("unknown")
	if Lang() != LangEN {
		t.Fatal("unknown should fall back to en")
	}
}

func TestTf(t *testing.T) {
	SetLang("en")
	got := Tf("tui.wait.waiting", map[string]string{"seconds": "5"})
	if got != "Waiting for daemon... (5s)" {
		t.Fatalf("got %q", got)
	}
}
