package calibrate

import (
	"context"
	"fmt"
	"strings"
)

// DataGenerator populates schemas with test data.
type DataGenerator struct {
	db  *DB
	cfg PipelineConfig
}

// NewDataGenerator creates a new data generator.
func NewDataGenerator(db *DB, cfg PipelineConfig) *DataGenerator {
	return &DataGenerator{db: db, cfg: cfg}
}

// PopulateSchema generates test data for all tables in a schema.
// It uses generate_series and random() for efficient bulk insertion.
func (dg *DataGenerator) PopulateSchema(ctx context.Context, schemaName string, domain Domain) error {
	// Optimize session for bulk inserts:
	// - Disable statement timeout (may be set by prior RunExplain on pooled connections)
	// - Disable synchronous_commit (data is disposable — dropped after EXPLAIN)
	if _, err := dg.db.conn.ExecContext(ctx, "SET statement_timeout = 0; SET synchronous_commit = off"); err != nil {
		return fmt.Errorf("setting bulk insert options: %w", err)
	}

	// Insert data respecting FK ordering (parent tables first)
	ordered := topologicalSort(domain)

	for _, table := range ordered {
		sql := dg.generateInsertSQL(schemaName, table, domain)
		if _, err := dg.db.conn.ExecContext(ctx, sql); err != nil {
			return fmt.Errorf("populating %s.%s: %w", schemaName, table.Name, err)
		}
	}

	// ANALYZE to update statistics
	for _, table := range domain.Tables {
		if _, err := dg.db.conn.ExecContext(ctx, fmt.Sprintf("ANALYZE %s.%s", schemaName, table.Name)); err != nil {
			return fmt.Errorf("analyzing %s.%s: %w", schemaName, table.Name, err)
		}
	}

	return nil
}

// hasCompositeUnique returns true if the table has a multi-column unique index.
func hasCompositeUnique(table TableDef, domain Domain) bool {
	for _, idx := range domain.Indexes {
		if idx.Table == table.Name && idx.Unique && len(idx.Columns) > 1 {
			return true
		}
	}
	return false
}

// tableRowMultiplier determines the row multiplier for a table based on its
// characteristics. High-volume tables (BIGSERIAL PK, event/log patterns, many
// inbound FKs) get more rows to push past memory thresholds where the query
// planner makes meaningfully different decisions.
func tableRowMultiplier(table TableDef, domain Domain) int {
	// Composite unique constraints cap row count — must check first
	if hasCompositeUnique(table, domain) {
		return 1
	}

	// BIGSERIAL PK indicates a high-volume table (readings, events, audit_log, vitals)
	if len(table.Columns) > 0 && table.Columns[0].Type == "BIGSERIAL" {
		return 10
	}

	// Count how many FKs reference this table — heavily-referenced tables
	// are typically high-volume (e.g., encounters with diagnoses, procedures, labs all pointing to it)
	inboundFKs := 0
	for _, fk := range domain.ForeignKeys {
		if fk.RefTable == table.Name {
			inboundFKs++
		}
	}
	if inboundFKs >= 4 {
		return 5 // mid-volume: many child tables reference this
	}

	// Regular child tables (have outbound FKs but not BIGSERIAL)
	if isChildTable(table, domain) {
		return 3
	}

	return 1 // root/parent tables
}

// generateInsertSQL creates an INSERT ... SELECT generate_series statement.
func (dg *DataGenerator) generateInsertSQL(schema string, table TableDef, domain Domain) string {
	baseRows := dg.cfg.RowsPerTable
	multiplier := tableRowMultiplier(table, domain)
	rows := baseRows * multiplier

	var cols []string
	var exprs []string

	for _, col := range table.Columns {
		if col.IsSerial {
			continue // auto-generated
		}
		cols = append(cols, col.Name)
		exprs = append(exprs, dataExpression(col, rows, baseRows, domain, table))
	}

	return fmt.Sprintf(
		"INSERT INTO %s.%s (%s)\nSELECT %s\nFROM generate_series(1, %d) AS i;\n",
		schema, table.Name,
		strings.Join(cols, ", "),
		strings.Join(exprs, ",\n       "),
		rows,
	)
}

