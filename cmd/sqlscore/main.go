package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sqlscore/scorer"
)

func main() {
	var (
		query    string
		file     string
		format   string
		verbose  bool
	)

	flag.StringVar(&query, "query", "", "SQL query to score (alternative to stdin)")
	flag.StringVar(&query, "q", "", "SQL query to score (shorthand)")
	flag.StringVar(&file, "file", "", "File containing SQL query")
	flag.StringVar(&file, "f", "", "File containing SQL query (shorthand)")
	flag.StringVar(&format, "format", "text", "Output format: text or json")
	flag.BoolVar(&verbose, "verbose", false, "Show detailed findings")
	flag.BoolVar(&verbose, "v", false, "Show detailed findings (shorthand)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: sqlscore [options] [SQL]\n\n")
		fmt.Fprintf(os.Stderr, "Score SQL queries for efficiency, memory/compute cost, and cognitive complexity.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  sqlscore -q 'SELECT * FROM users'\n")
		fmt.Fprintf(os.Stderr, "  echo 'SELECT * FROM users' | sqlscore\n")
		fmt.Fprintf(os.Stderr, "  sqlscore -f query.sql -format json\n")
	}

	flag.Parse()

	sql, err := resolveInput(query, file, flag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if strings.TrimSpace(sql) == "" {
		fmt.Fprintf(os.Stderr, "Error: no SQL input provided\n")
		flag.Usage()
		os.Exit(1)
	}

	report, err := scorer.ScoreQuery(sql)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := output(report, format, verbose); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func resolveInput(query, file string, args []string) (string, error) {
	if query != "" {
		return query, nil
	}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("reading file: %w", err)
		}
		return string(data), nil
	}
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	// Try reading from stdin if it's not a terminal.
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return string(data), nil
	}
	return "", nil
}

func output(report *scorer.Report, format string, verbose bool) error {
	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)

	case "text":
		printTextReport(report, verbose)
		return nil

	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

func printTextReport(r *scorer.Report, verbose bool) {
	fmt.Printf("SQL Query Score Report\n")
	fmt.Printf("======================\n\n")

	grade := scoreGrade(r.TotalScore)
	fmt.Printf("Total Score: %d (%s)\n\n", r.TotalScore, grade)

	printDimension(&r.Efficiency, verbose)
	printDimension(&r.MemoryCompute, verbose)
	printDimension(&r.CognitiveComplex, verbose)

	if r.TotalScore == 0 {
		fmt.Println("No issues detected.")
	}
}

func printDimension(ds *scorer.DimensionScore, verbose bool) {
	if ds.Score == 0 && !verbose {
		return
	}
	fmt.Printf("  %-22s %3d  (%d finding(s))\n", ds.Name+":", ds.Score, len(ds.Findings))
	if verbose {
		for _, f := range ds.Findings {
			fmt.Printf("    [%+d] %-25s %s\n", f.Penalty, f.Rule, f.Description)
		}
	}
}

func scoreGrade(score int) string {
	switch {
	case score == 0:
		return "excellent"
	case score <= 10:
		return "good"
	case score <= 25:
		return "fair"
	case score <= 50:
		return "poor"
	default:
		return "critical"
	}
}
