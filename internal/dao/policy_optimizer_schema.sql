CREATE TABLE IF NOT EXISTS optimization_iterations (
  iteration_id TEXT PRIMARY KEY,
  mode TEXT NOT NULL CHECK (mode IN ('analyze','compile')),
  status TEXT NOT NULL CHECK (status IN ('created','awaiting_mapping_approval','running','no_change','preview_ready','awaiting_regression','regression_failed','awaiting_release_approval','release_ready','released','interrupted','failed')),
  current_stage TEXT NOT NULL,
  base_policy_version TEXT NOT NULL,
  base_policy_hash TEXT NOT NULL CHECK (length(base_policy_hash) = 64),
  config_hash TEXT NOT NULL CHECK (length(config_hash) = 64),
  semantic_hash TEXT NOT NULL CHECK (length(semantic_hash) = 64),
  objective_json TEXT NOT NULL CHECK (json_valid(objective_json)),
  permissions_json TEXT NOT NULL CHECK (json_valid(permissions_json)),
  fatal_error_category TEXT NOT NULL DEFAULT '',
  recovery_count INTEGER NOT NULL DEFAULT 0 CHECK (recovery_count >= 0),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS optimization_sources (
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  source_id TEXT NOT NULL,
  source_type TEXT NOT NULL,
  source_path TEXT NOT NULL,
  source_format TEXT NOT NULL CHECK (source_format IN ('csv','jsonl','markdown')),
  source_sha256 TEXT NOT NULL CHECK (length(source_sha256) = 64),
  expected_count INTEGER NOT NULL CHECK (expected_count >= 0),
  actual_count INTEGER NOT NULL DEFAULT 0 CHECK (actual_count >= 0),
  selection_reason TEXT NOT NULL,
  trust_json TEXT NOT NULL CHECK (json_valid(trust_json)),
  mapping_status TEXT NOT NULL CHECK (mapping_status IN ('not_required','candidate','approved','invalidated')),
  mapping_sha256 TEXT NOT NULL DEFAULT '' CHECK (mapping_sha256 = '' OR length(mapping_sha256) = 64),
  normalization_status TEXT NOT NULL CHECK (normalization_status IN ('pending','running','succeeded','terminal_failed')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (iteration_id, source_id)
);

CREATE TABLE IF NOT EXISTS optimization_records (
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  record_id TEXT NOT NULL,
  source_id TEXT NOT NULL,
  sample_id TEXT NOT NULL,
  input_order INTEGER NOT NULL CHECK (input_order >= 0),
  scene TEXT NOT NULL CHECK (scene IN ('prompt','response')),
  policy_version TEXT NOT NULL,
  selection_reason TEXT NOT NULL,
  comparison_json TEXT NOT NULL CHECK (json_valid(comparison_json)),
  provenance_json TEXT NOT NULL CHECK (json_valid(provenance_json)),
  metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
  payload_artifact_id TEXT NOT NULL,
  canonical_sha256 TEXT NOT NULL CHECK (length(canonical_sha256) = 64),
  state TEXT NOT NULL CHECK (state IN ('normalized','selected','assigned','excluded')),
  created_at TEXT NOT NULL,
  PRIMARY KEY (iteration_id, record_id),
  UNIQUE (iteration_id, source_id, sample_id),
  FOREIGN KEY (iteration_id, source_id) REFERENCES optimization_sources(iteration_id, source_id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS optimization_batches (
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  batch_id TEXT NOT NULL,
  batch_order INTEGER NOT NULL CHECK (batch_order >= 0),
  mix_type TEXT NOT NULL CHECK (mix_type IN ('homogeneous','conflict','random','mixed')),
  stratum_key TEXT NOT NULL,
  record_ids_json TEXT NOT NULL CHECK (json_valid(record_ids_json)),
  record_count INTEGER NOT NULL CHECK (record_count >= 0),
  input_sha256 TEXT NOT NULL CHECK (length(input_sha256) = 64),
  status TEXT NOT NULL CHECK (status IN ('pending','running','retry_wait','succeeded','terminal_failed')),
  attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  recovery_count INTEGER NOT NULL DEFAULT 0 CHECK (recovery_count >= 0),
  next_attempt_at TEXT NOT NULL DEFAULT '',
  terminal_error_category TEXT NOT NULL DEFAULT '',
  active_artifact_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (iteration_id, batch_id),
  UNIQUE (iteration_id, batch_order)
);

CREATE TABLE IF NOT EXISTS optimization_skill_runs (
  run_id TEXT PRIMARY KEY,
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  stage TEXT NOT NULL,
  skill_id TEXT NOT NULL,
  work_id TEXT NOT NULL,
  executor TEXT NOT NULL CHECK (executor IN ('deterministic','model','model_with_deterministic_verifier')),
  input_sha256 TEXT NOT NULL CHECK (length(input_sha256) = 64),
  context_sha256 TEXT NOT NULL CHECK (length(context_sha256) = 64),
  model_profile TEXT NOT NULL DEFAULT '',
  model_family TEXT NOT NULL DEFAULT '',
  attempt_number INTEGER NOT NULL CHECK (attempt_number >= 1),
  status TEXT NOT NULL CHECK (status IN ('pending','running','retry_wait','succeeded','terminal_failed')),
  error_category TEXT NOT NULL DEFAULT '',
  next_attempt_at TEXT NOT NULL DEFAULT '',
  fallback_from_run_id TEXT NOT NULL DEFAULT '',
  output_artifact_id TEXT NOT NULL DEFAULT '',
  prompt_tokens INTEGER NOT NULL DEFAULT 0 CHECK (prompt_tokens >= 0),
  completion_tokens INTEGER NOT NULL DEFAULT 0 CHECK (completion_tokens >= 0),
  started_at TEXT NOT NULL DEFAULT '',
  completed_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (iteration_id, stage, skill_id, work_id, input_sha256, context_sha256, attempt_number)
);

CREATE TABLE IF NOT EXISTS optimization_artifacts (
  artifact_id TEXT PRIMARY KEY,
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  artifact_type TEXT NOT NULL,
  relative_path TEXT NOT NULL,
  media_type TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
  byte_size INTEGER NOT NULL CHECK (byte_size >= 0),
  producer_stage TEXT NOT NULL,
  producer_skill TEXT NOT NULL DEFAULT '',
  producer_model_profile TEXT NOT NULL DEFAULT '',
  parent_sha256_json TEXT NOT NULL CHECK (json_valid(parent_sha256_json)),
  sensitivity TEXT NOT NULL CHECK (sensitivity IN ('public_policy','review_metadata','restricted_payload')),
  is_active INTEGER NOT NULL CHECK (is_active IN (0,1)),
  created_at TEXT NOT NULL,
  UNIQUE (iteration_id, relative_path)
);

CREATE TABLE IF NOT EXISTS optimization_patterns (
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  pattern_id TEXT NOT NULL,
  pattern_level TEXT NOT NULL CHECK (pattern_level IN ('local','global')),
  status TEXT NOT NULL CHECK (status IN ('candidate','validated','rejected','superseded')),
  artifact_id TEXT NOT NULL REFERENCES optimization_artifacts(artifact_id) ON DELETE RESTRICT,
  coverage_count INTEGER NOT NULL CHECK (coverage_count >= 0),
  batch_count INTEGER NOT NULL CHECK (batch_count >= 0),
  source_distribution_json TEXT NOT NULL CHECK (json_valid(source_distribution_json)),
  model_distribution_json TEXT NOT NULL CHECK (json_valid(model_distribution_json)),
  created_at TEXT NOT NULL,
  PRIMARY KEY (iteration_id, pattern_id)
);

CREATE TABLE IF NOT EXISTS optimization_candidates (
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  candidate_version TEXT NOT NULL,
  base_version TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('preview','regression_pending','regression_failed','awaiting_approval','release_ready','released','blocked')),
  change_set_artifact_id TEXT NOT NULL REFERENCES optimization_artifacts(artifact_id) ON DELETE RESTRICT,
  policy_artifact_id TEXT NOT NULL REFERENCES optimization_artifacts(artifact_id) ON DELETE RESTRICT,
  compile_manifest_artifact_id TEXT NOT NULL REFERENCES optimization_artifacts(artifact_id) ON DELETE RESTRICT,
  critic_artifact_id TEXT NOT NULL REFERENCES optimization_artifacts(artifact_id) ON DELETE RESTRICT,
  candidate_sha256 TEXT NOT NULL CHECK (length(candidate_sha256) = 64),
  blocked_conflicts_json TEXT NOT NULL CHECK (json_valid(blocked_conflicts_json)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (iteration_id, candidate_version),
  UNIQUE (candidate_version)
);

CREATE TABLE IF NOT EXISTS optimization_regressions (
  regression_id TEXT PRIMARY KEY,
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  candidate_version TEXT NOT NULL,
  candidate_sha256 TEXT NOT NULL CHECK (length(candidate_sha256) = 64),
  suite_version TEXT NOT NULL,
  gate_policy_version TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('running','passed','failed','terminal_failed')),
  rotation_a_artifact_id TEXT NOT NULL DEFAULT '',
  rotation_b_artifact_id TEXT NOT NULL DEFAULT '',
  report_artifact_id TEXT NOT NULL DEFAULT '',
  gate_artifact_id TEXT NOT NULL DEFAULT '',
  report_sha256 TEXT NOT NULL DEFAULT '' CHECK (report_sha256 = '' OR length(report_sha256) = 64),
  passed INTEGER NOT NULL DEFAULT 0 CHECK (passed IN (0,1)),
  created_at TEXT NOT NULL,
  completed_at TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (iteration_id, candidate_version) REFERENCES optimization_candidates(iteration_id, candidate_version) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS optimization_approvals (
  approval_id TEXT PRIMARY KEY,
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  subject_type TEXT NOT NULL CHECK (subject_type IN ('mapping','gold','core_gold','policy_release')),
  subject_id TEXT NOT NULL,
  subject_sha256 TEXT NOT NULL CHECK (length(subject_sha256) = 64),
  related_sha256_json TEXT NOT NULL CHECK (json_valid(related_sha256_json)),
  approver_id TEXT NOT NULL,
  decision TEXT NOT NULL CHECK (decision IN ('approved','rejected')),
  note TEXT NOT NULL DEFAULT '',
  artifact_id TEXT NOT NULL REFERENCES optimization_artifacts(artifact_id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  UNIQUE (iteration_id, subject_type, subject_id, subject_sha256, approver_id)
);

CREATE TABLE IF NOT EXISTS optimization_releases (
  release_version TEXT PRIMARY KEY,
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  candidate_version TEXT NOT NULL,
  base_version TEXT NOT NULL,
  candidate_sha256 TEXT NOT NULL CHECK (length(candidate_sha256) = 64),
  regression_sha256 TEXT NOT NULL CHECK (length(regression_sha256) = 64),
  approval_sha256 TEXT NOT NULL CHECK (length(approval_sha256) = 64),
  release_sha256 TEXT NOT NULL CHECK (length(release_sha256) = 64),
  release_path TEXT NOT NULL UNIQUE,
  released_at TEXT NOT NULL,
  FOREIGN KEY (iteration_id, candidate_version) REFERENCES optimization_candidates(iteration_id, candidate_version) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_optimization_sources_state ON optimization_sources(iteration_id, normalization_status, source_id);
CREATE INDEX IF NOT EXISTS idx_optimization_records_state ON optimization_records(iteration_id, state, input_order);
CREATE INDEX IF NOT EXISTS idx_optimization_batches_claim ON optimization_batches(iteration_id, status, next_attempt_at, batch_order);
CREATE INDEX IF NOT EXISTS idx_optimization_skill_runs_work ON optimization_skill_runs(iteration_id, stage, skill_id, work_id, status);
CREATE INDEX IF NOT EXISTS idx_optimization_artifacts_hash ON optimization_artifacts(iteration_id, sha256);
CREATE UNIQUE INDEX IF NOT EXISTS idx_optimization_artifacts_active_singleton ON optimization_artifacts(iteration_id, artifact_type) WHERE is_active = 1 AND artifact_type IN ('preflight_report','critic_report','change_set','compile_manifest','regression_report','gate_report','release_manifest');
CREATE INDEX IF NOT EXISTS idx_optimization_patterns_level ON optimization_patterns(iteration_id, pattern_level, status);
CREATE INDEX IF NOT EXISTS idx_optimization_regressions_candidate ON optimization_regressions(iteration_id, candidate_version, created_at);
CREATE INDEX IF NOT EXISTS idx_optimization_approvals_subject ON optimization_approvals(iteration_id, subject_type, subject_id, created_at);
