package calibrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// ImportSchemaFile reads a .SQL DDL file and parses it into a Domain struct.
// It extracts CREATE TABLE, CREATE INDEX, and ALTER TABLE...ADD CONSTRAINT FOREIGN KEY statements.
func ImportSchemaFile(path string) (*Domain, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading schema file: %w", err)
	}
	return ParseDDL(string(data), path)
}

// ParseDDL parses SQL DDL text and returns a Domain.
func ParseDDL(sql string, sourceName string) (*Domain, error) {
	result, err := pg_query.Parse(sql)
	if err != nil {
		return nil, fmt.Errorf("parsing SQL: %w", err)
	}

	domain := &Domain{
		Name:        domainNameFromPath(sourceName),
		Description: fmt.Sprintf("Imported from %s", filepath.Base(sourceName)),
	}

	for _, stmt := range result.Stmts {
		rawStmt := stmt.Stmt
		if rawStmt == nil {
			continue
		}

		switch node := rawStmt.Node.(type) {
		case *pg_query.Node_CreateStmt:
			table, err := parseCreateTable(node.CreateStmt)
			if err != nil {
				return nil, fmt.Errorf("parsing CREATE TABLE: %w", err)
			}
			domain.Tables = append(domain.Tables, *table)

		case *pg_query.Node_IndexStmt:
			idx, err := parseCreateIndex(node.IndexStmt)
			if err != nil {
				return nil, fmt.Errorf("parsing CREATE INDEX: %w", err)
			}
			domain.Indexes = append(domain.Indexes, *idx)

		case *pg_query.Node_AlterTableStmt:
			fks, err := parseAlterTable(node.AlterTableStmt)
			if err != nil {
				return nil, fmt.Errorf("parsing ALTER TABLE: %w", err)
			}
			domain.ForeignKeys = append(domain.ForeignKeys, fks...)
		}
	}

	if len(domain.Tables) == 0 {
		return nil, fmt.Errorf("no CREATE TABLE statements found in %s", sourceName)
	}

	return domain, nil
}

func parseCreateTable(stmt *pg_query.CreateStmt) (*TableDef, error) {
	if stmt.Relation == nil {
		return nil, fmt.Errorf("CREATE TABLE has no relation name")
	}

	table := &TableDef{
		Name: stmt.Relation.Relname,
	}

	for _, elt := range stmt.TableElts {
		switch node := elt.Node.(type) {
		case *pg_query.Node_ColumnDef:
			col, err := parseColumnDef(node.ColumnDef)
			if err != nil {
				return nil, err
			}
			table.Columns = append(table.Columns, *col)
		}
	}

	return table, nil
}

func parseColumnDef(colDef *pg_query.ColumnDef) (*ColumnDef, error) {
	col := &ColumnDef{
		Name: colDef.Colname,
	}

	// Extract type name
	col.Type = extractTypeName(colDef.TypeName)

	// Check for SERIAL/BIGSERIAL
	upperType := strings.ToUpper(col.Type)
	if upperType == "SERIAL" || upperType == "BIGSERIAL" {
		col.IsSerial = true
	}

	// Parse constraints
	for _, constraint := range colDef.Constraints {
		if c, ok := constraint.Node.(*pg_query.Node_Constraint); ok {
			switch c.Constraint.Contype {
			case pg_query.ConstrType_CONSTR_NOTNULL:
				col.NotNull = true
			case pg_query.ConstrType_CONSTR_DEFAULT:
				if c.Constraint.RawExpr != nil {
					col.Default = deparseNode(c.Constraint.RawExpr)
				}
			}
		}
	}

	return col, nil
}

func extractTypeName(typeName *pg_query.TypeName) string {
	if typeName == nil {
		return "TEXT"
	}

	var parts []string
	for _, name := range typeName.Names {
		if str, ok := name.Node.(*pg_query.Node_String_); ok {
			// Skip "pg_catalog" schema prefix
			if str.String_.Sval == "pg_catalog" {
				continue
			}
			parts = append(parts, str.String_.Sval)
		}
	}

	typStr := strings.Join(parts, ".")

	// Map pg_catalog names to common SQL type names
	typStr = mapPgType(typStr)

	// Handle type modifiers (e.g., VARCHAR(255), NUMERIC(10,2))
	if len(typeName.Typmods) > 0 {
		var mods []string
		for _, mod := range typeName.Typmods {
			if intNode, ok := mod.Node.(*pg_query.Node_Integer); ok {
				mods = append(mods, fmt.Sprintf("%d", intNode.Integer.Ival))
			}
		}
		if len(mods) > 0 {
			typStr = fmt.Sprintf("%s(%s)", typStr, strings.Join(mods, ","))
		}
	}

	// Handle array type
	if len(typeName.ArrayBounds) > 0 {
		typStr += "[]"
	}

	return strings.ToUpper(typStr)
}

