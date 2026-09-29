CREATE TABLE IF NOT EXISTS review_tasks (
    task_id TEXT PRIMARY KEY,
    semantic_fingerprint TEXT NOT NULL,
    scene TEXT NOT NULL CHECK (scene IN ('prompt', 'response')),
    status TEXT NOT NULL CHECK (status IN
        ('created', 'preflight', 'running', 'stopping', 'completed', 'failed', 'interrupted')),
    snapshot_dir TEXT NOT NULL,
    fatal_error_category TEXT,
    fatal_error_summary TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS review_items (
    task_id TEXT NOT NULL,
    trace_id TEXT NOT NULL,
    input_index INTEGER NOT NULL CHECK (input_index >= 0),
    source_hash TEXT NOT NULL,
    raw_json BLOB NOT NULL,
    prompt TEXT NOT NULL,
    response TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN
        ('pending_initial', 'awaiting_initial', 'awaiting_experts',
         'pending_arbiter', 'resolved_safe', 'resolved_unsafe', 'quarantined')),
    independence_degraded INTEGER NOT NULL DEFAULT 0 CHECK (independence_degraded IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (task_id, trace_id),
    UNIQUE (task_id, input_index),
    FOREIGN KEY (task_id) REFERENCES review_tasks(task_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS review_stages (
    task_id TEXT NOT NULL,
    trace_id TEXT NOT NULL,
    stage_key TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('judge_a', 'judge_b', 'router', 'expert', 'arbiter')),
    axis TEXT CHECK (axis IS NULL OR axis IN ('attack_method', 'attack_domain')),
    category TEXT,
    state TEXT NOT NULL CHECK (state IN
        ('pending', 'running', 'retry_wait', 'succeeded', 'terminal_failed', 'skipped')),
    model_profile TEXT,
    model_family TEXT,
    fallback_index INTEGER NOT NULL DEFAULT 0 CHECK (fallback_index >= 0),
    request_attempts INTEGER NOT NULL DEFAULT 0 CHECK (request_attempts >= 0),
    repair_attempts INTEGER NOT NULL DEFAULT 0 CHECK (repair_attempts >= 0),
    refusal_attempts INTEGER NOT NULL DEFAULT 0 CHECK (refusal_attempts >= 0),
    next_attempt_at TEXT,
    result_json BLOB,
    error_category TEXT,
    error_summary TEXT,
    started_at TEXT,
    finished_at TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (task_id, trace_id, stage_key),
    FOREIGN KEY (task_id, trace_id) REFERENCES review_items(task_id, trace_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS review_stages_claim_idx
    ON review_stages(role, state, next_attempt_at, task_id, trace_id);

CREATE TABLE IF NOT EXISTS review_attempts (
    attempt_id INTEGER PRIMARY KEY,
    task_id TEXT NOT NULL,
    trace_id TEXT NOT NULL,
    stage_key TEXT NOT NULL,
    attempt_kind TEXT NOT NULL CHECK (attempt_kind IN
        ('classification', 'format_repair', 'refusal_reprompt')),
    model_profile TEXT NOT NULL,
    model_family TEXT NOT NULL,
    api_key_env TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL,
    http_status INTEGER,
    finish_reason TEXT,
    error_category TEXT,
    error_summary TEXT,
    retryable INTEGER NOT NULL CHECK (retryable IN (0, 1)),
    prompt_tokens INTEGER NOT NULL DEFAULT 0 CHECK (prompt_tokens >= 0),
    completion_tokens INTEGER NOT NULL DEFAULT 0 CHECK (completion_tokens >= 0),
    raw_response BLOB,
    validation_error TEXT,
    FOREIGN KEY (task_id, trace_id, stage_key)
        REFERENCES review_stages(task_id, trace_id, stage_key) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS review_decisions (
    task_id TEXT NOT NULL,
    trace_id TEXT NOT NULL,
    final_state TEXT NOT NULL CHECK (final_state IN
        ('resolved_safe', 'resolved_unsafe', 'quarantined')),
    label TEXT CHECK (label IS NULL OR label IN ('safe', 'unsafe')),
    is_attack INTEGER NOT NULL CHECK (is_attack IN (0, 1)),
    attack_methods_json BLOB NOT NULL,
    attack_domains_json BLOB NOT NULL,
    primary_attack_method TEXT,
    primary_attack_domain TEXT,
    primary_risk_type TEXT,
    case_type TEXT CHECK (case_type IS NULL OR case_type IN
        ('typical', 'borderline', 'variant', 'hard_negative')),
    evidence_json BLOB NOT NULL,
    decision_rules_json BLOB NOT NULL,
    rationale TEXT NOT NULL,
    quarantine_reason TEXT,
    decided_at TEXT NOT NULL,
    PRIMARY KEY (task_id, trace_id),
    FOREIGN KEY (task_id, trace_id) REFERENCES review_items(task_id, trace_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS review_preflight_runs (
    preflight_id INTEGER PRIMARY KEY,
    startup_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    role TEXT NOT NULL,
    model_profile TEXT NOT NULL,
    model_family TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('passed', 'failed')),
    latency_ms INTEGER NOT NULL CHECK (latency_ms >= 0),
    error_category TEXT,
    error_summary TEXT,
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL,
    FOREIGN KEY (task_id) REFERENCES review_tasks(task_id) ON DELETE CASCADE
);
