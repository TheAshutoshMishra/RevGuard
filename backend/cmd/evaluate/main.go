// Command evaluate runs RevGuard's deterministic, offline evaluation
// (Milestone 8, refined for accuracy in Milestone 9): it generates a
// SYNTHETIC dataset of recovery opportunities, runs two independent
// baseline strategies and the real RevGuard decision pipeline over it,
// and reports whether RevGuard recovers more incremental revenue than
// the baselines.
//
// This command makes no network calls, opens no database connection,
// and needs neither the AI service nor a running backend — it exercises
// only the deterministic, in-process decision logic in
// backend/internal/service/evaluation_*.go.
//
// Usage:
//
//	go run ./cmd/evaluate --seed 12345 --cases 1000
//	go run ./cmd/evaluate --seed 12345 --cases 1000 --output evaluation.json
//	go run ./cmd/evaluate --seed 12345 --cases 1000 --markdown-output evaluation.md --commit $(git rev-parse --short HEAD)
//
// Running this command twice with identical --seed/--cases always
// produces an identical JSON result — see
// backend/internal/service/evaluation_engine_test.go. --commit is a
// plain caller-supplied label (e.g. from `git rev-parse --short HEAD`,
// a read-only command); this binary never invokes git itself, so the
// evaluation result stays a pure function of --seed/--cases.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"revguard/backend/internal/service"
)

func main() {
	seed := flag.Int64("seed", 12345, "deterministic random seed for the synthetic dataset")
	cases := flag.Int("cases", 1000, "number of synthetic recovery opportunities to generate")
	output := flag.String("output", "", "if set, write the machine-readable JSON result to this file (otherwise printed to stdout)")
	markdownOutput := flag.String("markdown-output", "", "if set, write the human-readable Markdown report to this file (otherwise printed to stdout)")
	commit := flag.String("commit", "", "optional code/version identifier (e.g. a git commit hash) to stamp on the Markdown report; purely presentational")
	flag.Parse()

	result, err := service.RunEvaluation(*seed, *cases)
	if err != nil {
		log.Fatalf("evaluate: %v", err)
	}

	fmt.Print(service.FormatResultTable(result))

	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Fatalf("evaluate: failed to marshal result: %v", err)
	}
	if *output == "" {
		fmt.Println()
		fmt.Println(string(jsonBytes))
	} else {
		if err := os.WriteFile(*output, jsonBytes, 0o644); err != nil {
			log.Fatalf("evaluate: failed to write %s: %v", *output, err)
		}
		fmt.Printf("\nJSON result written to %s\n", *output)
	}

	markdown := service.FormatMarkdownReport(result, time.Now(), *commit)
	if *markdownOutput == "" {
		fmt.Println()
		fmt.Println(markdown)
	} else {
		if err := os.WriteFile(*markdownOutput, []byte(markdown), 0o644); err != nil {
			log.Fatalf("evaluate: failed to write %s: %v", *markdownOutput, err)
		}
		fmt.Printf("\nMarkdown report written to %s\n", *markdownOutput)
	}
}
