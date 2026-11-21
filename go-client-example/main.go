package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client communicates with TursoDB through the Hrana HTTP pipeline.
type Client struct {
	endpoint   string
	httpClient *http.Client
}

// NewClient creates a new TursoDB HTTP client.
func NewClient(serverURL string) *Client {
	serverURL = strings.TrimRight(serverURL, "/")

	if !strings.HasSuffix(serverURL, "/v2/pipeline") {
		serverURL += "/v2/pipeline"
	}

	return &Client{
		endpoint: serverURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// HranaValue represents a typed SQL value.
type HranaValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value,omitempty"`
}

func Text(value string) HranaValue {
	return makeValue("text", value)
}

func Integer(value int64) HranaValue {
	return makeValue("integer", strconv.FormatInt(value, 10))
}

func Float(value float64) HranaValue {
	return makeValue("float", strconv.FormatFloat(value, 'f', -1, 64))
}

func Null() HranaValue {
	return HranaValue{Type: "null"}
}

func makeValue(valueType string, value any) HranaValue {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}

	return HranaValue{
		Type:  valueType,
		Value: data,
	}
}

// SQL request structures.
type pipelineRequest struct {
	Baton    any              `json:"baton"`
	Requests []executeRequest `json:"requests"`
}

type executeRequest struct {
	Type string    `json:"type"`
	Stmt statement `json:"stmt"`
}

type statement struct {
	SQL      string       `json:"sql"`
	Args     []HranaValue `json:"args,omitempty"`
	WantRows bool         `json:"want_rows,omitempty"`
}

// Pipeline response structures.
type pipelineResponse struct {
	Results []pipelineResult `json:"results"`
}

type pipelineResult struct {
	Type     string          `json:"type"`
	Response json.RawMessage `json:"response"`
}

type executeResponse struct {
	Type   string        `json:"type"`
	Result executeResult `json:"result"`
}

type executeResult struct {
	Columns          []column        `json:"cols"`
	Rows             [][]HranaValue  `json:"rows"`
	AffectedRowCount int64           `json:"affected_row_count"`
	LastInsertRowID  json.RawMessage `json:"last_insert_rowid"`
}

type column struct {
	Name     string `json:"name"`
	DeclType string `json:"decltype"`
}

// QueryResult contains the returned SQL rows.
type QueryResult struct {
	Columns []column
	Rows    [][]any
}

// ExecResult contains the result of an INSERT, UPDATE or DELETE.
type ExecResult struct {
	AffectedRows    int64
	LastInsertRowID *int64
}

