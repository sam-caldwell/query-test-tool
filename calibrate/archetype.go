package calibrate

// Archetypes defines the 5 domain archetypes used for schema generation.
// Each represents a realistic business domain with proper normalization,
// indexes, and constraints.
func Archetypes() []Domain {
	return []Domain{
		ecommerceDomain(),
		blogDomain(),
		hrDomain(),
		inventoryDomain(),
		analyticsDomain(),
	}
}

func ecommerceDomain() Domain {
	return Domain{
		Name:        "ecommerce",
		Description: "E-commerce platform with users, products, orders",
		Tables: []TableDef{
			{
				Name: "users",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "email", Type: "VARCHAR(255)", NotNull: true},
					{Name: "name", Type: "VARCHAR(100)", NotNull: true},
					{Name: "status", Type: "VARCHAR(20)", NotNull: true, Default: "'active'"},
					{Name: "created_at", Type: "TIMESTAMPTZ", NotNull: true, Default: "now()"},
					{Name: "updated_at", Type: "TIMESTAMPTZ"},
				},
			},
			{
				Name: "categories",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "name", Type: "VARCHAR(100)", NotNull: true},
					{Name: "parent_id", Type: "INT"},
				},
			},
			{
				Name: "products",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "name", Type: "VARCHAR(200)", NotNull: true},
					{Name: "category_id", Type: "INT", NotNull: true},
					{Name: "price", Type: "NUMERIC(10,2)", NotNull: true},
					{Name: "stock_quantity", Type: "INT", NotNull: true, Default: "0"},
					{Name: "created_at", Type: "TIMESTAMPTZ", NotNull: true, Default: "now()"},
				},
			},
			{
				Name: "orders",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "user_id", Type: "INT", NotNull: true},
					{Name: "status", Type: "VARCHAR(20)", NotNull: true, Default: "'pending'"},
					{Name: "total_amount", Type: "NUMERIC(12,2)", NotNull: true},
					{Name: "created_at", Type: "TIMESTAMPTZ", NotNull: true, Default: "now()"},
				},
			},
			{
				Name: "order_items",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "order_id", Type: "INT", NotNull: true},
					{Name: "product_id", Type: "INT", NotNull: true},
					{Name: "quantity", Type: "INT", NotNull: true},
					{Name: "unit_price", Type: "NUMERIC(10,2)", NotNull: true},
				},
			},
		},
		Indexes: []IndexDef{
			{Name: "idx_users_email", Table: "users", Columns: []string{"email"}, Unique: true},
			{Name: "idx_users_status", Table: "users", Columns: []string{"status"}},
			{Name: "idx_users_created", Table: "users", Columns: []string{"created_at"}},
			{Name: "idx_products_category", Table: "products", Columns: []string{"category_id"}},
			{Name: "idx_products_price", Table: "products", Columns: []string{"price"}},
			{Name: "idx_orders_user", Table: "orders", Columns: []string{"user_id"}},
			{Name: "idx_orders_status", Table: "orders", Columns: []string{"status"}},
			{Name: "idx_orders_created", Table: "orders", Columns: []string{"created_at"}},
			{Name: "idx_order_items_order", Table: "order_items", Columns: []string{"order_id"}},
			{Name: "idx_order_items_product", Table: "order_items", Columns: []string{"product_id"}},
		},
		ForeignKeys: []FKDef{
			{Name: "fk_products_category", Table: "products", Column: "category_id", RefTable: "categories", RefColumn: "id"},
			{Name: "fk_orders_user", Table: "orders", Column: "user_id", RefTable: "users", RefColumn: "id"},
			{Name: "fk_order_items_order", Table: "order_items", Column: "order_id", RefTable: "orders", RefColumn: "id"},
			{Name: "fk_order_items_product", Table: "order_items", Column: "product_id", RefTable: "products", RefColumn: "id"},
		},
	}
}

