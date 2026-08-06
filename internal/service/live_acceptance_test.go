package service_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/configs"
	"sendllm/internal/service"
)

const (
	liveConfigEnv     = "SENDLLM_LIVE_CONFIG"
	liveInputEnv      = "SENDLLM_LIVE_INPUT"
	liveOutputEnv     = "SENDLLM_LIVE_OUTPUT"
	liveStateEnv      = "SENDLLM_LIVE_STATE"
	liveTaskIDEnv     = "SENDLLM_LIVE_TASK_ID"
	expectedLiveItems = 50
	maxLiveLineBytes  = 16 << 20
)

type livePaths struct {
	config string
	input  string
	output string
	state  string
	taskID string
}

type liveCounts struct {
	input          int
	output         int
	traceUnique    int
	traceSetEqual  int
	schemaValid    int
	businessValid  int
	annotationAuto int
	failedFile     int
	stateSucceeded int64
	stateFailed    int64
}

func TestLiveAcceptance(t *testing.T) {
	paths, ok := loadLivePaths()
	if !ok {
		t.Skip("未设置真实验收文件路径")
	}

	cfg, err := configs.Load(paths.config)
	if err != nil {
		t.Fatalf("configs.Load() error = %v", err)
	}
	validator, err := service.NewValidator(
		cfg.ResultSchema,
		cfg.RiskTypes,
		cfg.Output.ExplanationMinLength,
		cfg.Output.ExplanationMaxLength,
	)
	if err != nil {
		t.Fatalf("service.NewValidator() error = %v", err)
	}
	schema := compileLiveSchema(t, cfg.ResultSchema)
	inputIDs := readLiveInput(t, paths.input)
	counts := readLiveOutput(t, paths.output, inputIDs, schema, validator)
	counts.input = len(inputIDs)
	counts.failedFile = countLiveLines(t, liveFailedPath(paths.output))

	store, err := dao.Open(context.Background(), paths.state)
	if err != nil {
		t.Fatalf("dao.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Store.Close() error = %v", err)
		}
	})
	stateCounts, err := store.Counts(context.Background(), paths.taskID)
	if err != nil {
		t.Fatalf("Store.Counts() error = %v", err)
	}
	counts.stateSucceeded = stateCounts.Succeeded
	counts.stateFailed = stateCounts.Failed

	if counts.input != expectedLiveItems || counts.output != expectedLiveItems ||
		counts.traceUnique != expectedLiveItems || counts.traceSetEqual != expectedLiveItems ||
		counts.schemaValid != expectedLiveItems || counts.businessValid != expectedLiveItems ||
		counts.annotationAuto != expectedLiveItems || counts.failedFile != 0 ||
		stateCounts.Pending != 0 || stateCounts.Processing != 0 || stateCounts.RetryWait != 0 ||
		counts.stateSucceeded != expectedLiveItems || counts.stateFailed != 0 {
		t.Fatalf(
			"acceptance counts mismatch: input=%d output=%d trace_unique=%d trace_set_equal=%d "+
				"schema_valid=%d business_valid=%d annotation_auto=%d failed_file=%d "+
				"state_succeeded=%d state_failed=%d",
			counts.input,
			counts.output,
			counts.traceUnique,
			counts.traceSetEqual,
			counts.schemaValid,
			counts.businessValid,
			counts.annotationAuto,
			counts.failedFile,
			counts.stateSucceeded,
			counts.stateFailed,
		)
	}

	t.Logf(
		"live_acceptance=PASS input=%d output=%d trace_unique=%d trace_set_equal=%d "+
			"schema_valid=%d business_valid=%d annotation_auto=%d failed_file=%d "+
			"state_succeeded=%d state_failed=%d",
		counts.input,
		counts.output,
		counts.traceUnique,
		counts.traceSetEqual,
		counts.schemaValid,
		counts.businessValid,
		counts.annotationAuto,
		counts.failedFile,
		counts.stateSucceeded,
		counts.stateFailed,
	)
}

func loadLivePaths() (livePaths, bool) {
	paths := livePaths{
		config: os.Getenv(liveConfigEnv),
		input:  os.Getenv(liveInputEnv),
		output: os.Getenv(liveOutputEnv),
		state:  os.Getenv(liveStateEnv),
		taskID: os.Getenv(liveTaskIDEnv),
	}
	return paths, paths.config != "" && paths.input != "" && paths.output != "" && paths.state != "" &&
		paths.taskID != ""
}

