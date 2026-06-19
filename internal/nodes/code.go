package nodes

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Code struct{}

func (Code) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	language := strings.ToLower(stringParam(in.Node.Parameters, "language"))
	switch language {
	case "python", "pythonnative":
		return executePythonCode(ctx, in)
	case "go", "golang":
		return executeGoCode(ctx, in)
	}
	return executeJavaScriptCode(ctx, in)
}

func executeJavaScriptCode(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source := stringParam(in.Node.Parameters, "jsCode", "functionCode", "code")
	items := firstInput(in.InputData)
	if source == "" {
		return dataplane.MainOutput(items), nil
	}
	timeout := codeTimeout(in.Node.Parameters)
	mode := codeMode(in.Node.Parameters, in.Node.Type)
	if mode == "runOnceForEachItem" {
		output := make([]dataplane.Item, 0, len(items))
		for index, item := range items {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			result, err := runJavaScript(ctx, timeout, source, items, item, index, in.Node)
			if err != nil {
				return nil, err
			}
			if result == nil {
				output = append(output, item)
				continue
			}
			converted, err := codeItemsFromAny(result)
			if err != nil {
				return nil, err
			}
			output = append(output, converted...)
		}
		return dataplane.MainOutput(output), nil
	}
	current := dataplane.Item{JSON: map[string]any{}}
	if len(items) > 0 {
		current = items[0]
	}
	result, err := runJavaScript(ctx, timeout, source, items, current, 0, in.Node)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return dataplane.MainOutput(items), nil
	}
	converted, err := codeItemsFromAny(result)
	if err != nil {
		return nil, err
	}
	return dataplane.MainOutput(converted), nil
}

func runJavaScript(ctx context.Context, timeout time.Duration, source string, items []dataplane.Item, item dataplane.Item, index int, node dataplane.Node) (any, error) {
	vm := goja.New()
	jsItems := codeInputItems(items)
	jsItem := codeInputItem(item)
	_ = vm.Set("items", jsItems)
	_ = vm.Set("item", jsItem)
	_ = vm.Set("$json", item.JSON)
	_ = vm.Set("$binary", codeInputBinary(item.Binary))
	_ = vm.Set("$itemIndex", index)
	_ = vm.Set("$node", map[string]any{"id": node.ID, "name": node.Name, "type": node.Type})
	now := time.Now()
	if jsNow, err := vm.RunString(fmt.Sprintf("new Date(%d)", now.UnixMilli())); err == nil {
		_ = vm.Set("$now", jsNow)
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if jsToday, err := vm.RunString(fmt.Sprintf("new Date(%d)", today.UnixMilli())); err == nil {
		_ = vm.Set("$today", jsToday)
	}
	_ = vm.Set("$input", map[string]any{
		"all": func() []map[string]any {
			return jsItems
		},
		"first": func() any {
			if len(jsItems) == 0 {
				return nil
			}
			return jsItems[0]
		},
		"last": func() any {
			if len(jsItems) == 0 {
				return nil
			}
			return jsItems[len(jsItems)-1]
		},
		"item": func() any {
			return jsItem
		},
	})
	_ = vm.Set("console", map[string]any{
		"log":   func(...any) {},
		"error": func(...any) {},
		"warn":  func(...any) {},
	})
	_ = vm.Set("fetch", func(call goja.FunctionCall) goja.Value {
		return jsFetch(vm, ctx, timeout, call)
	})
	_ = vm.Set("require", goja.Undefined())
	_ = vm.Set("process", goja.Undefined())
	_ = vm.Set("$getWorkflowStaticData", func(scope string) map[string]any {
		return map[string]any{}
	})
	done := make(chan struct{})
	timer := time.AfterFunc(timeout, func() {
		vm.Interrupt("javascript code execution timed out")
	})
	defer timer.Stop()
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-done:
		}
	}()
	value, err := vm.RunString("(async function(){" + source + "\n})()")
	close(done)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if strings.Contains(err.Error(), "javascript code execution timed out") {
			return nil, fmt.Errorf("javascript code execution timed out")
		}
		return nil, err
	}
	if promise, ok := value.Export().(*goja.Promise); ok {
		switch promise.State() {
		case goja.PromiseStateFulfilled:
			value = promise.Result()
		case goja.PromiseStateRejected:
			return nil, fmt.Errorf("%v", promise.Result().Export())
		default:
			return nil, fmt.Errorf("javascript promise did not settle")
		}
	}
	if goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, nil
	}
	return value.Export(), nil
}