func blogDomain() Domain {
	return Domain{
		Name:        "blog",
		Description: "Blog platform with posts, comments, and tags",
		Tables: []TableDef{
			{
				Name: "authors",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "username", Type: "VARCHAR(50)", NotNull: true},
					{Name: "email", Type: "VARCHAR(255)", NotNull: true},
					{Name: "bio", Type: "TEXT"},
					{Name: "created_at", Type: "TIMESTAMPTZ", NotNull: true, Default: "now()"},
				},
			},
			{
				Name: "posts",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "author_id", Type: "INT", NotNull: true},
					{Name: "title", Type: "VARCHAR(300)", NotNull: true},
					{Name: "body", Type: "TEXT", NotNull: true},
					{Name: "status", Type: "VARCHAR(20)", NotNull: true, Default: "'draft'"},
					{Name: "published_at", Type: "TIMESTAMPTZ"},
					{Name: "created_at", Type: "TIMESTAMPTZ", NotNull: true, Default: "now()"},
				},
			},
			{
				Name: "comments",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "post_id", Type: "INT", NotNull: true},
					{Name: "author_id", Type: "INT"},
					{Name: "body", Type: "TEXT", NotNull: true},
					{Name: "created_at", Type: "TIMESTAMPTZ", NotNull: true, Default: "now()"},
				},
			},
			{
				Name: "tags",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "name", Type: "VARCHAR(50)", NotNull: true},
					{Name: "slug", Type: "VARCHAR(50)", NotNull: true},
				},
			},
			{
				Name: "post_tags",
				Columns: []ColumnDef{
					{Name: "post_id", Type: "INT", NotNull: true},
					{Name: "tag_id", Type: "INT", NotNull: true},
				},
			},
		},
		Indexes: []IndexDef{
			{Name: "idx_authors_username", Table: "authors", Columns: []string{"username"}, Unique: true},
			{Name: "idx_authors_email", Table: "authors", Columns: []string{"email"}, Unique: true},
			{Name: "idx_posts_author", Table: "posts", Columns: []string{"author_id"}},
			{Name: "idx_posts_status", Table: "posts", Columns: []string{"status"}},
			{Name: "idx_posts_published", Table: "posts", Columns: []string{"published_at"}},
			{Name: "idx_comments_post", Table: "comments", Columns: []string{"post_id"}},
			{Name: "idx_comments_author", Table: "comments", Columns: []string{"author_id"}},
			{Name: "idx_tags_slug", Table: "tags", Columns: []string{"slug"}, Unique: true},
			{Name: "idx_post_tags_post", Table: "post_tags", Columns: []string{"post_id"}},
			{Name: "idx_post_tags_tag", Table: "post_tags", Columns: []string{"tag_id"}},
		},
		ForeignKeys: []FKDef{
			{Name: "fk_posts_author", Table: "posts", Column: "author_id", RefTable: "authors", RefColumn: "id"},
			{Name: "fk_comments_post", Table: "comments", Column: "post_id", RefTable: "posts", RefColumn: "id"},
			{Name: "fk_comments_author", Table: "comments", Column: "author_id", RefTable: "authors", RefColumn: "id"},
			{Name: "fk_post_tags_post", Table: "post_tags", Column: "post_id", RefTable: "posts", RefColumn: "id"},
			{Name: "fk_post_tags_tag", Table: "post_tags", Column: "tag_id", RefTable: "tags", RefColumn: "id"},
		},
	}
}

func hrDomain() Domain {
	return Domain{
		Name:        "hr",
		Description: "Human resources with employees, departments, salaries",
		Tables: []TableDef{
			{
				Name: "departments",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "name", Type: "VARCHAR(100)", NotNull: true},
					{Name: "budget", Type: "NUMERIC(14,2)"},
					{Name: "manager_id", Type: "INT"},
				},
			},
			{
				Name: "employees",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "department_id", Type: "INT", NotNull: true},
					{Name: "name", Type: "VARCHAR(100)", NotNull: true},
					{Name: "email", Type: "VARCHAR(255)", NotNull: true},
					{Name: "hire_date", Type: "DATE", NotNull: true},
					{Name: "salary", Type: "NUMERIC(10,2)", NotNull: true},
					{Name: "is_active", Type: "BOOLEAN", NotNull: true, Default: "true"},
				},
			},
			{
				Name: "salaries",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "employee_id", Type: "INT", NotNull: true},
					{Name: "amount", Type: "NUMERIC(10,2)", NotNull: true},
					{Name: "effective_date", Type: "DATE", NotNull: true},
					{Name: "end_date", Type: "DATE"},
				},
			},
			{
				Name: "projects",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "name", Type: "VARCHAR(200)", NotNull: true},
					{Name: "department_id", Type: "INT", NotNull: true},
					{Name: "start_date", Type: "DATE", NotNull: true},
					{Name: "end_date", Type: "DATE"},
					{Name: "status", Type: "VARCHAR(20)", NotNull: true, Default: "'active'"},
				},
			},
			{
				Name: "assignments",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "employee_id", Type: "INT", NotNull: true},
					{Name: "project_id", Type: "INT", NotNull: true},
					{Name: "role", Type: "VARCHAR(50)", NotNull: true},
					{Name: "hours_per_week", Type: "INT", NotNull: true, Default: "40"},
				},
			},
		},
		Indexes: []IndexDef{
			{Name: "idx_employees_dept", Table: "employees", Columns: []string{"department_id"}},
			{Name: "idx_employees_email", Table: "employees", Columns: []string{"email"}, Unique: true},
			{Name: "idx_employees_hire", Table: "employees", Columns: []string{"hire_date"}},
			{Name: "idx_employees_salary", Table: "employees", Columns: []string{"salary"}},
			{Name: "idx_salaries_emp", Table: "salaries", Columns: []string{"employee_id"}},
			{Name: "idx_salaries_date", Table: "salaries", Columns: []string{"effective_date"}},
			{Name: "idx_projects_dept", Table: "projects", Columns: []string{"department_id"}},
			{Name: "idx_projects_status", Table: "projects", Columns: []string{"status"}},
			{Name: "idx_assignments_emp", Table: "assignments", Columns: []string{"employee_id"}},
			{Name: "idx_assignments_project", Table: "assignments", Columns: []string{"project_id"}},
		},
		ForeignKeys: []FKDef{
			{Name: "fk_employees_dept", Table: "employees", Column: "department_id", RefTable: "departments", RefColumn: "id"},
			{Name: "fk_salaries_emp", Table: "salaries", Column: "employee_id", RefTable: "employees", RefColumn: "id"},
			{Name: "fk_projects_dept", Table: "projects", Column: "department_id", RefTable: "departments", RefColumn: "id"},
			{Name: "fk_assignments_emp", Table: "assignments", Column: "employee_id", RefTable: "employees", RefColumn: "id"},
			{Name: "fk_assignments_project", Table: "assignments", Column: "project_id", RefTable: "projects", RefColumn: "id"},
		},
	}
}