func compileLiveSchema(t *testing.T, raw json.RawMessage) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("result-schema.json", bytes.NewReader(raw)); err != nil {
		t.Fatalf("add formal schema: %v", err)
	}
	schema, err := compiler.Compile("result-schema.json")
	if err != nil {
		t.Fatalf("compile formal schema: %v", err)
	}
	return schema
}

func readLiveInput(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	ids := make(map[string]struct{}, expectedLiveItems)
	scanLiveJSONL(t, path, func(lineNumber int, line []byte) {
		sample, err := dto.ParseSource(line)
		if err != nil {
			t.Fatalf("input line %d: source validation failed", lineNumber)
		}
		if _, exists := ids[sample.TraceID]; exists {
			t.Fatalf("input line %d: duplicate trace_id", lineNumber)
		}
		ids[sample.TraceID] = struct{}{}
	})
	return ids
}

func readLiveOutput(
	t *testing.T,
	path string,
	inputIDs map[string]struct{},
	schema *jsonschema.Schema,
	validator *service.Validator,
) liveCounts {
	t.Helper()
	counts := liveCounts{}
	seen := make(map[string]struct{}, expectedLiveItems)
	scanLiveJSONL(t, path, func(lineNumber int, line []byte) {
		counts.output++
		fields := make(map[string]json.RawMessage)
		if err := json.Unmarshal(line, &fields); err != nil {
			t.Fatalf("output line %d: invalid JSON", lineNumber)
		}
		var traceID string
		if err := json.Unmarshal(fields["trace_id"], &traceID); err != nil || traceID == "" {
			t.Fatalf("output line %d: invalid trace_id", lineNumber)
		}
		if _, exists := inputIDs[traceID]; !exists {
			t.Fatalf("output line %d: trace_id is not in input", lineNumber)
		}
		if _, exists := seen[traceID]; exists {
			t.Fatalf("output line %d: duplicate trace_id", lineNumber)
		}
		seen[traceID] = struct{}{}

		var meta dto.AnnotationMeta
		if err := json.Unmarshal(fields["annotation"], &meta); err != nil || meta.Method != "auto" {
			t.Fatalf("output line %d: annotation.method is not auto", lineNumber)
		}
		counts.annotationAuto++

		annotationFields := map[string]json.RawMessage{
			"label":       fields["label"],
			"explanation": fields["explanation"],
		}
		if extendedInfo, exists := fields["extended_info"]; exists {
			annotationFields["extended_info"] = extendedInfo
		}
		annotation, err := json.Marshal(annotationFields)
		if err != nil {
			t.Fatalf("output line %d: annotation encoding failed", lineNumber)
		}
		var decoded any
		if err := json.Unmarshal(annotation, &decoded); err != nil {
			t.Fatalf("output line %d: annotation decoding failed", lineNumber)
		}
		if err := schema.Validate(decoded); err != nil {
			t.Fatalf("output line %d: formal schema validation failed", lineNumber)
		}
		counts.schemaValid++
		if _, err := validator.Validate(annotation); err != nil {
			t.Fatalf("output line %d: business validation failed", lineNumber)
		}
		counts.businessValid++
	})
	counts.traceUnique = len(seen)
	if len(seen) == len(inputIDs) {
		counts.traceSetEqual = len(seen)
	}
	return counts
}

func scanLiveJSONL(t *testing.T, path string, visit func(int, []byte)) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open JSONL: %v", err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Errorf("close JSONL: %v", err)
		}
	})
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxLiveLineBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		visit(lineNumber, scanner.Bytes())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan JSONL: %v", err)
	}
}

func countLiveLines(t *testing.T, path string) int {
	t.Helper()
	count := 0
	scanLiveJSONL(t, path, func(_ int, line []byte) {
		if len(strings.TrimSpace(string(line))) != 0 {
			count++
		}
	})
	return count
}

func liveFailedPath(outputPath string) string {
	extension := filepath.Ext(outputPath)
	if strings.EqualFold(extension, ".jsonl") {
		return strings.TrimSuffix(outputPath, extension) + ".failed.jsonl"
	}
	return outputPath + ".failed.jsonl"
}