func mapPgType(t string) string {
	switch strings.ToLower(t) {
	case "int4":
		return "INT"
	case "int8":
		return "BIGINT"
	case "int2":
		return "SMALLINT"
	case "float4":
		return "REAL"
	case "float8":
		return "DOUBLE PRECISION"
	case "bool":
		return "BOOLEAN"
	case "varchar":
		return "VARCHAR"
	case "bpchar":
		return "CHAR"
	case "timestamptz":
		return "TIMESTAMPTZ"
	case "timestamp":
		return "TIMESTAMP"
	case "numeric":
		return "NUMERIC"
	case "text":
		return "TEXT"
	case "serial":
		return "SERIAL"
	case "bigserial":
		return "BIGSERIAL"
	case "date":
		return "DATE"
	case "jsonb":
		return "JSONB"
	case "json":
		return "JSON"
	case "uuid":
		return "UUID"
	default:
		return t
	}
}

func parseCreateIndex(stmt *pg_query.IndexStmt) (*IndexDef, error) {
	idx := &IndexDef{
		Name:   stmt.Idxname,
		Unique: stmt.Unique,
	}

	if stmt.Relation != nil {
		idx.Table = stmt.Relation.Relname
	}

	for _, param := range stmt.IndexParams {
		if elem, ok := param.Node.(*pg_query.Node_IndexElem); ok {
			if elem.IndexElem.Name != "" {
				idx.Columns = append(idx.Columns, elem.IndexElem.Name)
			} else if elem.IndexElem.Expr != nil {
				// Expression index
				idx.Expression = deparseNode(elem.IndexElem.Expr)
			}
		}
	}

	return idx, nil
}

func parseAlterTable(stmt *pg_query.AlterTableStmt) ([]FKDef, error) {
	if stmt.Relation == nil {
		return nil, nil
	}

	tableName := stmt.Relation.Relname
	var fks []FKDef

	for _, cmd := range stmt.Cmds {
		atCmd, ok := cmd.Node.(*pg_query.Node_AlterTableCmd)
		if !ok {
			continue
		}

		if atCmd.AlterTableCmd.Subtype != pg_query.AlterTableType_AT_AddConstraint {
			continue
		}

		def := atCmd.AlterTableCmd.Def
		if def == nil {
			continue
		}

		constraint, ok := def.Node.(*pg_query.Node_Constraint)
		if !ok {
			continue
		}

		if constraint.Constraint.Contype != pg_query.ConstrType_CONSTR_FOREIGN {
			continue
		}

		fk := FKDef{
			Name:  constraint.Constraint.Conname,
			Table: tableName,
		}

		// Extract local columns
		if len(constraint.Constraint.FkAttrs) > 0 {
			if str, ok := constraint.Constraint.FkAttrs[0].Node.(*pg_query.Node_String_); ok {
				fk.Column = str.String_.Sval
			}
		}

		// Extract referenced table
		if constraint.Constraint.Pktable != nil {
			fk.RefTable = constraint.Constraint.Pktable.Relname
		}

		// Extract referenced columns
		if len(constraint.Constraint.PkAttrs) > 0 {
			if str, ok := constraint.Constraint.PkAttrs[0].Node.(*pg_query.Node_String_); ok {
				fk.RefColumn = str.String_.Sval
			}
		}

		fks = append(fks, fk)
	}

	return fks, nil
}

// deparseNode converts a pg_query Node back to SQL text by wrapping it in a
// SELECT statement and extracting the expression portion.
func deparseNode(node *pg_query.Node) string {
	// Build a synthetic "SELECT <expr>" statement to deparse the node
	selectStmt := &pg_query.SelectStmt{
		TargetList: []*pg_query.Node{
			{Node: &pg_query.Node_ResTarget{ResTarget: &pg_query.ResTarget{Val: node}}},
		},
	}
	tree := &pg_query.ParseResult{
		Stmts: []*pg_query.RawStmt{
			{Stmt: &pg_query.Node{Node: &pg_query.Node_SelectStmt{SelectStmt: selectStmt}}},
		},
	}
	result, err := pg_query.Deparse(tree)
	if err != nil {
		return ""
	}
	// Strip the "SELECT " prefix
	if len(result) > 7 {
		return result[7:]
	}
	return result
}

func domainNameFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	// Sanitize: replace non-alphanumeric with underscore
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, name)
	if name == "" {
		name = "imported"
	}
	return strings.ToLower(name)
}
