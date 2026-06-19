package nodes

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type ExecuteCommand struct{}

type ReadWriteFile struct{}

func (ExecuteCommand) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	params := executeCommandParams(in.Node.Parameters)
	command := params.Command
	if command == "" {
		return dataplane.MainOutput(firstInput(in.InputData)), nil
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	if params.ExecuteOnce {
		resolved := fmt.Sprint(resolveValue(in, items, 0, command))
		item, err := runShellCommand(ctx, params, resolved)
		if err != nil {
			return nil, err
		}
		return dataplane.MainOutput([]dataplane.Item{item}), nil
	}
	output := make([]dataplane.Item, 0, len(items))
	for index := range items {
		resolved := fmt.Sprint(resolveValue(in, items, index, command))
		item, err := runShellCommand(ctx, params, resolved)
		if err != nil {
			return nil, fmt.Errorf("execute command item %d: %w", index, err)
		}
		output = append(output, item)
	}
	return dataplane.MainOutput(output), nil
}

type executeCommandConfig struct {
	Command          string
	ExecuteOnce      bool
	WorkingDirectory string
	Timeout          time.Duration
	Environment      map[string]string
	MaxOutputSize    int64
}

type limitedCommandWriter struct {
	buffer  *bytes.Buffer
	limit   int64
	written int64
	err     error
}

func executeCommandParams(params map[string]any) executeCommandConfig {
	options := mergeObject(params["options"])
	timeoutMS := intParam(options, "timeout", intParam(params, "timeout", 60000))
	if timeoutMS <= 0 {
		timeoutMS = 60000
	}
	maxOutput := int64(intParam(options, "maxOutputSize", intParam(params, "maxOutputSize", 10*1024*1024)))
	if maxOutput <= 0 {
		maxOutput = 10 * 1024 * 1024
	}
	return executeCommandConfig{
		Command:          stringParam(params, "command"),
		ExecuteOnce:      boolParam(params, "executeOnce", boolParam(options, "executeOnce", false)),
		WorkingDirectory: firstNonEmptyNode(stringParam(options, "workingDirectory"), stringParam(params, "workingDirectory"), stringParam(options, "cwd"), stringParam(params, "cwd")),
		Timeout:          time.Duration(timeoutMS) * time.Millisecond,
		Environment:      executeCommandEnvironment(params, options),
		MaxOutputSize:    maxOutput,
	}
}

func runShellCommand(ctx context.Context, params executeCommandConfig, command string) (dataplane.Item, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, params.Timeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(timeoutCtx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(timeoutCtx, "sh", "-c", command)
	}
	if strings.TrimSpace(params.WorkingDirectory) != "" {
		cmd.Dir = filepath.Clean(params.WorkingDirectory)
	}
	if len(params.Environment) > 0 {
		env := cmd.Environ()
		for key, value := range params.Environment {
			env = append(env, key+"="+value)
		}
		cmd.Env = env
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stdoutWriter := &limitedCommandWriter{buffer: &stdout, limit: params.MaxOutputSize}
	stderrWriter := &limitedCommandWriter{buffer: &stderr, limit: params.MaxOutputSize}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	err := cmd.Run()
	if stdoutWriter.err != nil {
		return dataplane.Item{}, stdoutWriter.err
	}
	if stderrWriter.err != nil {
		return dataplane.Item{}, stderrWriter.err
	}
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		if timeoutCtx.Err() != nil {
			if ctx.Err() != nil {
				return dataplane.Item{}, ctx.Err()
			}
			return dataplane.Item{}, fmt.Errorf("command execution timed out after %s", params.Timeout)
		}
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
			return dataplane.Item{}, fmt.Errorf("command failed with exit code %d: stdout: %s stderr: %s", exitCode, stdout.String(), stderr.String())
		}
		return dataplane.Item{}, err
	}
	return dataplane.Item{JSON: map[string]any{"stdout": stdout.String(), "stderr": stderr.String(), "exitCode": exitCode}}, nil
}

func (w *limitedCommandWriter) Write(data []byte) (int, error) {
	if w.limit <= 0 {
		return w.buffer.Write(data)
	}
	if w.written+int64(len(data)) > w.limit {
		remaining := w.limit - w.written
		if remaining > 0 {
			_, _ = w.buffer.Write(data[:remaining])
		}
		w.written = w.limit
		w.err = fmt.Errorf("command output exceeds %d bytes", w.limit)
		return len(data), w.err
	}
	n, err := w.buffer.Write(data)
	w.written += int64(n)
	return n, err
}

func executeCommandEnvironment(params map[string]any, options map[string]any) map[string]string {
	env := map[string]string{}
	for _, source := range []any{params["environmentVariables"], options["environmentVariables"], params["env"], options["env"]} {
		for key, value := range executeCommandEnvSource(source) {
			env[key] = value
		}
	}
	return env
}

