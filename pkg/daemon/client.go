package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

type Client struct {
	conn net.Conn
	nextID int
}

func Dial(runDir string) (*Client, error) {
	sock := SockPath(runDir)
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", sock, err)
	}
	return &Client{conn: conn}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) Call(method string, params any, result any) error {
	c.nextID++
	var raw json.RawMessage
	if params != nil {
		b, _ := json.Marshal(params)
		raw = b
	}
	req := Request{ID: c.nextID, Method: method, Params: raw}
	b, _ := json.Marshal(req)
	b = append(b, '\n')
	if _, err := c.conn.Write(b); err != nil {
		return err
	}
	scanner := bufio.NewScanner(c.conn)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	if !scanner.Scan() {
		return fmt.Errorf("no response")
	}
	var res Response
	if err := json.Unmarshal(scanner.Bytes(), &res); err != nil {
		return err
	}
	if res.Error != "" {
		return errors.New(res.Error)
	}
	if result != nil && len(res.Result) > 0 {
		return json.Unmarshal(res.Result, result)
	}
	return nil
}

func ListRuns(workDir string) ([]Status, error) {
	base := filepath.Join(workDir, ".syla", "runs")
	ents, err := os.ReadDir(base)
	if err != nil {
		return nil, nil
	}
	var out []Status
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		runDir := filepath.Join(base, e.Name())
		// try to dial, if alive get status, else read checkpoint
		c, err := Dial(runDir)
		if err == nil {
			var st Status
			if err := c.Call("Status", nil, &st); err == nil {
				out = append(out, st)
			}
			_ = c.Close()
			continue
		}
		// fallback: read checkpoint.json
		data, err := os.ReadFile(filepath.Join(runDir, "checkpoint.json"))
		if err != nil {
			continue
		}
		// Try to unmarshal as Status first (if checkpoint was written as Status)
		var st Status
		_ = json.Unmarshal(data, &st)
		// Also try as loop.Checkpoint to get provider/duration correctly
		var cp struct {
			RunID     string    `json:"run_id"`
			RunDir    string    `json:"run_dir"`
			Provider  string    `json:"provider"`
			Status    string    `json:"status"`
			Iteration int       `json:"iteration"`
			StartedAt time.Time `json:"started_at"`
			UpdatedAt time.Time `json:"updated_at"`
			StopReason string   `json:"stop_reason"`
		}
		_ = json.Unmarshal(data, &cp)
		if st.RunID == "" {
			st.RunID = cp.RunID
			if st.RunID == "" {
				st.RunID = e.Name()
			}
		}
		if st.RunDir == "" {
			st.RunDir = cp.RunDir
			if st.RunDir == "" {
				st.RunDir = runDir
			}
		}
		if st.Provider == "" {
			st.Provider = cp.Provider
		}
		if st.Status == "" || st.Status == "unknown" {
			if cp.Status != "" {
				st.Status = cp.Status
				if cp.StopReason != "" && cp.Status == "completed" {
					st.Status = cp.StopReason
				}
			} else {
				st.Status = "unknown"
			}
		}
		if st.Iteration == 0 {
			st.Iteration = cp.Iteration
		}
		// Duration: if completed/aborted, use total, else current
		if st.Elapsed == "" && st.Duration == "" {
			if !cp.StartedAt.IsZero() {
				var dur time.Duration
				if cp.Status == "completed" || cp.Status == "failed" || cp.Status == "aborted" || cp.StopReason != "" {
					if !cp.UpdatedAt.IsZero() {
						dur = cp.UpdatedAt.Sub(cp.StartedAt)
					} else {
						dur = time.Since(cp.StartedAt)
					}
				} else {
					dur = time.Since(cp.StartedAt)
				}
				st.Elapsed = dur.Truncate(time.Second).String()
				st.Duration = st.Elapsed
			}
		}
		if st.StopReason == "" {
			st.StopReason = cp.StopReason
		}
		out = append(out, st)
	}
	return out, nil
}
