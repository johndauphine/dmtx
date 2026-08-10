package secrets

import (
	"bytes"
	"testing"
)

func TestWithMasterKeyReplacesOnlyKeyLine(t *testing.T) {
	input := []byte("# keep\nencryption:\n  master_key: \"old\"\nfuture:\n  value: yes\n")
	got, err := withMasterKey(input, "new")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("# keep\nencryption:\n  master_key: \"new\"\nfuture:\n  value: yes\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestWithMasterKeyInsertsWithoutChangingUnknownSections(t *testing.T) {
	input := []byte("# comment\nencryption:\n  future_option: retained\nother:\n  x: y\n")
	got, err := withMasterKey(input, "key")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("# comment\nencryption:\n  master_key: \"key\"\n  future_option: retained\nother:\n  x: y\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestWithMasterKeyRejectsUnsafeOrMissingSection(t *testing.T) {
	for _, input := range [][]byte{[]byte("other: true\n"), []byte("encryption:\n")} {
		if _, err := withMasterKey(input, "bad\nkey"); err == nil {
			t.Fatal("unsafe key accepted")
		}
	}
	if _, err := withMasterKey([]byte("other: true\n"), "safe"); err == nil {
		t.Fatal("missing section accepted")
	}
}

func TestWithMasterKeyDoesNotInventOtherSections(t *testing.T) {
	input := []byte("encryption:\n# comment\nunknown:\n  keep: exact\n")
	got, err := withMasterKey(input, "safe")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("unknown:\n  keep: exact\n")) || bytes.Contains(got, []byte("ai:")) {
		t.Fatalf("unexpected edit: %q", got)
	}
}
func TestWithMasterKeyStopsAtSiblingMapping(t *testing.T) {
	input := []byte("# preserved\nencryption:\n  future: keep\nother:\n  master_key: \"not ours\"\n  nested:\n    master_key: \"also not ours\"\n")
	got, err := withMasterKey(input, "safe")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("encryption:\n  master_key: \"safe\"\n  future: keep\n")) {
		t.Fatalf("missing encryption key: %q", got)
	}
	if !bytes.Contains(got, []byte("other:\n  master_key: \"not ours\"\n  nested:\n    master_key: \"also not ours\"\n")) {
		t.Fatalf("sibling changed: %q", got)
	}
}
