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
	"github.com/ollama/ollama/api"
)

var (
	//go:embed prompts/sql.txt
	sqlPrompt string

	//go:embed prompts/answer.txt
	answerPrompt string

	debugMode bool
)

func rowsToCSV(rows *sql.Rows) (string, error) {
	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	wtr := csv.NewWriter(&buf)
	if err := wtr.Write(cols); err != nil {
		return "", err
	}

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

		if err := wtr.Write(strs); err != nil {
			return "", err
		}
	}
	wtr.Flush()

	return buf.String(), nil
}

func debug(name, msg string) {
	if !debugMode {
		return
	}

	fmt.Printf("DEBUG: %s:\n%s\n", name, msg)
}

func chat(ctx context.Context, client *api.Client, req *api.ChatRequest) (string, error) {
	var buf strings.Builder

	err := client.Chat(ctx, req, func(resp api.ChatResponse) error {
		buf.WriteString(resp.Message.Content)
		return nil
	})

	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func queryLLM(ctx context.Context, client *api.Client, model string, db *sql.DB, question string) (string, error) {
	req := api.ChatRequest{
		Model: model,
		Messages: []api.Message{
			{Role: "system", Content: sqlPrompt},
			{Role: "user", Content: question},
		},
	}

	sql, err := chat(ctx, client, &req)
	if err != nil {
		return "", err
	}

	debug("SQL", sql)

	rows, err := db.QueryContext(ctx, sql)
	if err != nil {
		return "", fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	csv, err := rowsToCSV(rows)
	if err != nil {
		return "", fmt.Errorf("scan: %w", err)
	}

	debug("CSV", csv)
	req.Messages = append(
		req.Messages,
		api.Message{Role: "system", Content: "SQL:\n" + sql},
		api.Message{Role: "system", Content: "Reults csv:\n" + csv},
		api.Message{Role: "system", Content: answerPrompt},
	)

	answer, err := chat(ctx, client, &req)
	if err != nil {
		return "", err
	}

	return answer, nil
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

	client, err := api.ClientFromEnvironment()
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

	fmt.Print("Welcome to bikes data system! Ask away.\n>>> ")
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		question := strings.TrimSpace(s.Text())
		if question == "" {
			fmt.Print(">>> ")
			continue
		}

		answer, err := queryLLM(ctx, client, model, db, question)
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
