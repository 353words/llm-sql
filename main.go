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
	"log/slog"
	"os"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/tmc/langchaingo/llms/openai"
)

var (
	//go:embed prompts/sql.txt
	sqlPrompt string

	//go:embed prompts/answer.txt
	answerPrompt string
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

func queryLLM(ctx context.Context, llm *openai.LLM, db *sql.DB, question string) (string, error) {
	prompt := fmt.Sprintf(sqlPrompt, question)

	sql, err := llm.Call(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("get SQL: %w", err)
	}

	slog.Debug("SQL", "query", sql)

	rows, err := db.QueryContext(ctx, sql)
	if err != nil {
		return "", fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	csv, err := rowsToCSV(rows)
	if err != nil {
		return "", fmt.Errorf("scan: %w", err)
	}

	slog.Debug("CSV", "data", csv)

	prompt = fmt.Sprintf(answerPrompt, question, sql, csv)
	answer, err := llm.Call(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("get SQL: %w", err)
	}

	return answer, nil
}

func main() {
	if os.Getenv("DEBUG") != "" {
		h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
		log := slog.New(h)
		slog.SetDefault(log)
	}

	baseURL := "http://localhost:8080/v1"
	if host := os.Getenv("KRONK_WEB_API_HOST"); host != "" {
		baseURL = host + "/v1"
	}

	llm, err := openai.New(
		openai.WithBaseURL(baseURL),
		openai.WithToken("x"),
		openai.WithModel("Ministral-3-14B-Instruct-2512-Q4_0"),
	)

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: connect: %s\n", err)
		os.Exit(1)
	}

	db, err := sql.Open("duckdb", "bikes.ddb")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open db: %s\n", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx := context.Background()
	fmt.Print("Welcome to bikes data system! Ask away.\n>>> ")
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
