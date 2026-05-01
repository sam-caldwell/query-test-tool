package scorer

import (
	"testing"
)

func TestMemoryCompute_UnboundedSort(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{"order by no limit", "SELECT * FROM users ORDER BY name", true},
		{"order by with limit", "SELECT * FROM users ORDER BY name LIMIT 10", false},
		{"order by with offset", "SELECT * FROM users ORDER BY name OFFSET 5", false},
		{"order by with limit and offset", "SELECT * FROM users ORDER BY name LIMIT 10 OFFSET 5", false},
		{"no order by", "SELECT * FROM users", false},
		{"multiple sort keys", "SELECT * FROM users ORDER BY name, id", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.MemoryCompute.Findings, "unbounded-sort")
			if hit != tt.wantHit {
				t.Errorf("unbounded-sort: got hit=%v, want %v", hit, tt.wantHit)
			}
		})
	}
}

func TestMemoryCompute_GroupByFanout(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{"group by with count", "SELECT dept, count(*) FROM employees GROUP BY dept", true},
		{"group by with sum", "SELECT dept, sum(salary) FROM employees GROUP BY dept", true},
		{"group by with avg", "SELECT dept, avg(salary) FROM employees GROUP BY dept", true},
		{"group by no agg", "SELECT dept FROM employees GROUP BY dept", false},
		{"no group by", "SELECT * FROM employees", false},
		{"group by with min", "SELECT dept, min(salary) FROM employees GROUP BY dept", true},
		{"group by with max", "SELECT dept, max(salary) FROM employees GROUP BY dept", true},
		{"group by with array_agg", "SELECT dept, array_agg(name) FROM employees GROUP BY dept", true},
		{"group by with string_agg", "SELECT dept, string_agg(name, ',') FROM employees GROUP BY dept", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.MemoryCompute.Findings, "group-by-fanout")
			if hit != tt.wantHit {
				t.Errorf("group-by-fanout: got hit=%v, want %v (findings=%v)", hit, tt.wantHit, report.MemoryCompute.Findings)
			}
		})
	}
}

func TestMemoryCompute_CartesianProduct(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{"cross join", "SELECT * FROM a CROSS JOIN b", true},
		{"implicit cross join", "SELECT * FROM a, b", true},
		{"inner join with on", "SELECT * FROM a JOIN b ON a.id = b.a_id", false},
		{"single table", "SELECT * FROM a", false},
		{"implicit join with where", "SELECT * FROM a, b WHERE a.id = b.a_id", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.MemoryCompute.Findings, "cartesian-product")
			if hit != tt.wantHit {
				t.Errorf("cartesian-product: got hit=%v, want %v (findings=%v)", hit, tt.wantHit, report.MemoryCompute.Findings)
			}
		})
	}
}

func TestMemoryCompute_WindowFunction(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
		rule    string
	}{
		{
			"window with partition",
			"SELECT row_number() OVER (PARTITION BY dept ORDER BY id) FROM employees",
			true,
			"window-function",
		},
		{
			"window without partition",
			"SELECT row_number() OVER (ORDER BY id) FROM employees",
			true,
			"window-function",
		},
		{
			"no window function",
			"SELECT id FROM employees",
			false,
			"window-function",
		},
		{
			"sum over partition",
			"SELECT sum(salary) OVER (PARTITION BY dept) FROM employees",
			true,
			"window-function",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.MemoryCompute.Findings, tt.rule)
			if hit != tt.wantHit {
				t.Errorf("%s: got hit=%v, want %v", tt.rule, hit, tt.wantHit)
			}
		})
	}
}

func TestMemoryCompute_WindowNoPartitionExtraPenalty(t *testing.T) {
	// Window without PARTITION BY should have higher penalty than with PARTITION BY
	withPartition, _ := ScoreQuery("SELECT row_number() OVER (PARTITION BY dept ORDER BY id) FROM employees")
	withoutPartition, _ := ScoreQuery("SELECT row_number() OVER (ORDER BY id) FROM employees")

	if withoutPartition.MemoryCompute.Score <= withPartition.MemoryCompute.Score {
		t.Errorf("window without partition (%d) should score higher than with partition (%d)",
			withoutPartition.MemoryCompute.Score, withPartition.MemoryCompute.Score)
	}
}

func TestMemoryCompute_MultipleFindings(t *testing.T) {
	sql := "SELECT dept, count(*), row_number() OVER (ORDER BY dept) FROM employees GROUP BY dept ORDER BY dept"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.MemoryCompute.Findings) < 3 {
		t.Errorf("expected at least 3 findings, got %d: %v", len(report.MemoryCompute.Findings), report.MemoryCompute.Findings)
	}
}

func TestMemoryCompute_CleanQuery(t *testing.T) {
	sql := "SELECT id FROM users WHERE id = 1"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}
	if report.MemoryCompute.Score != 0 {
		t.Errorf("clean query should have memory_compute score 0, got %d", report.MemoryCompute.Score)
	}
}

func TestMemoryCompute_Penalties(t *testing.T) {
	// Verify unbounded sort penalty
	report, _ := ScoreQuery("SELECT id FROM users ORDER BY id")
	if report.MemoryCompute.Score != PenaltyUnboundedSort {
		t.Errorf("unbounded sort penalty: got %d, want %d", report.MemoryCompute.Score, PenaltyUnboundedSort)
	}
}

func TestMemoryCompute_HasAggregateInExpr(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"nested count in expression",
			"SELECT dept, count(*) + 1 FROM employees GROUP BY dept",
			true,
		},
		{
			"no aggregate",
			"SELECT dept, name FROM employees GROUP BY dept, name",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ScoreQuery(tt.sql)
			if err != nil {
				t.Fatal(err)
			}
			hit := hasRule(report.MemoryCompute.Findings, "group-by-fanout")
			if hit != tt.wantHit {
				t.Errorf("group-by-fanout: got hit=%v, want %v", hit, tt.wantHit)
			}
		})
	}
}

func TestMemoryCompute_NestedCrossJoin(t *testing.T) {
	sql := "SELECT * FROM a JOIN b ON a.id = b.a_id CROSS JOIN c"
	report, err := ScoreQuery(sql)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRule(report.MemoryCompute.Findings, "cartesian-product") {
		t.Error("expected cartesian-product finding for nested CROSS JOIN")
	}
}
