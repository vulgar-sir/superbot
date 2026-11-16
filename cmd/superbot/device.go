package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type devicePollResponse struct {
	Job *struct {
		ID      string `json:"id"`
		Kind    string `json:"kind"`
		Command string `json:"command"`
	} `json:"job"`
}

func runDevice(args []string) error {
	flags := flag.NewFlagSet("device", flag.ContinueOnError)
	server := flags.String("server", "", "superbot server URL, e.g. http://202.189.8.109:32608")
	token := flags.String("token", "", "device token created in Settings → 电脑")
	interval := flags.Duration("interval", 2*time.Second, "poll interval")
	once := flags.Bool("once", false, "poll once and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*server) == "" || strings.TrimSpace(*token) == "" {
		return fmt.Errorf("--server and --token are required")
	}
	base := strings.TrimRight(strings.TrimSpace(*server), "/")
	client := &http.Client{Timeout: 60 * time.Second}
	fmt.Printf("superbot device polling %s (platform=%s)\n", base, runtime.GOOS)
	for {
		job, err := pollDeviceJob(context.Background(), client, base, *token)
		if err != nil {
			fmt.Fprintf(os.Stderr, "poll failed: %v\n", err)
			if *once {
				return err
			}
			time.Sleep(*interval)
			continue
		}
		if job.Job != nil {
			if job.Job.Kind == "screenshot" {
				path, shotErr := captureScreenshot()
				if shotErr != nil {
					_ = reportDeviceJob(context.Background(), client, base, *token, job.Job.ID, "", shotErr.Error())
				} else if uploadErr := uploadDeviceArtifact(context.Background(), client, base, *token, job.Job.ID, path); uploadErr != nil {
					_ = reportDeviceJob(context.Background(), client, base, *token, job.Job.ID, "", uploadErr.Error())
				} else {
					_ = reportDeviceJob(context.Background(), client, base, *token, job.Job.ID, "screenshot uploaded", "")
				}
				_ = os.Remove(path)
			} else {
				output, jobErr := executeDeviceJob(job.Job.Kind, job.Job.Command)
				if err := reportDeviceJob(context.Background(), client, base, *token, job.Job.ID, output, jobErr); err != nil {
					fmt.Fprintf(os.Stderr, "report failed: %v\n", err)
				}
			}
			fmt.Printf("job %s (%s) finished\n", job.Job.ID, job.Job.Kind)
		}
		if *once {
			return nil
		}
		time.Sleep(*interval)
	}
}

func pollDeviceJob(ctx context.Context, client *http.Client, base, token string) (devicePollResponse, error) {
	var out devicePollResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/device/poll", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("poll HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

func captureScreenshot() (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("screenshot is currently implemented on Windows only")
	}
	script := `Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$b=[System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$bmp=New-Object System.Drawing.Bitmap $b.Width,$b.Height
$g=[System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($b.Location,[System.Drawing.Point]::Empty,$b.Size)
$path=Join-Path $env:TEMP ("superbot-shot-"+[guid]::NewGuid().ToString()+".png")
$bmp.Save($path,[System.Drawing.Imaging.ImageFormat]::Png)
$g.Dispose()
$bmp.Dispose()
Write-Output $path`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("capture failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
	path := strings.TrimSpace(string(output))
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("screenshot file missing: %w", err)
	}
	return path, nil
}

func uploadDeviceArtifact(ctx context.Context, client *http.Client, base, token, jobID, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/device/jobs/"+jobID+"/artifact", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("artifact HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func executeDeviceJob(kind, command string) (string, string) {
	if kind != "shell" {
		return "", "unsupported job kind: " + kind
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	output, err := cmd.CombinedOutput()
	text := string(output)
	if len(text) > 256*1024 {
		text = text[:256*1024] + "\n…[truncated]"
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return text, "command timed out after 120s"
		}
		return text, err.Error()
	}
	return text, ""
}

func reportDeviceJob(ctx context.Context, client *http.Client, base, token, jobID, output, jobErr string) error {
	payload, _ := json.Marshal(map[string]string{"output": output, "error": jobErr})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/device/jobs/"+jobID+"/result", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("report HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
