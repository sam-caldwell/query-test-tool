package calibrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDDL_SimpleCreateTable(t *testing.T) {
	sql := `
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    name VARCHAR(100) NOT NULL,
    bio TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`
	domain, err := ParseDDL(sql, "test.sql")
	if err != nil {
		t.Fatalf("ParseDDL failed: %v", err)
	}

	if len(domain.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(domain.Tables))
	}

	table := domain.Tables[0]
	if table.Name != "users" {
		t.Errorf("expected table name 'users', got %q", table.Name)
	}

	if len(table.Columns) != 5 {
		t.Fatalf("expected 5 columns, got %d", len(table.Columns))
	}

	// Check id column
	col := table.Columns[0]
	if col.Name != "id" {
		t.Errorf("expected column name 'id', got %q", col.Name)
	}

	// Check email column
	col = table.Columns[1]
	if col.Name != "email" {
		t.Errorf("expected column name 'email', got %q", col.Name)
	}
	if !col.NotNull {
		t.Error("expected email to be NOT NULL")
	}

	// Check bio column (nullable)
	col = table.Columns[3]
	if col.Name != "bio" {
		t.Errorf("expected column name 'bio', got %q", col.Name)
	}
	if col.NotNull {
		t.Error("expected bio to be nullable")
	}
}

func TestParseDDL_CreateIndex(t *testing.T) {
	sql := `
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL
);

CREATE UNIQUE INDEX idx_users_email ON users (email);
CREATE INDEX idx_users_status ON users (status);
`
	domain, err := ParseDDL(sql, "test.sql")
	if err != nil {
		t.Fatalf("ParseDDL failed: %v", err)
	}

	if len(domain.Indexes) != 2 {
		t.Fatalf("expected 2 indexes, got %d", len(domain.Indexes))
	}

	// Unique index
	idx := domain.Indexes[0]
	if idx.Name != "idx_users_email" {
		t.Errorf("expected index name 'idx_users_email', got %q", idx.Name)
	}
	if idx.Table != "users" {
		t.Errorf("expected table 'users', got %q", idx.Table)
	}
	if !idx.Unique {
		t.Error("expected idx_users_email to be unique")
	}
	if len(idx.Columns) != 1 || idx.Columns[0] != "email" {
		t.Errorf("expected columns [email], got %v", idx.Columns)
	}

	// Non-unique index
	idx = domain.Indexes[1]
	if idx.Name != "idx_users_status" {
		t.Errorf("expected index name 'idx_users_status', got %q", idx.Name)
	}
	if idx.Unique {
		t.Error("expected idx_users_status to not be unique")
	}
}

func TestParseDDL_AlterTableForeignKey(t *testing.T) {
	sql := `
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL
);

CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    user_id INT NOT NULL,
    total NUMERIC(10,2) NOT NULL
);

ALTER TABLE orders ADD CONSTRAINT fk_orders_user
    FOREIGN KEY (user_id) REFERENCES users (id);
`
	domain, err := ParseDDL(sql, "test.sql")
	if err != nil {
		t.Fatalf("ParseDDL failed: %v", err)
	}

	if len(domain.ForeignKeys) != 1 {
		t.Fatalf("expected 1 foreign key, got %d", len(domain.ForeignKeys))
	}

	fk := domain.ForeignKeys[0]
	if fk.Name != "fk_orders_user" {
		t.Errorf("expected FK name 'fk_orders_user', got %q", fk.Name)
	}
	if fk.Table != "orders" {
		t.Errorf("expected FK table 'orders', got %q", fk.Table)
	}
	if fk.Column != "user_id" {
		t.Errorf("expected FK column 'user_id', got %q", fk.Column)
	}
	if fk.RefTable != "users" {
		t.Errorf("expected FK ref table 'users', got %q", fk.RefTable)
	}
	if fk.RefColumn != "id" {
		t.Errorf("expected FK ref column 'id', got %q", fk.RefColumn)
	}
}

