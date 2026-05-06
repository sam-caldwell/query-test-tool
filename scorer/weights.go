package scorer

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed weights.json
var weightsData []byte

// WeightsFile is the JSON structure of the embedded weights file.
type WeightsFile struct {
	Version     int            `json:"version"`
	Description string         `json:"description"`
	Weights     map[string]int `json:"weights"`
}

var (
	loadedWeights *WeightsFile
	weightsOnce   sync.Once
)

// Weights returns the embedded scoring weights, loaded once from weights.json.
// This file is embedded at build time — run cmd/calibrate to generate calibrated
// weights, then rebuild cmd/sqlscore to pick them up.
func Weights() *WeightsFile {
	weightsOnce.Do(func() {
		loadedWeights = &WeightsFile{}
		if err := json.Unmarshal(weightsData, loadedWeights); err != nil {
			// Fall back to hardcoded defaults if the embedded file is corrupt
			loadedWeights = defaultWeights()
		}
	})
	return loadedWeights
}

// Weight returns the penalty weight for a given rule name.
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
		},
	}
}
