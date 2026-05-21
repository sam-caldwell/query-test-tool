package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sam-caldwell/query-test-tool/dialect"
	"github.com/sam-caldwell/query-test-tool/scorer"
	mysqlscorer "github.com/sam-caldwell/query-test-tool/scorer/mysql"
)

// Set via -ldflags at build time.
var (
	version = "dev"
	commit  = "unknown"
)

func init() {
	// Register MySQL scorer (must be done from cmd/ to avoid import cycles).
	scorer.RegisterDialectScorer(dialect.MySQL, mysqlscorer.ScoreQuery)

	// Register dialects with their embedded weight data.
	dialect.Register(&dialect.Registration{
		Name:        dialect.PostgreSQL,
		Description: "PostgreSQL (calibrated from EXPLAIN ANALYZE)",
		WeightsData: scorer.PostgreSQLWeightsData,
	})
	dialect.Register(&dialect.Registration{
		Name:        dialect.MySQL,
		Description: "MySQL (default weights, uncalibrated)",
		WeightsData: scorer.MySQLWeightsData,
	})
}

var rootCmd = &cobra.Command{
	Use:   "sqlscore [flags] [SQL]",
	Short: "Score SQL queries for efficiency, memory/compute cost, and cognitive complexity",
	Long: `sqlscore statically analyzes SQL queries and produces a score predicting
how expensive the query will be. Supports multiple database dialects with
calibrated weights derived from real EXPLAIN ANALYZE data.`,
	Args:          cobra.ArbitraryArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          executeScore,
}

func init() {
	rootCmd.Flags().StringP("db", "d", "postgresql", "Database dialect: postgresql, mysql")
	rootCmd.Flags().StringP("query", "q", "", "SQL query to score")
	rootCmd.Flags().StringP("file", "f", "", "File containing SQL query")
	rootCmd.Flags().String("format", "text", "Output format: text or json")
	rootCmd.Flags().BoolP("verbose", "v", false, "Show detailed findings")

	rootCmd.SetVersionTemplate(versionTemplate())
	rootCmd.Version = fmt.Sprintf("%s (%s)", version, commit)
}

func versionTemplate() string {
	return `sqlscore {{.Version}}
`
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func executeScore(cmd *cobra.Command, args []string) error {
	dbFlag, _ := cmd.Flags().GetString("db")
	query, _ := cmd.Flags().GetString("query")
	file, _ := cmd.Flags().GetString("file")
	format, _ := cmd.Flags().GetString("format")
	verbose, _ := cmd.Flags().GetBool("verbose")

	// Validate dialect
	d := dialect.Dialect(dbFlag)
	if _, err := dialect.Get(d); err != nil {
		return err
	}

	// Show weights info when --version is handled by cobra,
	// but also support explicit version check in our flow
	if cmd.Flags().Changed("version") {
		w := scorer.WeightsFor(d)
		fmt.Printf("sqlscore %s (%s)\n", version, commit)
		fmt.Printf("Dialect: %s\n", d)
		fmt.Printf("Weights: version %d — %s\n", w.Version, w.Description)
		return nil
	}

	sql, err := resolveInput(query, file, args)
	if err != nil {
		return err
	}

	if strings.TrimSpace(sql) == "" {
		return fmt.Errorf("no SQL input provided\n\nUsage:\n  %s", cmd.UseLine())
	}

	report, err := scorer.ScoreQueryWithDialect(sql, d)
	if err != nil {
		return err
	}

	return output(report, format, verbose)
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
	fmt.Printf("SQL Query Score Report (%s)\n", r.Dialect)
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
