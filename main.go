package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
)

var (
	//go:embed prompts/query.txt
	sqlPrompt string

	//go:embed prompts/response.txt
	responsPrompt string

	debugMode bool
)

func rowsToCSV(rows *sql.Rows) (string, error) {
	cols, err := rows.Columns()
	if err != nil {
		return "", err
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
			return "", err
		}

		for i, v := range vals {
			strs[i] = fmt.Sprintf("%v", v)
		}
		wtr.Write(strs)
	}
	wtr.Flush()

	return buf.String(), nil
}

func debug(name, msg string) {
	if !debugMode {
		return
	}

	fmt.Printf("%s:\n%s\n", name, msg)
}

func queryLLM(ctx context.Context, llm *ollama.LLM, db *sql.DB, question string) (string, error) {
	prompt := fmt.Sprintf(sqlPrompt, question)

	completion, err := llms.GenerateFromSinglePrompt(ctx, llm, prompt)
	if err != nil {
		return "", fmt.Errorf("get SQL: %w", err)
	}

	debug("SQL", completion)

	rows, err := db.QueryContext(ctx, completion)
	if err != nil {
		return "", fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	csv, err := rowsToCSV(rows)
	if err != nil {
		return "", fmt.Errorf("scan: %w", err)
	}

	debug("CSV", csv)

	prompt = fmt.Sprintf(responsPrompt, question, csv)
	completion, err = llms.GenerateFromSinglePrompt(ctx, llm, prompt)
	if err != nil {
		return "", fmt.Errorf("get SQL: %w", err)
	}
	return completion, nil
}

func main() {
	model := os.Getenv("MODEL")
	if model == "" {
		model = "ministral-3"
	}

	dbFile := os.Getenv("DB_FILE")
	if dbFile == "" {
		dbFile = "bikes.ddb"
	}

	debugMode = os.Getenv("DEBUG") != ""

	llm, err := ollama.New(ollama.WithModel(model))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: connect to ollama: %s\n", err)
		os.Exit(1)
	}

	db, err := sql.Open("duckdb", dbFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open db: %s\n", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx := context.Background()
	fmt.Print("Welcome to bikes data system! Ask away\n>>> ")
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		question := strings.TrimSpace(s.Text())
		if question == "" {
			fmt.Print(">>> ")
			continue
		}

		answer, err := queryLLM(ctx, llm, db, question)
		if err != nil {
			fmt.Println("ERROR:", err)
		} else {
			fmt.Println(answer)
		}
		fmt.Print(">>> ")
	}

	if err := s.Err(); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "error: scan: %s\n", err)
		os.Exit(1)
	}
	fmt.Println("\nCiao!")
}
