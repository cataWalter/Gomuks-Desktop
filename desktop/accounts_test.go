package desktop

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestAccountRegistryPersistsAccountsAndActiveAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")
	registry, err := newAccountRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	first := AccountSummary{ID: "primary", Profile: AccountProfile{UserID: "@one:example.org", Homeserver: "https://example.org"}}
	second := AccountSummary{ID: "account-two", Profile: AccountProfile{UserID: "@two:example.net", Homeserver: "https://example.net"}}
	if err = registry.Upsert(first); err != nil {
		t.Fatal(err)
	}
	if err = registry.Upsert(second); err != nil {
		t.Fatal(err)
	}
	if err = registry.SetActive(second.ID); err != nil {
		t.Fatal(err)
	}

	reloaded, err := newAccountRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.All(); !reflect.DeepEqual(got, []AccountSummary{first, second}) {
		t.Fatalf("accounts = %#v, want %#v", got, []AccountSummary{first, second})
	}
	if got := reloaded.ActiveID(); got != second.ID {
		t.Fatalf("active account = %q, want %q", got, second.ID)
	}
}

func TestAccountRegistryRejectsUnknownActiveAccount(t *testing.T) {
	registry, err := newAccountRegistry(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.SetActive("unknown"); err == nil {
		t.Fatal("SetActive accepted an unknown account")
	}
}