package mysqldb

import "github.com/sam-caldwell/query-test-tool/calibrate"

// MapPgTypesToMySQL converts PostgreSQL column types to MySQL equivalents.
func MapPgTypesToMySQL(d calibrate.Domain) calibrate.Domain {
	out := d
	out.Tables = make([]calibrate.TableDef, len(d.Tables))
	for i, t := range d.Tables {
		out.Tables[i] = t
		out.Tables[i].Columns = make([]calibrate.ColumnDef, len(t.Columns))
		for j, col := range t.Columns {
			out.Tables[i].Columns[j] = col
			out.Tables[i].Columns[j].Type = mapColumnType(col.Type)
		}
	}
	// Indexes and FKs reference column names, not types — no changes needed
	out.Indexes = make([]calibrate.IndexDef, len(d.Indexes))
	copy(out.Indexes, d.Indexes)
	out.ForeignKeys = make([]calibrate.FKDef, len(d.ForeignKeys))
	copy(out.ForeignKeys, d.ForeignKeys)
	return out
}

func mapColumnType(pgType string) string {
	switch pgType {
	case "SERIAL":
		return "INT AUTO_INCREMENT"
	case "BIGSERIAL":
		return "BIGINT AUTO_INCREMENT"
	case "TIMESTAMPTZ", "TIMESTAMP":
		return "DATETIME"
	case "JSONB":
		return "JSON"
	case "BOOLEAN":
		return "TINYINT(1)"
	case "TEXT":
		return "TEXT"
	case "DATE":
		return "DATE"
	case "SMALLINT":
		return "SMALLINT"
	case "INT":
		return "INT"
	case "BIGINT":
		return "BIGINT"
	default:
		// VARCHAR(N), NUMERIC(x,y) → DECIMAL(x,y)
		if len(pgType) >= 7 && pgType[:7] == "NUMERIC" {
			return "DECIMAL" + pgType[7:]
		}
		return pgType
	}
}

// IsAutoIncrement returns true if the MySQL type is AUTO_INCREMENT.
func IsAutoIncrement(mysqlType string) bool {
	return len(mysqlType) > 14 && mysqlType[len(mysqlType)-14:] == "AUTO_INCREMENT"
}
