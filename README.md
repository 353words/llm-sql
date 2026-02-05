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

Next, you need to install since [ollama](https://ollama.com/) as the system to run LLMs.
You can run `brew install ollama` or by download it from [the download site](https://ollama.com/download).

Start `ollama` with `ollama serve`.
In a second terminal, pull the LLM model you're going to use by running `ollama pull ministral-3:latest`.

### Architecture Overview

LLMs don't have memory, in every call you need to provide all the relevant information they need (called context) in order to answer you.
Out application flow will be:

- Ask LLM to generate SQL based on user question
- Query the database with generated SQL
- Ask LLM to generate an answer based on user question and results from database

Our application will run a loop that will accept a user query and then run the above steps for it.

### Initialization

First, you'll create connections to the `ollama` and to the database.

**Listing 1: LLM and Database connections**


```go
101 func main() {
102     model := os.Getenv("MODEL")
103     if model == "" {
104         model = "ministral-3"
105     }
106 
107     dbFile := os.Getenv("DB_FILE")
108     if dbFile == "" {
109         dbFile = "bikes.ddb"
110     }
111 
112     debugMode = os.Getenv("DEBUG") != ""
113 
114     llm, err := ollama.New(ollama.WithModel(model))
115     if err != nil {
116         fmt.Fprintf(os.Stderr, "error: connect to ollama: %s\n", err)
117         os.Exit(1)
118     }
119 
120     db, err := sql.Open("duckdb", dbFile)
121     if err != nil {
122         fmt.Fprintf(os.Stderr, "error: open db: %s\n", err)
123         os.Exit(1)
124     }
125     defer db.Close()
```

Listing 1 shows initial connection.
On lines 102-112 you get options from the environment: The model to use, the database file location and if you're in debug mode.
On lines 114-118 you connect to `ollama` with the required model and on lines 120-125 you connect to the database.

_Note: I'm using [duckdb](https://duckdb.org/) since it's simple to use, but this code can work with any SQL database._

### User loop

Let's look at the loop that asks the user for a question and returns an answer:

**Listing 2: User Loop**

```go
127     ctx := context.Background()
128     fmt.Print("Welcome to bikes data system! Ask away.\n>>> ")
129     s := bufio.NewScanner(os.Stdin)
130     for s.Scan() {
131         question := strings.TrimSpace(s.Text())
132         if question == "" {
133             fmt.Print(">>> ")
134             continue
135         }
136 
137         answer, err := queryLLM(ctx, llm, db, question)
138         if err != nil {
139             fmt.Println("ERROR:", err)
140         } else {
141             fmt.Println(answer)
142         }
143         fmt.Print(">>> ")
144     }
145 
146     if err := s.Err(); err != nil && !errors.Is(err, io.EOF) {
147         fmt.Fprintf(os.Stderr, "error: scan: %s\n", err)
148         os.Exit(1)
149     }
150     fmt.Println("\nCiao!")
151 }

```

Listing 2 shows the question/answer loop, which is the second part of `main`.
On line 127 you use `context.Background()` as the context. You'd probably want to have a time limit.
One line 128 you print a greeting and on line 129 you create a scanner.
One lines 130-144 you loop over user questions and call `queryLLM` to get an answer.

### Querying LLMs

Before querying the LLM, you need to construct a prompt.
You have two prompts:
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
027 
028     debugMode bool
029 )
```

Listing 3 shows the prompts
On lines 22-26 you use `go:embed` directive to embed the prompt from a text file.

Using text files is easier and allows you to edit and play with prompt without changing the code.
Crafting a good prompt is an art (that I'm still learning).
Play around with several versions of prompts until you get good and consistent results from the LLM.

The query prompt is:

```
# Instructions

Convert a user query into SQL given the following schema:

CREATE TABLE stations(latitude DOUBLE, "location" VARCHAR, longitude DOUBLE, "name" VARCHAR, station_id BIGINT, status VARCHAR);
CREATE TABLE trips(bikeid DOUBLE, checkout_time TIME, duration_minutes BIGINT, end_station_id DOUBLE, end_station_name VARCHAR, "month" DOUBLE, start_station_id DOUBLE, start_station_name VARCHAR, start_time TIMESTAMP, subscriber_type VARCHAR, trip_id BIGINT, "year" DOUBLE);


Do not explain your answer, return only the SQL statement.

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

Armed with user question and prompts, you can now query the LLM.

**Listing 4: queryLLM**

```go
070 func queryLLM(ctx context.Context, llm *ollama.LLM, db *sql.DB, question string) (string, error) {
071     prompt := fmt.Sprintf(sqlPrompt, question)
072 
073     sql, err := llms.GenerateFromSinglePrompt(ctx, llm, prompt)
074     if err != nil {
075         return "", fmt.Errorf("get SQL: %w", err)
076     }
077 
078     debug("SQL", sql)
079 
080     rows, err := db.QueryContext(ctx, sql)
081     if err != nil {
082         return "", fmt.Errorf("query: %w", err)
083     }
084     defer rows.Close()
085 
086     csv, err := rowsToCSV(rows)
087     if err != nil {
088         return "", fmt.Errorf("scan: %w", err)
089     }
090 
091     debug("CSV", csv)
092 
093     prompt = fmt.Sprintf(answerPrompt, question, sql, csv)
094     answer, err := llms.GenerateFromSinglePrompt(ctx, llm, prompt)
095     if err != nil {
096         return "", fmt.Errorf("get SQL: %w", err)
097     }
098     return answer, nil
099 }
```

Listing 4 show how to query the LLM.
On line 71 you create prompt that will return SQL and on line 74 we query the LLM with the prompt.
On line 80 you query the database using the returned SQL.
One line 86 you convert the return database rows to CSV.
On line 93 you generate prompt for the final answer and on line 94 we call the LLM with this prompt.
Finally on line 98 you return the answer from the LLM.

_Note: The `debug` function prints intermediate results. It is very helpful when debugging your application. To see debug prints, set the `DEBUG` environment variable to non-empty value (say `yes`)._

**Listing 5: Utility Functions**

```go
062 func debug(name, msg string) {
063     if !debugMode {
064         return
065     }
066 
067     fmt.Printf("DEBUG: %s:\n%s\n", name, msg)
068 }
...

031 func rowsToCSV(rows *sql.Rows) (string, error) {
032     cols, err := rows.Columns()
033     if err != nil {
034         return "", err
035     }
036     var buf bytes.Buffer
037     wtr := csv.NewWriter(&buf)
038     wtr.Write(cols)
039 
040     vals := make([]any, len(cols))
041     ptrs := make([]any, len(cols))
042     for i := range vals {
043         ptrs[i] = &vals[i]
044     }
045     strs := make([]string, len(cols))
046 
047     for rows.Next() {
048         if err := rows.Scan(ptrs...); err != nil {
049             return "", err
050         }
051 
052         for i, v := range vals {
053             strs[i] = fmt.Sprintf("%v", v)
054         }
055         wtr.Write(strs)
056     }
057     wtr.Flush()
058 
059     return buf.String(), nil
060 }
```

Listing 5 shows two utility functions: `debug` and `rowsToCSV`. Both of these functions are not essential so I'm not going to explain the code. I'm including it here for completeness.

### Running The code

Finally, you can run the code (use `CTRL-D` to quit):

```
$ go run .
Welcome to bikes data system! Ask away.
>>> How many rides are in the database?
The database contains **649,231 rides**.
>>> How many stations are there?
The database indicates there are **72 stations** in total.
>>> What is the date range of the rides?
The rides took place between **December 21, 2013**, and **July 31, 2017**.
>>> How many rides are in January 2014?
The database indicates that there were **3,375 rides** in January 2014.
>>> What is the longest ride?
The longest ride in the database is the trip with the following details:

- **Trip ID:** 9900012849
- **Bike ID:** 19
- **Start Station:** Barton Springs @ Kinney Ave
- **End Station:** Stolen
- **Duration:** **21,296 minutes** (which is approximately **358.27 hours** or **15 days and 8 hours**).
>>>
Ciao!
```

### Summary

In about 150 lines of code we wrote a system that allow users to query a database using plain English.
Using LLMs from Go is simple, most of the time you'll spend tweaking prompts to get good results.
As usual, you can see the code and database [in the GitHub repo](https://github.com/353words/llm-sql)

How are you using LLMs in your Go code? Let me know at miki@ardanlabs.com