// dataExpression generates a SQL expression to produce realistic test data for a column.
// totalRows is the row count for the current table, baseRows is the parent table row count.
func dataExpression(col ColumnDef, totalRows int, baseRows int, domain Domain, table TableDef) string {
	// FK columns: reference parent table IDs (parent tables always have baseRows rows with IDs 1..baseRows)
	for _, fk := range domain.ForeignKeys {
		if fk.Table == table.Name && fk.Column == col.Name {
			return fmt.Sprintf("((i - 1) %% %d) + 1", baseRows)
		}
	}

	// Unique indexed columns must use deterministic values based on i.
	// For VARCHAR columns, use lpad to ensure the value fits within the column width.
	if isUniqueColumn(col.Name, table.Name, domain) {
		switch {
		case strings.HasPrefix(col.Type, "VARCHAR") || col.Type == "TEXT":
			// Extract max length from VARCHAR(N) to build a value that fits
			maxLen := 50 // default for TEXT
			if strings.HasPrefix(col.Type, "VARCHAR(") {
				var n int
				fmt.Sscanf(col.Type, "VARCHAR(%d)", &n)
				if n > 0 {
					maxLen = n
				}
			}
			// Use a short prefix + lpad to guarantee fit
			prefix := col.Name
			if len(prefix) > maxLen/2 {
				prefix = prefix[:maxLen/2]
			}
			padLen := maxLen - len(prefix) - 1 // -1 for underscore
			if padLen < 1 {
				padLen = 1
			}
			return fmt.Sprintf("'%s_' || lpad(i::text, %d, '0')", prefix, padLen)
		default:
			return "i"
		}
	}

	expr := baseValueExpression(col, totalRows)

	// Inject sporadic NULLs for nullable columns (~10% NULL rate).
	// This ensures queries encounter real NULL values for realistic cost estimates.
	if !col.NotNull {
		expr = fmt.Sprintf("CASE WHEN random() < 0.10 THEN NULL ELSE %s END", expr)
	}

	return expr
}

// baseValueExpression generates the core value expression for a column type.
// Uses high-cardinality distributions: zipfian for text, varied ranges for numerics.
func baseValueExpression(col ColumnDef, totalRows int) string {
	switch {
	case strings.Contains(col.Type, "SERIAL"):
		return "i"
	case col.Type == "TEXT" || strings.HasPrefix(col.Type, "VARCHAR"):
		return textExpression(col.Name, totalRows)
	case col.Type == "SMALLINT":
		// SMALLINT max is 32767 — cap the range
		maxVal := totalRows
		if maxVal > 32000 {
			maxVal = 32000
		}
		return fmt.Sprintf("CASE WHEN random() < 0.3 THEN (random() * 10)::int ELSE (random() * %d)::int END", maxVal)
	case col.Type == "INT" || col.Type == "BIGINT":
		// Skewed distribution: mix of low-cardinality hot values and high-cardinality tail
		return fmt.Sprintf("CASE WHEN random() < 0.3 THEN (random() * 10)::int ELSE (random() * %d)::int END", totalRows)
	case strings.HasPrefix(col.Type, "NUMERIC"):
		maxVal := 10000.0
		if len(col.Type) > 8 {
			parts := strings.TrimPrefix(col.Type, "NUMERIC(")
			parts = strings.TrimSuffix(parts, ")")
			fields := strings.Split(parts, ",")
			if len(fields) == 2 {
				var prec, scale int
				fmt.Sscanf(fields[0], "%d", &prec)
				fmt.Sscanf(fields[1], "%d", &scale)
				intDigits := prec - scale
				if intDigits > 0 {
					maxVal = 1.0
					for j := 0; j < intDigits; j++ {
						maxVal *= 10
					}
					// Leave 1% margin to prevent rounding overflow at type boundary
					maxVal *= 0.99
				}
			}
		}
		// Skewed: some values cluster near 0, others spread across range
		return fmt.Sprintf("(power(random(), 2) * %.2f)::%s", maxVal, col.Type)
	case col.Type == "BOOLEAN":
		// Skewed: 70% true, 30% false (realistic for is_active type columns)
		return "(random() > 0.3)"
	case col.Type == "DATE":
		// Cluster recent dates more heavily (exponential decay)
		return "CURRENT_DATE - (power(random(), 2) * 730)::int"
	case col.Type == "TIMESTAMPTZ" || col.Type == "TIMESTAMP":
		return "now() - (power(random(), 2) * interval '730 days')"
	case col.Type == "JSONB":
		return "jsonb_build_object('key', i, 'value', random(), 'tags', ARRAY[(random()*10)::int, (random()*10)::int])"
	default:
		return "'test_value'"
	}
}

