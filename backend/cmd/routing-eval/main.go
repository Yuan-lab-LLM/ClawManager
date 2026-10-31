// routing-eval evaluates synthetic cases using the real Gateway rule classifier.
// It does not construct a Gateway service or access databases, models or K8s.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"clawreef/internal/aigateway"
)

type evalCase struct {
	ID           string                      `json:"id"`
	Suite        string                      `json:"suite"`
	ExpectedTier int                         `json:"expected_tier"`
	Note         string                      `json:"note"`
	Turns        []aigateway.RoutingTaskTurn `json:"turns"`
}

type result struct {
	ID        string   `json:"id"`
	Tier      int      `json:"tier"`
	LatencyMs float64  `json:"latency_ms"`
	Signals   []string `json:"signals"`
	Warnings  []string `json:"warnings"`
}

func main() {
	casesPath := flag.String("cases", "", "synthetic case JSON")
	output := flag.String("output", "", "new output JSON path; stdout when empty")
	flag.Parse()
	if err := run(*casesPath, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(casesPath, output string) error {
	raw, err := os.ReadFile(casesPath)
	if err != nil {
		return err
	}
	var cases []evalCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		return err
	}
	if len(cases) == 0 {
		return fmt.Errorf("empty corpus")
	}
	seen := map[string]bool{}
	classifier := aigateway.RuleRoutingClassifier{}
	results := make([]result, 0, len(cases))
	for _, c := range cases {
		if c.ID == "" || seen[c.ID] || c.ExpectedTier < 1 || c.ExpectedTier > 4 || len(c.Turns) == 0 || len(c.Turns) > 8 {
			return fmt.Errorf("invalid case %q", c.ID)
		}
		seen[c.ID] = true
		input := aigateway.RoutingClassificationInput{Turns: c.Turns}
		// Amortize over batches to avoid zero-duration measurements on Windows
		// clocks for very fast individual calls. Exclude process startup.
		if _, err := classifier.Classify(context.Background(), input); err != nil {
			return err
		}
		samples := make([]float64, 5)
		var classification aigateway.RoutingClassification
		for i := range samples {
			start := time.Now()
			count := 0
			for count < 32 || time.Since(start) < 20*time.Millisecond {
				classification, err = classifier.Classify(context.Background(), input)
				if err != nil {
					return err
				}
				count++
			}
			samples[i] = float64(time.Since(start).Nanoseconds()) / 1e6 / float64(count)
		}
		sort.Float64s(samples)
		results = append(results, result{c.ID, classification.RequiredTier, samples[2], classification.Signals, classification.Warnings})
	}
	writer := os.Stdout
	if output != "" {
		writer, err = os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer writer.Close()
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(results)
}
