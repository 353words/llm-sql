package main

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/csv"
	"fmt"
	"os"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
)

var (
	//go:embed prompts/query.txt
	sqlPrompt string

	//go:embed prompts/response.txt
	responsPrompt string
)

func main() {
	llm, err := ollama.New(ollama.WithModel("ministral-3"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: connect to ollama: %s\n", err)
		os.Exit(1)
	}

	userQuery := "What's the average duration of bike rides in August across all years?"
	prompt := fmt.Sprintf(sqlPrompt, userQuery)

	ctx := context.Background()
	completion, err := llms.GenerateFromSinglePrompt(ctx, llm, prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: get SQL: %s\n", err)
		os.Exit(1)
	}
	fmt.Println(completion)

	db, err := sql.Open("duckdb", "bikes.ddb")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open db: %s\n", err)
		os.Exit(1)
	}
	defer db.Close()

	rows, err := db.Query(completion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: query: %s\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: columns: %s\n", err)
		os.Exit(1)
	}
	var buf bytes.Buffer
	wtr := csv.NewWriter(&buf)
	wtr.Write(cols)

	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	strs := make([]string, len(cols))
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			fmt.Fprintf(os.Stderr, "error: scan: %s\n", err)
			os.Exit(1)
		}
		for i, v := range vals {
			strs[i] = fmt.Sprintf("%v", v)
		}
		wtr.Write(strs)
	}
	wtr.Flush()
	fmt.Println(buf.String())

	prompt = fmt.Sprintf(responsPrompt, userQuery, buf.String())
	completion, err = llms.GenerateFromSinglePrompt(ctx, llm, prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: get SQL: %s\n", err)
		os.Exit(1)
	}
	fmt.Println(completion)
}