func jsFetch(vm *goja.Runtime, ctx context.Context, timeout time.Duration, call goja.FunctionCall) goja.Value {
	if len(call.Arguments) == 0 {
		panic(vm.NewGoError(fmt.Errorf("fetch requires a URL")))
	}
	url := call.Arguments[0].String()
	method := http.MethodGet
	headers := map[string]string{}
	var body io.Reader
	if len(call.Arguments) > 1 && !goja.IsUndefined(call.Arguments[1]) && !goja.IsNull(call.Arguments[1]) {
		options := call.Arguments[1].ToObject(vm)
		if value := options.Get("method"); !goja.IsUndefined(value) && !goja.IsNull(value) {
			method = strings.ToUpper(value.String())
		}
		if value := options.Get("headers"); !goja.IsUndefined(value) && !goja.IsNull(value) {
			for _, key := range value.ToObject(vm).Keys() {
				headers[key] = value.ToObject(vm).Get(key).String()
			}
		}
		if value := options.Get("body"); !goja.IsUndefined(value) && !goja.IsNull(value) {
			switch typed := value.Export().(type) {
			case string:
				body = strings.NewReader(typed)
			case []byte:
				body = bytes.NewReader(typed)
			default:
				encoded, err := json.Marshal(typed)
				if err != nil {
					panic(vm.NewGoError(err))
				}
				body = bytes.NewReader(encoded)
				if _, ok := headers["content-type"]; !ok {
					headers["content-type"] = "application/json"
				}
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		panic(vm.NewGoError(err))
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		panic(vm.NewGoError(err))
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(vm.NewGoError(err))
	}
	response := vm.NewObject()
	_ = response.Set("status", resp.StatusCode)
	_ = response.Set("statusText", resp.Status)
	_ = response.Set("ok", resp.StatusCode >= 200 && resp.StatusCode < 300)
	_ = response.Set("headers", map[string]any{
		"content-type": resp.Header.Get("Content-Type"),
	})
	_ = response.Set("text", func() string {
		return string(payload)
	})
	_ = response.Set("json", func() any {
		var decoded any
		if err := json.Unmarshal(payload, &decoded); err != nil {
			panic(vm.NewGoError(err))
		}
		return decoded
	})
	return response
}

func executePythonCode(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	source := stringParam(in.Node.Parameters, "pythonCode", "code")
	if source == "" {
		return dataplane.MainOutput(firstInput(in.InputData)), nil
	}
	pythonBin, err := pythonBinary()
	if err != nil {
		return nil, err
	}
	timeout := codeTimeout(in.Node.Parameters)
	mode := codeMode(in.Node.Parameters, in.Node.Type)
	if mode == "runOnceForEachItem" {
		items := firstInput(in.InputData)
		output := make([]dataplane.Item, 0, len(items))
		worker, err := startPythonWorker(ctx, pythonBin, source, items, in.Node)
		if err != nil {
			return nil, err
		}
		defer worker.close()
		for index, item := range items {
			result, err := worker.call(ctx, timeout, item, index)
			if err != nil {
				return nil, err
			}
			output = append(output, result...)
		}
		return dataplane.MainOutput(output), nil
	}
	items := firstInput(in.InputData)
	current := dataplane.Item{JSON: map[string]any{}}
	if len(items) > 0 {
		current = items[0]
	}
	result, err := runPython(ctx, pythonBin, timeout, source, items, current, 0, in.Node)
	if err != nil {
		return nil, err
	}
	return dataplane.MainOutput(result), nil
}

type pythonWorker struct {
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	pipe   io.WriteCloser
	stdout *bufio.Reader
	stderr *bytes.Buffer
	items  []dataplane.Item
	node   dataplane.Node
}

func startPythonWorker(ctx context.Context, pythonBin string, source string, items []dataplane.Item, node dataplane.Node) (*pythonWorker, error) {
	cmd := exec.CommandContext(ctx, pythonBin, pythonCommandArgs(pythonBin, "-c", pythonWorkerScript(source))...)
	cmd.Env = pythonEnvironment()
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &pythonWorker{
		cmd:    cmd,
		stdin:  bufio.NewWriter(stdinPipe),
		pipe:   stdinPipe,
		stdout: bufio.NewReader(stdoutPipe),
		stderr: stderr,
		items:  items,
		node:   node,
	}, nil
}

func (worker *pythonWorker) call(ctx context.Context, timeout time.Duration, item dataplane.Item, index int) ([]dataplane.Item, error) {
	payload := pythonPayload(worker.items, item, index, worker.node)
	input, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	type response struct {
		line string
		err  error
	}
	done := make(chan response, 1)
	go func() {
		if _, err := worker.stdin.WriteString(string(input) + "\n"); err != nil {
			done <- response{err: err}
			return
		}
		if err := worker.stdin.Flush(); err != nil {
			done <- response{err: err}
			return
		}
		line, err := worker.stdout.ReadString('\n')
		done <- response{line: line, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		worker.kill()
		return nil, ctx.Err()
	case <-timer.C:
		worker.kill()
		return nil, fmt.Errorf("python code execution timed out")
	case result := <-done:
		if result.err != nil {
			message := strings.TrimSpace(worker.stderr.String())
			if message != "" {
				return nil, fmt.Errorf("python worker failed: %s", message)
			}
			return nil, result.err
		}
		if len(result.line) > 10*1024*1024 {
			worker.kill()
			return nil, fmt.Errorf("python code output exceeds 10MB")
		}
		return decodePythonWorkerOutput([]byte(result.line))
	}
}

func (worker *pythonWorker) close() {
	if worker == nil || worker.cmd == nil {
		return
	}
	if worker.pipe != nil {
		_ = worker.pipe.Close()
	}
	_ = worker.cmd.Wait()
}

func (worker *pythonWorker) kill() {
	if worker == nil || worker.cmd == nil || worker.cmd.Process == nil {
		return
	}
	_ = worker.cmd.Process.Kill()
}

func runPython(ctx context.Context, pythonBin string, timeout time.Duration, source string, items []dataplane.Item, item dataplane.Item, index int, node dataplane.Node) ([]dataplane.Item, error) {
	payload := pythonPayload(items, item, index, node)
	input, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(timeoutCtx, pythonBin, pythonCommandArgs(pythonBin, "-c", pythonScript(source))...)
	cmd.Env = pythonEnvironment()
	cmd.Stdin = bytes.NewReader(input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if timeoutCtx.Err() != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("python code execution timed out")
		}
		return nil, fmt.Errorf("python code execution failed: %s", strings.TrimSpace(stderr.String()))
	}
	if stdout.Len() > 10*1024*1024 {
		return nil, fmt.Errorf("python code output exceeds 10MB")
	}
	var decoded any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		return nil, fmt.Errorf("parse python output: %w", err)
	}
	return codeItemsFromAny(decoded)
}

func pythonPayload(items []dataplane.Item, item dataplane.Item, index int, node dataplane.Node) map[string]any {
	return map[string]any{
		"items":     items,
		"item":      item,
		"itemIndex": index,
		"node":      map[string]any{"id": node.ID, "name": node.Name, "type": node.Type},
	}
}

func pythonScript(source string) string {
	return `import datetime, json, sys, traceback
payload = json.load(sys.stdin)
items = payload.get("items") or []
item = payload.get("item") or {"json": {}}
json_data = item.get("json") or {}
binary = item.get("binary") or {}
item_index = payload.get("itemIndex", 0)
node = payload.get("node") or {}
now = datetime.datetime.utcnow().isoformat() + "Z"
today = datetime.date.today().isoformat()
class ItemWrapper:
    def __init__(self, data):
        self.data = data or {}
        self.json = self.data.get("json") or {}
        self.binary = self.data.get("binary") or {}
    def __getitem__(self, key):
        return self.data.get(key)
class InputWrapper:
    def all(self):
        return [ItemWrapper(value) for value in items]
    def first(self):
        return ItemWrapper(items[0]) if items else None
    def last(self):
        return ItemWrapper(items[-1]) if items else None
    def item(self):
        return ItemWrapper(item)
input = InputWrapper()
def __n8n_user():
` + indentPython(pythonUserSource(source)) + `
try:
    result = __n8n_user()
    print(json.dumps(result))
except Exception as exc:
    print(json.dumps({"error": str(exc), "traceback": traceback.format_exc()}), file=sys.stderr)
    sys.exit(1)
`
}

func pythonWorkerScript(source string) string {
	return `import datetime, json, sys, traceback
items = []
item = {"json": {}}
json_data = {}
binary = {}
item_index = 0
node = {}
now = datetime.datetime.utcnow().isoformat() + "Z"
today = datetime.date.today().isoformat()
class ItemWrapper:
    def __init__(self, data):
        self.data = data or {}
        self.json = self.data.get("json") or {}
        self.binary = self.data.get("binary") or {}
    def __getitem__(self, key):
        return self.data.get(key)
class InputWrapper:
    def all(self):
        return [ItemWrapper(value) for value in items]
    def first(self):
        return ItemWrapper(items[0]) if items else None
    def last(self):
        return ItemWrapper(items[-1]) if items else None
    def item(self):
        return ItemWrapper(item)
input = InputWrapper()
def __n8n_user():
` + indentPython(pythonUserSource(source)) + `
for line in sys.stdin:
    try:
        payload = json.loads(line)
        items = payload.get("items") or []
        item = payload.get("item") or {"json": {}}
        json_data = item.get("json") or {}
        binary = item.get("binary") or {}
        item_index = payload.get("itemIndex", 0)
        node = payload.get("node") or {}
        now = datetime.datetime.utcnow().isoformat() + "Z"
        today = datetime.date.today().isoformat()
        result = __n8n_user()
        print(json.dumps({"ok": True, "result": result}), flush=True)
    except Exception as exc:
        print(json.dumps({"ok": False, "error": str(exc), "traceback": traceback.format_exc()}), flush=True)
`
}

func decodePythonWorkerOutput(data []byte) ([]dataplane.Item, error) {
	decoded := map[string]any{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("parse python output: %w", err)
	}
	if ok, _ := decoded["ok"].(bool); !ok {
		return nil, fmt.Errorf("python code execution failed: %v", decoded["error"])
	}
	return codeItemsFromAny(decoded["result"])
}

func pythonEnvironment() []string {
	envMap := map[string]string{}
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		key := parts[0]
		value := ""
		if len(parts) == 2 {
			value = parts[1]
		}
		envMap[key] = value
	}
	envMap["PYTHONDONTWRITEBYTECODE"] = "1"
	envMap["PYTHONUNBUFFERED"] = "1"
	envMap["PYTHONPATH"] = ""
	if strings.TrimSpace(envMap["HOME"]) == "" {
		envMap["HOME"] = os.TempDir()
	}
	env := make([]string, 0, len(envMap))
	for key, value := range envMap {
		env = append(env, key+"="+value)
	}
	return env
}

