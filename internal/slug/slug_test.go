package slug

import "testing"

func TestUnicodeSlugifyAndDisambiguation(t *testing.T) {
	cases := map[string]string{"Gödel": "gödel", "Erdős": "erdős", "日本語": "日本語", "中文": "中文", "Αθήνα": "αθήνα", "User: Pegasus": "user:pegasus", "My Page": "my-page"}
	for input, want := range cases {
		if got := Slugify(input); got != want {
			t.Errorf("%q => %q, expected %q", input, got, want)
		}
	}
	if Slugify("☃") == Slugify("☼") {
		t.Fatal("non-letter titles must not all collide")
	}
}

func TestPunctuationAndNFC(t *testing.T) {
	if got := Slugify("C++"); got != "c-plus-plus" {
		t.Fatalf("C++: %s", got)
	}
	if got := Slugify("C#"); got != "c-sharp" {
		t.Fatalf("C#: %s", got)
	}
	if got := Slugify("C"); got != "c" {
		t.Fatalf("C: %s", got)
	}
	if got := Slugify("Gödel"); got != "gödel" {
		t.Fatalf("NFC: %s", got)
	}
	if got := LegacyV22("C++"); got != "c" {
		t.Fatalf("legacy C++: %s", got)
	}
}
