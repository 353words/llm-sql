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

_Note: We're using `ollama`, but the same idea applies to many other systems such as [OpenAI](https://openai.com/), [Claude](https://claude.ai/), [HuggingFace](https://huggingface.co/) and even our very own [Kronk](https://github.com/ardanlabs/kronk)._


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
133 func main() {
134     model := os.Getenv("MODEL")
135     if model == "" {
136         model = "ministral-3"
137     }
138 
139     dbFile := os.Getenv("DB_FILE")
140     if dbFile == "" {
141         dbFile = "bikes.ddb"
142     }
143 
144     debugMode = os.Getenv("DEBUG") != ""
145 
146     client, err := api.ClientFromEnvironment()
147     if err != nil {
148         fmt.Fprintf(os.Stderr, "error: connect to ollama: %s\n", err)
149         os.Exit(1)
150     }
151 
152     db, err := sql.Open("duckdb", dbFile)
153     if err != nil {
154         fmt.Fprintf(os.Stderr, "error: open db: %s\n", err)
155         os.Exit(1)
156     }
157     defer db.Close()
```

Listing 1 shows initial setup.
On lines 135-145 you get options from the environment: The model to use, the database file location and if you're in debug mode.
On lines 147-151 you connect to the Ollama API client from environment and on lines 153-157 you connect to the database.

_Note: I'm using [duckdb](https://duckdb.org/) since it's simple to use, but this code can work with any SQL database._

### User loop

Let's look at the loop that asks the user for a question and returns an answer:

**Listing 2: User Loop**

```go
159     ctx := context.Background()
161     fmt.Print("Welcome to bikes data system! Ask away.\n>>> ")
162     s := bufio.NewScanner(os.Stdin)
163     for s.Scan() {
164         question := strings.TrimSpace(s.Text())
165         if question == "" {
166             fmt.Print(">>> ")
167             continue
168         }
169 
170         answer, err := queryLLM(ctx, client, model, db, question)
171         if err != nil {
172             fmt.Println("ERROR:", err)
173         } else {
174             fmt.Println(answer)
175         }
176         fmt.Print(">>> ")
177     }
178 
179     if err := s.Err(); err != nil && !errors.Is(err, io.EOF) {
180         fmt.Fprintf(os.Stderr, "error: scan: %s\n", err)
181         os.Exit(1)
182     }
183     fmt.Println("\nCiao!")
184 }

```

Listing 2 shows the question/answer loop, which is the second part of `main`.
On line 159 you use `context.Background()` as the context. You'd probably want to have a time limit.
On line 160 you print a greeting and on line 161 you create a scanner.
On lines 162-176 you loop over user questions and call `queryLLM` with the client, model, database, and question.

### Prompts

Before querying the LLM, you need to construct a prompt.
You have two prompts:
- Generate SQL query from user question
- Given user question and answer from database, answer the user

**Listing 3: Prompts**

Listing 3 shows the prompts.

```go

020 var (
021     //go:embed prompts/sql.txt
022     sqlPrompt string
023 
024     //go:embed prompts/answer.txt
025     answerPrompt string
026 
027     debugMode bool
028 )
```

Listing 3 shows the prompts using `go:embed` directive to embed prompts from text files.

Using text files is easier and allows you to edit and play with prompts without changing the code.
Crafting a good prompt is an art (that I'm still learning).
Play around with several versions of prompts until you get good and consistent results from the LLM.

The sql prompt is:

```
# Insturctions

Convert a user query into SQL given the following schema:

CREATE TABLE stations(latitude DOUBLE, "location" VARCHAR, longitude DOUBLE, "name" VARCHAR, station_id BIGINT, status VARCHAR);
CREATE TABLE trips(bikeid DOUBLE, checkout_time TIME, duration_minutes BIGINT, end_station_id DOUBLE, end_station_name VARCHAR, "month" DOUBLE, start_station_id DOUBLE, start_station_name VARCHAR, start_time TIMESTAMP, subscriber_type VARCHAR, trip_id BIGINT, "year" DOUBLE);


