package keychain

import "testing"

type memory struct{ values map[string]string }

func (m *memory) Get(s, u string) (string, error) {
	v, ok := m.values[s+u]
	if !ok {
		return "", errMissing{}
	}
	return v, nil
}
func (m *memory) Set(s, u, v string) error { m.values[s+u] = v; return nil }
func (m *memory) Delete(s, u string) error { delete(m.values, s+u); return nil }

type errMissing struct{}

func (errMissing) Error() string { return "missing" }

func TestStoreLoadDelete(t *testing.T) {
	m := &memory{values: map[string]string{}}
	restore := BackendForTest(m)
	defer restore()
	var key [32]byte
	key[0] = 7
	if err := Store("/tmp/one", key); err != nil {
		t.Fatal(err)
	}
	got, err := Load("/tmp/one")
	if err != nil || got != key {
		t.Fatalf("load=%x err=%v", got, err)
	}
	if err := Delete("/tmp/one"); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("/tmp/one"); err == nil {
		t.Fatal("expected missing key")
	}
}
