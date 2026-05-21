package dialect

import "testing"

func TestDialectString(t *testing.T) {
	if PostgreSQL.String() != "postgresql" {
		t.Errorf("PostgreSQL.String() = %q", PostgreSQL.String())
	}
	if MySQL.String() != "mysql" {
		t.Errorf("MySQL.String() = %q", MySQL.String())
	}
}

func TestRegisterAndGet(t *testing.T) {
	Register(&Registration{Name: "testdb", Description: "test"})
	r, err := Get("testdb")
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "testdb" {
		t.Errorf("name = %q, want testdb", r.Name)
	}
}

func TestGetUnsupported(t *testing.T) {
	_, err := Get("oracle")
	if err == nil {
		t.Error("expected error for unsupported dialect")
	}
}

func TestValid(t *testing.T) {
	Register(&Registration{Name: "validdb"})
	if !Dialect("validdb").Valid() {
		t.Error("validdb should be valid after registration")
	}
	if Dialect("nope").Valid() {
		t.Error("nope should not be valid")
	}
}

func TestSupported(t *testing.T) {
	Register(&Registration{Name: "sup1"})
	s := Supported()
	if len(s) == 0 {
		t.Error("Supported() should not be empty after registration")
	}
}

func TestWeightDefault(t *testing.T) {
	// With no WeightFunc set, should return 0
	old := WeightFunc
	WeightFunc = nil
	defer func() { WeightFunc = old }()

	if w := Weight("anything"); w != 0 {
		t.Errorf("Weight with nil func = %d, want 0", w)
	}
}

func TestWeightWithFunc(t *testing.T) {
	old := WeightFunc
	defer func() { WeightFunc = old }()

	WeightFunc = func(rule string) int {
		if rule == "test-rule" {
			return 42
		}
		return 0
	}

	if w := Weight("test-rule"); w != 42 {
		t.Errorf("Weight(test-rule) = %d, want 42", w)
	}
	if w := Weight("other"); w != 0 {
		t.Errorf("Weight(other) = %d, want 0", w)
	}
}