// textExpression generates realistic text data based on column name.
// Uses high-cardinality distributions with realistic patterns.
func textExpression(colName string, totalRows int) string {
	switch {
	case strings.Contains(colName, "email"):
		return "'user_' || i || '@example.com'"

	// Accounting-specific patterns (before generic matches)
	case strings.Contains(colName, "ein"):
		return "'0' || lpad(((i * 7 + 13) % 90 + 10)::text, 2, '0') || '-' || lpad(((i * 31 + 7) % 9000000 + 1000000)::text, 7, '0')"
	case strings.Contains(colName, "tax_id"):
		return "'TID-' || lpad(i::text, 9, '0')"
	case strings.Contains(colName, "account_code"):
		// Realistic COA numbering: 1xxx assets, 2xxx liabilities, 3xxx equity, 4xxx revenue, 5xxx expense
		return "lpad(((i % 5 + 1) * 1000 + (i % 200))::text, 4, '0')"
	case strings.Contains(colName, "account_name"):
		return "(ARRAY['Cash','Accounts Receivable','Inventory','Equipment','Accounts Payable','Revenue','Cost of Goods Sold','Rent Expense','Payroll Expense','Depreciation'])[1 + (i % 10)]"
	case strings.Contains(colName, "account_type"):
		return "(ARRAY['asset','liability','equity','revenue','expense'])[1 + (i % 5)]"
	case strings.Contains(colName, "normal_balance"):
		return "CASE WHEN i % 5 < 3 THEN 'debit' ELSE 'credit' END"
	case strings.Contains(colName, "entity_type"):
		return "(ARRAY['sole_prop','llc','s_corp','c_corp','partnership'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "invoice_number"):
		return "'INV-' || lpad(i::text, 8, '0')"
	case strings.Contains(colName, "bill_number"):
		return "'BILL-' || lpad(i::text, 8, '0')"
	case strings.Contains(colName, "check_number"):
		return "'CHK-' || lpad((10000 + i)::text, 8, '0')"
	case strings.Contains(colName, "reference_number"):
		return "'REF-' || lpad(i::text, 8, '0')"
	case strings.Contains(colName, "routing_number"):
		return "lpad(((i * 37 + 11) % 900000000 + 100000000)::text, 9, '0')"
	case strings.Contains(colName, "account_number"):
		return "lpad(((i * 73 + 29) % 90000000 + 10000000)::text, 10, '0')"
	case strings.Contains(colName, "payment_terms"):
		return "(ARRAY['net_15','net_30','net_45','net_60','due_on_receipt'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "institution"):
		return "(ARRAY['Chase','Bank of America','Wells Fargo','Citibank','US Bank','PNC','Capital One','TD Bank'])[1 + (random() * 7)::int]"
	case strings.Contains(colName, "vendor_type"):
		return "(ARRAY['supplier','contractor','consultant','utility','insurance','landlord'])[1 + (random() * 5)::int]"
	case strings.Contains(colName, "entry_type"):
		return "(ARRAY['standard','adjusting','closing','reversing'])[1 + (random() * 3)::int]"
	case strings.Contains(colName, "mrn"):
		return "'MRN-' || lpad(i::text, 8, '0')"
	case strings.Contains(colName, "npi"):
		return "'1' || lpad(((i * 97 + 13) % 999999999)::text, 9, '0')"
	case strings.Contains(colName, "icd_code"):
		return "(ARRAY['J06.9','M54.5','E11.9','I10','J18.9','K21.0','F32.9','G43.909','R10.9','N39.0'])[1 + (i % 10)]"
	case strings.Contains(colName, "cpt_code"):
		return "(ARRAY['99213','99214','99215','99203','99204','36415','80053','85025','71046','93000'])[1 + (i % 10)]"
	case strings.Contains(colName, "ndc_code"):
		return "'0' || lpad(((i * 41 + 7) % 99999)::text, 5, '0') || '-' || lpad((i % 9999)::text, 4, '0') || '-' || lpad((i % 99)::text, 2, '0')"
	case strings.Contains(colName, "drug_class"):
		return "(ARRAY['antibiotic','analgesic','antihypertensive','antidiabetic','antidepressant','statin','proton_pump_inhibitor','bronchodilator'])[1 + (random() * 7)::int]"
	case strings.Contains(colName, "specialty"):
		return "(ARRAY['internal_medicine','cardiology','orthopedics','neurology','oncology','pediatrics','emergency','surgery','radiology','psychiatry'])[1 + (random() * 9)::int]"
	case strings.Contains(colName, "encounter_type"):
		return "(ARRAY['inpatient','outpatient','emergency','observation','telehealth'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "diagnosis_type"):
		return "CASE WHEN random() < 0.3 THEN 'primary' ELSE 'secondary' END"
	case strings.Contains(colName, "blood_type"):
		return "(ARRAY['A+','A-','B+','B-','AB+','AB-','O+','O-'])[1 + (random() * 7)::int]"
	case strings.Contains(colName, "gender"):
		return "(ARRAY['male','female','other'])[1 + (random() * 2)::int]"
	case strings.Contains(colName, "dosage"):
		return "(ARRAY['5mg','10mg','20mg','25mg','50mg','100mg','250mg','500mg'])[1 + (random() * 7)::int]"
	case strings.Contains(colName, "frequency"):
		return "(ARRAY['once_daily','twice_daily','three_daily','four_daily','as_needed','weekly'])[1 + (random() * 5)::int]"
	case strings.Contains(colName, "abnormal_flag"):
		return "CASE WHEN random() < 0.2 THEN (ARRAY['H','L','HH','LL','A'])[1 + (random() * 4)::int] ELSE NULL END"
	case strings.Contains(colName, "reference_range"):
		return "'0-' || (10 + (random() * 90)::int)::text"
	case strings.Contains(colName, "result_unit"):
		return "(ARRAY['mg/dL','mmol/L','g/dL','mEq/L','IU/L','ng/mL','%','cells/uL'])[1 + (random() * 7)::int]"
	case strings.Contains(colName, "claim_number"):
		return "'CLM-' || lpad(i::text, 10, '0')"
	case strings.Contains(colName, "member_id"):
		return "'MBR-' || lpad(i::text, 8, '0')"
	case strings.Contains(colName, "plan_name"):
		return "(ARRAY['Blue Cross PPO','Aetna HMO','United Healthcare','Cigna EPO','Kaiser Permanente','Medicare Part A','Medicaid'])[1 + (random() * 6)::int]"
	case strings.Contains(colName, "plan_type"):
		return "(ARRAY['ppo','hmo','epo','pos','hdhp','medicare','medicaid'])[1 + (random() * 6)::int]"
	case strings.Contains(colName, "group_number"):
		return "'GRP-' || lpad(((i * 17 + 3) % 99999)::text, 5, '0')"
	case strings.Contains(colName, "allergen"):
		return "(ARRAY['Penicillin','Sulfa','Aspirin','Latex','Peanut','Shellfish','Codeine','Iodine','Eggs','Amoxicillin'])[1 + (random() * 9)::int]"
	case strings.Contains(colName, "allergy_type"):
		return "(ARRAY['drug','food','environmental','latex','contrast'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "reaction"):
		return "(ARRAY['rash','anaphylaxis','hives','swelling','nausea','difficulty_breathing','itching'])[1 + (random() * 6)::int]"
	case strings.Contains(colName, "emergency_contact"):
		return "'Emergency Contact ' || i"
	case strings.Contains(colName, "vaccine_name"):
		return "(ARRAY['COVID-19 mRNA','Influenza','Tdap','MMR','Hepatitis B','Pneumococcal','Shingles','HPV'])[1 + (random() * 7)::int]"
	case strings.Contains(colName, "vaccine_code"):
		return "(ARRAY['CVX-208','CVX-197','CVX-115','CVX-94','CVX-45','CVX-33','CVX-187','CVX-62'])[1 + (random() * 7)::int]"
	case strings.Contains(colName, "lot_number"):
		return "'LOT-' || lpad(((i * 61 + 17) % 999999)::text, 6, '0')"
	case strings.Contains(colName, "site") && !strings.Contains(colName, "file"):
		return "(ARRAY['left_deltoid','right_deltoid','left_thigh','right_thigh','left_gluteal'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "referral_reason"):
		return "'Referral for ' || (ARRAY['specialist evaluation','diagnostic imaging','surgical consultation','second opinion','therapy'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "authorization_number"):
		return "'AUTH-' || lpad(i::text, 8, '0')"
	case strings.Contains(colName, "priority"):
		return "(ARRAY['routine','urgent','emergent','elective'])[1 + (random() * 3)::int]"
	case strings.Contains(colName, "goal"):
		return "'Clinical goal: ' || (ARRAY['reduce pain','improve mobility','manage blood glucose','lower blood pressure','weight management'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "interventions"):
		return "'Interventions: ' || (ARRAY['medication adjustment','physical therapy','dietary changes','follow-up labs','specialist referral'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "outcome"):
		return "CASE WHEN random() < 0.6 THEN (ARRAY['improved','stable','resolved'])[1 + (random() * 2)::int] ELSE NULL END"
	case strings.Contains(colName, "condition_operator"):
		return "(ARRAY['>','<','>=','<=','='])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "condition"):
		return "(ARRAY['Hypertension','Type 2 Diabetes','Asthma','COPD','Depression','Anxiety','Osteoarthritis','GERD','Hypothyroidism','Chronic Kidney Disease'])[1 + (random() * 9)::int]"
	case strings.Contains(colName, "history_type"):
		return "(ARRAY['chronic','surgical','family','social','past_acute'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "facility"):
		return "(ARRAY['Main Hospital','East Wing Clinic','Surgery Center','Emergency Dept','Rehab Center','Outpatient Pavilion'])[1 + (random() * 5)::int]"
	case strings.Contains(colName, "license_number"):
		return "'LIC-' || lpad(i::text, 6, '0')"
	case strings.Contains(colName, "visibility"):
		return "(ARRAY['private','team','public','restricted'])[1 + (random() * 3)::int]"
	case strings.Contains(colName, "ip_address"):
		return "'10.' || ((random() * 254 + 1)::int)::text || '.' || ((random() * 254 + 1)::int)::text || '.' || ((random() * 254 + 1)::int)::text"
	case strings.Contains(colName, "password_hash"):
		return "'$2b$12$' || md5(i::text)"
	case strings.Contains(colName, "user_agent"):
		return "(ARRAY['Mozilla/5.0 Chrome/120','Mozilla/5.0 Firefox/121','Mozilla/5.0 Safari/17','curl/8.4'])[1 + (random() * 3)::int]"
	case strings.Contains(colName, "sensor_type"):
		return "(ARRAY['temperature','humidity','pressure','co2','pm25','vibration','flow','voltage'])[1 + (random() * 7)::int]"
	case strings.Contains(colName, "reading_type"):
		return "(ARRAY['temperature','humidity','pressure','co2','pm25','vibration','flow_rate','voltage','current','power'])[1 + (random() * 9)::int]"
	case strings.Contains(colName, "firmware_version"):
		return "'v' || (1 + (random() * 4)::int)::text || '.' || (random() * 20)::int::text || '.' || (random() * 99)::int::text"
	case strings.Contains(colName, "severity"):
		return "(ARRAY['info','warning','critical','emergency'])[1 + (random() * 3)::int]"
	case strings.Contains(colName, "metric_name"):
		return "(ARRAY['avg_temp','max_pressure','min_humidity','peak_co2','rms_vibration'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "maintenance_type"):
		return "(ARRAY['calibration','replacement','firmware_update','cleaning','inspection'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "carrier"):
		return "(ARRAY['FedEx','UPS','DHL','USPS','Maersk','Freight Corp','Rail Express'])[1 + (random() * 6)::int]"
	case strings.Contains(colName, "tracking_number"):
		return "'TRK-' || lpad(i::text, 12, '0')"
	case strings.Contains(colName, "po_number"):
		return "'PO-' || lpad(i::text, 8, '0')"
	case strings.Contains(colName, "product_sku"):
		return "'SKU-' || lpad(((i * 31 + 7) % 99999)::text, 6, '0')"
	case strings.Contains(colName, "case_number"):
		return "'CASE-' || lpad(i::text, 8, '0')"
	case strings.Contains(colName, "case_type"):
		return "(ARRAY['civil','criminal','family','bankruptcy','immigration','corporate','ip','tax'])[1 + (random() * 7)::int]"
	case strings.Contains(colName, "jurisdiction"):
		return "(ARRAY['federal','state_ny','state_ca','state_tx','state_fl','state_il','district_dc'])[1 + (random() * 6)::int]"
	case strings.Contains(colName, "court_name"):
		return "(ARRAY['US District Court SDNY','Superior Court LA','Circuit Court Cook County','District Court Harris County','US Bankruptcy Court DE'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "filing_type"):
		return "(ARRAY['complaint','answer','motion','brief','discovery','subpoena','order','judgment'])[1 + (random() * 7)::int]"
	case strings.Contains(colName, "document_type"):
		return "(ARRAY['brief','motion','exhibit','deposition','contract','correspondence','order','opinion'])[1 + (random() * 7)::int]"
	case strings.Contains(colName, "annotation_type"):
		return "(ARRAY['highlight','comment','redaction','bookmark','strikethrough'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "citation_type"):
		return "(ARRAY['case_law','statute','regulation','secondary','treatise'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "citation_text"):
		return "'Smith v. Jones, ' || (100 + i % 900)::text || ' F.3d ' || (1 + i % 999)::text || ' (' || (2010 + i % 15)::text || ')'"
	case strings.Contains(colName, "party_type"):
		return "(ARRAY['individual','corporation','llc','government','nonprofit'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "mime_type"):
		return "(ARRAY['application/pdf','application/msword','text/plain','image/png','application/zip'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "content_hash"):
		return "md5(i::text || random()::text)"
	case strings.Contains(colName, "file_path"):
		return "'/documents/' || i || '/' || md5(i::text) || '.pdf'"
	case strings.Contains(colName, "change_summary"):
		return "'Revision ' || i || ': updated content'"
	case strings.Contains(colName, "industry"):
		return "(ARRAY['construction','healthcare','retail','manufacturing','technology','professional_services','restaurant','real_estate','transportation','agriculture'])[1 + (random() * 9)::int]"
	case strings.Contains(colName, "phone"):
		return "'(' || lpad(((i * 7 + 200) % 800 + 200)::text, 3, '0') || ') ' || lpad(((i * 13 + 100) % 900 + 100)::text, 3, '0') || '-' || lpad((i % 10000)::text, 4, '0')"
	case strings.Contains(colName, "address"):
		return "(ARRAY['100 Main St','200 Oak Ave','300 Elm Blvd','400 Pine Dr','500 Maple Ln'])[1 + (random() * 4)::int] || ', ' || (ARRAY['New York, NY','Chicago, IL','Houston, TX','Phoenix, AZ','Dallas, TX'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "memo") || strings.Contains(colName, "description"):
		return "'Transaction memo for record ' || i"
	case strings.Contains(colName, "name") || strings.Contains(colName, "title"):
		// High cardinality: unique-ish values with some repeats for realistic join behavior
		return fmt.Sprintf("'name_' || (power(random(), 0.5) * %d)::int", totalRows)
	case strings.Contains(colName, "status"):
		// Skewed: 60% active, 20% completed, 10% pending, 10% cancelled (realistic)
		return "CASE WHEN random() < 0.6 THEN 'active' WHEN random() < 0.8 THEN 'completed' WHEN random() < 0.9 THEN 'pending' ELSE 'cancelled' END"
	case strings.Contains(colName, "url"):
		return "'https://example.com/page/' || i"
	case strings.Contains(colName, "country"):
		return "(ARRAY['US','UK','DE','FR','JP','CA','AU','BR','IN','CN'])[1 + (random() * 9)::int]"
	case strings.Contains(colName, "type"):
		return "(ARRAY['type_a','type_b','type_c','type_d'])[1 + (random() * 3)::int]"
	case strings.Contains(colName, "slug"):
		return "'slug-' || i"
	case strings.Contains(colName, "sku"):
		return "'SKU-' || lpad(i::text, 6, '0')"
	case strings.Contains(colName, "role"):
		return "(ARRAY['engineer','manager','designer','analyst'])[1 + (random() * 3)::int]"
	case strings.Contains(colName, "source"):
		return "(ARRAY['google','direct','email','social','referral'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "bio") || strings.Contains(colName, "body"):
		return "'Lorem ipsum dolor sit amet ' || i"
	case strings.Contains(colName, "location"):
		return "(ARRAY['New York','London','Tokyo','Berlin','Sydney'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "device"):
		return "(ARRAY['mobile','desktop','tablet'])[1 + (random() * 2)::int]"
	case strings.Contains(colName, "section"):
		return "(ARRAY['home','blog','docs','pricing','about'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "external_id"):
		return "'ext_' || lpad(i::text, 8, '0')"
	default:
		return "'value_' || i"
	}
}