func executeCommandEnvSource(source any) map[string]string {
	result := map[string]string{}
	if source == nil {
		return result
	}
	if direct, ok := source.(map[string]any); ok {
		if values, ok := direct["values"].([]any); ok {
			return executeCommandEnvList(values)
		}
		if values, ok := direct["environmentVariables"].([]any); ok {
			return executeCommandEnvList(values)
		}
		for key, value := range direct {
			if key != "values" && key != "environmentVariables" {
				result[key] = fmt.Sprint(value)
			}
		}
		return result
	}
	if values, ok := source.([]any); ok {
		return executeCommandEnvList(values)
	}
	return result
}

func executeCommandEnvList(values []any) map[string]string {
	result := map[string]string{}
	for _, value := range values {
		entry := mergeObject(value)
		key := firstNonEmptyNode(stringParam(entry, "name"), stringParam(entry, "key"))
		if key == "" || strings.Contains(key, "=") {
			continue
		}
		result[key] = stringParam(entry, "value")
	}
	return result
}

func (ReadWriteFile) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	params := readWriteFileParams(in.Node.Parameters)
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	output := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		result, err := executeReadWriteFileItem(ctx, in, params, items, item, index)
		if err != nil {
			return nil, fmt.Errorf("readWriteFile item %d: %w", index, err)
		}
		output = append(output, result)
	}
	return dataplane.MainOutput(output), nil
}

type readWriteFileConfig struct {
	Operation       string
	FilePath        string
	NewPath         string
	DataProperty    string
	WriteToFile     string
	TextContent     any
	Append          bool
	ReturnObjType   string
	OutputProperty  string
	FileName        string
	AllowedPaths    []string
	MaxFileSize     int64
	CreateDirectory bool
}

func readWriteFileParams(params map[string]any) readWriteFileConfig {
	options := mergeObject(params["options"])
	maxSize := int64(intParam(options, "maxFileSize", intParam(params, "maxFileSize", 50*1024*1024)))
	if maxSize <= 0 {
		maxSize = 50 * 1024 * 1024
	}
	return readWriteFileConfig{
		Operation:       firstNonEmptyNode(stringParam(params, "operation"), "read"),
		FilePath:        stringParam(params, "filePath", "path"),
		NewPath:         stringParam(params, "newPath", "destinationPath", "targetPath"),
		DataProperty:    firstNonEmptyNode(stringParam(params, "dataPropertyName", "binaryPropertyName", "binaryProperty"), "data"),
		WriteToFile:     firstNonEmptyNode(stringParam(params, "writeToFile", "source"), "binary"),
		TextContent:     firstNonNil(params["textContent"], params["content"], params["data"]),
		Append:          boolParam(params, "appendToFile", boolParam(options, "appendToFile", false)),
		ReturnObjType:   firstNonEmptyNode(stringParam(options, "returnObjType"), stringParam(params, "returnObjType"), "binary"),
		OutputProperty:  firstNonEmptyNode(stringParam(options, "dataPropertyName"), stringParam(params, "outputPropertyName"), "data"),
		FileName:        stringParam(options, "fileName"),
		AllowedPaths:    readWriteAllowedPaths(params, options),
		MaxFileSize:     maxSize,
		CreateDirectory: boolParam(options, "createDirectory", boolParam(params, "createDirectory", true)),
	}
}

func executeReadWriteFileItem(ctx context.Context, in engine.ExecuteInput, params readWriteFileConfig, items []dataplane.Item, item dataplane.Item, index int) (dataplane.Item, error) {
	path, err := resolveReadWritePath(in, params.FilePath, items, index, params.AllowedPaths)
	if err != nil {
		return dataplane.Item{}, err
	}
	switch strings.ToLower(params.Operation) {
	case "read":
		return readFileItem(ctx, in, params, path)
	case "write":
		return writeFileItem(ctx, in, params, items, item, index, path)
	case "delete":
		return deleteFileItem(path)
	case "rename", "move":
		newPath, err := resolveReadWritePath(in, params.NewPath, items, index, params.AllowedPaths)
		if err != nil {
			return dataplane.Item{}, err
		}
		if err := os.MkdirAll(filepath.Dir(newPath), 0o750); err != nil {
			return dataplane.Item{}, err
		}
		if err := os.Rename(path, newPath); err != nil {
			return dataplane.Item{}, err
		}
		return dataplane.Item{JSON: map[string]any{"oldPath": path, "newPath": newPath, "moved": true}}, nil
	case "copy":
		newPath, err := resolveReadWritePath(in, params.NewPath, items, index, params.AllowedPaths)
		if err != nil {
			return dataplane.Item{}, err
		}
		written, err := copyFile(path, newPath)
		if err != nil {
			return dataplane.Item{}, err
		}
		return dataplane.Item{JSON: map[string]any{"srcPath": path, "dstPath": newPath, "bytesCopied": written}}, nil
	case "list":
		return listDirectoryItem(path)
	default:
		return dataplane.Item{}, fmt.Errorf("unsupported readWriteFile operation %s", params.Operation)
	}
}