func pythonUserSource(source string) string {
	replacements := []struct {
		from string
		to   string
	}{
		{"$input", "input"},
		{"$json", "json_data"},
		{"$binary", "binary"},
		{"$itemIndex", "item_index"},
		{"$node", "node"},
		{"$now", "now"},
		{"$today", "today"},
	}
	for _, replacement := range replacements {
		source = strings.ReplaceAll(source, replacement.from, replacement.to)
	}
	return source
}

func indentPython(source string) string {
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

func pythonBinary() (string, error) {
	for _, candidate := range []string{os.Getenv("N8N_TURBO_PYTHON_BIN"), "python3", "python", "py"} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			if resolved, ok := resolvePythonBinary(path); ok {
				return resolved, nil
			}
		}
	}
	return "", fmt.Errorf("python code execution requires python3 or N8N_TURBO_PYTHON_BIN")
}

func resolvePythonBinary(path string) (string, bool) {
	base := strings.ToLower(filepath.Base(path))
	if base == "py" || base == "py.exe" {
		resolved, err := resolvePyLauncher(path)
		if err != nil {
			return "", false
		}
		return resolved, pythonWorks(resolved)
	}
	return path, pythonWorks(path)
}

func resolvePyLauncher(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-3", "-c", "import sys; print(sys.executable)")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	resolved := strings.TrimSpace(string(output))
	if resolved == "" {
		return "", fmt.Errorf("py launcher returned empty executable path")
	}
	return resolved, nil
}

