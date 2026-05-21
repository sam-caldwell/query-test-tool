package scorer

import (
	_ "embed"
	"encoding/json"
	"sync"

	"github.com/sam-caldwell/query-test-tool/dialect"
)

//go:embed weights/postgresql.json
var PostgreSQLWeightsData []byte

//go:embed weights/mysql.json
var MySQLWeightsData []byte

// WeightsFile is the JSON structure of an embedded weights file.
type WeightsFile struct {
	Version     int            `json:"version"`
	Description string         `json:"description"`
	RSquared    float64        `json:"r_squared"`
	SampleSize  int            `json:"sample_size"`
	Weights     map[string]int `json:"weights"`
}

var (
	dialectWeights map[dialect.Dialect]*WeightsFile
	weightsOnce    sync.Once
	activeDialect  dialect.Dialect = dialect.PostgreSQL
)

func loadAllWeights() {
	dialectWeights = make(map[dialect.Dialect]*WeightsFile)

	pairs := []struct {
		d    dialect.Dialect
		data []byte
	}{
		{dialect.PostgreSQL, PostgreSQLWeightsData},
		{dialect.MySQL, MySQLWeightsData},
	}

	for _, p := range pairs {
		w := &WeightsFile{}
		if err := json.Unmarshal(p.data, w); err != nil {
			w = defaultWeights()
		}
		dialectWeights[p.d] = w
	}
}

// SetDialect sets the active dialect for weight lookups.
// Must be called before any scoring (typically at CLI startup).
func SetDialect(d dialect.Dialect) {
	activeDialect = d
}

// ActiveDialect returns the currently active dialect.
func ActiveDialect() dialect.Dialect {
	return activeDialect
}

// Weights returns the embedded scoring weights for the active dialect.
func Weights() *WeightsFile {
	weightsOnce.Do(loadAllWeights)
	if w, ok := dialectWeights[activeDialect]; ok {
		return w
	}
	return defaultWeights()
}

// WeightsFor returns the weights for a specific dialect.
func WeightsFor(d dialect.Dialect) *WeightsFile {
	weightsOnce.Do(loadAllWeights)
	if w, ok := dialectWeights[d]; ok {
		return w
	}
	return defaultWeights()
}

// Weight returns the penalty weight for a given rule name using the active dialect.
func Weight(rule string) int {
	w := Weights()
	if v, ok := w.Weights[rule]; ok {
		return v
	}
	return 0
}

func defaultWeights() *WeightsFile {
	return &WeightsFile{
		Version: 0,
		Weights: map[string]int{
			"select-star":                5,
			"missing-predicate":          10,
			"correlated-subquery":        15,
			"non-sargable":              10,
			"distinct-dedup":            8,
			"unbounded-sort":            8,
			"group-by-fanout":           5,
			"window-function":           6,
			"window-no-partition-extra":  4,
			"cartesian-product":         15,
			"subquery-nesting":          3,
			"join":                      2,
			"outer-join":               3,
			"boolean-nesting":           2,
			"cte":                       2,
			"case-expression":           2,
			"set-operation":             3,
			"join-count-squared":         1,
			"null-coalesce-in-predicate": 2,
			"null-check-chain":           2,
			"expensive-function":         2,
			"volatile-function":          3,
			"missing-where-clause":       15,
			"large-offset":               8,
			"recursive-cte":              10,
			"large-in-list":              5,
			"like-leading-wildcard":      10,
			"implicit-cast-in-predicate": 10,
			"lateral-join":               8,
			"returning-clause":           2,
			"grouping-sets":              8,
			"for-update-lock":            5,
			"union-distinct":             5,
			"ddl-statement":              5,
			"cascade-drop":               15,
		},
	}
}