func readFileItem(ctx context.Context, in engine.ExecuteInput, params readWriteFileConfig, path string) (dataplane.Item, error) {
	info, err := os.Stat(path)
	if err != nil {
		return dataplane.Item{}, err
	}
	if info.Size() > params.MaxFileSize {
		return dataplane.Item{}, fmt.Errorf("file %s exceeds max file size %d", path, params.MaxFileSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return dataplane.Item{}, err
	}
	mimeType := http.DetectContentType(data)
	fileName := firstNonEmptyNode(params.FileName, filepath.Base(path))
	if strings.EqualFold(params.ReturnObjType, "text") {
		return dataplane.Item{JSON: map[string]any{params.OutputProperty: string(data), "fileName": fileName, "mimeType": mimeType, "filePath": path, "fileSize": int64(len(data))}}, nil
	}
	binary := dataplane.Binary{Data: base64.StdEncoding.EncodeToString(data), MimeType: mimeType, FileName: fileName, FileSize: int64(len(data)), FileExtension: strings.TrimPrefix(filepath.Ext(fileName), ".")}
	if in.BinaryStore != nil {
		ref, err := in.BinaryStore.Put(ctx, mimeType, fileName, bytes.NewReader(data))
		if err != nil {
			return dataplane.Item{}, err
		}
		binary = binarydata.BinaryFromRef(ref)
	}
	return dataplane.Item{JSON: map[string]any{"filePath": path, "fileName": fileName, "fileSize": int64(len(data))}, Binary: map[string]dataplane.Binary{params.OutputProperty: binary}}, nil
}

func writeFileItem(ctx context.Context, in engine.ExecuteInput, params readWriteFileConfig, items []dataplane.Item, item dataplane.Item, index int, path string) (dataplane.Item, error) {
	if params.CreateDirectory {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return dataplane.Item{}, err
		}
	}
	flags := os.O_CREATE | os.O_WRONLY
	if params.Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	var data []byte
	if strings.EqualFold(params.WriteToFile, "text") {
		resolved := resolveValue(in, items, index, params.TextContent)
		data = []byte(fmt.Sprint(resolved))
	} else {
		binary, ok := item.Binary[params.DataProperty]
		if !ok {
			return dataplane.Item{}, fmt.Errorf("binary property %s not found", params.DataProperty)
		}
		read, err := binarydata.Read(ctx, in.BinaryStore, binary)
		if err != nil {
			return dataplane.Item{}, err
		}
		data = read
	}
	file, err := os.OpenFile(path, flags, 0o640)
	if err != nil {
		return dataplane.Item{}, err
	}
	defer file.Close()
	written, err := file.Write(data)
	if err != nil {
		return dataplane.Item{}, err
	}
	return dataplane.Item{JSON: map[string]any{"filePath": path, "bytesWritten": written, "appended": params.Append}}, nil
}

func deleteFileItem(path string) (dataplane.Item, error) {
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return dataplane.Item{JSON: map[string]any{"filePath": path, "deleted": false, "reason": "file not found"}}, nil
		}
		return dataplane.Item{}, err
	}
	return dataplane.Item{JSON: map[string]any{"filePath": path, "deleted": true}}, nil
}

func copyFile(source string, destination string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return 0, err
	}
	src, err := os.Open(source)
	if err != nil {
		return 0, err
	}
	defer src.Close()
	dst, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return 0, err
	}
	defer dst.Close()
	return io.Copy(dst, src)
}

func listDirectoryItem(path string) (dataplane.Item, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return dataplane.Item{}, err
	}
	files := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return dataplane.Item{}, err
		}
		files = append(files, map[string]any{"name": entry.Name(), "path": filepath.Join(path, entry.Name()), "isDir": entry.IsDir(), "size": info.Size()})
	}
	return dataplane.Item{JSON: map[string]any{"path": path, "files": files}}, nil
}

func resolveReadWritePath(in engine.ExecuteInput, raw any, items []dataplane.Item, index int, allowed []string) (string, error) {
	resolved := strings.TrimSpace(fmt.Sprint(resolveValue(in, items, index, raw)))
	if resolved == "" || resolved == "<nil>" {
		return "", fmt.Errorf("filePath is required")
	}
	clean := filepath.Clean(resolved)
	if containsPathTraversal(resolved) {
		return "", fmt.Errorf("invalid path traversal %s", resolved)
	}
	if len(allowed) > 0 {
		absClean, err := filepath.Abs(clean)
		if err != nil {
			return "", err
		}
		for _, path := range allowed {
			absAllowed, err := filepath.Abs(filepath.Clean(path))
			if err != nil {
				continue
			}
			if absClean == absAllowed || strings.HasPrefix(absClean, absAllowed+string(os.PathSeparator)) {
				return clean, nil
			}
		}
		return "", fmt.Errorf("path %s is outside allowed paths", clean)
	}
	return clean, nil
}

func containsPathTraversal(path string) bool {
	normalized := filepath.ToSlash(path)
	return normalized == ".." || strings.HasPrefix(normalized, "../") || strings.Contains(normalized, "/../")
}

func readWriteAllowedPaths(params map[string]any, options map[string]any) []string {
	for _, raw := range []any{options["allowedPaths"], params["allowedPaths"]} {
		paths := stringList(raw)
		if len(paths) > 0 {
			return paths
		}
	}
	return nil
}

func stringList(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, value := range typed {
			result = append(result, fmt.Sprint(value))
		}
		return result
	case string:
		parts := strings.Split(typed, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	default:
		return nil
	}
}