func pythonWorks(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, pythonCommandArgs(path, "--version")...)
	return cmd.Run() == nil
}

func pythonCommandArgs(path string, args ...string) []string {
	command := []string{}
	base := strings.ToLower(filepath.Base(path))
	if base == "py" || base == "py.exe" {
		command = append(command, "-3")
	}
	command = append(command, args...)
	return command
}

func executeGoCode(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	source := stringParam(in.Node.Parameters, "goCode", "code")
	if source == "" {
		return dataplane.MainOutput(firstInput(in.InputData)), nil
	}
	goBin, err := goBinary()
	if err != nil {
		return nil, err
	}
	timeout := codeTimeout(in.Node.Parameters)
	mode := codeMode(in.Node.Parameters, in.Node.Type)
	items := firstInput(in.InputData)
	if mode == "runOnceForEachItem" {
		output := make([]dataplane.Item, 0, len(items))
		for index, item := range items {
			result, err := runGo(ctx, goBin, timeout, source, items, item, index, in.Node)
			if err != nil {
				return nil, err
			}
			output = append(output, result...)
		}
		return dataplane.MainOutput(output), nil
	}
	current := dataplane.Item{JSON: map[string]any{}}
	if len(items) > 0 {
		current = items[0]
	}
	result, err := runGo(ctx, goBin, timeout, source, items, current, 0, in.Node)
	if err != nil {
		return nil, err
	}
	return dataplane.MainOutput(result), nil
}