func inventoryDomain() Domain {
	return Domain{
		Name:        "inventory",
		Description: "Inventory management with warehouses, items, stock levels",
		Tables: []TableDef{
			{
				Name: "warehouses",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "name", Type: "VARCHAR(100)", NotNull: true},
					{Name: "location", Type: "VARCHAR(200)", NotNull: true},
					{Name: "capacity", Type: "INT", NotNull: true},
				},
			},
			{
				Name: "suppliers",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "name", Type: "VARCHAR(200)", NotNull: true},
					{Name: "contact_email", Type: "VARCHAR(255)"},
					{Name: "country", Type: "VARCHAR(50)", NotNull: true},
					{Name: "rating", Type: "NUMERIC(3,2)"},
				},
			},
			{
				Name: "items",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "sku", Type: "VARCHAR(50)", NotNull: true},
					{Name: "name", Type: "VARCHAR(200)", NotNull: true},
					{Name: "supplier_id", Type: "INT", NotNull: true},
					{Name: "unit_cost", Type: "NUMERIC(10,2)", NotNull: true},
					{Name: "weight_kg", Type: "NUMERIC(8,3)"},
				},
			},
			{
				Name: "stock_levels",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "warehouse_id", Type: "INT", NotNull: true},
					{Name: "item_id", Type: "INT", NotNull: true},
					{Name: "quantity", Type: "INT", NotNull: true, Default: "0"},
					{Name: "last_restocked", Type: "TIMESTAMPTZ"},
				},
			},
			{
				Name: "purchase_orders",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "supplier_id", Type: "INT", NotNull: true},
					{Name: "warehouse_id", Type: "INT", NotNull: true},
					{Name: "item_id", Type: "INT", NotNull: true},
					{Name: "quantity", Type: "INT", NotNull: true},
					{Name: "status", Type: "VARCHAR(20)", NotNull: true, Default: "'pending'"},
					{Name: "ordered_at", Type: "TIMESTAMPTZ", NotNull: true, Default: "now()"},
				},
			},
		},
		Indexes: []IndexDef{
			{Name: "idx_items_sku", Table: "items", Columns: []string{"sku"}, Unique: true},
			{Name: "idx_items_supplier", Table: "items", Columns: []string{"supplier_id"}},
			{Name: "idx_stock_warehouse", Table: "stock_levels", Columns: []string{"warehouse_id"}},
			{Name: "idx_stock_item", Table: "stock_levels", Columns: []string{"item_id"}},
			{Name: "idx_stock_wh_item", Table: "stock_levels", Columns: []string{"warehouse_id", "item_id"}, Unique: true},
			{Name: "idx_po_supplier", Table: "purchase_orders", Columns: []string{"supplier_id"}},
			{Name: "idx_po_warehouse", Table: "purchase_orders", Columns: []string{"warehouse_id"}},
			{Name: "idx_po_item", Table: "purchase_orders", Columns: []string{"item_id"}},
			{Name: "idx_po_status", Table: "purchase_orders", Columns: []string{"status"}},
			{Name: "idx_po_ordered", Table: "purchase_orders", Columns: []string{"ordered_at"}},
		},
		ForeignKeys: []FKDef{
			{Name: "fk_items_supplier", Table: "items", Column: "supplier_id", RefTable: "suppliers", RefColumn: "id"},
			{Name: "fk_stock_warehouse", Table: "stock_levels", Column: "warehouse_id", RefTable: "warehouses", RefColumn: "id"},
			{Name: "fk_stock_item", Table: "stock_levels", Column: "item_id", RefTable: "items", RefColumn: "id"},
			{Name: "fk_po_supplier", Table: "purchase_orders", Column: "supplier_id", RefTable: "suppliers", RefColumn: "id"},
			{Name: "fk_po_warehouse", Table: "purchase_orders", Column: "warehouse_id", RefTable: "warehouses", RefColumn: "id"},
			{Name: "fk_po_item", Table: "purchase_orders", Column: "item_id", RefTable: "items", RefColumn: "id"},
		},
	}
}