func TestParseDDL_MultiStatement(t *testing.T) {
	sql := `
CREATE TABLE categories (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    parent_id INT
);

CREATE TABLE products (
    id SERIAL PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    category_id INT NOT NULL,
    price NUMERIC(10,2) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE order_items (
    id SERIAL PRIMARY KEY,
    product_id INT NOT NULL,
    quantity INT NOT NULL,
    unit_price NUMERIC(10,2) NOT NULL
);

CREATE UNIQUE INDEX idx_categories_name ON categories (name);
CREATE INDEX idx_products_category ON products (category_id);
CREATE INDEX idx_products_price ON products (price);
CREATE INDEX idx_order_items_product ON order_items (product_id);

ALTER TABLE products ADD CONSTRAINT fk_products_category
    FOREIGN KEY (category_id) REFERENCES categories (id);
ALTER TABLE order_items ADD CONSTRAINT fk_order_items_product
    FOREIGN KEY (product_id) REFERENCES products (id);
`
	domain, err := ParseDDL(sql, "ecommerce.sql")
	if err != nil {
		t.Fatalf("ParseDDL failed: %v", err)
	}

	if domain.Name != "ecommerce" {
		t.Errorf("expected domain name 'ecommerce', got %q", domain.Name)
	}

	if len(domain.Tables) != 3 {
		t.Errorf("expected 3 tables, got %d", len(domain.Tables))
	}

	if len(domain.Indexes) != 4 {
		t.Errorf("expected 4 indexes, got %d", len(domain.Indexes))
	}

	if len(domain.ForeignKeys) != 2 {
		t.Errorf("expected 2 foreign keys, got %d", len(domain.ForeignKeys))
	}

	// Verify tables by name
	tableNames := make(map[string]bool)
	for _, tbl := range domain.Tables {
		tableNames[tbl.Name] = true
	}
	for _, expected := range []string{"categories", "products", "order_items"} {
		if !tableNames[expected] {
			t.Errorf("expected table %q not found", expected)
		}
	}
}

func TestParseDDL_ErrorCases(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{
			name: "invalid SQL",
			sql:  "NOT VALID SQL AT ALL @@@ !!!",
		},
		{
			name: "no CREATE TABLE",
			sql:  "CREATE INDEX idx_foo ON bar (baz);",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDDL(tc.sql, "test.sql")
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestParseDDL_CompositeIndex(t *testing.T) {
	sql := `
CREATE TABLE stock_levels (
    id SERIAL PRIMARY KEY,
    warehouse_id INT NOT NULL,
    item_id INT NOT NULL,
    quantity INT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX idx_stock_wh_item ON stock_levels (warehouse_id, item_id);
`
	domain, err := ParseDDL(sql, "test.sql")
	if err != nil {
		t.Fatalf("ParseDDL failed: %v", err)
	}

	if len(domain.Indexes) != 1 {
		t.Fatalf("expected 1 index, got %d", len(domain.Indexes))
	}

	idx := domain.Indexes[0]
	if len(idx.Columns) != 2 {
		t.Fatalf("expected 2 columns in index, got %d", len(idx.Columns))
	}
	if idx.Columns[0] != "warehouse_id" || idx.Columns[1] != "item_id" {
		t.Errorf("expected columns [warehouse_id, item_id], got %v", idx.Columns)
	}
	if !idx.Unique {
		t.Error("expected composite index to be unique")
	}
}

func TestImportSchemaFile(t *testing.T) {
	// Create a temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "myschema.sql")

	sql := `
CREATE TABLE accounts (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    name VARCHAR(100)
);

CREATE UNIQUE INDEX idx_accounts_email ON accounts (email);
`
	if err := os.WriteFile(path, []byte(sql), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	domain, err := ImportSchemaFile(path)
	if err != nil {
		t.Fatalf("ImportSchemaFile failed: %v", err)
	}

	if domain.Name != "myschema" {
		t.Errorf("expected domain name 'myschema', got %q", domain.Name)
	}
	if len(domain.Tables) != 1 {
		t.Errorf("expected 1 table, got %d", len(domain.Tables))
	}
	if len(domain.Indexes) != 1 {
		t.Errorf("expected 1 index, got %d", len(domain.Indexes))
	}
}

func TestImportSchemaFile_NotFound(t *testing.T) {
	_, err := ImportSchemaFile("/nonexistent/path/schema.sql")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestDomainNameFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/tmp/ecommerce.sql", "ecommerce"},
		{"/path/to/my-schema.sql", "my_schema"},
		{"schema.SQL", "schema"},
		{"/foo/bar/123_test.sql", "123_test"},
	}

	for _, tc := range tests {
		got := domainNameFromPath(tc.path)
		if got != tc.expected {
			t.Errorf("domainNameFromPath(%q) = %q, want %q", tc.path, got, tc.expected)
		}
	}
}