func runGo(ctx context.Context, goBin string, timeout time.Duration, source string, items []dataplane.Item, item dataplane.Item, index int, node dataplane.Node) ([]dataplane.Item, error) {
	payload := pythonPayload(items, item, index, node)
	input, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp("", "n8n-turbo-go-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	mainPath := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(mainPath, []byte(goScript(source)), 0600); err != nil {
		return nil, err
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(timeoutCtx, goBin, "run", mainPath)
	cmd.Dir = tempDir
	cmd.Env = goEnvironment()
	cmd.Stdin = bytes.NewReader(input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if timeoutCtx.Err() != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if detail != "" {
				return nil, fmt.Errorf("go code execution timed out: %s", detail)
			}
			return nil, fmt.Errorf("go code execution timed out")
		}
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("go code execution failed: %s", detail)
	}
	if stdout.Len() > 10*1024*1024 {
		return nil, fmt.Errorf("go code output exceeds 10MB")
	}
	var decoded any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		return nil, fmt.Errorf("parse go output: %w", err)
	}
	return codeItemsFromAny(decoded)
}

func goScript(source string) string {
	return `package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	_ = io.EOF
	_ = http.MethodGet
	_ = url.Values{}
	_ = strconv.IntSize
	_ = strings.TrimSpace
)

type InputHelper struct {
	items []map[string]any
	item  map[string]any
}

func (h *InputHelper) All() []map[string]any {
	return h.items
}

func (h *InputHelper) First() map[string]any {
	if len(h.items) == 0 {
		return nil
	}
	return h.items[0]
}

func (h *InputHelper) Last() map[string]any {
	if len(h.items) == 0 {
		return nil
	}
	return h.items[len(h.items)-1]
}

func (h *InputHelper) Item() map[string]any {
	return h.item
}

func asMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

func asItems(value any) []map[string]any {
	if value == nil {
		return []map[string]any{}
	}
	if items, ok := value.([]any); ok {
		result := make([]map[string]any, 0, len(items))
		for _, item := range items {
			result = append(result, asMap(item))
		}
		return result
	}
	if items, ok := value.([]map[string]any); ok {
		return items
	}
	return []map[string]any{}
}

func user(items []map[string]any, item map[string]any, jsonData map[string]any, binary map[string]any, itemIndex int, node map[string]any, input *InputHelper, now string, today string) (any, error) {
` + indentGo(source) + `
}

func main() {
	decoder := json.NewDecoder(os.Stdin)
	payload := map[string]any{}
	if err := decoder.Decode(&payload); err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
	items := asItems(payload["items"])
	item := asMap(payload["item"])
	jsonData := asMap(item["json"])
	binary := asMap(item["binary"])
	itemIndex := 0
	switch typed := payload["itemIndex"].(type) {
	case float64:
		itemIndex = int(typed)
	case int:
		itemIndex = typed
	}
	node := asMap(payload["node"])
	input := &InputHelper{items: items, item: item}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	today := time.Now().Format("2006-01-02")
	result, err := user(items, item, jsonData, binary, itemIndex, node, input, now, today)
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
}
`
}