func analyticsDomain() Domain {
	return Domain{
		Name:        "analytics",
		Description: "Web analytics with events, sessions, and conversions",
		Tables: []TableDef{
			{
				Name: "users",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "external_id", Type: "VARCHAR(100)", NotNull: true},
					{Name: "first_seen", Type: "TIMESTAMPTZ", NotNull: true, Default: "now()"},
					{Name: "last_seen", Type: "TIMESTAMPTZ"},
					{Name: "country", Type: "VARCHAR(2)"},
					{Name: "device_type", Type: "VARCHAR(20)"},
				},
			},
			{
				Name: "sessions",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "user_id", Type: "INT", NotNull: true},
					{Name: "started_at", Type: "TIMESTAMPTZ", NotNull: true},
					{Name: "ended_at", Type: "TIMESTAMPTZ"},
					{Name: "page_views", Type: "INT", NotNull: true, Default: "0"},
					{Name: "utm_source", Type: "VARCHAR(100)"},
				},
			},
			{
				Name: "events",
				Columns: []ColumnDef{
					{Name: "id", Type: "BIGSERIAL", IsSerial: true},
					{Name: "session_id", Type: "INT", NotNull: true},
					{Name: "event_type", Type: "VARCHAR(50)", NotNull: true},
					{Name: "page_url", Type: "VARCHAR(500)"},
					{Name: "created_at", Type: "TIMESTAMPTZ", NotNull: true, Default: "now()"},
					{Name: "properties", Type: "JSONB"},
				},
			},
			{
				Name: "pages",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "url", Type: "VARCHAR(500)", NotNull: true},
					{Name: "title", Type: "VARCHAR(300)"},
					{Name: "section", Type: "VARCHAR(50)"},
				},
			},
			{
				Name: "conversions",
				Columns: []ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "session_id", Type: "INT", NotNull: true},
					{Name: "event_id", Type: "BIGINT", NotNull: true},
					{Name: "conversion_type", Type: "VARCHAR(50)", NotNull: true},
					{Name: "revenue", Type: "NUMERIC(10,2)"},
					{Name: "created_at", Type: "TIMESTAMPTZ", NotNull: true, Default: "now()"},
				},
			},
		},
		Indexes: []IndexDef{
			{Name: "idx_users_external", Table: "users", Columns: []string{"external_id"}, Unique: true},
			{Name: "idx_users_country", Table: "users", Columns: []string{"country"}},
			{Name: "idx_sessions_user", Table: "sessions", Columns: []string{"user_id"}},
			{Name: "idx_sessions_started", Table: "sessions", Columns: []string{"started_at"}},
			{Name: "idx_events_session", Table: "events", Columns: []string{"session_id"}},
			{Name: "idx_events_type", Table: "events", Columns: []string{"event_type"}},
			{Name: "idx_events_created", Table: "events", Columns: []string{"created_at"}},
			{Name: "idx_pages_url", Table: "pages", Columns: []string{"url"}, Unique: true},
			{Name: "idx_pages_section", Table: "pages", Columns: []string{"section"}},
			{Name: "idx_conversions_session", Table: "conversions", Columns: []string{"session_id"}},
			{Name: "idx_conversions_type", Table: "conversions", Columns: []string{"conversion_type"}},
		},
		ForeignKeys: []FKDef{
			{Name: "fk_sessions_user", Table: "sessions", Column: "user_id", RefTable: "users", RefColumn: "id"},
			{Name: "fk_events_session", Table: "events", Column: "session_id", RefTable: "sessions", RefColumn: "id"},
			{Name: "fk_conversions_session", Table: "conversions", Column: "session_id", RefTable: "sessions", RefColumn: "id"},
			{Name: "fk_conversions_event", Table: "conversions", Column: "event_id", RefTable: "events", RefColumn: "id"},
		},
	}
}
