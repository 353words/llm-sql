## Query Database Using Plain English
++
title = "Query Database Using Plain English"
date = "FIXME"
tags = ["golang"]
categories = ["golang", "llm", "ai", "database", "sql"]
url = "FIXME"
author = "mikit"
+++

### Introduction

In this post you'll see how you can create a system that allows users to query a relational database using plain English.
This allows users not familiar with SQL or business intelligence systems to get insights from data.

### Setting Up

If you want to follow along, you'll need to clone the code from [the GitHub repo](https://github.com/353words/llm-sql).
This will download the code, and the database file containing the data (`bikes.ddb`)

_Note: The data is from the [Austin Bike Share dataset](https://www.kaggle.com/datasets/jboysen/austin-bike)._

Next, you need to install our very own [kronk](https://github.com/ardanlabs/kronk/) as the system to run LLMs.

_Note: You can use [ollama](https://ollama.com/), OpenAI, Claude and many other systems as well. I'm using `kronk` since it runs locally (no charges) and supports OpenAI API._

Run `go install github.com/ardanlabs/kronk/cmd/kronk@latest`, `kronk` will be installed to `$(go env GOPATH)/bin`, 
which in most systems is `~/go/bin`. You can run `kronk` as `~/go/bin/kronk` or add `$(go env GOPATH)/bin` to the `PATH` environment variable.

Start kronk with: `kronk server start`.

Next, you need to install the model, we're going to use the `ministral` model. In a second terminal run:

```
$ kronk model pull https://huggingface.co/unsloth/Ministral-3-14B-Instruct-2512-GGUF/resolve/main/Ministral-3-14B-Instruct-2512-Q4_0.gguf
```

You might want to use other models, you can query HuggingFace to find a suitable model and then get the URL for the `.gguf` file.


### Architecture Overview

LLMs don't have memory, in every call you need to provide all the relevant information they need (called context) in order to answer you.
Our application flow will be:

- Ask LLM to generate SQL based on user question
- Query the database with generated SQL
- Ask LLM to generate an answer based on user question and results from database

Our application will run a loop that will accept a user query and then run the above steps for it.

### Initialization

First, you'll create connections to the `ollama` and to the database.

**Listing 1: LLM and Database connections**


```go
092 func main() {
093     if os.Getenv("DEBUG") != "" {
094         h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
095         log := slog.New(h)
096         slog.SetDefault(log)
097     }
098 
099     baseURL := "http://localhost:8080/v1"
100     if host := os.Getenv("KRONK_WEB_API_HOST"); host != "" {
101         baseURL = host + "/v1"
102     }
103 
104     llm, err := openai.New(
105         openai.WithBaseURL(baseURL),
106         openai.WithToken("x"),
107         openai.WithModel("Ministral-3-14B-Instruct-2512-Q4_0"),
108     )
109 
110     if err != nil {
111         fmt.Fprintf(os.Stderr, "error: connect: %s\n", err)
112         os.Exit(1)
113     }
114 
115     db, err := sql.Open("duckdb", "bikes.ddb")
116     if err != nil {
117         fmt.Fprintf(os.Stderr, "error: open db: %s\n", err)
118         os.Exit(1)
119     }
120     defer db.Close()
```

Listing 1 shows initial connection.
On lines 93-101 you get options from the environment: If you're in debug mode and the kronk server base URL.
On lines 104-108 you connect to `kronk` with the required model and base URL. Since the `openai` requires a key, we create a fake key that `kronk` will ignore.  
On lines 115-120 you connect to the database.

_Note: I'm using [duckdb](https://duckdb.org/) since it's simple to use, but this code can work with any SQL database._

### User loop

Let's look at the loop that asks the user for a question and returns an answer:

**Listing 2: User Loop**

```go
122     ctx := context.Background()
123     fmt.Print("Welcome to bikes data system! Ask away.\n>>> ")
124     s := bufio.NewScanner(os.Stdin)
125 
126     for s.Scan() {
127         question := strings.TrimSpace(s.Text())
128         if question == "" {
129             fmt.Print(">>> ")
130             continue
131         }
132 
133         answer, err := queryLLM(ctx, llm, db, question)
134         if err != nil {
135             fmt.Println("ERROR:", err)
136         } else {
137             fmt.Println(answer)
138         }
139         fmt.Print(">>> ")
140     }
141 
142     if err := s.Err(); err != nil && !errors.Is(err, io.EOF) {
143         fmt.Fprintf(os.Stderr, "error: scan: %s\n", err)
144         os.Exit(1)
145     }
146 
147     fmt.Println("\nCiao!")
148 }

```

Listing 2 shows the question/answer loop, which is the second part of `main`.
On line 122 you use `context.Background()` as the context. You'd probably want to have a time limit.
One line 123 you print a greeting and on line 124 you create a scanner.
One lines 126-140 you loop over user questions and call `queryLLM` to get an answer.

### Querying LLMs

Before querying the LLM, you need to construct a prompt.
You need two prompts:
- Generate SQL query from user question
- Given user question and answer from database, answer the user

**Listing 3: Prompts**

Listing 3 shows the prompts.

```go
021 var (
022     //go:embed prompts/sql.txt
023     sqlPrompt string
024 
025     //go:embed prompts/answer.txt
026     answerPrompt string
027 )
```

Listing 3 shows the prompts
On lines 21-27 you use `go:embed` directive to embed the prompt from a text file.

Using text files is easier and allows you to edit and play with prompt without changing the code.
Crafting a good prompt is an art (that I'm still learning).
Play around with several versions of prompts until you get good and consistent results from the LLM.

The SQL prompt is:

```
# Instructions

Convert a user query into SQL given the following schema:

CREATE TABLE stations(latitude DOUBLE, "location" VARCHAR, longitude DOUBLE, "name" VARCHAR, station_id BIGINT, status VARCHAR);
CREATE TABLE trips(bikeid DOUBLE, checkout_time TIME, duration_minutes BIGINT, end_station_id DOUBLE, end_station_name VARCHAR, "month" DOUBLE, start_station_id DOUBLE, start_station_name VARCHAR, start_time TIMESTAMP, subscriber_type VARCHAR, trip_id BIGINT, "year" DOUBLE);


Do not explain your answer, return only the SQL statement without any markdown or other decorations.

# User Query
%s
```

You use `%s` as a placeholder for the user question.

The answer prompt is:

```
Provide an answer to the user question with the following results from the database.

User question:
%s

SQL query:
%s

Database results in CSV format:
%s
```

### Querying the LLM

Armed with user questions and the prompts, you can now query the LLM.

**Listing 4: queryLLM**

```go
060 func queryLLM(ctx context.Context, llm *openai.LLM, db *sql.DB, question string) (string, error) {
061     prompt := fmt.Sprintf(sqlPrompt, question)
062 
063     sql, err := llm.Call(ctx, prompt)
064     if err != nil {
065         return "", fmt.Errorf("get SQL: %w", err)
066     }
067 
068     slog.Debug("SQL", "query", sql)
069 
070     rows, err := db.QueryContext(ctx, sql)
071     if err != nil {
072         return "", fmt.Errorf("query: %w", err)
073     }
074     defer rows.Close()
075 
076     csv, err := rowsToCSV(rows)
077     if err != nil {
078         return "", fmt.Errorf("scan: %w", err)
079     }
080 
081     slog.Debug("CSV", "data", csv)
082 
083     prompt = fmt.Sprintf(answerPrompt, question, sql, csv)
084     answer, err := llm.Call(ctx, prompt)
085     if err != nil {
086         return "", fmt.Errorf("get SQL: %w", err)
087     }
088 
089     return answer, nil
090 }
```

Listing 4 shows how to query the LLM.
On line 61 you create a prompt that will return SQL and on line 64 you query the LLM with the prompt.
On line 70 you query the database using the returned SQL.
One line 76 you convert the return database rows to CSV.
On line 83 you generate a prompt for the final answer and on line 84 you call the LLM with this prompt.
Finally on line 98 you return the answer from the LLM.

_Note: On lines 68 and 81 you use `slog.Debug` to emit intermediate results, this is very helpful when debugging your applications. `slog.Debug` will emit logs only if the `DEBUG` environment variable is set to non-empty value (say `yes`)._


**Listing 5: Convert Rows to CSV**

```go
029 func rowsToCSV(rows *sql.Rows) (string, error) {
030     cols, err := rows.Columns()
031     if err != nil {
032         return "", err
033     }
034     var buf bytes.Buffer
035     wtr := csv.NewWriter(&buf)
036     wtr.Write(cols)
037 
038     vals := make([]any, len(cols))
039     ptrs := make([]any, len(cols))
040     for i := range vals {
041         ptrs[i] = &vals[i]
042     }
043     strs := make([]string, len(cols))
044 
045     for rows.Next() {
046         if err := rows.Scan(ptrs...); err != nil {
047             return "", err
048         }
049 
050         for i, v := range vals {
051             strs[i] = fmt.Sprintf("%v", v)
052         }
053         wtr.Write(strs)
054     }
055     wtr.Flush()
056 
057     return buf.String(), nil
058 }
```

Listing 5 shows the `rowsToCSV` utility function. It is not essential so I'm not going to explain the code. I'm including it here for completeness.

### Running The code

Finally, you can run the code (use `CTRL-D` to quit):

```
$ go run .
Welcome to bikes data system! Ask away.
>>> How many rides in the database?
The database contains **649,231 rides** in the `trips` table.
>>> How many stations are there?
There are **72 stations** in total.
>>> What is the date range of the rides?
The date range of the rides in the dataset spans from **December 21, 2013**, to **July 31, 2017**.
>>> How many rides are in January 2014?
The number of rides in January 2014 is **3,375**.
>>> What is the longest ride?
The longest single ride recorded in the database was on **bike ID 19**, with a duration of **21,296 minutes** (which is approximately **354.93 hours** or **14 days and 20.93 hours** of continuous riding).

This suggests an extremely long trip—likely spanning multiple days—far exceeding typical bike rental durations. Would you like to explore further details about this ride (e.g., route, date, or other metrics)?
>>>
Ciao!
```

### Summary

In about 150 lines of code we wrote a system that allows users to query a database using plain English.
Using LLMs from Go is simple, most of the time you'll spend tweaking prompts to get good results.
As usual, you can see the code and database [in the GitHub repo](https://github.com/353words/llm-sql)

How are you using LLMs in your Go code? Let me know at miki@ardanlabs.com