func indentGo(source string) string {
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		lines[i] = "\t" + line
	}
	return strings.Join(lines, "\n")
}

func goEnvironment() []string {
	envMap := map[string]string{}
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		key := parts[0]
		value := ""
		if len(parts) == 2 {
			value = parts[1]
		}
		envMap[key] = value
	}
	envMap["GOWORK"] = "off"
	if strings.TrimSpace(envMap["HOME"]) == "" {
		envMap["HOME"] = os.TempDir()
	}
	env := make([]string, 0, len(envMap))
	for key, value := range envMap {
		env = append(env, key+"="+value)
	}
	return env
}

func goBinary() (string, error) {
	for _, candidate := range []string{os.Getenv("N8N_TURBO_GO_BIN"), "go"} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			if goWorks(path) {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("go code execution requires Go or N8N_TURBO_GO_BIN")
}

func goWorks(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "version")
	return cmd.Run() == nil
}

func codeMode(params map[string]any, nodeType string) string {
	mode := stringParam(params, "mode")
	if mode == "" {
		mode = stringParam(params, "executeMode")
	}
	if mode == "" && nodeType == "n8n-nodes-base.functionItem" {
		return "runOnceForEachItem"
	}
	if mode == "" || mode == "runOnceForAllItems" {
		return "runOnceForAllItems"
	}
	return mode
}

func codeTimeout(params map[string]any) time.Duration {
	if milliseconds := intParam(params, "timeoutMilliseconds", 0); milliseconds > 0 {
		return time.Duration(milliseconds) * time.Millisecond
	}
	timeout := time.Duration(intParam(params, "timeoutSeconds", 10)) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return timeout
}

func codeItemsFromAny(value any) ([]dataplane.Item, error) {
	if value == nil {
		return []dataplane.Item{}, nil
	}
	if payload, ok := value.(map[string]any); ok {
		if payload["error"] != nil {
			return nil, fmt.Errorf("%v", payload["error"])
		}
	}
	if item, ok := itemFromAny(value); ok {
		return []dataplane.Item{item}, nil
	}
	values, ok := value.([]any)
	if !ok {
		return []dataplane.Item{{JSON: map[string]any{"result": value}}}, nil
	}
	items := make([]dataplane.Item, 0, len(values))
	for _, current := range values {
		item, ok := itemFromAny(current)
		if !ok {
			return nil, fmt.Errorf("code node output item must be an object")
		}
		items = append(items, item)
	}
	return items, nil
}

func itemFromAny(value any) (dataplane.Item, bool) {
	switch typed := value.(type) {
	case dataplane.Item:
		return typed, true
	case map[string]any:
		if rawJSON, ok := typed["json"]; ok {
			jsonMap, ok := rawJSON.(map[string]any)
			if !ok {
				return dataplane.Item{}, false
			}
			item := dataplane.Item{JSON: jsonMap}
			if rawBinary, ok := typed["binary"].(map[string]any); ok {
				item.Binary = map[string]dataplane.Binary{}
				for key, value := range rawBinary {
					bytes, _ := json.Marshal(value)
					var binary dataplane.Binary
					_ = json.Unmarshal(bytes, &binary)
					item.Binary[key] = binary
				}
			}
			return item, true
		}
		return dataplane.Item{JSON: typed}, true
	}
	return dataplane.Item{}, false
}

func codeInputItems(items []dataplane.Item) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, codeInputItem(item))
	}
	return result
}

func codeInputItem(item dataplane.Item) map[string]any {
	return map[string]any{"json": item.JSON, "binary": codeInputBinary(item.Binary), "pairedItem": item.PairedItem}
}

func codeInputBinary(binary map[string]dataplane.Binary) map[string]any {
	if binary == nil {
		return nil
	}
	result := make(map[string]any, len(binary))
	for key, value := range binary {
		result[key] = map[string]any{
			"id":            value.ID,
			"data":          value.Data,
			"mimeType":      value.MimeType,
			"fileType":      value.FileType,
			"fileName":      value.FileName,
			"fileSize":      value.FileSize,
			"fileExtension": value.FileExtension,
			"directory":     value.Directory,
		}
	}
	return result
}
