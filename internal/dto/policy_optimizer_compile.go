package dto

// PolicyOptimizerCandidate 表示由基础发布包确定性生成的候选策略。
type PolicyOptimizerCandidate struct {
	Version     string `json:"candidate_version"`
	BaseVersion string `json:"base_version"`
	BaseSHA256  string `json:"base_sha256"`
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
}

// PolicyOptimizerBundleFile 表示候选发布包中的单个文件。
type PolicyOptimizerBundleFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// PolicyOptimizerBundleManifest 表示已验证的发布包清单。
type PolicyOptimizerBundleManifest struct {
	ReleaseVersion      string                      `json:"release_version"`
	SourcePolicyVersion string                      `json:"source_policy_version"`
	CompilerVersion     string                      `json:"compiler_version"`
	AggregateHash       string                      `json:"aggregate_hash"`
	Files               []PolicyOptimizerBundleFile `json:"files"`
	Coverage            map[string][]string         `json:"coverage"`
}

// PolicyOptimizerCompileManifest 表示 Prompt 编译审计清单。
type PolicyOptimizerCompileManifest struct {
	ReleaseVersion      string                      `json:"release_version"`
	SourcePolicyVersion string                      `json:"source_policy_version"`
	CompilerVersion     string                      `json:"compiler_version"`
	AggregateHash       string                      `json:"aggregate_hash"`
	Files               []PolicyOptimizerBundleFile `json:"files"`
	Coverage            map[string][]string         `json:"coverage"`
}