// isUniqueColumn returns true if the column has a unique index.
func isUniqueColumn(colName, tableName string, domain Domain) bool {
	for _, idx := range domain.Indexes {
		if idx.Table == tableName && idx.Unique && len(idx.Columns) == 1 && idx.Columns[0] == colName {
			return true
		}
	}
	return false
}

// isChildTable returns true if the table has a FK referencing another table.
func isChildTable(table TableDef, domain Domain) bool {
	for _, fk := range domain.ForeignKeys {
		if fk.Table == table.Name {
			return true
		}
	}
	return false
}

// topologicalSort orders tables so parents come before children.
func topologicalSort(domain Domain) []TableDef {
	// Build dependency graph
	deps := make(map[string][]string) // table -> tables it depends on
	tableMap := make(map[string]TableDef)
	for _, t := range domain.Tables {
		tableMap[t.Name] = t
		deps[t.Name] = nil
	}
	for _, fk := range domain.ForeignKeys {
		if fk.Table != fk.RefTable { // skip self-references
			deps[fk.Table] = append(deps[fk.Table], fk.RefTable)
		}
	}

	// Kahn's algorithm
	var sorted []TableDef
	visited := make(map[string]bool)
	var visit func(name string)
	visiting := make(map[string]bool)

	visit = func(name string) {
		if visited[name] {
			return
		}
		if visiting[name] {
			// Cycle — just add it
			visited[name] = true
			sorted = append(sorted, tableMap[name])
			return
		}
		visiting[name] = true
		for _, dep := range deps[name] {
			visit(dep)
		}
		visiting[name] = false
		visited[name] = true
		sorted = append(sorted, tableMap[name])
	}

	for _, t := range domain.Tables {
		visit(t.Name)
	}
	return sorted
}