Do not explain your answer, return only the SQL statement.
```

You use `%s` as a placeholder for the user question.

The answer prompt is:

```
Provide an answer, in plain English, to the user question from the preceeding SQL query and results in CSV format.
```

### Querying the LLM

Armed with user question and prompts, you can now query the LLM.

**Listing 4: queryLLM**

```go
090 func queryLLM(ctx context.Context, client *api.Client, model string, db *sql.DB, question string) (string, error) {
091     req := api.ChatRequest{
092         Model: model,
093         Messages: []api.Message{
094             {Role: "system", Content: sqlPrompt},
095             {Role: "user", Content: question},
096         },
097     }
098 
099     sql, err := chat(ctx, client, &req)
100     if err != nil {
101         return "", err
102     }
103 
104     debug("SQL", sql)
105 
106     rows, err := db.QueryContext(ctx, sql)
107     if err != nil {
108         return "", fmt.Errorf("query: %w", err)
109     }
110     defer rows.Close()
111 
112     csv, err := rowsToCSV(rows)
113     if err != nil {
114         return "", fmt.Errorf("scan: %w", err)
115     }
116 
117     debug("CSV", csv)
118     req.Messages = append(
119         req.Messages,
120         api.Message{Role: "system", Content: "SQL:\n" + sql},
121         api.Message{Role: "system", Content: "Reults csv:\n" + csv},
122         api.Message{Role: "system", Content: answerPrompt},
123     )
124 
125     answer, err := chat(ctx, client, &req)
126     if err != nil {
127         return "", err
128     }
129 
130     return answer, nil
131 }
```

Listing 4 shows how to query the LLM using the Ollama API.
On lines 91-97 you create a chat request with the SQL generation system prompt and user question.
On line 99 you call the chat function to get the generated SQL.
On line 106 you query the database using the returned SQL.
On line 112 you convert the database rows to CSV format.
On lines 118-123 you append the SQL results and answer prompt to the conversation for context.
Finally on line 125 you call chat again to get the final answer from the LLM.

_Note: The `debug` function prints intermediate results. It is very helpful when debugging your application. To see debug prints, set the `DEBUG` environment variable to non-empty value (say `yes`)._

**Listing 5: chat**

The ollama API returns one word at a time, and you need to collect these words to a full reply.
The `chat` function simplifies this:

```go
075 func chat(ctx context.Context, client *api.Client, req *api.ChatRequest) (string, error) {
076     var buf strings.Builder
077 
078     err := client.Chat(ctx, req, func(resp api.ChatResponse) error {
079         buf.WriteString(resp.Message.Content)
080         return nil
081     })
082 
083     if err != nil {
084         return "", err
085     }
086 
087     return buf.String(), nil
088 }
```

Listing 5 shows the `chat` function that simplifies calling `ollama`.
One line 75 we use a `strings.Builder` to collect the results.
On lines 78-81 we call `Chat`, providing it a function to handle the response. On line 79 we append the response content to the buffer.
On line 87 we return the content of the buffer.

**Listing 6: Utility Functions**

```go
067 func debug(name, msg string) {
068     if !debugMode {
069         return
070     }
071 
072     fmt.Printf("DEBUG: %s:\n%s\n", name, msg)
073 }

075 func chat(ctx context.Context, client *api.Client, req *api.ChatRequest) (string, error) {
076     var buf strings.Builder
077 
078     err := client.Chat(ctx, req, func(resp api.ChatResponse) error {
079         buf.WriteString(resp.Message.Content)
080         return nil
081     })
082 
083     if err != nil {
084         return "", err
085     }
086 
087     return buf.String(), nil
088 }
...
030 func rowsToCSV(rows *sql.Rows) (string, error) {
031     cols, err := rows.Columns()
032     if err != nil {
033         return "", err
034     }
035 
036     var buf bytes.Buffer
037     wtr := csv.NewWriter(&buf)
038     if err := wtr.Write(cols); err != nil {
039         return "", err
040     }
041 
042     vals := make([]any, len(cols))
043     ptrs := make([]any, len(cols))
044     for i := range vals {
045         ptrs[i] = &vals[i]
046     }
047     strs := make([]string, len(cols))
048 
049     for rows.Next() {
050         if err := rows.Scan(ptrs...); err != nil {
051             return "", err
052         }
053 
054         for i, v := range vals {
055             strs[i] = fmt.Sprintf("%v", v)
056         }
057 
058         if err := wtr.Write(strs); err != nil {
059             return "", err
060         }
061     }
062     wtr.Flush()
063 
064     return buf.String(), nil
065 }
```

Listing 6 shows utility functions: `debug`, `chat`, and `rowsToCSV`.
The `chat` function wraps the Ollama API client call and accumulates the response content.
The `rowsToCSV` function converts SQL query results to CSV format for passing to the LLM.
Both `debug` and helper functions are included here for completeness.

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

In less than 200 lines of code we wrote a system that allow users to query a database using plain English.
Using LLMs from Go is simple, most of the time you'll spend tweaking prompts to get good results.
As usual, you can see the code and database [in the GitHub repo](https://github.com/353words/llm-sql)

How are you using LLMs in your Go code? Let me know at miki@ardanlabs.com