// execute sends an SQL statement to the TursoDB server.
func (c *Client) execute(
	ctx context.Context,
	sql string,
	args []HranaValue,
	wantRows bool,
) (*executeResult, error) {
	payload := pipelineRequest{
		Baton: nil,
		Requests: []executeRequest{
			{
				Type: "execute",
				Stmt: statement{
					SQL:      sql,
					Args:     args,
					WantRows: wantRows,
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"HTTP %d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}

	var result pipelineResponse

	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("decode pipeline response: %w", err)
	}

	if len(result.Results) == 0 {
		return nil, fmt.Errorf("server returned no results")
	}

	pipelineResult := result.Results[0]

	if pipelineResult.Type != "ok" {
		return nil, fmt.Errorf(
			"SQL execution failed: %s",
			string(pipelineResult.Response),
		)
	}

	var execution executeResponse

	if err := json.Unmarshal(
		pipelineResult.Response,
		&execution,
	); err != nil {
		return nil, fmt.Errorf("decode SQL result: %w", err)
	}

	if execution.Type != "execute" {
		return nil, fmt.Errorf(
			"unexpected response type: %s",
			execution.Type,
		)
	}

	return &execution.Result, nil
}

// Query executes a SQL statement and returns its rows.
func (c *Client) Query(
	ctx context.Context,
	sql string,
	args ...HranaValue,
) (*QueryResult, error) {
	result, err := c.execute(ctx, sql, args, true)
	if err != nil {
		return nil, err
	}

	queryResult := &QueryResult{
		Columns: result.Columns,
		Rows:    make([][]any, 0, len(result.Rows)),
	}

	for _, row := range result.Rows {
		decodedRow := make([]any, len(row))

		for i, value := range row {
			decoded, err := decodeValue(value)
			if err != nil {
				return nil, fmt.Errorf(
					"decode column %d: %w",
					i,
					err,
				)
			}

			decodedRow[i] = decoded
		}

		queryResult.Rows = append(queryResult.Rows, decodedRow)
	}

	return queryResult, nil
}

// Exec executes INSERT, UPDATE or DELETE statements.
func (c *Client) Exec(
	ctx context.Context,
	sql string,
	args ...HranaValue,
) (*ExecResult, error) {
	result, err := c.execute(ctx, sql, args, false)
	if err != nil {
		return nil, err
	}

	execResult := &ExecResult{
		AffectedRows: result.AffectedRowCount,
	}

	if len(result.LastInsertRowID) > 0 &&
		string(result.LastInsertRowID) != "null" {

		var id int64

		var number json.Number

		if err := json.Unmarshal(
			result.LastInsertRowID,
			&number,
		); err == nil {
			id, err = strconv.ParseInt(number.String(), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse last insert row ID: %w", err)
			}
		} else {
			var text string

			if err := json.Unmarshal(
				result.LastInsertRowID,
				&text,
			); err != nil {
				return nil, fmt.Errorf("decode last insert row ID: %w", err)
			}

			id, err = strconv.ParseInt(text, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse last insert row ID: %w", err)
			}
		}

		execResult.LastInsertRowID = &id
	}

	return execResult, nil
}

// decodeValue converts Hrana values into Go values.
func decodeValue(value HranaValue) (any, error) {
	switch value.Type {
	case "null":
		return nil, nil

	case "text":
		var result string
		err := json.Unmarshal(value.Value, &result)
		return result, err

	case "integer":
		var result string

		if err := json.Unmarshal(value.Value, &result); err != nil {
			var number json.Number

			if err := json.Unmarshal(value.Value, &number); err != nil {
				return nil, err
			}

			result = number.String()
		}

		return strconv.ParseInt(result, 10, 64)

	case "float":
		var result string

		if err := json.Unmarshal(value.Value, &result); err != nil {
			var number json.Number

			if err := json.Unmarshal(value.Value, &number); err != nil {
				return nil, err
			}

			result = number.String()
		}

		return strconv.ParseFloat(result, 64)

	case "blob":
		var encoded string

		if err := json.Unmarshal(value.Value, &encoded); err != nil {
			return nil, err
		}

		return base64.StdEncoding.DecodeString(encoded)

	default:
		return nil, fmt.Errorf(
			"unsupported Hrana value type: %s",
			value.Type,
		)
	}
}

// printRows prints query results.
func printRows(result *QueryResult) {
	for _, row := range result.Rows {
		for i, value := range row {
			if i > 0 {
				fmt.Print(" | ")
			}

			fmt.Printf("%s=%v", result.Columns[i].Name, value)
		}

		fmt.Println()
	}
}

func main() {
	// Use the WSL IP instead of 127.0.0.1 when running
	// this client directly from Windows.
	serverURL := "http://127.0.0.1:8080"

	client := NewClient(serverURL)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	// ----------------------------------------
	// 1. SELECT
	// ----------------------------------------

	fmt.Println("1. SELECT")

	result, err := client.Query(
		ctx,
		"SELECT id, hostname, created_at FROM server_info",
	)
	if err != nil {
		fmt.Println("Query error:", err)
		return
	}

	printRows(result)

	// ----------------------------------------
	// 2. INSERT
	// ----------------------------------------

	fmt.Println("\n2. INSERT")

	hostname := fmt.Sprintf(
		"go-client-%d",
		time.Now().UnixNano(),
	)

	insertResult, err := client.Query(
		ctx,
		`INSERT INTO server_info (hostname)
		 VALUES (?)
		 RETURNING id, hostname, created_at`,
		Text(hostname),
	)
	if err != nil {
		fmt.Println("Insert error:", err)
		return
	}

	fmt.Println("Inserted records:")
	printRows(insertResult)

	// ----------------------------------------
	// 3. UPDATE
	// ----------------------------------------

	fmt.Println("\n3. UPDATE")

	updateResult, err := client.Query(
		ctx,
		`UPDATE server_info
		 SET hostname = ?
		 WHERE hostname = ?
		 RETURNING id, hostname, created_at`,
		Text("go-client-updated"),
		Text(hostname),
	)
	if err != nil {
		fmt.Println("Update error:", err)
		return
	}

	fmt.Println("Updated records:")
	printRows(updateResult)

	// ----------------------------------------
	// 4. VERIFY UPDATE
	// ----------------------------------------

	fmt.Println("\n4. VERIFY UPDATE")

	verifyResult, err := client.Query(
		ctx,
		"SELECT id, hostname, created_at FROM server_info WHERE hostname = ?",
		Text("go-client-updated"),
	)
	if err != nil {
		fmt.Println("Verification error:", err)
		return
	}

	printRows(verifyResult)

	// ----------------------------------------
	// 5. DELETE EXAMPLE
	// ----------------------------------------

	// Uncomment this block if you want to test DELETE.
	//
	// fmt.Println("\n5. DELETE")
	//
	// deleteResult, err := client.Query(
	//     ctx,
	//     "DELETE FROM server_info WHERE hostname = ? RETURNING id, hostname",
	//     Text("go-client-updated"),
	// )
	// if err != nil {
	//     fmt.Println("Delete error:", err)
	//     return
	// }
	//
	// fmt.Println("Deleted records:")
	// printRows(deleteResult)
}
